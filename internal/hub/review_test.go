package hub

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// changedFiles stands in for the database's files changed by turns.
type changedFiles struct {
	files []store.ChangedFile
	since time.Time
}

func (c *changedFiles) ListChangedFiles(_ context.Context, _ string, since time.Time, _ int) ([]store.ChangedFile, error) {
	c.since = since
	var out []store.ChangedFile
	for _, f := range c.files {
		if f.At.After(since) {
			out = append(out, f)
		}
	}
	return out, nil
}

func TestWikiReviews(t *testing.T) {
	now := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	ago := func(days int) time.Time { return now.Add(-time.Duration(days) * 24 * time.Hour) }
	page := func(p, typ string, checked time.Time, mentions ...string) wiki.Summary {
		return wiki.Summary{Path: p, Type: typ, Title: p, Status: okf.Stable, Generated: okf.Stamp{By: "codex/default", At: checked}, Mentions: mentions}
	}
	resident := page("/conventions/units.md", "Convention", ago(100))
	resident.Tags = []string{"resident"}
	stale := page("/pitfalls/port.md", "Pitfall", ago(2))
	stale.StaleAfter = ago(1)
	gone := page("/decisions/old.md", "Decision", ago(400))
	gone.Status = okf.Deprecated
	pages := []wiki.Summary{
		page("/decisions/payload.md", "Decision", ago(200)),
		resident,
		page("/facts/brief.md", "Fact", ago(10), "internal/hub/brief.go"),
		page("/facts/quiet.md", "Fact", ago(10), "internal/hub/turns.go"),
		page("/topics/brief-rework.md", "Topic", ago(10), "internal/hub/brief.go"),
		stale,
		gone,
		page(wiki.MemoryPath, wiki.MemoryType, ago(400)),
		page("/modules/fresh.md", "Module", ago(3), "internal/hub"),
	}
	st := &changedFiles{files: []store.ChangedFile{
		// Changed by a turn that started after the page was checked.
		{Path: "/repo/internal/hub/brief.go", TurnID: "t2", ThreadID: "th2", TopicNumber: 12, At: ago(5)},
		// By one that started before: the turn that wrote the page, say.
		{Path: "internal/hub/turns.go", TurnID: "t1", TopicNumber: 9, At: ago(11)},
	}}
	reviews, err := wikiReviews(context.Background(), st, "p1", pages, now)
	if err != nil {
		t.Fatal(err)
	}
	if !st.since.Equal(ago(10)) {
		t.Errorf("files are looked for since the oldest check of a page naming any: %v", st.since)
	}
	want := map[string]string{
		"/decisions/payload.md": reviewPeriod,
		"/conventions/units.md": reviewPeriod,
		"/facts/brief.md":       reviewChanged,
		"/pitfalls/port.md":     reviewStale,
	}
	got := map[string]string{}
	for p, r := range reviews {
		got[p] = r.Why
	}
	if len(got) != len(want) {
		t.Errorf("due %v, want %v", got, want)
	}
	for p, why := range want {
		if got[p] != why {
			t.Errorf("%s: %q, want %q", p, got[p], why)
		}
	}
	if r := reviews["/facts/brief.md"]; r.File != "/repo/internal/hub/brief.go" || r.TopicNumber != 12 || r.TurnID != "t2" || !r.ChangedAt.Equal(ago(5)) {
		t.Errorf("what changed: %+v", r)
	}
	if r := reviews["/decisions/payload.md"]; r.Every != 180 {
		t.Errorf("a decision holds 180 days: %+v", r)
	}
	if r := reviews["/conventions/units.md"]; r.Every != 90 {
		t.Errorf("a resident convention, half as long: %+v", r)
	}

	// A change first, then what every turn carries, then the longest
	// unchecked.
	var order []string
	for _, p := range reviewOrder(pages, reviews) {
		order = append(order, p.Path)
	}
	if want := []string{"/facts/brief.md", "/conventions/units.md", "/decisions/payload.md", "/pitfalls/port.md"}; !slices.Equal(order, want) {
		t.Errorf("order %q, want %q", order, want)
	}
}
