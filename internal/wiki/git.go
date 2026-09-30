package wiki

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// repo runs git in a bundle's directory. The history stays local: Veyloom
// never pushes, pulls or adds a remote (design.md 5.5). Every call pins the
// settings a commit depends on, so the person's own git configuration
// (signing, hooks, line endings) cannot stall or change what is recorded,
// and GIT_* variables of the process that started Veyloom cannot point git
// at another repository.
type repo struct {
	dir string
	bin string
}

// The committer on every commit; the author is whoever made the change.
const (
	committerName  = "Veyloom"
	committerEmail = "veyloom@localhost"
)

func openRepo(ctx context.Context, dir string) (*repo, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("wiki: git keeps the wiki's history and was not found: %w", err)
	}
	r := &repo{dir: dir, bin: bin}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return r, nil
	}
	if _, err := r.run(ctx, "init", "-q", "--template=", "--initial-branch=main"); err != nil {
		return nil, err
	}
	return r, nil
}

// exitError carries git's exit code, for the commands that answer with it.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func (r *repo) run(ctx context.Context, args ...string) ([]byte, error) {
	full := []string{
		"-C", r.dir,
		"-c", "user.name=" + committerName, "-c", "user.email=" + committerEmail,
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null",
		"-c", "core.autocrlf=false", "-c", "core.quotepath=false",
	}
	cmd := exec.CommandContext(ctx, r.bin, append(full, args...)...)
	env := []string{"GIT_TERMINAL_PROMPT=0", "LC_ALL=C"}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") && !strings.HasPrefix(kv, "LC_ALL=") {
			env = append(env, kv)
		}
	}
	cmd.Env = env
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		err = fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return out.Bytes(), &exitError{code: exit.ExitCode(), err: err}
		}
		return out.Bytes(), err
	}
	return out.Bytes(), nil
}

