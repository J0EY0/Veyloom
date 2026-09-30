package wiki

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Skills in a bundle (docs/design.md 5.10, 5.11). A skill is a directory
// under skills/ named after it, holding its page SKILL.md: a concept of
// type Skill that carries the Agent Skills keys next to OKF's, and other
// files it refers to (references, scripts). The directory stays flat, not
// sorted by team: skill names are unique across the library, as the
// runtimes load them side by side.

// TeamKey is the metadata key that names the project owning a skill, by
// its wiki folder name.
const TeamKey = "veyloom-team"

// ReferenceType is the type the library gives the markdown files of a
// skill's folder that have none, its references and templates, so that
// they are OKF concepts like every page (okf.WithType).
const ReferenceType = "Reference"

// RuntimeTagPrefix starts a tag that keeps a skill for one runtime:
// runtime-claude, runtime-codex, runtime-pi. A skill with none is for all.
const RuntimeTagPrefix = "runtime-"

// SkillLeavesOut reports whether a file or folder of a skill, by its
// name, stays out of the library and away from the runtimes (docs/
// design.md 5.11): what version control keeps for itself, .git and the
// .gitignore, .gitattributes and .gitmodules that would change how the
// library's own history keeps the skill; what an operating system leaves
// behind; the environments and caches tools leave in a folder they ran
// in, .venv or .pytest_cache; and an environment file, which may hold real
// secrets, though not an .env.example. A skill's other hidden files, its
// settings and templates, come along.
func SkillLeavesOut(name string) bool {
	switch name {
	case ".git", ".gitignore", ".gitattributes", ".gitmodules", ".hg", ".svn",
		".DS_Store", "Thumbs.db", "__MACOSX",
		".venv", ".tox", ".nox", ".cache", ".ipynb_checkpoints", ".eslintcache",
		".env":
		return true
	}
	if strings.HasPrefix(name, ".") && (strings.HasSuffix(name, "_cache") || strings.HasSuffix(name, "-cache")) {
		return true
	}
	if kind, ok := strings.CutPrefix(name, ".env."); ok {
		return kind != "example" && kind != "sample" && kind != "template"
	}
	return false
}

// HiddenPath reports whether p, a path in a skill's folder, has a hidden
// part: such a file is kept as it is, never as a page of the library.
func HiddenPath(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") && part != "." && part != ".." {
			return true
		}
	}
	return false
}

// SkillPath is where the page of the skill called name is.
func SkillPath(name string) string { return "/skills/" + name + "/" + okf.SkillFile }

// SkillOfFile is the skill a file of the library belongs to, its SKILL.md
// or any other file of its folder; "" when p is in no skill's folder.
func SkillOfFile(p string) string {
	rest, ok := strings.CutPrefix(path.Clean("/"+p), "/skills/")
	if !ok {
		return ""
	}
	name, inner, ok := strings.Cut(rest, "/")
	if !ok || inner == "" || name == "" {
		return ""
	}
	return name
}

// SkillName is the skill whose page is at p, "" when p is no skill's page.
func SkillName(p string) string {
	dir := path.Dir(p)
	if path.Base(p) != okf.SkillFile || path.Dir(dir) != "/skills" {
		return ""
	}
	return path.Base(dir)
}

// ForRuntime reports whether a runtime may load the skill: every runtime,
// unless runtime-<name> tags keep it for the ones they name.
func (s Summary) ForRuntime(runtime string) bool {
	kept := false
	for _, tag := range s.Tags {
		if name, ok := strings.CutPrefix(strings.ToLower(tag), RuntimeTagPrefix); ok {
			if name == runtime {
				return true
			}
			kept = true
		}
	}
	return !kept
}

// Skills lists the skills a runtime is given: current ones (not drafts,
// not deprecated) that are for it, by name.
func (b *Bundle) Skills(runtime string) []Summary {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []Summary
	for _, e := range b.pages {
		if e.sum.Type == "Skill" && SkillName(e.sum.Path) != "" && e.sum.Status == okf.Stable && e.sum.ForRuntime(runtime) {
			out = append(out, e.sum)
		}
	}
	slices.SortFunc(out, func(x, y Summary) int { return strings.Compare(x.Path, y.Path) })
	return out
}

// Limits of a skill, in the library and on its way to a runtime, which it
// makes with every turn it is installed for (docs/design.md 5.11): room
// for the skills people share, whose scripts, schemas, templates and fonts
// run to 83 files and 5 MB in Anthropic's public ones, and not for more
// than a turn should carry.
const (
	MaxSkillFile  = 10 << 20
	MaxSkillFiles = 512
	MaxSkillBytes = 50 << 20
)

// MB writes a size in megabytes to a tenth, for a person to read.
func MB(n int64) string { return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) }

// ProjectedSkill is a skill as a runtime loads it: files by their path in
// the skill's directory, SKILL.md among them, and which of them run as
// they are.
type ProjectedSkill struct {
	Name       string
	Files      map[string][]byte
	Executable []string
}

