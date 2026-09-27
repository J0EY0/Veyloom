package hub

import (
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// related_wiki walks the wiki out from a page, or from a path of the
// repository, telling how each page relates in the sentence that links it.
func TestLoop_RelatedWiki(t *testing.T) {
	l, _ := wikiLoop(t)
	page := func(typ, slug, title, body string) map[string]any {
		return call(runtime.WikiToolWrite, map[string]any{"type": typ, "slug": slug, "title": title, "description": title + ".", "body": body})
	}
	writer := l.member("Writer", nil)
	// Three topics, so what came from one topic is told apart.
	for i, calls := range [][]any{
		{
			page("Decision", "brief-shape", "Brief shape", "The brief is put together in internal/hub/brief.go, one section per part."),
			page("Fact", "brief-sections", "Brief sections", "Sections are written by internal/hub/brief.go too."),
		},
		{
			page("Pitfall", "empty-room", "Empty room", "An empty room once broke [the brief's shape](/decisions/brief-shape.md): check for it."),
			page("Module", "hub", "Hub", "internal/hub runs the chat."),
		},
		{page("Fact", "far", "Far", "See [the pitfall](/pitfalls/empty-room.md).")},
	} {
		l.setOptions(writer, map[string]any{"tool_calls": calls})
		l.say("@Writer 记下来", "", writer)
		l.waitTurns(i+1, store.TurnDone, "the writer's turn")
	}

	ask := func(args map[string]any) string {
		t.Helper()
		l.setOptions(writer, map[string]any{"tool_calls": []any{call(runtime.WikiToolRelated, args)}})
		asked := l.say("@Writer 看看关系", "", writer)
		eventually(t, func() bool {
			turns, _ := l.s.ListThreadTurns(l.ctx, l.topic(asked).ID)
			return len(turns) == 1 && turns[0].Status == store.TurnDone
		}, "the reader's turn")
		return l.root(l.topic(asked)).Body
	}

	got := ask(map[string]any{"path": "/decisions/brief-shape.md"})
	wantInOrder(t, got,
		`How /decisions/brief-shape.md "Brief shape" (Decision) bears on the rest of this project's wiki:`,
		"One step away:",
		`- /pitfalls/empty-room.md "Empty room" (Pitfall): it links to this page: "An empty room once broke [the brief's shape]: check for it."`,
		`- /facts/brief-sections.md "Brief sections" (Fact): it names internal/hub/brief.go, as this page does; it came from topic #1, as this page did`,
		"Read a page with read_wiki.",
	)
	if strings.Contains(got, "/facts/far.md") || strings.Contains(got, "/modules/hub.md") {
		t.Errorf("one step only:\n%s", got)
	}

	got = ask(map[string]any{"path": "/decisions/brief-shape.md", "depth": 2})
	wantInOrder(t, got,
		"Two steps away:",
		`- /facts/far.md "Far" (Fact): it links to /pitfalls/empty-room.md: "See [the pitfall]."`,
		`- /modules/hub.md "Hub" (Module): it came from topic #2, as /pitfalls/empty-room.md did`,
	)

	got = ask(map[string]any{"file": "internal/hub/brief.go"})
	wantInOrder(t, got,
		"Pages of this project's wiki naming internal/hub/brief.go:",
		`- /decisions/brief-shape.md "Brief shape" (Decision): it names internal/hub/brief.go`,
		`- /facts/brief-sections.md "Brief sections" (Fact): it names internal/hub/brief.go`,
		`- /modules/hub.md "Hub" (Module): it names internal/hub, which internal/hub/brief.go is in`,
		"One step from them:",
		`- /pitfalls/empty-room.md "Empty room" (Pitfall): it links to /decisions/brief-shape.md`,
	)

	for want, args := range map[string]map[string]any{
		"No page of this project's wiki names internal/unknown.go.": {"file": "internal/unknown.go"},
		"this project's wiki has no page /facts/nowhere.md":         {"path": "/facts/nowhere.md"},
		"give either path, a page of the wiki, or file":             {},
		"depth is 1 or 2": {"path": "/decisions/brief-shape.md", "depth": 3},
		"the skill library is shared by every project": {"file": "go.mod", "scope": "library"},
	} {
		if got := ask(args); !strings.Contains(got, want) {
			t.Errorf("%v: %q", args, got)
		}
	}
}

// The maintainer's health check names the pages that name the same path
// but do not link each other, and says what to do with them.
func TestUpkeepHealthPart_Unlinked(t *testing.T) {
	w := &briefWriter{}
	upkeepHealthPart(w, wiki.Health{Unlinked: []wiki.UnlinkedPair{{A: "/decisions/a.md", B: "/pitfalls/b.md", File: "internal/hub/brief.go"}}}, 4000)
	if got := w.sb.String(); !strings.Contains(got, "- Pages naming the same path of the repository that do not link each other: /decisions/a.md and /pitfalls/b.md both name internal/hub/brief.go\n") {
		t.Errorf("health:\n%s", got)
	}
	var steps briefWriter
	upkeepSteps(&steps, &upkeep{}, store.DefaultMemoryPrefs, upkeepFound{unhealthy: true})
	if !strings.Contains(steps.sb.String(), "Where two pages name the same path and one bears on the other, link it from the other") {
		t.Errorf("steps:\n%s", steps.sb.String())
	}
}
