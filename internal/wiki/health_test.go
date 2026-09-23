package wiki

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

func TestBundle_Health(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true})
	if h := b.Health(time.Now()); !h.Empty(0) {
		t.Fatalf("a new wiki is healthy: %+v", h)
	}
	agent := writer(t, b, "codex/default")
	page := func(p, body string) {
		t.Helper()
		d := okf.New("Fact")
		d.SetString(okf.KeyTitle, p)
		d.SetBody(body)
		if _, err := agent.Create(p, d); err != nil {
			t.Fatal(err)
		}
	}
	page("facts/hub.md", "See [the port](/facts/port.md).")
	page("facts/port.md", "Listens on 7788. See [the hub](/facts/hub.md) and [the index](/index.md).")
	page("facts/alone.md", "Nobody links here.")
	agent.Commit(ctx, "Turn 1")
	h := b.Health(time.Now())
	if got := paths(h.Orphans); !slices.Equal(got, []string{"/facts/alone.md"}) || len(h.Broken) != 0 || len(h.Stale) != 0 || h.ResidentChars != 0 {
		t.Fatalf("health %+v", h)
	}

	// A page written by hand: links to a page that is not there, has gone
	// stale, and every turn carries it.
	hand := "---\ntype: Fact\ntitle: Old\ntags: [resident]\nstale_after: 2026-01-01T00:00:00Z\n---\n\nSee [nothing](/facts/gone.md) and [the hub](/facts/hub.md).\n"
	if err := os.WriteFile(b.file("/facts/old.md"), []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	// And one that breaks OKF.
	if err := os.WriteFile(b.file("/facts/bad.md"), []byte("no frontmatter at all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b.Sync(ctx)
	h = b.Health(time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC))
	if !slices.Equal(h.Broken, []BrokenLink{{From: "/facts/old.md", To: "/facts/gone.md"}}) {
		t.Errorf("broken %+v", h.Broken)
	}
	if got := paths(h.Stale); !slices.Equal(got, []string{"/facts/old.md"}) {
		t.Errorf("stale %v", got)
	}
	if got := paths(h.Orphans); !slices.Equal(got, []string{"/facts/alone.md", "/facts/old.md"}) {
		t.Errorf("orphans %v", got)
	}
	resident := b.Resident()
	if len(resident) != 1 || h.ResidentChars != utf8.RuneCountInString(resident[0].ResidentText()) {
		t.Errorf("resident chars %d of %+v", h.ResidentChars, paths(resident))
	}
	if h.Empty(h.ResidentChars) || !slices.ContainsFunc(h.Problems, func(p okf.Problem) bool { return p.Path == "/facts/bad.md" }) {
		t.Errorf("problems %+v", h.Problems)
	}

	// A deprecated page is no orphan, and an orphan linked from elsewhere
	// is one no longer.
	person := writer(t, b, okf.Human("owner"))
	if _, err := person.Deprecate("/facts/alone.md", "", "no longer true"); err != nil {
		t.Fatal(err)
	}
	if _, err := person.Edit("/facts/hub.md", []Edit{{Op: OpAppend, Content: "Old notes: [old](/facts/old.md)."}}, ""); err != nil {
		t.Fatal(err)
	}
	person.Commit(ctx, "Tidy")
	if got := paths(b.Health(time.Now()).Orphans); len(got) != 0 {
		t.Errorf("orphans after tidying %v", got)
	}
}

// Pages naming the same path of the repository that do not link each other
// are found, a few; the memory is no orphan.
func TestBundle_HealthUnlinked(t *testing.T) {
	b := openTest(t, Options{Layout: ProjectLayout})
	w := writer(t, b, "codex/default")
	graphPage(t, w, "/decisions/brief.md", "Decision", "Brief", "The brief is put together in internal/hub/brief.go.")
	graphPage(t, w, "/pitfalls/brief.md", "Pitfall", "Brief pitfall", "Mind hub/brief.go when the room is empty.")
	graphPage(t, w, "/modules/hub.md", "Module", "Hub", "internal/hub/brief.go puts briefs together; see [the decision](/decisions/brief.md).")
	if _, err := w.PutMemory("Project memory", "How to work here.", []MemoryEntry{{Text: "Reply in Chinese."}}, ""); err != nil {
		t.Fatal(err)
	}
	h := b.Health(time.Now())
	// The decision and the module link; the pitfall stands apart from both.
	want := []UnlinkedPair{
		{A: "/decisions/brief.md", B: "/pitfalls/brief.md", File: "internal/hub/brief.go"},
		{A: "/modules/hub.md", B: "/pitfalls/brief.md", File: "internal/hub/brief.go"},
	}
	if !slices.Equal(h.Unlinked, want) {
		t.Errorf("unlinked %+v, want %+v", h.Unlinked, want)
	}
	if slices.Contains(paths(h.Orphans), MemoryPath) {
		t.Errorf("the memory is no orphan: %v", paths(h.Orphans))
	}
	if h.Empty(4000) {
		t.Error("pages to link are something to set right")
	}
}