// ProjectSkill reads a skill as Agent Skills has it, for a runtime to load
// or a person to take elsewhere. The library keeps its own record on the
// skill's pages, which a skill anywhere else has not: SKILL.md comes without
// OKF's fields, and the other markdown files of its folder as their author
// wrote them, without the type line the library put in front of them and
// the stamps of who changed them since (okf.WithoutType). Links into the
// folder written from the library's root, as an agent improving the skill
// may write them, lead there from where each file is; the others stay as
// written. Other files come as they are, those the library keeps
// executable, and scripts opening with #! that came in without the bit,
// named executable. What SkillLeavesOut names, and files too big to be
// meant for reading, stay behind.
func (b *Bundle) ProjectSkill(name string) (ProjectedSkill, error) {
	page, err := b.Page(SkillPath(name))
	if err != nil {
		return ProjectedSkill{}, err
	}
	skill := page.Doc.AgentSkill()
	if body := inSkillLinks(page.Path, skill.Body()); body != skill.Body() {
		skill.SetBody(body)
	}
	main, err := skill.Bytes()
	if err != nil {
		return ProjectedSkill{}, err
	}
	out := ProjectedSkill{Name: name, Files: map[string][]byte{okf.SkillFile: main}}
	total := int64(len(main))
	dir := filepath.Dir(b.file(page.Path))
	err = filepath.WalkDir(dir, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if fp != dir && SkillLeavesOut(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(dir, fp)
		rel = filepath.ToSlash(rel)
		if rel == okf.SkillFile {
			return nil
		}
		if len(out.Files) >= MaxSkillFiles {
			return fmt.Errorf("%w: skill %s has more than %d files", store.ErrInvalidInput, name, MaxSkillFiles)
		}
		info, err := d.Info()
		if err != nil || info.Size() > MaxSkillFile {
			return err
		}
		if total += info.Size(); total > MaxSkillBytes {
			return fmt.Errorf("%w: skill %s comes to more than %s MB", store.ErrInvalidInput, name, MB(MaxSkillBytes))
		}
		data, err := os.ReadFile(fp)
		if err != nil {
			return err
		}
		if info.Mode()&0o111 != 0 || bytes.HasPrefix(data, []byte("#!")) {
			out.Executable = append(out.Executable, rel)
		}
		if p := path.Dir(page.Path) + "/" + rel; path.Ext(rel) == ".md" && !isReserved(p) && !HiddenPath(rel) {
			data = []byte(inSkillLinks(p, string(okf.WithoutType(data, ReferenceType))))
		}
		out.Files[rel] = data
		return nil
	})
	return out, err
}

// inSkillLinks rewrites the links of text, a file at p in a skill's
// folder, that lead into that folder from the library's root to lead there
// from p's own directory, as a runtime reads them where the skill is.
// Links written from where the file is, and links elsewhere, stay as they
// are.
func inSkillLinks(p, text string) string {
	name := SkillOfFile(p)
	folder := "/skills/" + name + "/"
	return okf.RewriteLinks(text, func(target string) (string, bool) {
		if name == "" || !strings.HasPrefix(target, "/") {
			return "", false
		}
		to, ok := okf.Resolve(p, target)
		if !ok || !strings.HasPrefix(to, folder) {
			return "", false
		}
		rest := ""
		if i := strings.IndexAny(target, "#?"); i >= 0 {
			rest = target[i:]
		}
		return relativePath(path.Dir(p), to) + rest, true
	})
}

// relativePath is the way from the directory dir to the file at to, both
// written from the bundle's root.
func relativePath(dir, to string) string {
	from := strings.Split(strings.Trim(dir, "/"), "/")
	parts := strings.Split(strings.Trim(to, "/"), "/")
	i := 0
	for i < len(from) && i < len(parts)-1 && from[i] == parts[i] {
		i++
	}
	var out []string
	for range from[i:] {
		out = append(out, "..")
	}
	return strings.Join(append(out, parts[i:]...), "/")
}

