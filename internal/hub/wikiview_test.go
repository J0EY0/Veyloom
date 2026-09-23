package hub

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

func TestLoop_TheWikiAsPeopleSeeIt(t *testing.T) {
	l, _ := wikiLoop(t)
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()
	writer := l.member("Writer", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, map[string]any{
			"type": "Fact", "slug": "port", "title": "The hub listens on 7788", "description": "Unless --addr says otherwise.",
			"body": "Set in [the config](/modules/config.md).", "topics": []any{1},
		}),
		call(runtime.WikiToolWrite, map[string]any{
			"type": "Module", "slug": "config", "title": "Config", "description": "Where settings come from.", "body": "See [the port](/facts/port.md).",
		}),
	}})
	asked := l.say("@Writer note the port", "", writer)
	turns := l.waitTurns(1, store.TurnDone, "Writer's turn")
	if ev := collectUntil(t, sub, EventWikiChanged); ev[len(ev)-1].ProjectID != l.project().ID {
		t.Errorf("the room hears the wiki changed: %+v", ev[len(ev)-1])
	}
	project, thread := l.project(), l.topic(asked)

	catalog, err := l.h.WikiCatalog(l.ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Pages) != 2 || len(catalog.Dirs) != 6 || catalog.Dirs[0].Type != "Decision" || !catalog.History {
		t.Errorf("catalog %+v", catalog)
	}
	if _, err := os.Stat(catalog.Folder); err != nil {
		t.Errorf("the folder is on disk: %v", err)
	}

	page, err := l.h.WikiPage(l.ctx, project.ID, "/facts/port.md")
	if err != nil {
		t.Fatal(err)
	}
	if page.Tier != "unverified" || page.VouchedAt != nil || page.Resident || page.GeneratedBy != "fake/default" || page.Body == "" {
		t.Errorf("page %+v", page.WikiPageInfo)
	}
	if len(page.Backlinks) != 1 || page.Backlinks[0] != (WikiLink{Path: "/modules/config.md", Title: "Config"}) {
		t.Errorf("backlinks %+v", page.Backlinks)
	}
	// Both sources the hub added lead back to the topic, the turn's to the turn too.
	if len(page.Sources) != 2 {
		t.Fatalf("sources %+v", page.Sources)
	}
	for _, s := range page.Sources {
		if s.ThreadID != thread.ID || s.RoomID != l.room.ID || s.TopicNumber != 1 {
			t.Errorf("source %+v should lead to topic #1", s)
		}
	}
	if i := slices.IndexFunc(page.Sources, func(s WikiSource) bool { return s.ID == "veyloom-turn" }); i < 0 || page.Sources[i].TurnID != turns[0].ID {
		t.Errorf("the turn's source names the turn: %+v", page.Sources)
	}
	if _, err := os.Stat(page.File); err != nil {
		t.Errorf("the page's file: %v", err)
	}

	// A person confirms it, then makes it resident.
	confirmed, err := l.h.VerifyWikiPage(l.ctx, project.ID, page.Path, l.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Tier != "human-reviewed" || confirmed.VouchedAt == nil || len(confirmed.Verified) != 1 || confirmed.Verified[0].By != "human:alice" {
		t.Errorf("confirmed %+v", confirmed)
	}
	collectUntil(t, sub, EventWikiChanged)
	resident, err := l.h.SetWikiResident(l.ctx, project.ID, page.Path, true, l.user.ID)
	if err != nil || !resident.Resident || !slices.Contains(resident.Tags, "resident") {
		t.Errorf("resident %+v %v", resident.WikiPageInfo, err)
	}

	history, err := l.h.WikiHistory(l.ctx, project.ID, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, c := range history {
		subjects = append(subjects, c.Subject)
	}
	if want := []string{"Resident: The hub listens on 7788", "Confirmed: The hub listens on 7788", "Writer in topic #1", "Set up the bundle"}; !slices.Equal(subjects, want) {
		t.Fatalf("history %q, want %q", subjects, want)
	}
	turn := history[2]
	if turn.Member != "Writer" || turn.TurnID != turns[0].ID || turn.ThreadID != thread.ID || turn.TopicNumber != 1 || !turn.Undoable ||
		len(turn.Changes) != 2 || turn.Changes[0].Kind != "Creation" || turn.Changes[0].Path != "/facts/port.md" {
		t.Errorf("the turn's commit %+v", turn)
	}
	if history[0].Author != "human:alice" || !history[0].Undoable || history[3].Undoable {
		t.Errorf("a person's commit can be undone, the setup cannot: %+v", history)
	}
	// A page written by hand may link from its own folder; the UI gets its
	// links from the wiki's root, and code as written.
	hand := "---\ntype: Topic\ntitle: By hand\n---\n\nSee [the port](../facts/port.md#top) and `[not](a.md)`.\n"
	if err := os.WriteFile(filepath.Join(catalog.Folder, "topics", "hand.md"), []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	if byHand, err := l.h.WikiPage(l.ctx, project.ID, "/topics/hand.md"); err != nil || byHand.Body != "See [the port](/facts/port.md) and `[not](a.md)`.\n" {
		t.Errorf("the hand-written page's body %q %v", byHand.Body, err)
	}
	if hits, err := l.h.SearchWiki(l.ctx, project.ID, "7788", 5); err != nil || len(hits) != 1 || hits[0].Path != page.Path {
		t.Errorf("search %+v %v", hits, err)
	}

	// Undoing: the turn's commit is under later changes to the same page;
	// the last change comes off cleanly.
	if _, err := l.h.RevertWiki(l.ctx, project.ID, turn.SHA, l.user.ID, ""); !errors.Is(err, store.ErrConflict) {
		t.Errorf("undoing under later changes: %v", err)
	}
	if _, err := l.h.RevertWiki(l.ctx, project.ID, history[0].SHA, l.user.ID, ""); err != nil {
		t.Fatal(err)
	}
	if again, _ := l.h.WikiPage(l.ctx, project.ID, page.Path); again.Resident || slices.Contains(again.Tags, "resident") {
		t.Errorf("undone, the page is resident no longer: %+v", again.WikiPageInfo)
	}
	if _, err := l.h.WikiPage(l.ctx, project.ID, "/facts/nope.md"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("an unknown page: %v", err)
	}
}
