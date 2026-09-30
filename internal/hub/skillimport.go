package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
// given the fields OKF asks for and keeps all it had; everything else goes
// in as it is, under the name it had, its other markdown files, references
// and templates, with a type line put in front when they have none, which
// the runtimes never see (wiki.Bundle.ProjectSkill).

var (
	importHeading  = regexp.MustCompile(`(?m)^#\s+(.+?)\s*#*\s*$`)
	skillNameShape = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// ImportSkill adds the skill in folder to the library as the person's,
// looked after by the team of the project team, or by none when team is
// "". It is not installed for anyone yet.
func (h *Hub) ImportSkill(ctx context.Context, folder, team, userID string) (WikiPageView, error) {
	return h.importSkill(ctx, folder, "", team, userID)
}

// Where a skill of the library came from (docs/design.md 5.15): the
// commit that brought it in from a folder of this machine, by import or
// update, names the folder and what it held then, for the library to tell
// when the folder changed since and to take it in again.
const (
	trailerSource     = "Veyloom-Source"
	trailerSourceHash = "Veyloom-Source-Hash"
)

// skillSource is a skill's folder on this machine as the library reads it.
type skillSource struct {
	// folder is where it was given, root where it really is.
	folder, root string
	name         string
	doc          *okf.Document // its SKILL.md
	files        []string      // the other files, by their path in it
}

// readSkillSource reads the skill in folder, refusing what the library
// cannot take.
func (h *Hub) readSkillSource(folder string) (skillSource, error) {
	folder = strings.TrimSpace(folder)
	if !filepath.IsAbs(folder) {
		return skillSource{}, store.Invalid("folderNotAbsolute", store.Params{"path": folder}, "%s is not a full path", folder)
	}
	folder = filepath.Clean(folder)
	if !isDir(folder) {
		return skillSource{}, store.Invalid("folderMissing", store.Params{"path": folder}, "%s is not a folder on this machine", folder)
	}
	// A runtime's skills folder often links to where a skill really is.
	root := folder
	if real, err := filepath.EvalSymlinks(folder); err == nil {
		root = real
	}
	main, err := os.ReadFile(filepath.Join(root, okf.SkillFile))
	if err != nil {
		return skillSource{}, store.Invalid("skillFileMissing", store.Params{"path": folder}, "%s holds no %s", folder, okf.SkillFile)
	}
	d, err := okf.Parse(main)
	if err != nil {
		return skillSource{}, store.Invalid("skillUnreadable", store.Params{"path": folder}, "the %s in %s does not read as Agent Skills: %v", okf.SkillFile, folder, err)
	}
	name, _ := d.String(okf.KeyName)
	if name == "" {
		name = filepath.Base(folder)
	}
	if !wikiSlug(name) {
		return skillSource{}, store.Invalid("skillBadName", store.Params{"name": name}, "%q is no skill name: lowercase letters, digits and hyphens", name)
	}
	if h.turns.isBuiltin(name) {
		// A runtime could not load both under the one name (design.md 5.23.6).
		return skillSource{}, store.Conflicting("skillBuiltin", store.Params{"name": name}, "Veyloom gives every agent a skill called %s already", name)
	}
	if t := d.Type(); t != "" && t != "Skill" {
		return skillSource{}, store.Invalid("skillUnreadable", store.Params{"path": folder}, "the %s in %s is a %s, not a skill", okf.SkillFile, folder, t)
	}
	if desc, _ := d.String(okf.KeyDescription); strings.TrimSpace(desc) == "" {
		return skillSource{}, store.Invalid("skillNoDescription", store.Params{"name": name}, "the skill %s has no description, which is what a runtime decides by", name)
	}
	files, err := importFilesOf(root)
	if err != nil {
		return skillSource{}, err
	}
	// Its fields beyond OKF's stay as they are: the runtimes act on them
	// (disable-model-invocation, hooks and the like).
	d.SetString(okf.KeyType, "Skill")
	d.SetString(okf.KeyName, name)
	if d.Title() == "" {
		d.SetString(okf.KeyTitle, importTitle(name, d.Body()))
	}
	return skillSource{folder: folder, root: root, name: name, doc: d, files: files}, nil
}

// importSkill is ImportSkill; from says where the folder came from for the
// library's history, when it is not the folder itself, as for an upload.
func (h *Hub) importSkill(ctx context.Context, folder, from, team, userID string) (WikiPageView, error) {
	src, err := h.readSkillSource(folder)
	if err != nil {
		return WikiPageView{}, err
	}
	r, err := h.openLibrary(ctx)
	if err != nil {
		return WikiPageView{}, err
	}
	target := wiki.SkillPath(src.name)
	if _, err := r.bundle.Page(target); err == nil {
		return WikiPageView{}, store.Conflicting("skillExists", store.Params{"name": src.name}, "the skill library has a skill %s already", src.name)
	}
	if team != "" {
		project, err := h.store.GetProject(ctx, team)
		if err != nil {
			return WikiPageView{}, err
		}
		src.doc.SetMetadata(wiki.TeamKey, project.WikiSlug)
	}
	person, err := h.personActor(ctx, userID)
	if err != nil {
		return WikiPageView{}, err
	}
	w, err := r.bundle.Writer(person)
	if err != nil {
		return WikiPageView{}, err
	}
	if _, err := w.Create(target, src.doc); err != nil {
		return WikiPageView{}, err
	}
	if err := putSkillFiles(w, src); err != nil {
		return WikiPageView{}, err
	}
	var trailers []wiki.Trailer
	if from == "" {
		from = src.folder
		if trailers, err = src.trailers(); err != nil {
			return WikiPageView{}, err
		}
	}
	if _, err := w.Commit(ctx, "Imported the skill "+src.name+" from "+from, trailers...); err != nil {
		return WikiPageView{}, err
	}
	h.changed(r)
	return h.LibraryPage(ctx, target)
}

// UpdateSkill takes the skill in folder into the library again, the one of
// its name there: what the folder holds now replaces the library's copy,
// files it no longer has going too, as one commit by the person that the
// library's history can undo. The folder is only read. The copy keeps what
// people set on it in the library: its team, the runtimes it is kept for,
// whether it is in use, and who it is installed for. A skill on trial is
// left alone, as the change an agent is trying would be lost unseen: a
// person keeps it or rolls it back first.
func (h *Hub) UpdateSkill(ctx context.Context, folder, userID string) (WikiPageView, error) {
	src, err := h.readSkillSource(folder)
	if err != nil {
		return WikiPageView{}, err
	}
	r, err := h.openLibrary(ctx)
	if err != nil {
		return WikiPageView{}, err
	}
	target := wiki.SkillPath(src.name)
	page, err := r.bundle.Page(target)
	if errors.Is(err, store.ErrNotFound) {
		return WikiPageView{}, store.Missing("skillNotInLibrary", store.Params{"name": src.name}, "the skill library has no skill %s to update: import it", src.name)
	}
	if err != nil {
		return WikiPageView{}, err
	}
	if _, err := h.store.OpenSkillTrial(ctx, src.name); err == nil {
		return WikiPageView{}, store.Conflicting("skillOnTrial", store.Params{"name": src.name}, "the skill %s is on trial: keep or roll back the change first", src.name)
	} else if !errors.Is(err, store.ErrNotFound) {
		return WikiPageView{}, err
	}
	if r.bundle.SkillBusy(src.name) {
		return WikiPageView{}, store.Conflicting("skillBusy", store.Params{"name": src.name}, "a turn is changing the skill %s right now; update it when the turn ends", src.name)
	}
	if team := page.Doc.Metadata()[wiki.TeamKey]; team != "" {
		src.doc.SetMetadata(wiki.TeamKey, team)
	} else {
		src.doc.DeleteMetadata(wiki.TeamKey)
	}
	src.doc.SetTags(page.Doc.Tags())
	if page.Doc.Has(okf.KeyStatus) {
		src.doc.SetStatus(page.Doc.Status())
	} else {
		src.doc.Delete(okf.KeyStatus)
	}
	person, err := h.personActor(ctx, userID)
	if err != nil {
		return WikiPageView{}, err
	}
	w, err := r.bundle.Writer(person)
	if err != nil {
		return WikiPageView{}, err
	}
	// What the folder no longer has goes from the library's copy, before
	// what it has comes in: a file whose name differs only in case, as the
	// library once named references, is the same file where case does not
	// count.
	kept := map[string]bool{okf.SkillFile: true}
	for _, f := range src.files {
		kept[f] = true
	}
	had, err := importFilesOf(filepath.Join(r.bundle.Dir(), "skills", src.name))
	if err != nil {
		return WikiPageView{}, err
	}
	for _, rel := range had {
		if !kept[rel] {
			if err := w.RemoveSkillFile(path.Dir(target) + "/" + rel); err != nil {
				return WikiPageView{}, err
			}
		}
	}
	if _, err := w.Put(target, src.doc, page.Hash); err != nil {
		return WikiPageView{}, err
	}
	if err := putSkillFiles(w, src); err != nil {
		return WikiPageView{}, err
	}
	trailers, err := src.trailers()
	if err != nil {
		return WikiPageView{}, err
	}
	if _, err := w.Commit(ctx, "Updated the skill "+src.name+" from "+src.folder, trailers...); err != nil {
		return WikiPageView{}, err
	}
	h.changed(r)
	return h.LibraryPage(ctx, target)
}

// putSkillFiles writes the files of a skill's folder into the library as
// they are, markdown with a type put in front where it has none.
func putSkillFiles(w *wiki.Writer, src skillSource) error {
	dir := path.Dir(wiki.SkillPath(src.name))
	for _, f := range src.files {
		data, err := os.ReadFile(filepath.Join(src.root, filepath.FromSlash(f)))
		if err != nil {
			return err
		}
		if base := path.Base(f); path.Ext(f) != ".md" || base == okf.IndexFile || base == okf.LogFile {
			if err := w.PutFile(dir+"/"+f, data); err != nil {
				return err
			}
			continue
		}
		if _, err := w.PutPage(dir+"/"+f, okf.WithType(data, wiki.ReferenceType)); err != nil {
			return err
		}
	}
	return nil
}

// trailers name the folder the skill came from, and what it held.
func (src skillSource) trailers() ([]wiki.Trailer, error) {
	hash, err := skillFolderHash(src.root, src.files)
	if err != nil {
		return nil, err
	}
	return []wiki.Trailer{{Key: trailerSource, Value: src.folder}, {Key: trailerSourceHash, Value: hash}}, nil
}

// skillFolderHash sums up what a skill's folder holds, its SKILL.md and
// the files an import takes, by path and content, for telling whether it
// changed.
func skillFolderHash(root string, files []string) (string, error) {
	sum := sha256.New()
	for _, rel := range append([]string{okf.SkillFile}, files...) {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(sum, "%s\x00%d\x00", rel, len(data))
		sum.Write(data)
	}
	return hex.EncodeToString(sum.Sum(nil))[:16], nil
}

// importFilesOf lists the files of a skill's folder besides its SKILL.md,
// by their path in it. Links are followed, to files and to folders, as a
// runtime reading the skill would, but not round in a circle; hidden files
// stay behind; too many, one too big, or too much in all for a turn to
// carry (wiki.MaxSkillFile and its likes) refuse the import.
func importFilesOf(folder string) ([]string, error) {
	var files []string
	var total int64
	within := map[string]bool{}
	var walk func(dir, rel string) error
	walk = func(dir, rel string) error {
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return err
		}
		if within[real] {
			// A folder linked to from inside itself.
			return nil
		}
		within[real] = true
		defer delete(within, real)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			fp, r := filepath.Join(dir, e.Name()), path.Join(rel, e.Name())
			info, err := os.Stat(fp)
			if err != nil {
				// A link to nothing: no runtime could read it either.
				continue
			}
			if info.IsDir() {
				if err := walk(fp, r); err != nil {
					return err
				}
				continue
			}
			if !info.Mode().IsRegular() || r == okf.SkillFile {
				continue
			}
			if info.Size() > wiki.MaxSkillFile {
				return store.Invalid("skillFileTooBig", store.Params{"path": r, "mb": wiki.MB(info.Size()), "max": wiki.MB(wiki.MaxSkillFile)},
					"%s is %s MB; a skill's file should stay under %s MB", r, wiki.MB(info.Size()), wiki.MB(wiki.MaxSkillFile))
			}
			files = append(files, r)
			if len(files) > wiki.MaxSkillFiles {
				return store.Invalid("skillTooManyFiles", store.Params{"max": strconv.Itoa(wiki.MaxSkillFiles)}, "a skill holds at most %d files", wiki.MaxSkillFiles)
			}
			if total += info.Size(); total > wiki.MaxSkillBytes {
				return store.Invalid("skillTooLarge", store.Params{"max": wiki.MB(wiki.MaxSkillBytes)}, "a skill holds at most %s MB in all", wiki.MB(wiki.MaxSkillBytes))
			}
		}
		return nil
	}
	err := walk(folder, "")
	slices.Sort(files)
	return files, err
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