func (r *repo) hasHead(ctx context.Context) bool {
	_, err := r.run(ctx, "rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

// commit records the given pages, and nothing else, as one commit by
// author. It returns "" when none of them changed.
func (r *repo) commit(ctx context.Context, paths []string, author, message string) (string, error) {
	if r.hasHead(ctx) {
		if _, err := r.run(ctx, "reset", "-q"); err != nil {
			return "", err
		}
	}
	var present, gone []string
	for _, p := range paths {
		rel := strings.TrimPrefix(p, "/")
		if existsAsNamed(filepath.Join(r.dir, filepath.FromSlash(rel))) {
			present = append(present, rel)
		} else {
			gone = append(gone, rel)
		}
	}
	// What went goes first: a file renamed only in case, where case does
	// not count, is then added under its new name, not kept under the old.
	if len(gone) > 0 {
		if _, err := r.run(ctx, append([]string{"rm", "--cached", "-q", "--ignore-unmatch", "--"}, gone...)...); err != nil {
			return "", err
		}
	}
	if len(present) > 0 {
		if _, err := r.run(ctx, append([]string{"add", "-A", "--"}, present...)...); err != nil {
			return "", err
		}
	}
	if staged, err := r.staged(ctx); err != nil || !staged {
		return "", err
	}
	return r.commitStaged(ctx, author, message)
}

// existsAsNamed reports whether a file is there under this very name: on
// a disk where case does not count, FORMS.md answers for forms.md too, but
// is not there as forms.md.
func existsAsNamed(fp string) bool {
	if _, err := os.Lstat(fp); err != nil {
		return false
	}
	entries, err := os.ReadDir(filepath.Dir(fp))
	if err != nil {
		return true
	}
	name := filepath.Base(fp)
	for _, e := range entries {
		if e.Name() == name {
			return true
		}
	}
	return false
}

// staged reports whether anything is staged.
func (r *repo) staged(ctx context.Context) (bool, error) {
	_, err := r.run(ctx, "diff", "--cached", "--quiet")
	var exit *exitError
	if errors.As(err, &exit) && exit.code == 1 {
		return true, nil
	}
	return false, err
}

func (r *repo) commitStaged(ctx context.Context, author, message string) (string, error) {
	if _, err := r.run(ctx, "commit", "-q", "--no-verify", "--author="+gitIdentity(author), "-m", message); err != nil {
		return "", err
	}
	out, err := r.run(ctx, "rev-parse", "HEAD")
	return strings.TrimSpace(string(out)), err
}

// status lists files that differ from the last commit, from the bundle
// root with a leading slash.
func (r *repo) status(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, rec := range strings.Split(string(out), "\x00") {
		if len(rec) > 3 {
			paths = append(paths, "/"+rec[3:])
		}
	}
	return paths, nil
}

// inHead reports whether a file is in the last commit.
func (r *repo) inHead(ctx context.Context, p string) bool {
	_, err := r.run(ctx, "cat-file", "-e", "HEAD:"+strings.TrimPrefix(p, "/"))
	return err == nil
}

// Commit is one entry of a bundle's history.
type Commit struct {
	SHA     string
	Author  string
	At      time.Time
	Subject string
	// Changes are the log lines the commit added, one per page it touched,
	// read back from its message.
	Changes  []Change
	Trailers map[string]string
}

// Change is one line of a commit: what happened, to which page.
type Change struct {
	// Kind is the log's word for it, an okf.Log* kind.
	Kind string
	// Path and Title are the page the line links to; empty for a line
	// that names no page, like an undo.
	Path  string
	Title string
	// Text is the line after its kind, as the log has it.
	Text string
}

// log returns the latest commits touching p (the whole bundle when p is
// ""), newest first, following a page across renames.
func (r *repo) log(ctx context.Context, p string, limit int) ([]Commit, error) {
	var paths []string
	if p != "" {
		paths = []string{"--follow", "--", strings.TrimPrefix(p, "/")}
	}
	return r.logOf(ctx, limit, paths...)
}

// logOf lists the commits, newest first, limited by git's own arguments
// after the format: paths, say.
func (r *repo) logOf(ctx context.Context, limit int, args ...string) ([]Commit, error) {
	if !r.hasHead(ctx) {
		return nil, nil
	}
	args = append([]string{"log", "-n", strconv.Itoa(limit), "--format=%H%x1f%an%x1f%aI%x1f%s%x1f%b%x1e"}, args...)
	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, rec := range strings.Split(string(out), "\x1e") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x1f")
		if len(f) < 5 {
			continue
		}
		at, _ := time.Parse(time.RFC3339, f[2])
		commits = append(commits, Commit{SHA: f[0], Author: f[1], At: at, Subject: f[3], Changes: changes(f[4]), Trailers: trailers(f[4])})
	}
	return commits, nil
}

var (
	trailerLine = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9-]*): (.+)$`)
	changeLine  = regexp.MustCompile(`^\* ([A-Za-z]+): (.+)$`)
	// pageLink is the link a change line opens with, as linkText escapes
	// the title.
	pageLink = regexp.MustCompile(`^\[((?:[^\]\\]|\\.)*)\]\((/[^)\s]+\.md)\)`)
)

// changes reads the log lines of a commit message back (see message).
func changes(body string) []Change {
	var out []Change
	for _, line := range strings.Split(body, "\n") {
		m := changeLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		c := Change{Kind: m[1], Text: m[2]}
		if l := pageLink.FindStringSubmatch(m[2]); l != nil {
			c.Title = strings.NewReplacer(`\[`, `[`, `\]`, `]`).Replace(l[1])
			c.Path = l[2]
		} else if p, _, ok := strings.Cut(m[2], " was removed"); ok && strings.HasPrefix(p, "/") {
			c.Path = p
		}
		out = append(out, c)
	}
	return out
}

// trailers reads "Key: value" lines from the last paragraph of a message.
func trailers(body string) map[string]string {
	paras := strings.Split(strings.TrimSpace(body), "\n\n")
	last := paras[len(paras)-1]
	out := map[string]string{}
	for _, line := range strings.Split(last, "\n") {
		if m := trailerLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out[m[1]] = m[2]
		}
	}
	return out
}

// revert applies the inverse of a commit to the working tree and index,
// without committing. Conflicts in the files Veyloom generates are settled
// by keeping what is there now, since those are written afresh anyway; a
// conflict in a page means a later change touched the same lines, and the
// revert is abandoned.
func (r *repo) revert(ctx context.Context, sha string, generated func(string) bool) error {
	_, revertErr := r.run(ctx, "revert", "--no-commit", sha)
	if revertErr == nil {
		return nil
	}
	out, err := r.run(ctx, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		_, _ = r.run(ctx, "revert", "--abort")
		return err
	}
	var pages, own []string
	for _, f := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		switch {
		case f == "":
		case generated("/" + f):
			own = append(own, f)
		default:
			pages = append(pages, "/"+f)
		}
	}
	switch {
	case len(pages) > 0:
		_, _ = r.run(ctx, "revert", "--abort")
		return store.Conflicting("undoBlocked", store.Params{"pages": strings.Join(pages, ", "), "commit": short(sha)},
			"later changes to %s get in the way of undoing %s", strings.Join(pages, ", "), short(sha))
	case len(own) == 0:
		_, _ = r.run(ctx, "revert", "--abort")
		// Not the pages: git itself failed, which is no person's to set right.
		return fmt.Errorf("cannot undo %s: %w", short(sha), revertErr)
	}
	_, err = r.run(ctx, append([]string{"checkout", "HEAD", "--"}, own...)...)
	return err
}

// gitIdentity turns an actor into the "Name <email>" git wants.
func gitIdentity(actor string) string {
	local := strings.Map(func(r rune) rune {
		if r < 128 && (r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return r
		}
		return '-'
	}, actor)
	return actor + " <" + local + "@veyloom.local>"
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
