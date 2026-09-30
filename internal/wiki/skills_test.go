package wiki

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// skill builds a skill page the way the hub writes one.
func skill(name, team, description, body string, tags ...string) *okf.Document {
	d := okf.New("Skill")
	d.SetString(okf.KeyName, name)
	d.SetString(okf.KeyTitle, strings.ReplaceAll(name, "-", " "))
	d.SetString(okf.KeyDescription, description)
	if team != "" {
		d.SetMetadata(TeamKey, team)
	}
	d.SetTags(tags)
	d.SetBody(body)
	return d
}

func openLibrary(t *testing.T) *Bundle {
	t.Helper()
	return openTest(t, Options{Git: true, Layout: LibraryLayout})
}

func TestSkills_ForEachRuntime(t *testing.T) {
	ctx := context.Background()
	b := openLibrary(t)
	w := writer(t, b, "codex/default")
	for _, s := range []*okf.Document{
		skill("go-table-tests", "veyloom", "Write Go tests as tables.", "Use a table."),
		skill("claude-only", "veyloom", "Only for Claude.", "x", "runtime-claude"),
		skill("unowned", "", "Nobody's.", "y"),
	} {
		if _, err := w.Create(SkillPath(s.Name()), s); err != nil {
			t.Fatalf("%s: %v", s.Name(), err)
		}
	}
	w.Commit(ctx, "Skills")
	names := func(runtime string) []string {
		var out []string
		for _, s := range b.Skills(runtime) {
			out = append(out, SkillName(s.Path))
		}
		return out
	}
	if got := names("claude"); !slices.Equal(got, []string{"claude-only", "go-table-tests", "unowned"}) {
		t.Errorf("claude gets %v", got)
	}
	if got := names("codex"); !slices.Equal(got, []string{"go-table-tests", "unowned"}) {
		t.Errorf("a skill kept for claude is not codex's: %v", got)
	}
	page, _ := b.Page(SkillPath("go-table-tests"))
	if page.Team != "veyloom" || page.Title != "go table tests" {
		t.Errorf("summary %+v", page.Summary)
	}
	// The index lists current skills by the team that owns them.
	index := readFile(t, b, "/skills/index.md")
	if i, j := strings.Index(index, "# Owned by veyloom"), strings.Index(index, "# Owned by no team"); i < 0 || j < i {
		t.Errorf("skills index:\n%s", index)
	}

	// Deprecated and draft skills are not given out.
	w.Deprecate(SkillPath("unowned"), "", "stale")
	w.Commit(ctx, "Retire")
	if got := names("pi"); !slices.Equal(got, []string{"go-table-tests"}) {
		t.Errorf("pi gets %v", got)
	}
	if SkillName("/skills/a/b/SKILL.md") != "" || SkillName("/patterns/a.md") != "" || SkillName("/skills/go/SKILL.md") != "go" {
		t.Error("SkillName")
	}
}

func TestProjectSkill(t *testing.T) {
	b := openLibrary(t)
	w := writer(t, b, "codex/default")
	s := skill("go-table-tests", "veyloom", "Write Go tests as tables.", "Use a table.\n\nSee [the cases](references/cases.md).")
	s.AddSource(okf.Source{ID: "p1", Resource: "/patterns/flaky-tests.md", Title: "Flaky tests"})
	if _, err := w.Create(SkillPath("go-table-tests"), s); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(b.Dir(), "skills", "go-table-tests")
	os.MkdirAll(filepath.Join(dir, "scripts"), 0o755)
	os.WriteFile(filepath.Join(dir, "scripts", "run.sh"), []byte("#!/bin/sh\ngo test ./...\n"), 0o755)
	os.WriteFile(filepath.Join(dir, ".secret"), []byte("hidden"), 0o644)
	sparse(t, filepath.Join(dir, "huge.bin"), MaxSkillFile+1)

	got, err := b.ProjectSkill("go-table-tests")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for f := range got.Files {
		files = append(files, f)
	}
	slices.Sort(files)
	if !slices.Equal(files, []string{"SKILL.md", "scripts/run.sh"}) {
		t.Errorf("files %v", files)
	}
	main := string(got.Files["SKILL.md"])
	// Only what Agent Skills knows: the runtime's file is to its spec.
	for _, want := range []string{"name: go-table-tests", "description: Write Go tests as tables.", "veyloom-team: veyloom", "Use a table."} {
		if !strings.Contains(main, want) {
			t.Errorf("projected SKILL.md lacks %q:\n%s", want, main)
		}
	}
	for _, gone := range []string{"type:", "title:", "generated:", "sources:"} {
		if strings.Contains(main, gone) {
			t.Errorf("projected SKILL.md keeps %q:\n%s", gone, main)
		}
	}
	if !strings.Contains(readFile(t, b, SkillPath("go-table-tests")), "type: Skill") {
		t.Error("the page in the library keeps OKF's keys")
	}
}

