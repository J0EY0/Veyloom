package hub

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// writeTree writes files under dir by their paths.
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The skills on this machine, for the library to take in with a tick: a
// person's own, under a home directory of the test's, and a project's, in
// its checkout. The same skill reached twice is listed once; what the
// import would refuse says why.
func TestHub_LocalSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	l, _ := wikiLoop(t)
	writeTree(t, home, map[string]string{
		".agents/skills/pdf/SKILL.md":           "---\nname: pdf\ndescription: Use when a task reads or fills in PDFs.\n---\n\nUse pdfplumber.\n",
		".agents/skills/.DS_Store":              "junk",
		".codex/skills/Bad_Name/SKILL.md":       "---\ndescription: Use it.\n---\nx\n",
		".pi/agent/skills/quiet/SKILL.md":       "---\nname: quiet\n---\nx\n",
		".pi/agent/skills/notes-only/README.md": "no skill here",
	})
	// Claude Code's folder links to the skill kept for every runtime.
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, ".agents", "skills", "pdf"), filepath.Join(home, ".claude", "skills", "pdf")); err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	writeTree(t, repo, map[string]string{".agents/skills/deploy/SKILL.md": "---\nname: deploy\ndescription: Use when shipping Acme.\n---\nx\n"})
	if _, _, err := l.s.CreateProject(l.ctx, store.NewProject{Name: "Acme", RepoPath: repo}); err != nil {
		t.Fatal(err)
	}
	// One is in the library already.
	if _, err := l.h.ImportSkill(l.ctx, filepath.Join(repo, ".agents", "skills", "deploy"), "", l.user.ID); err != nil {
		t.Fatal(err)
	}

	skills, err := l.h.LocalSkills(l.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range skills {
		got = append(got, fmt.Sprintf("%s|%s|%s|%v|%s", s.Name, s.Where, s.Description, s.InLibrary, s.Problem))
	}
	want := []string{
		"pdf|~/.claude/skills|Use when a task reads or fills in PDFs.|false|",
		"Bad_Name|~/.codex/skills|Use it.|false|skillBadName",
		"quiet|~/.pi/agent/skills||false|skillNoDescription",
		"deploy|Acme · .agents/skills|Use when shipping Acme.|true|",
	}
	if !slices.Equal(got, want) {
		t.Errorf("local skills:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if skills[0].Folder != filepath.Join(home, ".claude", "skills", "pdf") {
		t.Errorf("found where it was looked for: %s", skills[0].Folder)
	}
}

// zipOf is a zip file of files by their paths.
func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for k := range files {
			if !yield(k) {
				return
			}
		}
	}) {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte(files[name]))
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Skills from the browser: a zip of one at its root, of one in its folder,
// of a skills repository holding two; the files of a folder by their
// paths. What leaves the upload's folder refuses it all.
func TestHub_UploadSkills(t *testing.T) {
	l, _ := wikiLoop(t)
	upload := func(name string, data []byte) ([]UploadedSkill, error) {
		return l.h.UploadSkills(l.ctx, SkillUpload{Name: name, Zip: bytes.NewReader(data), ZipSize: int64(len(data)), UserID: l.user.ID})
	}
	names := func(got []UploadedSkill) string {
		var out []string
		for _, s := range got {
			out = append(out, s.Name+" "+s.Path+" "+s.Code)
		}
		return strings.Join(out, "; ")
	}

	// A skill at the zip's root, named after the zip.
	got, err := upload("notes.zip", zipOf(t, map[string]string{"SKILL.md": "---\ndescription: Use when taking notes.\n---\n\nKeep them short.\n"}))
	if err != nil || names(got) != "notes /skills/notes/SKILL.md " {
		t.Fatalf("at the root: %s %v", names(got), err)
	}
	if history, _ := l.h.LibraryHistory(l.ctx, "", 1); len(history) == 0 || history[0].Subject != "Imported the skill notes from the upload notes.zip" {
		t.Errorf("history %+v", history)
	}
	// In its folder, as the library exports one, with what a Mac adds.
	got, err = upload("relay-word.zip", zipOf(t, map[string]string{
		"relay-word/SKILL.md":            "---\nname: relay-word\ndescription: Use when asked for the relay word.\n---\n\nSee [the guide](references/guide.md).\n",
		"relay-word/references/guide.md": "# Guide\n\nFERN-77\n",
		"relay-word/assets/blob.bin":     "\x00\xff\xfe",
		"__MACOSX/relay-word/._SKILL.md": "junk",
	}))
	if err != nil || names(got) != "relay-word /skills/relay-word/SKILL.md " {
		t.Fatalf("in its folder: %s %v", names(got), err)
	}
	// A skills repository as GitHub zips it: each skill, the README not.
	got, err = upload("skills-main.zip", zipOf(t, map[string]string{
		"skills-main/README.md":             "# Skills",
		"skills-main/skills/alpha/SKILL.md": "---\nname: alpha\ndescription: Use for alpha.\n---\nx\n",
		"skills-main/skills/beta/SKILL.md":  "---\nname: beta\ndescription: Use for beta.\n---\nx\n",
		"skills-main/skills/notes/SKILL.md": "---\nname: notes\ndescription: Use when taking notes.\n---\nx\n",
	}))
	if err != nil || names(got) != "alpha /skills/alpha/SKILL.md ; beta /skills/beta/SKILL.md ; notes  skillExists" {
		t.Fatalf("a repository: %s %v", names(got), err)
	}
	// The files of a folder, by their paths.
	folder := map[string]string{"tools/SKILL.md": "---\ndescription: Use for tools.\n---\nx\n", "tools/scripts/run.sh": "#!/bin/sh\necho run\n"}
	var files []UploadFile
	for _, p := range []string{"tools/SKILL.md", "tools/scripts/run.sh"} {
		files = append(files, UploadFile{Path: p, Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader(folder[p])), nil }})
	}
	got, err = l.h.UploadSkills(l.ctx, SkillUpload{Name: "tools", Files: files, UserID: l.user.ID})
	if err != nil || names(got) != "tools /skills/tools/SKILL.md " {
		t.Fatalf("a folder: %s %v", names(got), err)
	}

	for name, tc := range map[string]struct {
		up   SkillUpload
		code string
	}{
		"a path out":   {SkillUpload{Name: "x.zip", Zip: bytes.NewReader(zipOf(t, map[string]string{"../evil/SKILL.md": "x"}))}, "uploadBadPath"},
		"no skill":     {SkillUpload{Name: "x.zip", Zip: bytes.NewReader(zipOf(t, map[string]string{"README.md": "x"}))}, "uploadNoSkill"},
		"no zip":       {SkillUpload{Name: "x.zip", Zip: strings.NewReader("not a zip")}, "uploadNotZip"},
		"a folder out": {SkillUpload{Name: "x", Files: []UploadFile{{Path: "/etc/x/SKILL.md", Open: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("x")), nil }}}}, "uploadBadPath"},
	} {
		if tc.up.Zip != nil {
			switch r := tc.up.Zip.(type) {
			case *bytes.Reader:
				tc.up.ZipSize = r.Size()
			case *strings.Reader:
				tc.up.ZipSize = r.Size()
			}
		}
		tc.up.UserID = l.user.ID
		_, err := l.h.UploadSkills(l.ctx, tc.up)
		var p *store.Problem
		if !errors.As(err, &p) || p.Code != tc.code {
			t.Errorf("%s: %v, want %s", name, err, tc.code)
		}
	}
	// Too many files for one upload.
	many := map[string]string{"big/SKILL.md": "---\ndescription: d\n---\nx"}
	for i := range MaxUploadFiles {
		many[fmt.Sprintf("big/data/f%04d.txt", i)] = "x"
	}
	if _, err := upload("big.zip", zipOf(t, many)); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("too many files: %v", err)
	}
}
