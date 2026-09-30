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
	"time"

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
	// A skill's own hidden files come along; an environment file and what
	// version control and the operating system keep do not.
	os.WriteFile(filepath.Join(dir, ".eslintrc.json"), []byte("{}"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".github"), 0o755)
	os.WriteFile(filepath.Join(dir, ".github", "TEMPLATE.md"), []byte("# Template\n"), 0o644)
	for _, name := range []string{".env", ".DS_Store", ".gitignore"} {
		os.WriteFile(filepath.Join(dir, name), []byte("hidden"), 0o644)
	}
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
	if !slices.Equal(files, []string{".eslintrc.json", ".github/TEMPLATE.md", "SKILL.md", "scripts/run.sh"}) {
		t.Errorf("files %v", files)
	}
	if string(got.Files[".github/TEMPLATE.md"]) != "# Template\n" {
		t.Errorf("a hidden markdown file as it is: %q", got.Files[".github/TEMPLATE.md"])
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

// Pages the library made of a skill's references before it kept them as
// written, and links an agent writes from the library's root: a runtime
// reads them linking from where each file is, and without the library's
// type and stamps. What the page has of its own, its title here, stays.
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
	want := "---\ntitle: Guide\n---\n\n# Guide\n\nRead [the list](list.md#words), then [the skill](../SKILL.md) again, or [elsewhere](/patterns/relays.md).\n"
	if got := string(got.Files["references/guide.md"]); got != want {
		t.Errorf("the guide:\n%q\nwant\n%q", got, want)
	}
	if got := string(got.Files["references/list.md"]); got != "---\ntitle: List\n---\n\nThe relay word is FERN-77.\n" {
		t.Errorf("the list: %q", got)
	}
	if !strings.Contains(readFile(t, b, "/skills/relay-word/references/guide.md"), "](/skills/relay-word/references/list.md#words)") {
		t.Error("the library's page links from its root")
	}
}