// SkillHistory lists the commits that changed the folder of the skill
// called name, any file of it, newest first.
func (b *Bundle) SkillHistory(ctx context.Context, name string, limit int) ([]Commit, error) {
	if !slug.MatchString(name) {
		return nil, fmt.Errorf("%w: %q is no skill name", store.ErrInvalidInput, name)
	}
	if b.git == nil {
		return nil, nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.git.logOf(ctx, limit, "--", "skills/"+name+"/")
}

// SkillDiff is how the folder of the skill called name changed from
// commit from to commit to, a diff as git shows it; a commit may be
// followed by ^ for the one before it.
func (b *Bundle) SkillDiff(ctx context.Context, name, from, to string) (string, error) {
	if !slug.MatchString(name) {
		return "", fmt.Errorf("%w: %q is no skill name", store.ErrInvalidInput, name)
	}
	if !commitRef.MatchString(from) || !commitRef.MatchString(to) {
		return "", fmt.Errorf("%w: %q or %q names no commit", store.ErrInvalidInput, from, to)
	}
	if b.git == nil {
		return "", ErrNoHistory
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	out, err := b.git.run(ctx, "diff", "--no-color", "--no-ext-diff", from, to, "--", "skills/"+name+"/")
	return string(out), err
}

// commitRef is a commit by its hash, whole or cut short, or the one before.
var commitRef = regexp.MustCompile(`^[0-9a-f]{4,64}\^?$`)

// SkillBusy reports whether a writer has changed a file of the skill
// called name and not committed it yet: a turn improving it.
func (b *Bundle) SkillBusy(name string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for p, n := range b.busy {
		if n > 0 && SkillOfFile(p) == name {
			return true
		}
	}
	return false
}

// RemoveSkillFile deletes a file of a skill's folder, a page of it or not,
// as part of the writer's commit: one the skill no longer has, updated from
// where it came from. The skill's SKILL.md is not removed this way.
func (w *Writer) RemoveSkillFile(p string) error {
	p = path.Clean("/" + p)
	if !okf.SkillFolderFile(p) {
		return fmt.Errorf("%w: %s is no file of a skill's folder besides its %s", store.ErrInvalidInput, p, okf.SkillFile)
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	c := w.touch(p)
	fp := w.b.file(p)
	if err := os.Remove(fp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// Folders left empty go too, up to the skill's own.
	top := w.b.file("/skills/" + SkillOfFile(p))
	for dir := filepath.Dir(fp); dir != top && strings.HasPrefix(dir, top+string(filepath.Separator)); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			break
		}
	}
	delete(w.b.pages, p)
	delete(w.b.problems, p)
	c.gone, c.content = true, true
	return nil
}

// SetStatus sets whether a page is in use, as a person decides it: a
// skill they retire, or put back in use. The page says what it said, by
// whom it said it; stable, the page's own default, leaves the key out.
func (w *Writer) SetStatus(p string, status okf.Status) (Page, error) {
	p, err := CleanPath(p)
	if err != nil {
		return Page{}, err
	}
	if !status.Valid() {
		return Page{}, fmt.Errorf("%w: status %q", store.ErrInvalidInput, status)
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	e, err := w.current(p, "")
	if err != nil {
		return Page{}, err
	}
	d, err := okf.Parse(e.data)
	if err != nil {
		return Page{}, err
	}
	if d.Status() == status {
		return e.page()
	}
	if status == okf.Stable {
		d.Delete(okf.KeyStatus)
	} else {
		d.SetStatus(status)
	}
	return w.save(p, d, mechanical, func(c *change) { c.content, c.deprecated = true, status == okf.Deprecated })
}

// RemoveSkill deletes the folder of the skill called name, all of it, as
// part of the writer's commit: a skill a person removes from the library.
// The log says so; the history keeps it.
func (w *Writer) RemoveSkill(name string) error {
	if !slug.MatchString(name) {
		return fmt.Errorf("%w: %q is no skill name", store.ErrInvalidInput, name)
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	page := SkillPath(name)
	if w.b.pages[page] == nil {
		return fmt.Errorf("%w: no skill %s", store.ErrNotFound, name)
	}
	dir := w.b.file("/skills/" + name)
	var files []string
	err := filepath.WalkDir(dir, func(fp string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(w.b.dir, fp)
		files = append(files, "/"+filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		return err
	}
	for _, p := range files {
		c := w.touch(p)
		c.gone, c.content = true, true
		delete(w.b.pages, p)
		delete(w.b.problems, p)
	}
	w.touch(page).removed = true
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return w.b.regenerate(page)
}

// AddSource adds a source to a page unless it cites that resource already:
// a pattern a skill draws on, say. Like SetMetadata it is the page's
// record, not its content: the author stamp stays as it is.
func (w *Writer) AddSource(p string, src okf.Source) (Page, error) {
	p, err := CleanPath(p)
	if err != nil {
		return Page{}, err
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	e, err := w.current(p, "")
	if err != nil {
		return Page{}, err
	}
	d, err := okf.Parse(e.data)
	if err != nil {
		return Page{}, err
	}
	for _, s := range d.Sources() {
		if s.Resource == src.Resource {
			return e.page()
		}
	}
	d.AddSource(src)
	return w.save(p, d, mechanical, func(c *change) { c.content = true })
}

// SetMetadata sets one entry of a skill page's metadata, or removes it
// when value is empty: the owning team, say. Like Tag, it files the page
// differently and leaves its content and its author stamp as they are.
func (w *Writer) SetMetadata(p, key, value string) (Page, error) {
	p, err := CleanPath(p)
	if err != nil {
		return Page{}, err
	}
	w.b.mu.Lock()
	defer w.b.mu.Unlock()
	e, err := w.current(p, "")
	if err != nil {
		return Page{}, err
	}
	d, err := okf.Parse(e.data)
	if err != nil {
		return Page{}, err
	}
	if d.Metadata()[key] == value {
		return e.page()
	}
	if value == "" {
		d.DeleteMetadata(key)
	} else {
		d.SetMetadata(key, value)
	}
	return w.save(p, d, mechanical, func(c *change) { c.content = true })
}
