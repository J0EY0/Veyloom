package okf

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func rules(problems []Problem) []string {
	var out []string
	for _, p := range problems {
		out = append(out, p.Rule)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func TestCheck_OfficialSamplesConform(t *testing.T) {
	root := filepath.Join("testdata", "acme_retail")
	err := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || filepath.Ext(p) != ".md" {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		isRoot := filepath.Dir(rel) == "."
		if problems := CheckFile("/"+filepath.ToSlash(rel), data, isRoot, Conformance); len(problems) > 0 {
			t.Errorf("%s should conform: %v", rel, problems)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The strict profile is Veyloom's own: the sample uses a key of its own,
	// and links and source paths without the leading slash, none of which
	// Veyloom writes.
	data, _ := os.ReadFile(filepath.Join(root, "metrics", "gross-margin.md"))
	if got := rules(CheckFile("/metrics/gross-margin.md", data, false, Strict)); !slices.Equal(got, []string{"key", "link", "path"}) {
		t.Errorf("strict rules broken by the sample: %v", got)
	}
}

func TestCheck_Conformance(t *testing.T) {
	for _, tc := range []struct {
		path, src string
		root      bool
		want      []string
	}{
		{"/a.md", "# no frontmatter\n", false, []string{"frontmatter"}},
		{"/a.md", "---\ntitle: Untyped\n---\n", false, []string{"type"}},
		{"/a.md", "---\ntype: \"  \"\n---\n", false, []string{"type"}},
		{"/a.md", "---\ntype: [a]\n---\n", false, []string{"type"}},
		{"/index.md", "# Pages\n\n* [A](/a.md) - one\n", false, nil},
		{"/index.md", "---\nokf_version: \"0.2\"\n---\n# Pages\n", true, nil},
		{"/index.md", "---\nokf_version: \"0.2\"\ntitle: x\n---\n", true, []string{"index"}},
		{"/sub/index.md", "---\nokf_version: \"0.2\"\n---\n", false, []string{"index"}},
		{"/log.md", "# Log\n\n## 2026-09-21\n* **Update**: x\n", false, nil},
		{"/log.md", "---\ntype: Log\n---\n# Log\n\n## 2026-09-21\n", false, nil},
		{"/log.md", "# Log\n\n## Yesterday\n", false, []string{"log"}},
	} {
		if got := rules(CheckFile(tc.path, []byte(tc.src), tc.root, Conformance)); !slices.Equal(got, tc.want) {
			t.Errorf("%s %q: rules %v, want %v", tc.path, tc.src, got, tc.want)
		}
	}
	d := New("Fact")
	if got := rules(CheckConcept("/log.md", d, Conformance)); !slices.Equal(got, []string{"reserved"}) {
		t.Errorf("a concept at a reserved name: %v", got)
	}
}

func TestCheck_Strict(t *testing.T) {
	good := "---\ntype: Fact\ntitle: Ok\nstatus: stable\ntags: [a]\n" +
		"generated:\n  by: codex/gpt-5.5\n  at: 2026-09-21T01:02:03Z\n" +
		"verified:\n  - by: human:owner\n    at: 2026-09-21T02:00:00+08:00\n" +
		"sources:\n  - id: s1\n    resource: veyloom://turns/7\n  - resource: all queries in project x\n---\n\n" +
		"Said so.[^s1] See [b](/facts/b.md) and [web](https://example.com).\n\n[^s1]: turn 7\n"
	if p := CheckFile("/facts/a.md", []byte(good), false, Strict); len(p) > 0 {
		t.Fatalf("a well-formed page should pass: %v", p)
	}
	for _, tc := range []struct {
		name, from, to, want string
	}{
		{"unknown key", "title: Ok\n", "title: Ok\nowner: me\n", "key"},
		{"proposal key", "title: Ok\n", "title: Ok\nrelationships: []\n", "key"},
		{"legacy key", "title: Ok\n", "title: Ok\ntimestamp: 2026-01-01T00:00:00Z\n", "key"},
		{"twice", "title: Ok\n", "title: Ok\ntitle: Again\n", "key"},
		{"status", "status: stable", "status: retired", "status"},
		{"tags", "tags: [a]", "tags: a", "tags"},
		{"actor", "by: codex/gpt-5.5", "by: codex", "actor"},
		{"no offset", "at: 2026-09-21T01:02:03Z", "at: 2026-09-21T01:02:03", "timestamp"},
		{"verified shape", "  - by: human:owner\n    at: 2026-09-21T02:00:00+08:00\n", "  - human:owner\n", "verified"},
		{"source without resource", "  - resource: all queries in project x\n", "  - title: nothing to follow\n", "sources"},
		{"relative source", "resource: veyloom://turns/7", "resource: facts/c.md", "path"},
		{"footnote", "[^s1]: turn 7", "[^s1]: turn 7 [^s2]", "footnote"},
		{"relative link", "(/facts/b.md)", "(b.md)", "link"},
		{"stale after", "title: Ok\n", "title: Ok\nstale_after: soon\n", "timestamp"},
	} {
		src := strings.Replace(good, tc.from, tc.to, 1)
		if src == good {
			t.Fatalf("%s: replacement did not apply", tc.name)
		}
		if got := rules(CheckFile("/facts/a.md", []byte(src), false, Strict)); !slices.Contains(got, tc.want) {
			t.Errorf("%s: rules %v, want %s", tc.name, got, tc.want)
		}
	}
	if got := rules(CheckFile("/log.md", []byte("---\ntype: Log\n---\n# Log\n"), false, Strict)); !slices.Equal(got, []string{"log"}) {
		t.Errorf("Veyloom's own logs have no frontmatter: %v", got)
	}
}

func TestCheck_Skill(t *testing.T) {
	skill := "---\ntype: Skill\nname: commit-message\ndescription: Write detailed commit messages. Use when committing.\n" +
		"metadata:\n  veyloom-team: veyloom\n---\n\n# Instructions\n"
	if p := CheckFile("/skills/commit-message/SKILL.md", []byte(skill), false, Strict); len(p) > 0 {
		t.Fatalf("a well-formed skill should pass: %v", p)
	}
	for _, tc := range []struct{ name, path, from, to string }{
		{"dir mismatch", "/skills/other/SKILL.md", "", ""},
		{"bad name", "/skills/Commit_Message/SKILL.md", "name: commit-message", "name: Commit_Message"},
		{"no description", "/skills/commit-message/SKILL.md", "description: Write detailed commit messages. Use when committing.\n", ""},
		{"metadata values", "/skills/commit-message/SKILL.md", "veyloom-team: veyloom", "veyloom-team: [a, b]"},
	} {
		src := strings.Replace(skill, tc.from, tc.to, 1)
		if got := rules(CheckFile(tc.path, []byte(src), false, Strict)); !slices.Contains(got, "skill") {
			t.Errorf("%s: rules %v, want skill", tc.name, got)
		}
	}
	// A description longer than Agent Skills advises is the author's: no
	// runtime refuses it.
	long := strings.Replace(skill, "Write detailed commit messages. Use when committing.", strings.Repeat("Write detailed commit messages. ", 40), 1)
	if p := CheckFile("/skills/commit-message/SKILL.md", []byte(long), false, Strict); len(p) > 0 {
		t.Errorf("a long description passes: %v", p)
	}
	// What runtimes add to Agent Skills stays on a skill as it was written.
	withSettings := strings.Replace(skill, "metadata:", "disable-model-invocation: true\nallowed-tools: Bash(git:*)\nhooks:\n  PreToolUse: [x]\nmetadata:", 1)
	if p := CheckFile("/skills/commit-message/SKILL.md", []byte(withSettings), false, Strict); len(p) > 0 {
		t.Errorf("a skill's settings for runtimes pass: %v", p)
	}
	// Skill fields are only allowed on a skill's own file.
	onFact := strings.Replace(skill, "type: Skill", "type: Fact", 1)
	if got := rules(CheckFile("/facts/x.md", []byte(onFact), false, Strict)); !slices.Equal(got, []string{"key"}) {
		t.Errorf("skill fields on a fact: %v", got)
	}
}

// A skill links as its author wrote it, from its directory, where a
// runtime reads it; other pages link from the bundle's root.
func TestCheck_SkillLinksAsWritten(t *testing.T) {
	page := "---\ntype: Skill\nname: go-tests\ndescription: d\n---\n\nSee [cases](references/cases.md), [up](../other/SKILL.md) and [root](/patterns/a.md).\n"
	if probs := CheckFile("/skills/go-tests/SKILL.md", []byte(page), false, Strict); len(probs) > 0 {
		t.Errorf("a skill's links pass as written: %v", probs)
	}
	fact := "---\ntype: Fact\n---\n\nSee [cases](references/cases.md).\n"
	if probs := CheckFile("/facts/a.md", []byte(fact), false, Strict); len(probs) != 1 {
		t.Errorf("other pages link from the root: %v", probs)
	}
}

// The other files of a skill's folder are its author's: a type is all
// they need, and an index.md or log.md there is no listing or history.
func TestCheck_SkillFolderFiles(t *testing.T) {
	template := "---\ntype: Reference\nname: reviewer\ntools: Read, Grep\nstatus: beta\n---\n\nSee [the form](../FORMS.md).\n"
	if probs := CheckFile("/skills/pdf/templates/agent.md", []byte(template), false, Strict); len(probs) > 0 {
		t.Errorf("a template of a skill passes as written: %v", probs)
	}
	if got := rules(CheckFile("/facts/agent.md", []byte(strings.Replace(template, "Reference", "Fact", 1)), false, Strict)); !slices.Contains(got, "key") || !slices.Contains(got, "link") {
		t.Errorf("elsewhere the same is held to the strict rules: %v", got)
	}
	if got := rules(CheckFile("/skills/pdf/FORMS.md", []byte("---\nname: x\n---\nx\n"), false, Strict)); !slices.Equal(got, []string{"type"}) {
		t.Errorf("a type it needs: %v", got)
	}
	for _, name := range []string{"index.md", "log.md"} {
		if probs := CheckFile("/skills/pdf/references/"+name, []byte("---\ntitle: x\n---\n# x\n"), false, Strict); len(probs) > 0 {
			t.Errorf("a skill's own %s: %v", name, probs)
		}
	}
}