// The markdown files of a skill's folder are kept as their author wrote
// them, a type line in front, and reach a runtime to the byte: names,
// frontmatter and links as they were, an index.md of the skill's own as a
// file. Changed by an agent since, a file keeps its own keys and text and
// loses the library's.
func TestProjectSkill_FilesAsTheyWere(t *testing.T) {
	ctx := context.Background()
	b := openLibrary(t)
	w := writer(t, b, okf.Human("alice"))
	if _, err := w.Create(SkillPath("pdf"), skill("pdf", "", "Use for PDFs.", "If you fill a form, read FORMS.md.\n\nSee [the API](references/API_Guide.md).")); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"FORMS.md":                "# Forms\n\nFill them.\n",
		"references/API_Guide.md": "# API\n\nBack to [forms](../FORMS.md) and [the list](index.md).\n",
		"templates/agent.md":      "---\nname: reviewer\ndescription: Reviews code.\ntools: Read, Grep\n---\n\nYou review code.\n",
		"templates/SKILL.md":      "---\nname: my-template\ndescription: A template.\n---\n\n# Template\n",
		"big.md":                  "# Big\n\n" + strings.Repeat("A line of a long guide.\n", 317<<10/24),
	}
	for rel, text := range files {
		if _, err := w.PutPage("/skills/pdf/"+rel, okf.WithType([]byte(text), ReferenceType)); err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
	}
	if err := w.PutFile("/skills/pdf/references/index.md", []byte("---\ntitle: All\n---\n# All\n"), false); err != nil {
		t.Fatal(err)
	}
	files["references/index.md"] = "---\ntitle: All\n---\n# All\n"
	if _, err := w.Commit(ctx, "Import pdf"); err != nil {
		t.Fatal(err)
	}
	if page, err := b.Page("/skills/pdf/references/API_Guide.md"); err != nil || page.Type != ReferenceType || page.Title != "API_Guide" {
		t.Errorf("a page of the library, named as it was: %+v %v", page.Summary, err)
	}
	if h := b.Health(time.Now()); len(h.Problems) > 0 || len(h.Broken) > 0 {
		t.Errorf("the library holds them to OKF: %+v %+v", h.Problems, h.Broken)
	}
	got, err := b.ProjectSkill("pdf")
	if err != nil {
		t.Fatal(err)
	}
	for rel, text := range files {
		if string(got.Files[rel]) != text {
			t.Errorf("%s:\n%q\nwant\n%q", rel, got.Files[rel], text)
		}
	}
	if main := string(got.Files["SKILL.md"]); !strings.Contains(main, "read FORMS.md.\n\nSee [the API](references/API_Guide.md).") {
		t.Errorf("SKILL.md links as it was written:\n%s", main)
	}

	// An agent improves the template; the runtime gets its keys and the
	// change, not the library's.
	agent := writer(t, b, "codex/gpt-5")
	if _, err := agent.Edit("/skills/pdf/templates/agent.md", []Edit{{Op: "replace", Target: "You review code.", Content: "You review code closely, with [the form](/skills/pdf/FORMS.md)."}}, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = b.ProjectSkill("pdf")
	if out := string(got.Files["templates/agent.md"]); out != "---\nname: reviewer\ndescription: Reviews code.\ntools: Read, Grep\n---\n\nYou review code closely, with [the form](../FORMS.md).\n" {
		t.Errorf("the changed template:\n%q", out)
	}

	// Too big for a skill's file, a page is refused as one; a page outside
	// a skill's folder is no file of a skill.
	var p *store.Problem
	if _, err := w.PutPage("/skills/pdf/huge.md", okf.WithType([]byte(strings.Repeat("x", MaxSkillFile+1)), ReferenceType)); !errors.As(err, &p) || p.Code != "skillFileTooBig" {
		t.Errorf("too big: %v", err)
	}
	for _, bad := range []string{"/patterns/x.md", "/skills/pdf/SKILL.md", "/skills/pdf/.hidden.md", "/skills/pdf/index.md"} {
		if _, err := w.PutPage(bad, []byte("---\ntype: Reference\n---\nx\n")); err == nil {
			t.Errorf("%s: written", bad)
		}
	}
	if _, err := w.PutPage("/skills/pdf/notes.md", []byte("no type\n")); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("no concept: %v", err)
	}
}

// How a skill's folder changed between two commits, and before one; a
// name or commit that is none is refused.
func TestSkillDiff(t *testing.T) {
	ctx := context.Background()
	b := openLibrary(t)
	w := writer(t, b, okf.Human("alice"))
	if _, err := w.Create(SkillPath("notes"), skill("notes", "", "Use when taking notes.", "Keep them short.")); err != nil {
		t.Fatal(err)
	}
	first, err := w.Commit(ctx, "Add notes")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Edit(SkillPath("notes"), []Edit{{Op: "append", Content: "Date each one."}}, ""); err != nil {
		t.Fatal(err)
	}
	second, err := w.Commit(ctx, "Date notes")
	if err != nil {
		t.Fatal(err)
	}
	diff, err := b.SkillDiff(ctx, "notes", first[:7], second)
	if err != nil || !strings.Contains(diff, "+Date each one.") || !strings.Contains(diff, "skills/notes/SKILL.md") {
		t.Errorf("the diff %v:\n%s", err, diff)
	}
	if diff, err := b.SkillDiff(ctx, "notes", first, second+"^"); err != nil || strings.TrimSpace(diff) != "" {
		t.Errorf("nothing between a commit and itself: %v %q", err, diff)
	}
	for _, bad := range [][2]string{{"--output=x", second}, {first, "HEAD"}, {first, second + "^^"}} {
		if _, err := b.SkillDiff(ctx, "notes", bad[0], bad[1]); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	if _, err := b.SkillDiff(ctx, "../x", first, second); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("no skill name: %v", err)
	}
}

