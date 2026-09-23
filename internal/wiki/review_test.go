package wiki

import (
	"slices"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

func TestMentions(t *testing.T) {
	body := "The brief is put together in internal/hub/brief.go (see ./internal/hub/brief_test.go:42).\n" +
		"Front end: web/src/features/wiki/, styles in `web/src/styles/theme.css`.\n" +
		"Go version is in `go.mod`; run `make test-db` first. 版本见 docs/design.md。\n" +
		"In code, `t.Parallel()`, `m.name`, `v0.2` and `1.26` are not files, but `package.json` and `CLAUDE.md` are.\n" +
		"See [the payload decision](/decisions/payload-json.md) and https://pressly.github.io/goose/, and ![the map](/files/arch/map.png).\n" +
		"Plain words.like.these without a slash are not paths, e.g. this."
	want := []string{
		"web/src/styles/theme.css", "go.mod", "package.json", "CLAUDE.md",
		"internal/hub/brief.go", "internal/hub/brief_test.go", "web/src/features/wiki", "docs/design.md",
	}
	if got := Mentions(body); !slices.Equal(got, want) {
		t.Errorf("Mentions:\n got %q\nwant %q", got, want)
	}
}

func TestNamesFile(t *testing.T) {
	for _, tc := range []struct {
		mention, file string
		want          bool
	}{
		{"internal/hub/brief.go", "internal/hub/brief.go", true},
		// Written in full, from a worktree, or from the root.
		{"internal/hub/brief.go", "/Users/joey/.veyloom/worktrees/m1/internal/hub/brief.go", true},
		{"hub/brief.go", "internal/hub/brief.go", true},
		// A directory it is in.
		{"web/src/features/wiki", "web/src/features/wiki/Memory.tsx", true},
		{"internal/hub", "/repo/internal/hub/memory.go", true},
		{"internal/hub", "internal/hubble/x.go", false},
		{"internal/hub/brief.go", "internal/hub/brief.go.orig", false},
		{"go.mod", "tools/go.mod", true},
		{"go.mod", "go.modx", false},
	} {
		if got := NamesFile(tc.mention, tc.file); got != tc.want {
			t.Errorf("NamesFile(%q, %q) = %v, want %v", tc.mention, tc.file, got, tc.want)
		}
	}
}

func TestReviewEvery(t *testing.T) {
	now := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		sum  Summary
		want time.Duration
	}{
		{"a decision", Summary{Type: "Decision"}, 180 * day},
		{"a pitfall", Summary{Type: "Pitfall"}, 30 * day},
		{"a resident convention", Summary{Type: "Convention", Tags: []string{"resident"}}, 90 * day},
		{"a topic's write-up", Summary{Type: "Topic"}, 0},
		{"the memory", Summary{Type: MemoryType}, 0},
		{"a deprecated fact", Summary{Type: "Fact", Status: okf.Deprecated}, 0},
	} {
		if got := tc.sum.ReviewEvery(); got != tc.want {
			t.Errorf("%s: every %v, want %v", tc.name, got, tc.want)
		}
	}

	// Checked when last written or confirmed, whichever is later; a page
	// written by hand without either, when its file last changed.
	written, confirmed := now.Add(-10*day), now.Add(-3*day)
	if got := (Summary{Generated: okf.Stamp{At: written}, Verified: confirmed}).CheckedAt(); !got.Equal(confirmed) {
		t.Errorf("confirmed since written: %v", got)
	}
	if got := (Summary{Generated: okf.Stamp{At: confirmed}, Verified: written}).CheckedAt(); !got.Equal(confirmed) {
		t.Errorf("written since confirmed: %v", got)
	}
	if got := (Summary{Modified: written}).CheckedAt(); !got.Equal(written) {
		t.Errorf("by hand: %v", got)
	}
}
