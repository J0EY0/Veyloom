package hub

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// The searches that found nothing, folded from the lookups of the turns an
// upkeep goes over (docs/design.md 5.23.7).
func TestMissesIn(t *testing.T) {
	var id int64
	search := func(turn string, scope store.WikiScope, query string, hits int) store.WikiLookup {
		id++
		return store.WikiLookup{ID: id, TurnID: turn, Scope: scope, Query: query, Hits: hits}
	}
	read := func(turn string, scope store.WikiScope, path string) store.WikiLookup {
		id++
		return store.WikiLookup{ID: id, TurnID: turn, Scope: scope, Path: path}
	}
	lookups := []store.WikiLookup{
		search("A", store.WikiProject, "Approval timeout", 0),
		search("A", store.WikiProject, "z", 0),
		read("A", store.WikiProject, "/p.md"),
		// The same words again, spaced and cased otherwise: one search.
		search("A", store.WikiProject, " approval   TIMEOUT ", 0),
		// A search that found something is no miss, and reads after it
		// still count for the one before.
		search("A", store.WikiProject, "ok", 3),
		read("A", store.WikiProject, "/q.md"),
		search("B", store.WikiProject, "approval timeout", 0),
		read("B", store.WikiLibrary, "/skills/s/SKILL.md"),
		// The same words in the other wiki are another search.
		search("C", store.WikiLibrary, "approval timeout", 0),
		// A read in another turn is not what this one went on to find,
		// even between its lookups, turns running at once.
		search("D", store.WikiProject, "never found", 0),
		read("E", store.WikiProject, "/elsewhere.md"),
		search("F", store.WikiProject, "interleaved", 0),
		read("G", store.WikiProject, "/g.md"),
		read("F", store.WikiProject, "/f.md"),
	}
	var got []string
	for _, miss := range missesIn(lookups) {
		got = append(got, fmt.Sprintf("%s|%s|%s|%s", miss.query, miss.scope, strings.Join(miss.turns, ","), strings.Join(miss.then, ",")))
	}
	want := []string{
		"Approval timeout|project|A,B|/p.md,/q.md,/skills/s/SKILL.md (the skill library)",
		"z|project|A|/p.md",
		"approval timeout|library|C|",
		"never found|project|D|",
		"interleaved|project|F|/f.md",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("misses:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if len(missesIn(nil)) != 0 || len(missesIn([]store.WikiLookup{search("A", store.WikiProject, "found", 1)})) != 0 {
		t.Error("nothing missed")
	}
}

func TestMissesPart(t *testing.T) {
	w := &briefWriter{}
	turns := []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7"}
	missesPart(w, []searchMiss{
		{query: "审批 超时", scope: store.WikiProject, turns: turns, then: []string{"/facts/approvals.md"}},
		{query: "retry", scope: store.WikiLibrary, turns: []string{"t1"}},
	}, 17)
	for _, want := range []string{
		"Searches that found nothing as written (17; these 2 first, asked most first):",
		`- "审批 超时" in the project wiki, by turns t1, t2, t3, t4, t5 and 2 more; then read /facts/approvals.md`,
		`- "retry" in the skill library, by turn t1; read nothing after`,
	} {
		if !strings.Contains(w.sb.String(), want) {
			t.Errorf("want %q in:\n%s", want, w.sb.String())
		}
	}
	empty := &briefWriter{}
	missesPart(empty, nil, 0)
	unreadPart(empty, nil, 0, time.Now())
	if empty.sb.Len() != 0 {
		t.Errorf("nothing to list says nothing: %q", empty.sb.String())
	}
}

func TestUnreadPart(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.Local)
	w := &briefWriter{}
	unreadPart(w, []wiki.Summary{{
		Path: "/facts/ci-cache.md", Title: "The CI cache", Type: "Fact", Description: "Keyed by go.sum.", Status: okf.Stable,
		Generated: okf.Stamp{At: now.Add(-45 * 24 * time.Hour)},
	}}, 1, now)
	for _, want := range []string{
		"Pages no turn has read in 30 days (1, the longest unchecked first):",
		`- /facts/ci-cache.md "The CI cache" (Fact), last checked 2026-08-13 (45 days ago): Keyed by go.sum.`,
	} {
		if !strings.Contains(w.sb.String(), want) {
			t.Errorf("want %q in:\n%s", want, w.sb.String())
		}
	}
}
