package wiki

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
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

// RuntimeTagPrefix starts a tag that keeps a skill for one runtime:
// runtime-claude, runtime-codex, runtime-pi. A skill with none is for all.
const RuntimeTagPrefix = "runtime-"

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

// Limits of a projected skill: its files are text a runtime reads, and
// travel to the machine with every turn.
const (
	maxSkillFile  = 256 << 10
	maxSkillFiles = 64
)

// ProjectedSkill is a skill as a runtime loads it: files by their path in
// the skill's directory, SKILL.md among them.
type ProjectedSkill struct {
	Name  string
	Files map[string][]byte
}

// ProjectSkill reads a skill for a runtime: SKILL.md in Agent Skills form,
// the directory's other files as they are. Hidden files, and files too big
// to be meant for reading, stay behind.
func (b *Bundle) ProjectSkill(name string) (ProjectedSkill, error) {
	page, err := b.Page(SkillPath(name))
	if err != nil {
		return ProjectedSkill{}, err
	}
	main, err := page.Doc.AgentSkill().Bytes()
	if err != nil {
		return ProjectedSkill{}, err
	}
	out := ProjectedSkill{Name: name, Files: map[string][]byte{okf.SkillFile: main}}
	dir := filepath.Dir(b.file(page.Path))
	err = filepath.WalkDir(dir, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if fp != dir && strings.HasPrefix(d.Name(), ".") {
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
		if len(out.Files) >= maxSkillFiles {
			return fmt.Errorf("%w: skill %s has more than %d files", store.ErrInvalidInput, name, maxSkillFiles)
		}
		info, err := d.Info()
		if err != nil || info.Size() > maxSkillFile {
			return err
		}
		data, err := os.ReadFile(fp)
		if err != nil {
			return err
		}
		out.Files[rel] = data
		return nil
	})
	return out, err
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