// What a runtime reads, and a person takes elsewhere, is the skill as it
// is anywhere else: its references plain markdown that link from where
// they are, not pages of the library linking from its root.
func TestProjectSkill_ReferencesAsTheyWere(t *testing.T) {
	b := openLibrary(t)
	w := writer(t, b, okf.Human("alice"))
	s := skill("relay-word", "", "Use when asked for the relay word.", "The word is kept in [the guide](/skills/relay-word/references/guide.md), see [a pattern](/patterns/relays.md).")
	if _, err := w.Create(SkillPath("relay-word"), s); err != nil {
		t.Fatal(err)
	}
	guide := okf.New("Reference")
	guide.SetString(okf.KeyTitle, "Guide")
	guide.SetBody("# Guide\n\nRead [the list](/skills/relay-word/references/list.md#words), then [the skill](/skills/relay-word/SKILL.md) again, or [elsewhere](/patterns/relays.md).")
	if _, err := w.Create("/skills/relay-word/references/guide.md", guide); err != nil {
		t.Fatal(err)
	}
	list := okf.New("Reference")
	list.SetString(okf.KeyTitle, "List")
	list.SetBody("The relay word is FERN-77.")
	if _, err := w.Create("/skills/relay-word/references/list.md", list); err != nil {
		t.Fatal(err)
	}
	got, err := b.ProjectSkill("relay-word")
	if err != nil {
		t.Fatal(err)
	}
	main := string(got.Files["SKILL.md"])
	if !strings.Contains(main, "](references/guide.md)") || !strings.Contains(main, "](/patterns/relays.md)") {
		t.Errorf("SKILL.md links into its folder from there:\n%s", main)
	}
	want := "# Guide\n\nRead [the list](list.md#words), then [the skill](../SKILL.md) again, or [elsewhere](/patterns/relays.md).\n"
	if got := string(got.Files["references/guide.md"]); got != want {
		t.Errorf("the guide:\n%q\nwant\n%q", got, want)
	}
	if got := string(got.Files["references/list.md"]); got != "The relay word is FERN-77.\n" {
		t.Errorf("the list: %q", got)
	}
	if !strings.Contains(readFile(t, b, "/skills/relay-word/references/guide.md"), "](/skills/relay-word/references/list.md#words)") {
		t.Error("the library's page links from its root")
	}
}

// sparse makes a file of size bytes that takes no room on disk.
func sparse(t *testing.T, p string, size int64) {
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

// A skill too much for a turn to carry stays behind, whole: too many
// files, or too much in all.
func TestProjectSkill_Limits(t *testing.T) {
	b := openLibrary(t)
	w := writer(t, b, okf.Human("alice"))
	for _, name := range []string{"many", "heavy"} {
		if _, err := w.Create(SkillPath(name), skill(name, "", "Use when testing limits.", "x")); err != nil {
			t.Fatal(err)
		}
	}
	many := filepath.Join(b.Dir(), "skills", "many", "data")
	os.MkdirAll(many, 0o755)
	for i := range MaxSkillFiles {
		os.WriteFile(filepath.Join(many, fmt.Sprintf("f%03d.txt", i)), []byte("x"), 0o644)
	}
	if _, err := b.ProjectSkill("many"); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("too many files: %v", err)
	}
	heavy := filepath.Join(b.Dir(), "skills", "heavy", "assets")
	os.MkdirAll(heavy, 0o755)
	for i := range int(MaxSkillBytes/MaxSkillFile) + 1 {
		sparse(t, filepath.Join(heavy, fmt.Sprintf("f%d.bin", i)), MaxSkillFile)
	}
	if _, err := b.ProjectSkill("heavy"); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("too much in all: %v", err)
	}
}

