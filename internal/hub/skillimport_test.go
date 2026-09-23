package hub

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
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
	// SKILL.md keeps what Agent Skills knows, and a value of its own in
	// metadata; it links to the files under their new names, as before.
	main := read("SKILL.md")
	for _, want := range []string{"type: Skill", "license: MIT", "allowed-tools: Bash(git log:*)", "version: \"2\"", "veyloom-team: " + home.WikiSlug,
		"](references/style-guide.md)", "](references/index-page.md)"} {
		if !strings.Contains(main, want) {
			t.Errorf("SKILL.md lacks %q:\n%s", want, main)
		}
	}
	if strings.Contains(main, "hooks") {
		t.Errorf("a list of no field is dropped:\n%s", main)
	}
	// References are pages of the library, linked from its root.
	guide := read("references/style-guide.md")
	if !strings.Contains(guide, "type: Reference") || !strings.Contains(guide, "title: House style") || !strings.Contains(guide, "](/skills/release-notes/SKILL.md)") {
		t.Errorf("the guide:\n%s", guide)
	}
	list := read("references/index-page.md")
	if !strings.Contains(list, "title: All of them") || strings.Contains(list, "owner") || !strings.Contains(list, "](/skills/release-notes/references/style-guide.md)") {
		t.Errorf("the list:\n%s", list)
	}
	for _, rel := range []string{"SKILL.md", "references/style-guide.md", "references/index-page.md"} {
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
	if len(history) == 0 || history[0].Subject != "Imported the skill release-notes from "+src || history[0].Author != "human:alice" || len(history[0].Changes) != 3 {
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
	if err != nil || !slices.Equal(files, []string{"SKILL.md", "references/index-page.md", "references/style-guide.md", "scripts/draft.sh"}) {
		t.Errorf("projected %v %v", files, err)
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
		{"a file too big", skillFolder(t, "big", map[string]string{"SKILL.md": "---\ndescription: d\n---\nx", "data.bin": strings.Repeat("x", importFileSize+1)}), "skillFileTooBig", store.ErrInvalidInput},
	} {
		_, err := l.h.ImportSkill(l.ctx, tc.folder, "", l.user.ID)
		var p *store.Problem
		if !errors.Is(err, tc.kind) || !errors.As(err, &p) || p.Code != tc.code {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	many := map[string]string{"SKILL.md": "---\ndescription: d\n---\nx"}
	for i := range importFiles + 1 {
		many[filepath.Join("data", strings.Repeat("a", i+1)+".txt")] = "x"
	}
	if _, err := l.h.ImportSkill(l.ctx, skillFolder(t, "many", many), "", l.user.ID); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("too many files: %v", err)
	}
	if catalog, _ := l.h.LibraryCatalog(l.ctx); len(catalog.Pages) != 5 {
		t.Errorf("nothing refused is left behind: %d pages", len(catalog.Pages))
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
