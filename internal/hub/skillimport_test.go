package hub

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// skillFolder writes files, by their path in it, to a new folder name.
func skillFolder(t *testing.T, name string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	for rel, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestHub_ImportSkill(t *testing.T) {
	l, dir := wikiLoop(t)
	home := l.project()
	src := skillFolder(t, "Release Notes", map[string]string{
		"SKILL.md": "---\nname: release-notes\ndescription: Use when writing release notes.\nlicense: MIT\nallowed-tools: Bash(git log:*)\nversion: \"2\"\nhooks: [a, b]\n---\n\n" +
			"# Writing release notes\n\nRead [the guide](references/Style_Guide.md) and [the list](references/index.md), then run `scripts/draft.sh`.\n",
		"references/Style_Guide.md": "# House style\n\nShort lines. Back to [the skill](../SKILL.md).\n",
		"references/index.md":       "---\ntype: Reference\ntitle: All of them\nowner: docs\n---\n\nSee [the guide](Style_Guide.md).\n",
		"scripts/draft.sh":          "#!/bin/sh\ngit log --oneline\n",
		".DS_Store":                 "junk",
		".git/config":               "junk",
	})

	page, err := l.h.ImportSkill(l.ctx, src, home.ID, l.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if page.Path != "/skills/release-notes/SKILL.md" || page.Team != home.WikiSlug || page.Title != "Writing release notes" || page.GeneratedBy != "human:alice" {
		t.Errorf("the skill %+v", page.WikiPageInfo)
	}
	lib := filepath.Join(dir, "library", "skills", "release-notes")
	read := func(rel string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(lib, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	// SKILL.md keeps all it had, what runtimes add to Agent Skills too, as
	// it was written, and links to its files as it did.
	main := read("SKILL.md")
	for _, want := range []string{"type: Skill", "license: MIT", "allowed-tools: Bash(git log:*)", "version: \"2\"", "hooks: [a, b]", "veyloom-team: " + home.WikiSlug,
		"](references/Style_Guide.md)", "](references/index.md)"} {
		if !strings.Contains(main, want) {
			t.Errorf("SKILL.md lacks %q:\n%s", want, main)
		}
	}
	if d, err := okf.Parse([]byte(main)); err != nil || d.Metadata()["version"] != "" {
		t.Errorf("a field stays where it was, not in metadata: %v %v", d.Metadata(), err)
	}
	// The other markdown files keep their names and what they say, a type
	// line put in front of one that has none; an index.md of the skill's
	// own is a file of it, as it was.
	if guide := read("references/Style_Guide.md"); guide != "---\ntype: Reference\n---\n# House style\n\nShort lines. Back to [the skill](../SKILL.md).\n" {
		t.Errorf("the guide:\n%q", guide)
	}
	if list := read("references/index.md"); list != "---\ntype: Reference\ntitle: All of them\nowner: docs\n---\n\nSee [the guide](Style_Guide.md).\n" {
		t.Errorf("the list:\n%q", list)
	}
	for _, rel := range []string{"SKILL.md", "references/Style_Guide.md", "references/index.md"} {
		if probs := okf.CheckFile("/skills/release-notes/"+rel, []byte(read(rel)), false, okf.Strict); len(probs) > 0 {
			t.Errorf("%s is to the spec: %v", rel, probs)
		}
	}
	if read("scripts/draft.sh") != "#!/bin/sh\ngit log --oneline\n" {
		t.Error("the script goes in as it is")
	}
	for _, hidden := range []string{".DS_Store", ".git"} {
		if _, err := os.Stat(filepath.Join(lib, hidden)); !os.IsNotExist(err) {
			t.Errorf("%s stays behind: %v", hidden, err)
		}
	}
	// One commit holds it all, by the person.
	history, _ := l.h.LibraryHistory(l.ctx, "", 5)
	if len(history) == 0 || history[0].Subject != "Imported the skill release-notes from "+src || history[0].Author != "human:alice" || len(history[0].Changes) != 2 {
		t.Errorf("history %+v", history)
	}
	// The runtimes get all of it.
	r, _ := l.h.openLibrary(l.ctx)
	projected, err := r.bundle.ProjectSkill("release-notes")
	var files []string
	for f := range projected.Files {
		files = append(files, f)
	}
	slices.Sort(files)
	if err != nil || !slices.Equal(files, []string{"SKILL.md", "references/Style_Guide.md", "references/index.md", "scripts/draft.sh"}) {
		t.Errorf("projected %v %v", files, err)
	}

	// Taken out again it is the folder it was, in Agent Skills form: the
	// library's own record gone, every other file to the byte, the script
	// still one to run.
	zipped, err := l.h.ExportSkill(l.ctx, "release-notes")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if err != nil {
		t.Fatal(err)
	}
	exported := map[string]string{}
	for _, f := range archive.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		exported[f.Name] = string(data)
		if f.Name == "release-notes/scripts/draft.sh" && f.Mode().Perm() != 0o755 {
			t.Errorf("the script's mode %v", f.Mode())
		}
	}
	if names := slices.Sorted(maps.Keys(exported)); !slices.Equal(names, []string{"release-notes/SKILL.md", "release-notes/references/Style_Guide.md", "release-notes/references/index.md", "release-notes/scripts/draft.sh"}) {
		t.Errorf("exported %v", names)
	}
	for _, rel := range []string{"references/Style_Guide.md", "references/index.md", "scripts/draft.sh"} {
		if want, _ := os.ReadFile(filepath.Join(src, filepath.FromSlash(rel))); exported["release-notes/"+rel] != string(want) {
			t.Errorf("exported %s:\n%q\nwant\n%q", rel, exported["release-notes/"+rel], want)
		}
	}
	out := exported["release-notes/SKILL.md"]
	for _, gone := range []string{"type:", "title:", "generated:"} {
		if strings.Contains(out, gone) {
			t.Errorf("exported SKILL.md keeps %q:\n%s", gone, out)
		}
	}
	for _, want := range []string{"name: release-notes", "license: MIT", "hooks: [a, b]", "](references/Style_Guide.md)"} {
		if !strings.Contains(out, want) {
			t.Errorf("exported SKILL.md lacks %q:\n%s", want, out)
		}
	}
	if _, err := l.h.ExportSkill(l.ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("no such skill: %v", err)
	}
	if _, err := l.h.ExportSkill(l.ctx, "../x"); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("no skill name: %v", err)
	}

	// A folder linked to from a runtime's skills folder is read where it is.
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(skillFolder(t, "real", map[string]string{
		"SKILL.md": "---\ndescription: Use when checking links.\n---\n\nCheck them.\n", "notes.md": "# Notes\n",
	}), linked); err != nil {
		t.Fatal(err)
	}
	if got, err := l.h.ImportSkill(l.ctx, linked, "", l.user.ID); err != nil || got.Path != "/skills/linked/SKILL.md" || got.Team != "" || got.Title != "Linked" {
		t.Errorf("a linked folder, named by the folder: %+v %v", got.WikiPageInfo, err)
	} else if _, err := os.Stat(filepath.Join(dir, "library", "skills", "linked", "notes.md")); err != nil {
		t.Errorf("its files come along: %v", err)
	}

	big := skillFolder(t, "big", map[string]string{"SKILL.md": "---\ndescription: d\n---\nx"})
	sparseFile(t, filepath.Join(big, "data.bin"), wiki.MaxSkillFile+1)
	heavy := skillFolder(t, "heavy", map[string]string{"SKILL.md": "---\ndescription: d\n---\nx"})
	for i := range int(wiki.MaxSkillBytes/wiki.MaxSkillFile) + 1 {
		sparseFile(t, filepath.Join(heavy, fmt.Sprintf("f%d.bin", i)), wiki.MaxSkillFile)
	}
	for _, tc := range []struct {
		name, folder, code string
		kind               error
	}{
		{"a relative folder", "skills/x", "folderNotAbsolute", store.ErrInvalidInput},
		{"no such folder", filepath.Join(t.TempDir(), "gone"), "folderMissing", store.ErrInvalidInput},
		{"no SKILL.md", skillFolder(t, "empty", map[string]string{"README.md": "x"}), "skillFileMissing", store.ErrInvalidInput},
		{"no frontmatter to read", skillFolder(t, "broken", map[string]string{"SKILL.md": "---\nname: [\n---\nx"}), "skillUnreadable", store.ErrInvalidInput},
		{"another type", skillFolder(t, "fact", map[string]string{"SKILL.md": "---\ntype: Fact\ndescription: d\n---\nx"}), "skillUnreadable", store.ErrInvalidInput},
		{"no name to use", skillFolder(t, "Not A Name", map[string]string{"SKILL.md": "---\ndescription: d\n---\nx"}), "skillBadName", store.ErrInvalidInput},
		{"no description", skillFolder(t, "quiet", map[string]string{"SKILL.md": "---\nname: quiet\n---\nx"}), "skillNoDescription", store.ErrInvalidInput},
		{"there already", src, "skillExists", store.ErrConflict},
		{"a file too big", big, "skillFileTooBig", store.ErrInvalidInput},
		{"too much in all", heavy, "skillTooLarge", store.ErrInvalidInput},
	} {
		_, err := l.h.ImportSkill(l.ctx, tc.folder, "", l.user.ID)
		var p *store.Problem
		if !errors.Is(err, tc.kind) || !errors.As(err, &p) || p.Code != tc.code {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	many := map[string]string{"SKILL.md": "---\ndescription: d\n---\nx"}
	for i := range wiki.MaxSkillFiles + 1 {
		many[filepath.Join("data", fmt.Sprintf("f%03d.txt", i))] = "x"
	}
	if _, err := l.h.ImportSkill(l.ctx, skillFolder(t, "many", many), "", l.user.ID); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("too many files: %v", err)
	}
	if catalog, _ := l.h.LibraryCatalog(l.ctx); len(catalog.Pages) != 4 {
		t.Errorf("nothing refused is left behind: %d pages", len(catalog.Pages))
	}
}

// A skill as its author keeps it comes in whole: a guide longer than a
// wiki page may be, a template skill inside it with its own frontmatter,
// files and folders reached by links, a folder linking back to its own.
func TestHub_ImportSkill_AsWritten(t *testing.T) {
	l, _ := wikiLoop(t)
	shared := skillFolder(t, "shared", map[string]string{"run.py": "print('shared')\n", "assets/logo.txt": "LOGO\n"})
	guide := "# Migration\n\n" + strings.Repeat("A line of the migration guide.\n", 317<<10/31)
	folder := skillFolder(t, "claude-api", map[string]string{
		"SKILL.md":                     "---\nname: claude-api\ndescription: Use when building on the Claude API.\n---\n\nRead `shared/model-migration.md`, then `python/README.md`.\n",
		"shared/model-migration.md":    guide,
		"python/README.md":             "# Python\n",
		"templates/SKILL.md":           "---\nname: my-template\ndescription: A template.\n---\n\n# Template\n",
		"templates/agent.md":           "---\nname: reviewer\ntools: Read, Grep\n---\n\nYou review code.\n",
		"scripts/.cache/state":         "hidden",
		"references/dangling-link.txt": "",
	})
	os.Remove(filepath.Join(folder, "references", "dangling-link.txt"))
	for link, to := range map[string]string{
		"scripts/shared.py":            filepath.Join(shared, "run.py"),
		"assets":                       filepath.Join(shared, "assets"),
		"templates/loop":               filepath.Join(folder, "templates"),
		"references/dangling-link.txt": filepath.Join(shared, "gone.txt"),
	} {
		if err := os.Symlink(to, filepath.Join(folder, filepath.FromSlash(link))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.h.ImportSkill(l.ctx, folder, "", l.user.ID); err != nil {
		t.Fatal(err)
	}
	r, _ := l.h.openLibrary(l.ctx)
	skill, err := r.bundle.ProjectSkill("claude-api")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"shared/model-migration.md": guide,
		"python/README.md":          "# Python\n",
		"templates/SKILL.md":        "---\nname: my-template\ndescription: A template.\n---\n\n# Template\n",
		"templates/agent.md":        "---\nname: reviewer\ntools: Read, Grep\n---\n\nYou review code.\n",
		"scripts/shared.py":         "print('shared')\n",
		"assets/logo.txt":           "LOGO\n",
	}
	names := append(slices.Collect(maps.Keys(want)), "SKILL.md")
	slices.Sort(names)
	if got := slices.Sorted(maps.Keys(skill.Files)); !slices.Equal(got, names) {
		t.Errorf("the files %v, want %v", got, names)
	}
	for rel, text := range want {
		if string(skill.Files[rel]) != text {
			t.Errorf("%s comes as it was: %q", rel, excerpt(string(skill.Files[rel]), 80))
		}
	}
}

// sparseFile makes a file of size bytes that takes no room on disk.
func sparseFile(t *testing.T, p string, size int64) {
	t.Helper()
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
}

// A skill as people share them, past the limits the library once had:
// more files than 64, one of more than 256 KB, and ones that are not text,
// which reach the runtime as they are.
func TestHub_ImportSkill_AsPeopleShareThem(t *testing.T) {
	l, _ := wikiLoop(t)
	files := map[string]string{"SKILL.md": "---\nname: fonts\ndescription: Use when a design needs our fonts.\n---\n\nThe fonts are in assets/.\n"}
	for i := range 90 {
		files[fmt.Sprintf("assets/licence-%02d.txt", i)] = "OFL"
	}
	folder := skillFolder(t, "fonts", files)
	font := bytes.Repeat([]byte{0x00, 0x01, 0xff, 0xfe}, 300<<10/4)
	if err := os.WriteFile(filepath.Join(folder, "assets", "Serif.ttf"), font, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.h.ImportSkill(l.ctx, folder, "", l.user.ID); err != nil {
		t.Fatal(err)
	}
	r, _ := l.h.openLibrary(l.ctx)
	skills := l.h.turns.skillsOf(r.bundle, "claude", func(name string) bool { return name == "fonts" })
	if len(skills) != 1 || len(skills[0].Files) != 91 || !bytes.Equal(skills[0].Blobs["assets/Serif.ttf"], font) || skills[0].Files["assets/Serif.ttf"] != "" {
		t.Fatalf("the turn gets its %d text files and the font as it is: %d blobs", len(skills[0].Files), len(skills[0].Blobs))
	}
	// Taken out as a zip, the font is as it was too.
	zipped, err := l.h.ExportSkill(l.ctx, "fonts")
	if err != nil {
		t.Fatal(err)
	}
	archive, _ := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if f, err := archive.Open("fonts/assets/Serif.ttf"); err != nil {
		t.Errorf("the font in the zip: %v", err)
	} else if got, _ := io.ReadAll(f); !bytes.Equal(got, font) {
		t.Error("the font comes out of the zip as it went in")
	}
	// The font is part of what names the set: another font, another set.
	other := slices.Clone(font)
	other[0] = 0x7f
	changed := []runtime.Skill{{Name: "fonts", Files: skills[0].Files, Blobs: map[string][]byte{"assets/Serif.ttf": other}}}
	if skillHash(skills) == skillHash(changed) {
		t.Error("the set's hash takes in its blobs")
	}
}

func TestHub_InstallSkill(t *testing.T) {
	l, _ := wikiLoop(t)
	coder := l.member("Coder", nil)
	l.addSkill("go-table-tests", "Use when writing Go tests with several cases.", "Write the cases as a table.", "")
	installed, err := l.h.InstallSkill(l.ctx, "go-table-tests", coder.AgentID, true)
	if err != nil || len(installed) != 1 || installed[0].ID != coder.AgentID {
		t.Fatalf("installed %+v %v", installed, err)
	}
	if again, _ := l.h.InstallSkill(l.ctx, "go-table-tests", coder.AgentID, true); len(again) != 1 {
		t.Errorf("installed once: %+v", again)
	}
	for _, name := range []string{"nope", "../x", "/skills/go-table-tests/SKILL.md"} {
		if _, err := l.h.InstallSkill(l.ctx, name, coder.AgentID, true); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := l.h.InstallSkill(l.ctx, "go-table-tests", "00000000-0000-0000-0000-000000000000", true); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("no such agent: %v", err)
	}
	// Retired, it can be taken off but not installed again.
	r, _ := l.h.openLibrary(l.ctx)
	w, _ := r.bundle.Writer(okf.Human("alice"))
	if _, err := w.Deprecate("/skills/go-table-tests/SKILL.md", "", "stale"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(l.ctx, "Retire"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.h.InstallSkill(l.ctx, "go-table-tests", coder.AgentID, false); err != nil {
		t.Errorf("taken off: %v", err)
	}
	if _, err := l.h.InstallSkill(l.ctx, "go-table-tests", coder.AgentID, true); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a retired skill: %v", err)
	}
}
