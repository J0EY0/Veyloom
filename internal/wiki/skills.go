package wiki

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
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
// the skill's directory, SKILL.md among them.
type ProjectedSkill struct {
	Name  string
	Files map[string][]byte
}

// ProjectSkill reads a skill as Agent Skills has it, for a runtime to load
// or a person to take elsewhere. The library keeps its own record on the
// skill's pages, which a skill anywhere else has not: SKILL.md comes without
// OKF's fields, and the other pages of its folder, its references, as the
// plain markdown they were, their links into the folder written from where
// each file is, the library's pages linking from its root. Other files come
// as they are. Hidden files, and files too big to be meant for reading,
// stay behind.
func (b *Bundle) ProjectSkill(name string) (ProjectedSkill, error) {
	page, err := b.Page(SkillPath(name))
	if err != nil {
		return ProjectedSkill{}, err
	}
	skill := page.Doc.AgentSkill()
	skill.SetBody(inSkillLinks(page.Path, skill.Body()))
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
		if path.Ext(rel) == ".md" {
			if doc, err := okf.Parse(data); err == nil && doc.Type() != "" {
				data = []byte(inSkillLinks(path.Dir(page.Path)+"/"+rel, doc.Body()))
			}
		}
		out.Files[rel] = data
		return nil
	})
	return out, err
}

// inSkillLinks rewrites the links of body, the text of the file at p in a
// skill's folder, that lead into that folder to lead there from p's own
// directory, as a runtime reads them where the skill is. Links elsewhere
// stay as they are.
func inSkillLinks(p, body string) string {
	name := SkillOfFile(p)
	folder := "/skills/" + name + "/"
	return okf.RewriteLinks(body, func(target string) (string, bool) {
		to, ok := okf.Resolve(p, target)
		if !ok || name == "" || !strings.HasPrefix(to, folder) {
			return "", false
		}
		rest := ""
		if i := strings.IndexAny(target, "#?"); i >= 0 {
			rest = target[i:]
		}
		rel := relativePath(path.Dir(p), to) + rest
		return rel, rel != target
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
