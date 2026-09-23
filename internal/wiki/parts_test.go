package wiki

import (
	"errors"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{
		"decisions/a.md":    "/decisions/a.md",
		"/decisions/a.md":   "/decisions/a.md",
		" /facts/./b.md ":   "/facts/b.md",
		"facts/x/../b.md":   "/facts/b.md",
		"skills/x/SKILL.md": "/skills/x/SKILL.md",
		"老页面/Mixed Case.md": "/老页面/Mixed Case.md",
	} {
		if got, err := CleanPath(in); got != want || err != nil {
			t.Errorf("CleanPath(%q) = %q %v, want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "..", "../a.md", "/a/../../b.md", ".git/config.md", "a/.hidden.md", "a.txt", "a\\b.md", "index.md", "x/log.md"} {
		if _, err := CleanPath(in); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("CleanPath(%q) should be refused, got %v", in, err)
		}
	}
	// A person follows a link to a folder's index to it, so that one says
	// what it is, by code.
	var p *store.Problem
	if _, err := CleanPath("/metrics/index.md"); !errors.As(err, &p) || p.Code != "reservedFile" || p.Params["name"] != "index.md" {
		t.Errorf("an index: %v", err)
	}
	for in, ok := range map[string]bool{
		"/decisions/payload-json.md":      true,
		"/skills/commit-message/SKILL.md": true,
		"/decisions/Payload.md":           false,
		"/decisions/a--b.md":              false,
		"/老页面/a.md":                       false,
		"/skills/x/skill.md":              true,
	} {
		if err := checkNewPath(in); (err == nil) != ok {
			t.Errorf("checkNewPath(%q) = %v", in, err)
		}
	}
}

func TestScanner(t *testing.T) {
	text := "line one\n" +
		"aws AKIAABCDEFGHIJKLMNOP\n" +
		"key sk-ant-api03-" + strings.Repeat("x", 30) + "\n" +
		"-----BEGIN OPENSSH PRIVATE KEY-----\n" +
		"harmless: sk-dummy, AKIA-not-a-key, the word token\n"
	found := DefaultScanner().Scan(text)
	var got []string
	for _, f := range found {
		got = append(got, f.Rule+":"+string(rune('0'+f.Line)))
	}
	if strings.Join(got, " ") != "private-key:4 aws-access-key:2 anthropic-key:3" {
		t.Errorf("found %v", got)
	}
	custom, err := DefaultScanner().With("internal-host", `\bcorp\.internal\b`)
	if err != nil {
		t.Fatal(err)
	}
	if f := custom.Scan("see db.corp.internal"); len(f) != 1 || f[0].Rule != "internal-host" {
		t.Errorf("custom rule %v", f)
	}
	if _, err := DefaultScanner().With("bad", `(`); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a bad pattern: %v", err)
	}
}

func TestApplyEdits_InsertsAfterALineOnItsOwnLine(t *testing.T) {
	got, err := applyEdits("- a\n- b\n", []Edit{{Op: OpInsertAfter, Target: "- a", Content: "- a2\n"}})
	if err != nil || got != "- a\n- a2\n- b\n" {
		t.Errorf("got %q %v", got, err)
	}
	got, err = applyEdits("say hello world", []Edit{{Op: OpInsertAfter, Target: "hello", Content: " there"}})
	if err != nil || got != "say hello there world" {
		t.Errorf("inside a line: %q %v", got, err)
	}
	got, err = applyEdits("no newline", []Edit{{Op: OpAppend, Content: "next"}})
	if err != nil || got != "no newline\nnext\n" {
		t.Errorf("append: %q %v", got, err)
	}
}

func TestChanges(t *testing.T) {
	body := "* Creation: [a \\[b\\] c](/facts/x.md) by codex/default\n" +
		"* Rename: [New](/facts/new.md) was /facts/old.md by human:owner\n" +
		"* Update: /facts/gone.md was removed outside Veyloom\n" +
		"* Revert: undid 1234567 (Turn 3) by human:owner\n" +
		"not a change\n\nVeyloom-Turn: 7\n"
	got := changes(body)
	want := []Change{
		{Kind: "Creation", Path: "/facts/x.md", Title: "a [b] c", Text: "[a \\[b\\] c](/facts/x.md) by codex/default"},
		{Kind: "Rename", Path: "/facts/new.md", Title: "New", Text: "[New](/facts/new.md) was /facts/old.md by human:owner"},
		{Kind: "Update", Path: "/facts/gone.md", Text: "/facts/gone.md was removed outside Veyloom"},
		{Kind: "Revert", Text: "undid 1234567 (Turn 3) by human:owner"},
	}
	if len(got) != len(want) {
		t.Fatalf("changes = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("change %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