func TestRelativePath(t *testing.T) {
	for _, tc := range []struct{ dir, to, want string }{
		{"/skills/x", "/skills/x/references/a.md", "references/a.md"},
		{"/skills/x/references", "/skills/x/SKILL.md", "../SKILL.md"},
		{"/skills/x/references", "/skills/x/references/b.md", "b.md"},
		{"/skills/x/references/deep", "/skills/x/scripts/run.sh", "../../scripts/run.sh"},
	} {
		if got := relativePath(tc.dir, tc.to); got != tc.want {
			t.Errorf("relativePath(%q, %q) = %q, want %q", tc.dir, tc.to, got, tc.want)
		}
	}
}

func TestWriter_SetMetadata(t *testing.T) {
	ctx := context.Background()
	b := openLibrary(t)
	agent := writer(t, b, "codex/default")
	page, _ := agent.Create(SkillPath("go-table-tests"), skill("go-table-tests", "veyloom", "d", "b"))
	agent.Commit(ctx, "Skill")

	person := writer(t, b, okf.Human("owner"))
	moved, err := person.SetMetadata(page.Path, TeamKey, "docs-site")
	if err != nil || moved.Team != "docs-site" || moved.Generated.By != "codex/default" {
		t.Fatalf("moved %+v %v", moved.Summary, err)
	}
	if again, _ := person.SetMetadata(page.Path, TeamKey, "docs-site"); again.Team != "docs-site" || len(person.Pending()) != 1 {
		t.Errorf("setting the same value again writes nothing more: %v", person.Pending())
	}
	orphaned, err := person.SetMetadata(page.Path, TeamKey, "")
	if err != nil || orphaned.Team != "" || strings.Contains(readFile(t, b, page.Path), "metadata:") {
		t.Errorf("orphaned %+v %v:\n%s", orphaned.Summary, err, readFile(t, b, page.Path))
	}
	if probs := okf.CheckFile(page.Path, []byte(readFile(t, b, page.Path)), false, okf.Strict); len(probs) > 0 {
		t.Errorf("still a strict page: %v", probs)
	}
}

