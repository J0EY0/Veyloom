package wiki

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestParseMemory(t *testing.T) {
	body := "Kept by the team.\n\n" +
		"- Reply in Chinese. (2026-09-20, Joey)\n" +
		"* Run make test-db before saying done (it needs Docker). (2026-09-21, Claude in topic #4)\n" +
		"- Keep commits small (2026-09-22)\n" +
		"- No note at all\n" +
		"-\n" +
		"  - Indented still counts (see #12)\n"
	want := []MemoryEntry{
		{Text: "Reply in Chinese.", Date: "2026-09-20", Source: "Joey"},
		{Text: "Run make test-db before saying done (it needs Docker).", Date: "2026-09-21", Source: "Claude in topic #4"},
		{Text: "Keep commits small", Date: "2026-09-22"},
		{Text: "No note at all"},
		{Text: "Indented still counts (see #12)"},
	}
	got := ParseMemory(body)
	if !slices.Equal(got, want) {
		t.Fatalf("ParseMemory:\n got %+v\nwant %+v", got, want)
	}
	// Written back, the entries read the same.
	if again := ParseMemory(MemoryText(got)); !slices.Equal(again, want) {
		t.Errorf("round trip: %+v", again)
	}
	if n := MemoryChars(want[:1]); n != len([]rune("- Reply in Chinese. (2026-09-20, Joey)\n")) {
		t.Errorf("chars %d", n)
	}
}

func TestCleanMemoryText(t *testing.T) {
	for in, want := range map[string]string{
		"  - Reply\n in   Chinese. ": "Reply in Chinese.",
		"* 用中文回复":                    "用中文回复",
		"\n\t":                       "",
		" - ":                        "",
	} {
		if got := CleanMemoryText(in); got != want {
			t.Errorf("CleanMemoryText(%q) = %q, want %q", in, got, want)
		}
	}
}

// A memory is one page, made the first time, then changed from what was
// read; a change from a stale read is refused.
func TestWriter_PutMemory(t *testing.T) {
	b := openTest(t, Options{Git: true, Layout: PersonalLayout})
	if entries, hash, err := b.Memory(); err != nil || entries != nil || hash != "" {
		t.Fatalf("no memory yet: %v %q %v", entries, hash, err)
	}
	w := writer(t, b, "human:joey")
	first := []MemoryEntry{{Text: "Reply in Chinese.", Date: "2026-09-20", Source: "Joey"}}
	if _, err := w.PutMemory("Personal memory", "What Joey wants in every project.", first, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(context.Background(), "Remembered"); err != nil {
		t.Fatal(err)
	}
	entries, hash, err := b.Memory()
	if err != nil || !slices.Equal(entries, first) || hash == "" {
		t.Fatalf("the memory: %+v %q %v", entries, hash, err)
	}
	page, _ := b.Page(MemoryPath)
	if page.Type != MemoryType || page.Title != "Personal memory" {
		t.Errorf("the page: %+v", page.Summary)
	}
	if !strings.Contains(readFile(t, b, "/index.md"), "(/memory.md)") {
		t.Error("the root index lists the memory")
	}

	second := append(first, MemoryEntry{Text: "Commit messages in English.", Date: "2026-09-23", Source: "Joey"})
	if _, err := w.PutMemory("Personal memory", "", second, hash); err != nil {
		t.Fatal(err)
	}
	if entries, _, _ := b.Memory(); !slices.Equal(entries, second) {
		t.Errorf("changed: %+v", entries)
	}
	if page, _ := b.Page(MemoryPath); page.Description != "What Joey wants in every project." {
		t.Errorf("a change keeps the page's own description: %q", page.Description)
	}
	if _, err := w.PutMemory("Personal memory", "", first, hash); !errors.Is(err, store.ErrConflict) {
		t.Errorf("a change from a stale read: %v", err)
	}
}