func TestSkillLeavesOut(t *testing.T) {
	for name, want := range map[string]bool{
		".git": true, ".gitignore": true, ".gitattributes": true, ".gitmodules": true, ".DS_Store": true, "__MACOSX": true,
		".env": true, ".env.local": true, ".env.production": true,
		".venv": true, ".tox": true, ".cache": true, ".pytest_cache": true, ".mypy_cache": true, ".ruff_cache": true, ".parcel-cache": true,
		"venv": false, "_cache": false, ".vscode": false,
		".env.example": false, ".env.sample": false, ".env.template": false, ".eslintrc": false, ".github": false, "SKILL.md": false,
	} {
		if got := SkillLeavesOut(name); got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
	if !HiddenPath(".github/TEMPLATE.md") || !HiddenPath("a/.b/c.md") || HiddenPath("references/x.md") || HiddenPath("../x.md") {
		t.Error("hidden paths")
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
	if err := w.PutFile("/skills/release-notes/scripts/draft.sh", []byte("#!/bin/sh\necho draft\n"), false); err != nil {
		t.Fatal(err)
	}
	// A tool built for the machine, which only its mode says runs.
	tool := []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0}
	if err := w.PutFile("/skills/release-notes/bin/tool", tool, true); err != nil {
		t.Fatal(err)
	}
	if err := w.PutFile("/skills/release-notes/bin/data", tool, false); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(ctx, "Add release-notes"); err != nil {
		t.Fatal(err)
	}
	modes := func() string {
		out, err := b.git.run(ctx, "ls-files", "-s", "skills/release-notes/bin")
		if err != nil {
			t.Fatal(err)
		}
		var modes []string
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			f := strings.Fields(line)
			modes = append(modes, f[0]+" "+f[3])
		}
		return strings.Join(modes, ", ")
	}
	if got := modes(); got != "100644 skills/release-notes/bin/data, 100755 skills/release-notes/bin/tool" {
		t.Errorf("the history keeps which runs: %s", got)
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
	// The tool runs, and so does the script, which came without the bit.
	if want := []string{"bin/tool", "scripts/draft.sh"}; !slices.Equal(skill.Executable, want) {
		t.Errorf("executable %v, want %v", skill.Executable, want)
	}
	// Put again without the bit, it loses it, in the history too.
	if err := w.PutFile("/skills/release-notes/bin/tool", tool, false); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(ctx, "Keep the tool from running"); err != nil {
		t.Fatal(err)
	}
	if got := modes(); got != "100644 skills/release-notes/bin/data, 100644 skills/release-notes/bin/tool" {
		t.Errorf("after: %s", got)
	}
	for _, bad := range []string{"/patterns/x.sh", "/skills/release-notes/notes.md", "/skills/release-notes/.env", "/skills/release-notes/.git/config",
		"/skills/release-notes/.gitignore", "/skills/Bad Name/x.sh", "/skills/release-notes"} {
		if err := w.PutFile(bad, []byte("x"), false); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	for _, good := range []string{"/skills/release-notes/.eslintrc", "/skills/release-notes/.github/TEMPLATE.md", "/skills/release-notes/.env.example"} {
		if err := w.PutFile(good, []byte("x"), false); err != nil {
			t.Errorf("%s: %v", good, err)
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
	if err := person.PutFile("/skills/release-notes/scripts/draft.sh", []byte("#!/bin/sh\necho v1\n"), true); err != nil {
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
	agent.PutFile("/skills/release-notes/scripts/draft.sh", []byte("#!/bin/sh\necho v2\n"), true)
	agent.PutFile("/skills/release-notes/scripts/extra.sh", []byte("#!/bin/sh\n"), true)
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
	if info, err := os.Stat(filepath.Join(b.Dir(), "skills", "release-notes", "scripts", "draft.sh")); err != nil || info.Mode()&0o111 == 0 {
		t.Errorf("the script still runs: %v %v", info, err)
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
