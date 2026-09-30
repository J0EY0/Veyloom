package hub

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
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
	// One holds a file too big for a turn to carry.
	writeTree(t, home, map[string]string{".agents/skills/heavy/SKILL.md": "---\nname: heavy\ndescription: Use for heavy work.\n---\nx\n"})
	sparseFile(t, filepath.Join(home, ".agents", "skills", "heavy", "data.bin"), wiki.MaxSkillFile+1)
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
		"heavy|~/.agents/skills|Use for heavy work.|false|skillFileTooBig",
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
	if p := skills[1].ProblemParams; p["path"] != "data.bin" || p["max"] != wiki.MB(wiki.MaxSkillFile) {
		t.Errorf("why, with the import's params: %v", p)
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

// A skill in the library remembers the folder it came from: the list of
// this machine's skills says when that folder changed, and an update takes
// it in again, replacing the library's copy, files it lost going too, while
// what people set on the copy in the library stays. A skill on trial is
// left alone; one the library does not have is imported, not updated.
func TestHub_UpdateSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	l, dir := wikiLoop(t)
	writeTree(t, home, map[string]string{
		".claude/skills/notes/SKILL.md":            "---\nname: notes\ndescription: Use when taking notes.\n---\n\nKeep them short.\n",
		".claude/skills/notes/references/guide.md": "# Guide\n",
		".claude/skills/notes/scripts/old.sh":      "#!/bin/sh\necho old\n",
	})
	folder := filepath.Join(home, ".claude", "skills", "notes")
	if _, err := l.h.ImportSkill(l.ctx, folder, l.project().ID, l.user.ID); err != nil {
		t.Fatal(err)
	}
	r, _ := l.h.openLibrary(l.ctx)
	history, _ := r.bundle.SkillHistory(l.ctx, "notes", 5)
	if len(history) == 0 || history[0].Trailers[trailerSource] != folder || history[0].Trailers[trailerSourceHash] == "" {
		t.Fatalf("the import names the folder and what it held: %+v", history)
	}
	origin := func() LocalSkill {
		t.Helper()
		skills, err := l.h.LocalSkills(l.ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range skills {
			if s.Name == "notes" {
				return s
			}
		}
		t.Fatal("notes is not listed")
		return LocalSkill{}
	}
	if s := origin(); !s.InLibrary || s.Origin != "same" || s.EditsSince != 0 {
		t.Errorf("just imported: %+v", s)
	}

	// The person changes the folder; the library's copy is changed too,
	// kept for Claude Code only.
	writeTree(t, home, map[string]string{
		".claude/skills/notes/SKILL.md":          "---\nname: notes\ndescription: Use when taking notes.\n---\n\nKeep them short, with dates.\n",
		".claude/skills/notes/references/new.md": "# New\n",
	})
	os.Remove(filepath.Join(folder, "scripts", "old.sh"))
	page, _ := r.bundle.Page(wiki.SkillPath("notes"))
	page.Doc.SetTags([]string{"runtime-claude"})
	w, _ := r.bundle.Writer("human:alice")
	if _, err := w.Put(page.Path, page.Doc, page.Hash); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(l.ctx, "Keep notes for Claude Code"); err != nil {
		t.Fatal(err)
	}
	// A person's confirmation keeps a record of it: no change to replace.
	if _, err := l.h.VerifyLibraryPage(l.ctx, wiki.SkillPath("notes"), l.user.ID); err != nil {
		t.Fatal(err)
	}
	if s := origin(); s.Origin != "changed" || s.EditsSince != 1 {
		t.Errorf("changed on both sides: %+v", s)
	}

	// On trial, it is left alone.
	trial, err := l.s.StartSkillTrial(l.ctx, store.NewSkillTrial{Skill: "notes", BaseSHA: history[0].SHA, ChangedBy: "Coder", ProjectName: "p"})
	if err != nil {
		t.Fatal(err)
	}
	var p *store.Problem
	if _, err := l.h.UpdateSkill(l.ctx, folder, l.user.ID); !errors.As(err, &p) || p.Code != "skillOnTrial" {
		t.Errorf("on trial: %v", err)
	}
	if _, err := l.s.EndSkillTrial(l.ctx, trial.ID, store.TrialKept, "human:alice", ""); err != nil {
		t.Fatal(err)
	}

	got, err := l.h.UpdateSkill(l.ctx, folder, l.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, "with dates") || got.Team != l.project().WikiSlug || !slices.Equal(got.Tags, []string{"runtime-claude"}) {
		t.Errorf("updated, keeping its team and runtimes: %+v", got.WikiPageInfo)
	}
	lib := filepath.Join(dir, "library", "skills", "notes")
	if _, err := os.Stat(filepath.Join(lib, "scripts", "old.sh")); !os.IsNotExist(err) {
		t.Errorf("the file the folder lost is gone: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(lib, "references", "new.md")); err != nil || string(data) != "---\ntype: Reference\n---\n# New\n" {
		t.Errorf("the new file came in: %q %v", data, err)
	}
	history, _ = r.bundle.SkillHistory(l.ctx, "notes", 5)
	if history[0].Subject != "Updated the skill notes from "+folder || history[0].Author != "human:alice" || history[0].Trailers[trailerSource] != folder {
		t.Errorf("one commit by the person: %+v", history[0])
	}
	if s := origin(); s.Origin != "same" || s.EditsSince != 0 {
		t.Errorf("up to date again: %+v", s)
	}
	if out, _ := exec.Command("git", "-C", filepath.Join(dir, "library"), "status", "--porcelain").Output(); len(out) > 0 {
		t.Errorf("all of it committed:\n%s", out)
	}

	// One the library does not have.
	writeTree(t, home, map[string]string{".claude/skills/fresh/SKILL.md": "---\nname: fresh\ndescription: Use when fresh.\n---\nx\n"})
	if _, err := l.h.UpdateSkill(l.ctx, filepath.Join(home, ".claude", "skills", "fresh"), l.user.ID); !errors.As(err, &p) || p.Code != "skillNotInLibrary" {
		t.Errorf("not in the library: %v", err)
	}

	// Imported before the library named the folder apart, it can be
	// updated but not told changed.
	writeTree(t, home, map[string]string{".agents/skills/old/SKILL.md": "---\nname: old\ndescription: Use when old.\n---\nx\n"})
	legacy := filepath.Join(home, ".agents", "skills", "old")
	d := okf.New("Skill")
	d.SetString(okf.KeyName, "old")
	d.SetString(okf.KeyDescription, "Use when old.")
	d.SetBody("x")
	if _, err := w.Create(wiki.SkillPath("old"), d); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(l.ctx, "Imported the skill old from "+legacy); err != nil {
		t.Fatal(err)
	}
	skills, _ := l.h.LocalSkills(l.ctx)
	for _, s := range skills {
		if s.Name == "old" && s.Origin != "unknown" {
			t.Errorf("imported before: %+v", s)
		}
	}
}

// A skill imported before the library kept a skill's files as written, its
// references renamed and given a title: the list says it cannot tell
// whether its folder changed, and an update from the folder brings the
// files back under their own names, in the folder and in its history,
// though they differ from the old ones only in case.
func TestHub_UpdateSkill_ImportedBefore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	l, dir := wikiLoop(t)
	writeTree(t, home, map[string]string{
		".claude/skills/pdf/SKILL.md":                "---\nname: pdf\ndescription: Use for PDFs.\n---\n\nRead FORMS.md, then references/API_Guide.md.\n",
		".claude/skills/pdf/FORMS.md":                "# Forms\n\nFill them.\n",
		".claude/skills/pdf/references/API_Guide.md": "---\nname: api\n---\n\n# API\n",
	})
	folder := filepath.Join(home, ".claude", "skills", "pdf")
	r, _ := l.h.openLibrary(l.ctx)
	w, _ := r.bundle.Writer("human:alice")
	d := okf.New("Skill")
	d.SetString(okf.KeyName, "pdf")
	d.SetString(okf.KeyDescription, "Use for PDFs.")
	d.SetBody("Read FORMS.md, then references/API_Guide.md.")
	if _, err := w.Create(wiki.SkillPath("pdf"), d); err != nil {
		t.Fatal(err)
	}
	for p, title := range map[string]string{"/skills/pdf/forms.md": "Forms", "/skills/pdf/references/api-guide.md": "API"} {
		ref := okf.New("Reference")
		ref.SetString(okf.KeyTitle, title)
		ref.SetBody("# " + title)
		if _, err := w.Create(p, ref); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Commit(l.ctx, "Imported the skill pdf from "+folder); err != nil {
		t.Fatal(err)
	}
	skills, err := l.h.LocalSkills(l.ctx)
	if err != nil || len(skills) != 1 || skills[0].Origin != "unknown" {
		t.Fatalf("imported before: %+v %v", skills, err)
	}

	if _, err := l.h.UpdateSkill(l.ctx, folder, l.user.ID); err != nil {
		t.Fatal(err)
	}
	lib := filepath.Join(dir, "library")
	files, _ := exec.Command("git", "-C", lib, "ls-files", "skills/pdf").Output()
	if got := strings.Fields(string(files)); !slices.Equal(got, []string{"skills/pdf/FORMS.md", "skills/pdf/SKILL.md", "skills/pdf/references/API_Guide.md"}) {
		t.Errorf("the history names them as they are: %v", got)
	}
	if out, _ := exec.Command("git", "-C", lib, "status", "--porcelain").Output(); len(out) > 0 {
		t.Errorf("all of it committed:\n%s", out)
	}
	skill, err := r.bundle.ProjectSkill("pdf")
	if err != nil || string(skill.Files["FORMS.md"]) != "# Forms\n\nFill them.\n" || string(skill.Files["references/API_Guide.md"]) != "---\nname: api\n---\n\n# API\n" || len(skill.Files) != 3 {
		t.Errorf("the runtimes get the files as written: %v %v", skill.Files, err)
	}
	if skills, _ := l.h.LocalSkills(l.ctx); skills[0].Origin != "same" {
		t.Errorf("up to date now: %+v", skills[0])
	}
}
