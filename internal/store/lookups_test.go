package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestWikiLookups_RecordListPrune(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	first, err := f.s.CreateTurn(ctx, f.newTurn())
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.s.CreateTurn(ctx, f.newTurn())
	if err != nil {
		t.Fatal(err)
	}
	project := f.room.ProjectID
	for _, l := range []store.WikiLookup{
		{TurnID: first.ID, Scope: store.WikiProject, Query: "审批 超时"},
		{TurnID: first.ID, Scope: store.WikiProject, Path: "/decisions/approvals.md"},
		{TurnID: second.ID, Scope: store.WikiLibrary, Query: "go tests", Hits: 2},
		{TurnID: second.ID, Scope: store.WikiLibrary, Path: "/skills/go-testing/SKILL.md"},
		{TurnID: first.ID, Scope: store.WikiProject, Query: "retry", Hits: 1},
	} {
		l.ProjectID = project
		if err := f.s.RecordWikiLookup(ctx, l); err != nil {
			t.Fatalf("%+v: %v", l, err)
		}
	}
	for _, bad := range []store.WikiLookup{
		{TurnID: first.ID, Scope: store.WikiProject},
		{TurnID: first.ID, Scope: store.WikiProject, Query: "x", Path: "/x.md"},
		{TurnID: first.ID, Scope: store.WikiPersonal, Query: "x"},
		{TurnID: first.ID, Scope: store.WikiProject, Query: "x", Hits: -1},
	} {
		bad.ProjectID = project
		if err := f.s.RecordWikiLookup(ctx, bad); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("%+v: got %v, want ErrInvalidInput", bad, err)
		}
	}

	all, err := f.s.ListTurnWikiLookups(ctx, []string{first.ID, second.ID})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range all {
		what := l.Path
		if l.Search() {
			what = l.Query
		}
		got = append(got, l.TurnID[:4]+" "+string(l.Scope)+" "+what)
	}
	// Each turn's in the order they were made; which turn comes first is
	// the store's.
	want := map[string][]string{
		first.ID:  {"project 审批 超时", "project /decisions/approvals.md", "project retry"},
		second.ID: {"library go tests", "library /skills/go-testing/SKILL.md"},
	}
	for id, lines := range want {
		var mine []string
		for _, l := range all {
			if l.TurnID == id {
				what := l.Path
				if l.Search() {
					what = l.Query
				}
				mine = append(mine, string(l.Scope)+" "+what)
			}
		}
		if len(mine) != len(lines) {
			t.Fatalf("turn %s: %q (all %q)", id, mine, got)
		}
		for i := range lines {
			if mine[i] != lines[i] {
				t.Errorf("turn %s: %q, want %q", id, mine, lines)
			}
		}
	}
	if none, err := f.s.ListTurnWikiLookups(ctx, nil); err != nil || len(none) != 0 {
		t.Errorf("no turns: %v %v", none, err)
	}

	use, err := f.s.WikiLookedUpSince(ctx, project, time.Now().Add(-time.Hour))
	if err != nil || !use.Used || use.Covered || len(use.Read) != 1 || !use.Read["/decisions/approvals.md"] {
		t.Errorf("the project wiki's reads, none kept from before: %+v %v", use, err)
	}
	if use, err := f.s.WikiLookedUpSince(ctx, project, time.Now().Add(time.Hour)); err != nil || use.Used || !use.Covered || len(use.Read) != 0 {
		t.Errorf("nothing since then, all kept from before: %+v %v", use, err)
	}
	// One kept from two hours ago covers the last hour.
	if err := f.s.Exec(ctx, "UPDATE wiki_lookups SET created_at = now() - interval '2 hours' WHERE query = 'retry'"); err != nil {
		t.Fatal(err)
	}
	if use, err := f.s.WikiLookedUpSince(ctx, project, time.Now().Add(-time.Hour)); err != nil || !use.Used || !use.Covered {
		t.Errorf("kept from before: %+v %v", use, err)
	}

	if n, err := f.s.PruneWikiLookups(ctx, time.Now().Add(-3*time.Hour)); err != nil || n != 0 {
		t.Errorf("nothing that old: %d %v", n, err)
	}
	if n, err := f.s.PruneWikiLookups(ctx, time.Now().Add(-time.Hour)); err != nil || n != 1 {
		t.Errorf("the old one pruned: %d %v", n, err)
	}
	if n, err := f.s.PruneWikiLookups(ctx, time.Now().Add(time.Hour)); err != nil || n != 4 {
		t.Errorf("pruned %d, %v", n, err)
	}
}
