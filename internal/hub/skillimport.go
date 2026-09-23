package hub

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Adding a skill to the library from a folder on this machine (docs/
// design.md 5.15). Skills come from people, and many have some already, in
// ~/.claude/skills say, in Agent Skills form: the folder's SKILL.md is
// given the fields OKF asks for; its other markdown files, references,
// become Reference pages under names the library allows, the links to them
// following; everything else goes in as it is.

// Limits of an imported skill, as for one the library projects.
const (
	importFiles    = 64
	importFileSize = 256 << 10
)

var (
	importSlugJunk = regexp.MustCompile(`[^a-z0-9]+`)
	importHeading  = regexp.MustCompile(`(?m)^#\s+(.+?)\s*#*\s*$`)
	skillNameShape = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// ImportSkill adds the skill in folder to the library as the person's,
// looked after by the team of the project team, or by none when team is
// "". It is not installed for anyone yet.
func (h *Hub) ImportSkill(ctx context.Context, folder, team, userID string) (WikiPageView, error) {
	folder = strings.TrimSpace(folder)
	if !filepath.IsAbs(folder) {
		return WikiPageView{}, store.Invalid("folderNotAbsolute", store.Params{"path": folder}, "%s is not a full path", folder)
	}
	folder = filepath.Clean(folder)
	if !isDir(folder) {
		return WikiPageView{}, store.Invalid("folderMissing", store.Params{"path": folder}, "%s is not a folder on this machine", folder)
	}
	// A runtime's skills folder often links to where a skill really is.
	root := folder
	if real, err := filepath.EvalSymlinks(folder); err == nil {
		root = real
	}
	main, err := os.ReadFile(filepath.Join(root, okf.SkillFile))
	if err != nil {
		return WikiPageView{}, store.Invalid("skillFileMissing", store.Params{"path": folder}, "%s holds no %s", folder, okf.SkillFile)
	}
	d, err := okf.Parse(main)
	if err != nil {
		return WikiPageView{}, store.Invalid("skillUnreadable", store.Params{"path": folder}, "the %s in %s does not read as Agent Skills: %v", okf.SkillFile, folder, err)
	}
	name, _ := d.String(okf.KeyName)
	if name == "" {
		name = filepath.Base(folder)
	}
	if !wikiSlug(name) {
		return WikiPageView{}, store.Invalid("skillBadName", store.Params{"name": name}, "%q is no skill name: lowercase letters, digits and hyphens", name)
	}
	if t := d.Type(); t != "" && t != "Skill" {
		return WikiPageView{}, store.Invalid("skillUnreadable", store.Params{"path": folder}, "the %s in %s is a %s, not a skill", okf.SkillFile, folder, t)
	}
	if desc, _ := d.String(okf.KeyDescription); strings.TrimSpace(desc) == "" {
		return WikiPageView{}, store.Invalid("skillNoDescription", store.Params{"name": name}, "the skill %s has no description, which is what a runtime decides by", name)
	}

	r, err := h.openLibrary(ctx)
	if err != nil {
		return WikiPageView{}, err
	}
	target := wiki.SkillPath(name)
	if _, err := r.bundle.Page(target); err == nil {
		return WikiPageView{}, store.Conflicting("skillExists", store.Params{"name": name}, "the skill library has a skill %s already", name)
	}
	files, err := importFilesOf(root)
	if err != nil {
		return WikiPageView{}, err
	}
	d.KeepToSpec(true)
	d.SetString(okf.KeyType, "Skill")
	d.SetString(okf.KeyName, name)
	if d.Title() == "" {
		d.SetString(okf.KeyTitle, importTitle(name, d.Body()))
	}
	if team != "" {
		project, err := h.store.GetProject(ctx, team)
		if err != nil {
			return WikiPageView{}, err
		}
		d.SetMetadata(wiki.TeamKey, project.WikiSlug)
	}

	// Pages keep the names the library allows; the links to them follow.
	dir := path.Dir(target)
	renamed := map[string]string{}
	for _, f := range files {
		if path.Ext(f) == ".md" {
			renamed[dir+"/"+f] = dir + "/" + importPagePath(f, renamed)
		}
	}
	// SKILL.md links to its files as Agent Skills does, from its folder.
	d.SetBody(okf.RewriteLinks(d.Body(), func(link string) (string, bool) {
		to, ok := okf.Resolve(target, link)
		if !ok || renamed[to] == "" {
			return "", false
		}
		return strings.TrimPrefix(renamed[to], dir+"/"), true
	}))

	person, err := h.personActor(ctx, userID)
	if err != nil {
		return WikiPageView{}, err
	}
	w, err := r.bundle.Writer(person)
	if err != nil {
		return WikiPageView{}, err
	}
	if _, err := w.Create(target, d); err != nil {
		return WikiPageView{}, err
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			return WikiPageView{}, err
		}
		if path.Ext(f) != ".md" {
			if err := w.PutFile(dir+"/"+f, data); err != nil {
				return WikiPageView{}, err
			}
			continue
		}
		page := renamed[dir+"/"+f]
		if _, err := w.Create(page, importReference(dir+"/"+f, data, renamed)); err != nil {
			return WikiPageView{}, err
		}
	}
	if _, err := w.Commit(ctx, "Imported the skill "+name+" from "+folder); err != nil {
		return WikiPageView{}, err
	}
	h.changed(r)
	return h.LibraryPage(ctx, target)
}

// importFilesOf lists the files of a skill's folder besides its SKILL.md,
// by their path in it: hidden ones stay behind; too many, or one too big
// for a runtime to read, refuse the import.
func importFilesOf(folder string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(folder, func(fp string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if fp != folder && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		rel, _ := filepath.Rel(folder, fp)
		rel = filepath.ToSlash(rel)
		if rel == okf.SkillFile {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > importFileSize {
			return store.Invalid("skillFileTooBig", store.Params{"path": rel, "kb": strconv.FormatInt(info.Size()>>10, 10), "max": strconv.Itoa(importFileSize >> 10)},
				"%s is %d KB; a skill's file should stay under %d KB", rel, info.Size()>>10, importFileSize>>10)
		}
		files = append(files, rel)
		if len(files) > importFiles {
			return store.Invalid("skillTooManyFiles", store.Params{"max": strconv.Itoa(importFiles)}, "a skill holds at most %d files", importFiles)
		}
		return nil
	})
	slices.Sort(files)
	return files, err
}

// importPagePath is the path in its skill's folder a markdown file goes to:
// each part lowercase words joined by hyphens, numbered when two come out
// the same.
func importPagePath(rel string, taken map[string]string) string {
	parts := strings.Split(strings.TrimSuffix(rel, ".md"), "/")
	for i, p := range parts {
		parts[i] = strings.Trim(importSlugJunk.ReplaceAllString(strings.ToLower(p), "-"), "-")
		if parts[i] == "" {
			parts[i] = "page"
		}
	}
	// index.md and log.md are the bundle's own listing and history.
	if last := parts[len(parts)-1]; last == "index" || last == "log" {
		parts[len(parts)-1] = last + "-page"
	}
	base := strings.Join(parts, "/")
	used := func(p string) bool {
		for _, v := range taken {
			if strings.HasSuffix(v, "/"+p) {
				return true
			}
		}
		return false
	}
	out := base + ".md"
	for n := 2; used(out); n++ {
		out = base + "-" + strconv.Itoa(n) + ".md"
	}
	return out
}

// importReference makes a Reference page of a skill's markdown file, which
// was at from in the library's terms: its own frontmatter kept, to what
// OKF defines, when it has a type, else a title given; its links written
// from the library's root, as a page's are, following renames.
func importReference(from string, data []byte, renamed map[string]string) *okf.Document {
	d, err := okf.Parse(data)
	if err != nil || d.Type() == "" {
		d = okf.New("Reference")
		body := string(data)
		if parsed, err := okf.Parse(data); err == nil {
			body = parsed.Body()
		}
		title := strings.TrimSuffix(path.Base(from), ".md")
		if m := importHeading.FindStringSubmatch(body); m != nil {
			title = m[1]
		}
		d.SetString(okf.KeyTitle, title)
		d.SetBody(body)
	} else {
		d.KeepToSpec(false)
	}
	d.SetBody(okf.RewriteLinks(d.Body(), func(link string) (string, bool) {
		to, ok := okf.Resolve(from, link)
		if !ok {
			return "", false
		}
		if r := renamed[to]; r != "" {
			return r, true
		}
		return to, true
	}))
	return d
}

// importTitle is a title for a skill that has none: the heading its
// instructions open with, or failing one its name as words.
func importTitle(name, body string) string {
	if m := importHeading.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	words := strings.ReplaceAll(name, "-", " ")
	return strings.ToUpper(words[:1]) + words[1:]
}

// wikiSlug reports whether s is lowercase words joined by hyphens, at most
// 64 characters: a skill's name, as Agent Skills has it.
func wikiSlug(s string) bool {
	return len(s) <= 64 && skillNameShape.MatchString(s)
}
