package hub

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

var memorySoFar = []wiki.MemoryEntry{
	{Text: "Reply in Chinese.", Date: "2026-09-20", Source: "alice"},
	{Text: "Run make test-db before saying a change is done.", Date: "2026-09-21", Source: "Claude in topic #4"},
}

func TestRemember(t *testing.T) {
	next, added, already, err := remember(projectMemory, memorySoFar,
		[]string{"  - Keep commits\n small. ", "reply in chinese.", "- Reply in Chinese. (2026-09-20, alice)"}, "2026-09-23", "Pi in topic #7", 3000)
	if err != nil {
		t.Fatal(err)
	}
	want := append(slices.Clone(memorySoFar), wiki.MemoryEntry{Text: "Keep commits small.", Date: "2026-09-23", Source: "Pi in topic #7"})
	if !slices.Equal(next, want) {
		t.Errorf("next %+v", next)
	}
	// What it has already, however it is written, is left as it is.
	if !slices.Equal(added, []string{"Keep commits small."}) || !slices.Equal(already, []string{"Reply in Chinese.", "Reply in Chinese."}) {
		t.Errorf("added %q, already %q", added, already)
	}
	if slices.Equal(memorySoFar, next[:2]) && &memorySoFar[0] == &next[0] {
		t.Error("the memory it was given is left alone")
	}

	for name, tc := range map[string]struct {
		texts  []string
		budget int
		want   string
	}{
		"over the budget": {[]string{"One more."}, wiki.MemoryChars(memorySoFar) + 5, "the project memory would take"},
		"empty":           {[]string{" - "}, 3000, "an entry says nothing"},
		"too long":        {[]string{strings.Repeat("长", memoryEntryMax+1)}, 3000, "at most 400 characters"},
	} {
		if _, _, _, err := remember(projectMemory, memorySoFar, tc.texts, "2026-09-23", "Pi", tc.budget); !errors.Is(err, store.ErrInvalidInput) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestForget(t *testing.T) {
	// Whole, as the brief shows it, or a piece only one entry has.
	for _, text := range []string{"Reply in Chinese.", "- Reply in Chinese. (2026-09-20, alice)", "in chinese"} {
		next, gone, err := forget(projectMemory, memorySoFar, []string{text})
		if err != nil || !slices.Equal(next, memorySoFar[1:]) || !slices.Equal(gone, []string{"Reply in Chinese."}) {
			t.Errorf("forget %q: %+v %q %v", text, next, gone, err)
		}
	}
	next, gone, err := forget(projectMemory, memorySoFar, []string{"reply in", "make test-db"})
	if err != nil || len(next) != 0 || len(gone) != 2 {
		t.Errorf("forget everything: %+v %q %v", next, gone, err)
	}
	// Unless every one is found, nothing is taken out.
	for name, texts := range map[string][]string{
		"not there":   {"Reply in Chinese.", "Use tabs."},
		"in two":      {"e"},
		"nothing yet": {" "},
	} {
		if _, _, err := forget(projectMemory, memorySoFar, texts); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPersonEntries(t *testing.T) {
	got, err := personEntries(personalMemory, memorySoFar,
		[]string{"Run make test-db before saying a change is done.", "", "Reply in Chinese, always.", "Reply in Chinese, always."}, "2026-09-23", "alice", 2000)
	if err != nil {
		t.Fatal(err)
	}
	// Left as it was, an entry keeps its day and source; a changed one is
	// the person's, today; a blank line and a repeat drop out.
	want := []wiki.MemoryEntry{memorySoFar[1], {Text: "Reply in Chinese, always.", Date: "2026-09-23", Source: "alice"}}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v", got)
	}
	if _, err := personEntries(personalMemory, nil, []string{strings.Repeat("x", 300), strings.Repeat("y", 300)}, "2026-09-23", "alice", 500); !errors.Is(err, store.ErrInvalidInput) || !strings.Contains(err.Error(), "the personal memory would take") {
		t.Errorf("over the budget: %v", err)
	}
}
