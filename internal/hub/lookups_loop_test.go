package hub

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// What the chat's turns looked up in the wiki reaches the maintainer
// (docs/design.md 5.23.7): the searches that found nothing, with what their
// turns read next, and the pages no turn has read in a month.
func TestLoop_TheMaintainerSeesWhatTheChatLookedUp(t *testing.T) {
	// The hub's clock runs ahead by as much as the test says.
	var ahead atomic.Int64
	l := newLoopWith(t, Config{WikiDir: t.TempDir()}, WithClock(func() time.Time { return time.Now().Add(time.Duration(ahead.Load())) }))
	scribe := l.member("Scribe", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, fact("approval-timeouts", "Approval timeouts", "An approval waits ten minutes.")),
		call(runtime.WikiToolWrite, fact("staging-db-port", "Staging database port", "6544.")),
		call(runtime.WikiToolWrite, fact("ci-cache", "The CI cache", "Keyed by go.sum.")),
	}})
	l.say("@Scribe write these down", "", scribe)
	written := l.waitTurns(1, store.TurnDone, "the pages written")[0]
	// A month and more later, lookups kept all along.
	ahead.Store(int64(40 * 24 * time.Hour))
	l.keptLookupsFrom(written, 40*24*time.Hour)

	coder := l.member("Coder", map[string]any{"reply": "Ten minutes.", "tool_calls": []any{
		call(runtime.WikiToolSearch, map[string]any{"query": "审批 超时"}),
		call(runtime.WikiToolSearch, map[string]any{"query": "approval  TIMEOUTS"}),
		call(runtime.WikiToolRead, map[string]any{"path": "/facts/approval-timeouts.md"}),
		call(runtime.WikiToolSearch, map[string]any{"query": "flaky tests"}),
		call(runtime.WikiToolSearch, map[string]any{"scope": "library", "query": "no such skill"}),
	}})
	l.say("@Coder how long does an approval wait?", "", coder)
	first := l.waitTurns(2, store.TurnDone, "Coder's turn")[0]
	l.setOptions(coder, map[string]any{"reply": "Still ten.", "tool_calls": []any{
		call(runtime.WikiToolSearch, map[string]any{"query": "审批   超时"}),
	}})
	l.say("@Coder and now?", "", coder)
	second := l.waitTurns(3, store.TurnDone, "Coder's second turn")[0]

	lookups, err := l.s.ListTurnWikiLookups(l.ctx, []string{first.ID})
	if err != nil || len(lookups) != 5 || lookups[1].Hits == 0 || lookups[2].Path != "/facts/approval-timeouts.md" || lookups[4].Scope != store.WikiLibrary {
		t.Fatalf("what the first turn looked up: %+v %v", lookups, err)
	}

	keeper := l.member("Keeper", map[string]any{"reply": "Kept.", "tool_calls": []any{
		call(runtime.WikiToolSearch, map[string]any{"query": "nothing like it"}),
	}})
	l.keep(keeper, store.UpkeepManual)
	if _, err := l.h.StartUpkeep(l.ctx, l.room.ProjectID); err != nil {
		t.Fatal(err)
	}
	upkeep := l.upkeeps(1)[0]
	brief := specOf(t, upkeep).Prompt
	wantInOrder(t, brief,
		"Go over the \"Searches that found nothing as written\"",
		"Look at the \"Pages no turn has read\"",
		"Searches that found nothing as written (3, asked most first):",
		`- "审批 超时" in the project wiki, by turns `+first.ID+", "+second.ID+"; then read /facts/approval-timeouts.md",
		`- "flaky tests" in the project wiki, by turn `+first.ID+"; read nothing after",
		`- "no such skill" in the skill library, by turn `+first.ID+"; read nothing after",
		"Pages no turn has read in 30 days (2, the longest unchecked first):",
	)
	unread := brief[strings.Index(brief, "Pages no turn has read in 30 days"):]
	unread = unread[:strings.Index(unread, "\n\n")+1]
	if !strings.Contains(unread, "/facts/staging-db-port.md") || !strings.Contains(unread, "/facts/ci-cache.md") || strings.Contains(unread, "approval-timeouts") {
		t.Errorf("the pages no turn read, not the one read:\n%s", unread)
	}
	if strings.Contains(brief, "approval  TIMEOUTS") {
		t.Error("a search that found something is listed")
	}
	// The maintainer looks the wiki up to keep it, not to use it.
	if mine, err := l.s.ListTurnWikiLookups(l.ctx, []string{upkeep.ID}); err != nil || len(mine) != 0 {
		t.Errorf("the upkeep's lookups were kept: %+v %v", mine, err)
	}
}