func TestWriter_PutFile(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true, Layout: LibraryLayout})
	w := writer(t, b, okf.Human("owner"))
	d := okf.New("Skill")
	d.SetString(okf.KeyName, "release-notes")
	d.SetString(okf.KeyTitle, "Release notes")
	d.SetString(okf.KeyDescription, "Use when writing release notes.")
	d.SetBody("Run scripts/draft.sh first.")
	if _, err := w.Create(SkillPath("release-notes"), d); err != nil {
		t.Fatal(err)
	}
	if err := w.PutFile("/skills/release-notes/scripts/draft.sh", []byte("#!/bin/sh\necho draft\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(ctx, "Add release-notes"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, b, "/skills/release-notes/scripts/draft.sh"); got != "#!/bin/sh\necho draft\n" {
		t.Errorf("the script %q", got)
	}
	if status, _ := b.git.status(ctx); len(status) != 0 {
		t.Errorf("the script is committed with the skill: %v", status)
	}
	skill, err := b.ProjectSkill("release-notes")
	if err != nil || string(skill.Files["scripts/draft.sh"]) != "#!/bin/sh\necho draft\n" {
		t.Errorf("projected with the skill: %v %v", skill.Files, err)
	}
	for _, bad := range []string{"/patterns/x.sh", "/skills/release-notes/notes.md", "/skills/release-notes/.env", "/skills/Bad Name/x.sh", "/skills/release-notes"} {
		if err := w.PutFile(bad, []byte("x")); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestRestoreSkill(t *testing.T) {
	ctx := context.Background()
	b := openLibrary(t)
	person := writer(t, b, okf.Human("alice"))
	if _, err := person.Create(SkillPath("release-notes"), skill("release-notes", "", "Use when writing release notes.", "Group by kind.")); err != nil {
		t.Fatal(err)
	}
	if err := person.PutFile("/skills/release-notes/scripts/draft.sh", []byte("#!/bin/sh\necho v1\n")); err != nil {
		t.Fatal(err)
	}
	base, err := person.Commit(ctx, "Add release-notes")
	if err != nil {
		t.Fatal(err)
	}
	if head, _ := b.Head(ctx); head != base {
		t.Errorf("head %s, want %s", head, base)
	}

	// An agent changes it, adds a reference and a script, and changes the
	// one it had.
	agent := writer(t, b, "codex/default")
	if _, err := agent.Edit(SkillPath("release-notes"), []Edit{{Op: "append", Content: "Lead with what users see."}}, ""); err != nil {
		t.Fatal(err)
	}
	ref := okf.New("Reference")
	ref.SetString(okf.KeyTitle, "Examples")
	ref.SetBody("One per line.")
	if _, err := agent.Create("/skills/release-notes/references/examples.md", ref); err != nil {
		t.Fatal(err)
	}
	agent.PutFile("/skills/release-notes/scripts/draft.sh", []byte("#!/bin/sh\necho v2\n"))
	agent.PutFile("/skills/release-notes/scripts/extra.sh", []byte("#!/bin/sh\n"))
	if _, err := agent.Commit(ctx, "Improve release-notes"); err != nil {
		t.Fatal(err)
	}

	// While a turn is changing it, it is not rolled back.
	busy := writer(t, b, "claude/haiku")
	if _, err := busy.Edit(SkillPath("release-notes"), []Edit{{Op: "append", Content: "More."}}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := b.RestoreSkill(ctx, "release-notes", base, okf.Human("alice"), "worse"); !errors.Is(err, store.ErrConflict) {
		t.Errorf("rolled back under a turn: %v", err)
	}
	if _, err := busy.Commit(ctx, "More"); err != nil {
		t.Fatal(err)
	}

	sha, err := b.RestoreSkill(ctx, "release-notes", base, okf.Human("alice"), "it made the notes  longer")
	if err != nil || sha == "" {
		t.Fatalf("rolled back %q %v", sha, err)
	}
	if got := readFile(t, b, SkillPath("release-notes")); strings.Contains(got, "Lead with") || strings.Contains(got, "More.") || !strings.Contains(got, "Group by kind.") {
		t.Errorf("SKILL.md as it was:\n%s", got)
	}
	if got := readFile(t, b, "/skills/release-notes/scripts/draft.sh"); got != "#!/bin/sh\necho v1\n" {
		t.Errorf("the script as it was: %q", got)
	}
	for _, gone := range []string{"references/examples.md", "scripts/extra.sh"} {
		if _, err := os.Stat(filepath.Join(b.Dir(), "skills", "release-notes", filepath.FromSlash(gone))); !os.IsNotExist(err) {
			t.Errorf("%s the skill gained since goes: %v", gone, err)
		}
	}
	if _, err := b.Page("/skills/release-notes/references/examples.md"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the reference is gone from the catalog: %v", err)
	}
	if status, _ := b.git.status(ctx); len(status) != 0 {
		t.Errorf("all of it committed: %v", status)
	}
	if log := readFile(t, b, "/log.md"); !strings.Contains(log, "rolled back to "+short(base)+" by human:alice: it made the notes longer") {
		t.Errorf("the log says so:\n%s", log)
	}
	history, _ := b.History(ctx, "", 1)
	if len(history) != 1 || history[0].Subject != "Roll back release-notes to "+short(base) || history[0].Author != "human:alice" {
		t.Errorf("history %+v", history)
	}

	if _, err := b.RestoreSkill(ctx, "release-notes", "0000000", okf.Human("alice"), ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("no such commit: %v", err)
	}
	if _, err := b.RestoreSkill(ctx, "other", base, okf.Human("alice"), ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a skill that was not there: %v", err)
	}
}

func TestWriter_Note(t *testing.T) {
	ctx := context.Background()
	b := openLibrary(t)
	w := writer(t, b, okf.Human("alice"))
	w.Create(SkillPath("go"), skill("go", "", "Use for Go.", "Tables."))
	w.Commit(ctx, "Add go")
	trial := writer(t, b, "process:skill-trial")
	if err := trial.Note(SkillPath("go"), "not written"); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a note on a page not written: %v", err)
	}
	if _, err := trial.Verify(SkillPath("go")); err != nil {
		t.Fatal(err)
	}
	if err := trial.Note(SkillPath("go"), "kept after  3 turns used it"); err != nil {
		t.Fatal(err)
	}
	trial.Commit(ctx, "Keep go")
	if log := readFile(t, b, "/log.md"); !strings.Contains(log, "(/skills/go/SKILL.md) by process:skill-trial: kept after 3 turns used it") {
		t.Errorf("the log:\n%s", log)
	}
	if page, _ := b.Page(SkillPath("go")); page.Tier != okf.MachineConfirmed {
		t.Errorf("kept by a process, the skill is machine-confirmed: %v", page.Tier)
	}
}
