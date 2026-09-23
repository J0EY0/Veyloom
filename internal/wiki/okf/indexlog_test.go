package okf

import "testing"

func TestRenderIndex(t *testing.T) {
	root := RenderIndex(true, []IndexSection{
		{Heading: "Directories", Entries: []IndexEntry{
			{Title: "decisions", Link: "/decisions/index.md", Description: "Choices that were made."},
		}},
		{Heading: "Pages", Entries: []IndexEntry{
			{Title: "A [draft] title", Link: "/a.md", Description: "Two\nlines  squashed."},
			{Title: "No description", Link: "/b.md"},
		}},
	})
	want := `---
okf_version: "0.2"
---

# Directories

* [decisions](/decisions/index.md) - Choices that were made.

# Pages

* [A \[draft\] title](/a.md) - Two lines squashed.
* [No description](/b.md)
`
	if string(root) != want {
		t.Errorf("got:\n%s\nwant:\n%s", root, want)
	}
	if v := IndexVersion(root); v != Version {
		t.Errorf("version %q", v)
	}
	if p := CheckFile("/index.md", root, true, Strict); len(p) > 0 {
		t.Errorf("rendered root index should conform: %v", p)
	}
	sub := RenderIndex(false, []IndexSection{{Heading: "Decisions"}})
	if string(sub) != "# Decisions\n" || IndexVersion(sub) != "" {
		t.Errorf("sub index %q", sub)
	}
}

func TestAppendLog(t *testing.T) {
	entry := func(text string) []LogEntry { return []LogEntry{{Kind: LogUpdate, Text: text}} }
	log := NewLog("Update Log")
	log = AppendLog(log, "2026-09-20", []LogEntry{{Kind: LogInitialization, Text: "created the bundle"}})
	log = AppendLog(log, "2026-09-21", entry("first of the day"))
	log = AppendLog(log, "2026-09-21", entry("second of the day"))
	want := `# Update Log

## 2026-09-21

* **Update**: first of the day
* **Update**: second of the day

## 2026-09-20

* **Initialization**: created the bundle
`
	if string(log) != want {
		t.Errorf("got:\n%s\nwant:\n%s", log, want)
	}
	if p := CheckFile("/log.md", log, false, Strict); len(p) > 0 {
		t.Errorf("a log Veyloom wrote should pass: %v", p)
	}
	if got := AppendLog(log, "2026-09-22", nil); string(got) != string(log) {
		t.Error("appending nothing changes nothing")
	}
	// A date heading with nothing under it yet, and a file without a
	// trailing newline.
	odd := AppendLog([]byte("# Log\n\n## 2026-09-21\n\n## 2026-09-20\n* **Update**: older"), "2026-09-21", entry("new"))
	if want := "# Log\n\n## 2026-09-21\n\n* **Update**: new\n\n## 2026-09-20\n* **Update**: older\n"; string(odd) != want {
		t.Errorf("got:\n%q\nwant:\n%q", odd, want)
	}
	if got := AppendLog(nil, "2026-09-21", entry("x")); string(got) != "## 2026-09-21\n\n* **Update**: x\n" {
		t.Errorf("empty log: %q", got)
	}
}
