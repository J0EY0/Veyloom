package okf

import (
	"strings"
	"testing"
	"time"
)

func TestSkillFolderFile(t *testing.T) {
	for p, want := range map[string]bool{
		"/skills/pdf/FORMS.md":             true,
		"/skills/pdf/references/API.md":    true,
		"/skills/pdf/templates/SKILL.md":   true,
		"/skills/pdf/SKILL.md":             false,
		"/skills/index.md":                 false,
		"/patterns/flaky-tests.md":         false,
		"/decisions/skills/pdf/x.md":       false,
		"skills/pdf/references/../FORM.md": true,
	} {
		if got := SkillFolderFile(p); got != want {
			t.Errorf("%s: %v, want %v", p, got, want)
		}
	}
}

// A file of a skill's folder becomes a concept with a type line put in
// front, and comes back from one to the byte, whatever shape it was in.
func TestWithType_RoundTrip(t *testing.T) {
	for name, tc := range map[string]struct{ in, stored string }{
		"no frontmatter": {"# Forms\n\nFill them.\n", "---\ntype: Reference\n---\n# Forms\n\nFill them.\n"},
		"its own frontmatter": {"---\nname: reviewer\ndescription: Reviews code.\ntools: Read, Grep # the tools\n---\n\nYou review code.\n",
			"---\ntype: Reference\nname: reviewer\ndescription: Reviews code.\ntools: Read, Grep # the tools\n---\n\nYou review code.\n"},
		"a type of its own":  {"---\ntype: post\ntitle: \"{{ title }}\"\n---\nBody.\n", "---\ntype: post\ntitle: \"{{ title }}\"\n---\nBody.\n"},
		"windows lines":      {"---\r\nname: x\r\n---\r\nBody.\r\n", "---\r\ntype: Reference\r\nname: x\r\n---\r\nBody.\r\n"},
		"a byte order mark":  {"\ufeff---\nname: x\n---\nBody.\n", "---\ntype: Reference\n---\n\ufeff---\nname: x\n---\nBody.\n"},
		"templated":          {"---\ntitle: {{ .Title }}\n---\nBody.\n", "---\ntype: Reference\ntitle: {{ .Title }}\n---\nBody.\n"},
		"no yaml":            {"---\ntitle: [unclosed\n---\nBody.\n", "---\ntype: Reference\n---\n---\ntitle: [unclosed\n---\nBody.\n"},
		"an empty type":      {"---\ntype: \"\"\n---\nBody.\n", "---\ntype: Reference\n---\n---\ntype: \"\"\n---\nBody.\n"},
		"a rule to open":     {"---\nSome words.\n---\nMore.\n", "---\ntype: Reference\n---\n---\nSome words.\n---\nMore.\n"},
		"not closed":         {"---\nname: x\n", "---\ntype: Reference\n---\n---\nname: x\n"},
		"empty":              {"", "---\ntype: Reference\n---\n"},
		"the template skill": {"---\nname: my-template\ndescription: A template.\n---\n\n# Template\n", "---\ntype: Reference\nname: my-template\ndescription: A template.\n---\n\n# Template\n"},
	} {
		stored := WithType([]byte(tc.in), "Reference")
		if string(stored) != tc.stored {
			t.Errorf("%s: stored\n%q\nwant\n%q", name, stored, tc.stored)
		}
		if d, err := Parse(stored); err != nil || d.Type() == "" {
			t.Errorf("%s: stored as no concept: %v", name, err)
		}
		if back := WithoutType(stored, "Reference"); string(back) != tc.in {
			t.Errorf("%s: back\n%q\nwant\n%q", name, back, tc.in)
		}
	}
}

// Changed since, by an agent's patch say, the file keeps its own keys and
// its text, and loses the library's.
func TestWithoutType_AfterAChange(t *testing.T) {
	stamp := Stamp{By: "codex/gpt-5", At: time.Date(2026, 9, 30, 1, 2, 3, 0, time.UTC)}
	for name, in := range map[string]string{
		"its own frontmatter": "---\nname: reviewer\ntools: Read, Grep\n---\n\nYou review code.\n",
		"no frontmatter":      "# Forms\n\nFill them.\n",
	} {
		d, err := Parse(WithType([]byte(in), "Reference"))
		if err != nil {
			t.Fatal(err)
		}
		d.SetBody(strings.NewReplacer("review code", "review code closely", "Fill them", "Fill them in ink").Replace(d.Body()))
		d.SetGenerated(stamp)
		d.AddVerified(stamp)
		data, err := d.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		out := string(WithoutType(data, "Reference"))
		for _, gone := range []string{"type:", "generated:", "verified:"} {
			if strings.Contains(out, gone) {
				t.Errorf("%s: keeps %q:\n%s", name, gone, out)
			}
		}
		if name == "its own frontmatter" && (!strings.HasPrefix(out, "---\nname: reviewer\ntools: Read, Grep\n---\n") || !strings.Contains(out, "review code closely")) {
			t.Errorf("%s:\n%s", name, out)
		}
		if name == "no frontmatter" && strings.HasPrefix(out, "---") {
			t.Errorf("%s: a frontmatter it never had:\n%s", name, out)
		}
	}
	// A page the library made of a reference before keeps its title.
	old := "---\ntype: Reference\ntitle: Guide\ngenerated:\n  by: human:alice\n  at: 2026-09-22T01:02:03Z\n---\n\n# Guide\n"
	if out := string(WithoutType([]byte(old), "Reference")); out != "---\ntitle: Guide\n---\n\n# Guide\n" {
		t.Errorf("an older page:\n%q", out)
	}
	// Not a concept, it is left as it is.
	if out := string(WithoutType([]byte("# Notes\n"), "Reference")); out != "# Notes\n" {
		t.Errorf("no concept: %q", out)
	}
}