// keptLookupsFrom has lookups kept from as long ago as ago, as if the
// hub had kept them for so long: one search of turn's that found a page,
// which no upkeep lists.
func (l *loop) keptLookupsFrom(turn store.Turn, ago time.Duration) {
	l.t.Helper()
	if err := l.s.RecordWikiLookup(l.ctx, store.WikiLookup{ProjectID: l.room.ProjectID, TurnID: turn.ID, Scope: store.WikiLibrary, Query: "how to write a fact", Hits: 1}); err != nil {
		l.t.Fatal(err)
	}
	if err := l.s.Exec(l.ctx, "UPDATE wiki_lookups SET created_at = now() - $1::interval WHERE turn_id = $2", fmt.Sprintf("%d seconds", int(ago.Seconds())), turn.ID); err != nil {
		l.t.Fatal(err)
	}
}

// Lookups kept for less than the month, a page no turn read in it may have
// been read before they were kept: none is listed as unread.
func TestLoop_PagesGoUnreadOnlyOnceLookupsCoverTheMonth(t *testing.T) {
	var ahead atomic.Int64
	l := newLoopWith(t, Config{WikiDir: t.TempDir()}, WithClock(func() time.Time { return time.Now().Add(time.Duration(ahead.Load())) }))
	scribe := l.member("Scribe", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, fact("ci-cache", "The CI cache", "Keyed by go.sum.")),
	}})
	l.say("@Scribe write it down", "", scribe)
	l.waitTurns(1, store.TurnDone, "the page written")
	ahead.Store(int64(40 * 24 * time.Hour))
	coder := l.member("Coder", map[string]any{"reply": "No idea.", "tool_calls": []any{
		call(runtime.WikiToolSearch, map[string]any{"query": "flaky tests"}),
	}})
	l.say("@Coder why are the tests flaky?", "", coder)
	l.waitTurns(2, store.TurnDone, "Coder's turn")
	keeper := l.member("Keeper", map[string]any{"reply": "Kept."})
	l.keep(keeper, store.UpkeepManual)
	if _, err := l.h.StartUpkeep(l.ctx, l.room.ProjectID); err != nil {
		t.Fatal(err)
	}
	brief := specOf(t, l.upkeeps(1)[0]).Prompt
	if !strings.Contains(brief, `- "flaky tests" in the project wiki`) || strings.Contains(brief, "Pages no turn has read") {
		t.Errorf("the search that found nothing, and no page unread:\n%s", brief)
	}
}

// Nobody using the wiki, no page stands out for going unread.
func TestLoop_AWikiNobodyUsesListsNoPageAsUnread(t *testing.T) {
	var ahead atomic.Int64
	l := newLoopWith(t, Config{WikiDir: t.TempDir()}, WithClock(func() time.Time { return time.Now().Add(time.Duration(ahead.Load())) }))
	scribe := l.member("Scribe", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, fact("ci-cache", "The CI cache", "Keyed by go.sum.")),
	}})
	l.say("@Scribe write it down", "", scribe)
	l.waitTurns(1, store.TurnDone, "the page written")
	ahead.Store(int64(40 * 24 * time.Hour))
	keeper := l.member("Keeper", map[string]any{"reply": "Kept."})
	l.keep(keeper, store.UpkeepManual)
	if _, err := l.h.StartUpkeep(l.ctx, l.room.ProjectID); err != nil {
		t.Fatal(err)
	}
	brief := specOf(t, l.upkeeps(1)[0]).Prompt
	if strings.Contains(brief, "Pages no turn has read") || strings.Contains(brief, "Searches that found nothing") {
		t.Errorf("nothing to say of the wiki's use:\n%s", brief)
	}
}
