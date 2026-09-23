package wiki

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Sync picks up pages changed on disk by something other than Veyloom,
// an editor say: it indexes them again and, with git and Options.Human,
// commits them under the person's name. It returns the pages it found
// changed. Pages a Writer has written and not committed yet are left for
// that Writer's commit. When nothing changed it only looks: it runs before
// every brief.
func (b *Bundle) Sync(ctx context.Context) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	changed, err := b.rescan()
	if err != nil || b.opts.ReadOnly || len(changed) == 0 {
		return changed, err
	}
	if err := b.regenerate(changed...); err != nil {
		return changed, err
	}
	if b.git == nil || b.opts.Human == "" {
		return changed, nil
	}
	return changed, b.commitOutside(ctx, b.opts.Human, "Record changes made outside Veyloom", "outside Veyloom")
}

// rescan compares the folder with the index by size and modification time
// and reindexes what differs.
func (b *Bundle) rescan() ([]string, error) {
	seen := map[string]bool{}
	var changed []string
	err := filepath.WalkDir(b.dir, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if fp != b.dir && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() || filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		rel, _ := filepath.Rel(b.dir, fp)
		p := "/" + filepath.ToSlash(rel)
		if name := d.Name(); name == okf.IndexFile || name == okf.LogFile {
			return nil
		}
		seen[p] = true
		info, err := d.Info()
		if err != nil {
			return err
		}
		if e := b.pages[p]; e != nil && e.size == info.Size() && e.mtime.Equal(info.ModTime()) {
			return nil
		}
		e, probs, err := b.load(p)
		if err != nil {
			return err
		}
		if e != nil {
			b.pages[p] = e
		} else {
			delete(b.pages, p)
		}
		if len(probs) > 0 {
			b.problems[p] = probs
		} else {
			delete(b.problems, p)
		}
		changed = append(changed, p)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("wiki: read %s: %w", b.dir, err)
	}
	for p := range b.pages {
		if !seen[p] {
			delete(b.pages, p)
			delete(b.problems, p)
			changed = append(changed, p)
		}
	}
	slices.Sort(changed)
	return changed, nil
}

// recordLeftovers commits what is on disk but not in the history when a
// bundle opens: a new bundle's first files, or changes made while Veyloom
// was not running, which are the person's when Options.Human names them.
func (b *Bundle) recordLeftovers(ctx context.Context, fresh bool) error {
	if fresh {
		dirty, err := b.git.status(ctx)
		if err != nil || len(dirty) == 0 {
			return err
		}
		_, err = b.git.commit(ctx, dirty, process, "Set up the bundle")
		return err
	}
	author := process
	if b.opts.Human != "" {
		author = b.opts.Human
	}
	if err := b.commitOutside(ctx, author, "Record changes found when Veyloom started", "while Veyloom was not running"); err != nil {
		return err
	}
	// Indexes written afresh on opening, after a layout change say.
	_, err := b.git.commit(ctx, b.indexes(), process, "Regenerate the indexes")
	return err
}

// commitOutside commits the pages that differ from the history and no
// Writer is holding, with a log line for each.
func (b *Bundle) commitOutside(ctx context.Context, author, subject, where string) error {
	dirty, err := b.git.status(ctx)
	if err != nil {
		return err
	}
	var pages []string
	for _, p := range dirty {
		if !generated(p) && b.busy[p] == 0 && strings.HasSuffix(p, ".md") {
			pages = append(pages, p)
		}
	}
	if len(pages) == 0 {
		return nil
	}
	var entries []okf.LogEntry
	for _, p := range pages {
		what := p // a file that is not a concept has no title to link
		if e := b.pages[p]; e != nil {
			what = "[" + linkText(e.sum.Title) + "](" + p + ")"
		}
		_, statErr := os.Lstat(b.file(p))
		switch {
		case statErr != nil:
			entries = append(entries, okf.LogEntry{Kind: okf.LogUpdate, Text: p + " was removed " + where})
		case !b.git.inHead(ctx, p):
			entries = append(entries, okf.LogEntry{Kind: okf.LogCreation, Text: what + " " + where})
		default:
			entries = append(entries, okf.LogEntry{Kind: okf.LogUpdate, Text: what + " " + where})
		}
	}
	if err := b.regenerate(pages...); err != nil {
		return err
	}
	if err := b.appendLog(entries); err != nil {
		return err
	}
	files := append(append(pages, "/"+okf.LogFile), indexFiles(pages...)...)
	_, err = b.git.commit(ctx, files, author, message(subject, entries, nil))
	return err
}

// History lists the latest commits touching p, newest first, following it
// across renames; with p empty, the whole bundle's.
func (b *Bundle) History(ctx context.Context, p string, limit int) ([]Commit, error) {
	if b.git == nil {
		return nil, nil
	}
	if p != "" {
		var err error
		if p, err = CleanPath(p); err != nil {
			return nil, err
		}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.git.log(ctx, p, limit)
}

// ErrNoHistory is returned when a bundle kept without git is asked to
// undo a commit.
var ErrNoHistory = errors.New("wiki: this bundle keeps no history")

// Revert undoes one commit, a turn's writes say, as a new commit by author
// (design.md 5.5). The log is not rolled back: it gains a line saying what
// was undone, and why when a reason is given, for whoever keeps the wiki
// not to do it again (5.15). When a later change touched the same lines, nothing is
// undone and the error wraps store.ErrConflict.
func (b *Bundle) Revert(ctx context.Context, sha, author, reason string) (string, error) {
	if b.git == nil {
		return "", ErrNoHistory
	}
	if !okf.ValidActor(author) {
		return "", fmt.Errorf("%w: %q is not an actor", store.ErrInvalidInput, author)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	target, err := b.git.show(ctx, sha)
	if err != nil {
		return "", fmt.Errorf("%w: no commit %s", store.ErrNotFound, sha)
	}
	if err := b.git.revert(ctx, target.SHA, generated); err != nil {
		return "", err
	}
	// The log keeps its history; the indexes are written afresh below.
	if _, err := b.git.run(ctx, "checkout", "HEAD", "--", okf.LogFile); err != nil {
		return "", err
	}
	if _, err := b.rescan(); err != nil {
		return "", err
	}
	if err := b.regenerateAll(); err != nil {
		return "", err
	}
	entry := okf.LogEntry{Kind: okf.LogRevert, Text: "undid " + short(target.SHA) + " (" + target.Subject + ") by " + author}
	if reason = strings.Join(strings.Fields(reason), " "); reason != "" {
		entry.Text += ": " + reason
	}
	if err := b.appendLog([]okf.LogEntry{entry}); err != nil {
		return "", err
	}
	if err := b.git.add(ctx, append([]string{"/" + okf.LogFile}, b.indexes()...)); err != nil {
		return "", err
	}
	return b.git.commitStaged(ctx, author, message("Undo "+short(target.SHA)+": "+target.Subject, []okf.LogEntry{entry}, nil))
}

// Head is the bundle's last commit: "" when it keeps no history, or has
// none yet.
func (b *Bundle) Head(ctx context.Context) (string, error) {
	if b.git == nil {
		return "", nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !b.git.hasHead(ctx) {
		return "", nil
	}
	out, err := b.git.run(ctx, "rev-parse", "HEAD")
	return strings.TrimSpace(string(out)), err
}

// RestoreSkill puts the folder of the skill called name back the way it
// was at commit sha, the version before a trial say (design.md 5.15), as a
// new commit by author; files the skill gained since go. The log keeps its
// history and gains a line saying so, and why when a reason is given. A
// skill a Writer is changing right now is left alone: the error wraps
// store.ErrConflict.
func (b *Bundle) RestoreSkill(ctx context.Context, name, sha, author, reason string) (string, error) {
	if b.git == nil {
		return "", ErrNoHistory
	}
	if !okf.ValidActor(author) {
		return "", fmt.Errorf("%w: %q is not an actor", store.ErrInvalidInput, author)
	}
	page := SkillPath(name)
	dir := strings.TrimSuffix(strings.TrimPrefix(page, "/"), "/"+okf.SkillFile)
	b.mu.Lock()
	defer b.mu.Unlock()
	for p, n := range b.busy {
		if n > 0 && SkillOfFile(p) == name {
			return "", store.Conflicting("skillBusy", store.Params{"name": name}, "a turn is changing the skill %s right now; roll it back when the turn ends", name)
		}
	}
	target, err := b.git.show(ctx, sha)
	if err != nil {
		return "", fmt.Errorf("%w: no commit %s", store.ErrNotFound, sha)
	}
	out, err := b.git.run(ctx, "ls-tree", "-r", "--name-only", target.SHA, "--", dir)
	if err != nil {
		return "", err
	}
	was := map[string]bool{}
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if f != "" {
			was[f] = true
		}
	}
	if !was[strings.TrimPrefix(page, "/")] {
		return "", fmt.Errorf("%w: the skill %s was not in the library at %s", store.ErrNotFound, name, short(target.SHA))
	}
	// What the skill has now and had not then goes; the rest comes back.
	err = filepath.WalkDir(b.file("/"+dir), func(fp string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(b.dir, fp)
		if err == nil && !was[filepath.ToSlash(rel)] {
			err = os.Remove(fp)
		}
		return err
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if _, err := b.git.run(ctx, "checkout", target.SHA, "--", dir); err != nil {
		return "", err
	}
	if _, err := b.rescan(); err != nil {
		return "", err
	}
	if err := b.regenerateAll(); err != nil {
		return "", err
	}
	link := page
	if e := b.pages[page]; e != nil {
		link = "[" + linkText(e.sum.Title) + "](" + page + ")"
	}
	entry := okf.LogEntry{Kind: okf.LogRevert, Text: link + " rolled back to " + short(target.SHA) + " by " + author}
	if reason = strings.Join(strings.Fields(reason), " "); reason != "" {
		entry.Text += ": " + reason
	}
	if err := b.appendLog([]okf.LogEntry{entry}); err != nil {
		return "", err
	}
	if err := b.git.add(ctx, append([]string{"/" + dir, "/" + okf.LogFile}, b.indexes()...)); err != nil {
		return "", err
	}
	return b.git.commitStaged(ctx, author, message("Roll back "+name+" to "+short(target.SHA), []okf.LogEntry{entry}, nil))
}

// indexes lists every index file Veyloom generates in the bundle.
func (b *Bundle) indexes() []string {
	pages := make([]string, 0, len(b.pages)+len(b.opts.Layout.Dirs))
	for p := range b.pages {
		pages = append(pages, p)
	}
	for _, d := range b.opts.Layout.Dirs {
		pages = append(pages, "/"+d.Name+"/x.md")
	}
	return indexFiles(pages...)
}

// add stages the files that exist among paths.
func (r *repo) add(ctx context.Context, paths []string) error {
	var present []string
	for _, p := range paths {
		rel := strings.TrimPrefix(p, "/")
		if _, err := os.Lstat(filepath.Join(r.dir, filepath.FromSlash(rel))); err == nil && !slices.Contains(present, rel) {
			present = append(present, rel)
		}
	}
	if len(present) == 0 {
		return nil
	}
	_, err := r.run(ctx, append([]string{"add", "-A", "--"}, present...)...)
	return err
}

// show looks up one commit.
func (r *repo) show(ctx context.Context, sha string) (Commit, error) {
	out, err := r.run(ctx, "show", "-s", "--format=%H%x1f%an%x1f%aI%x1f%s", sha+"^{commit}")
	if err != nil {
		return Commit{}, err
	}
	f := strings.Split(strings.TrimSpace(string(out)), "\x1f")
	if len(f) < 4 {
		return Commit{}, fmt.Errorf("git show %s: unexpected output", sha)
	}
	return Commit{SHA: f[0], Author: f[1], Subject: f[3]}, nil
}
