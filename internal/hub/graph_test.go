package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// graphIndex is a graph by what tests look up: nodes by id, edges by kind,
// from and to.
type graphIndex struct {
	nodes map[string]GraphNode
	edges map[string]GraphEdge
}

func indexGraph(g WikiGraph) graphIndex {
	ix := graphIndex{nodes: map[string]GraphNode{}, edges: map[string]GraphEdge{}}
	for _, n := range g.Nodes {
		ix.nodes[n.ID] = n
	}
	for _, e := range g.Edges {
		ix.edges[e.Kind+" "+e.From+" > "+e.To] = e
	}
	return ix
}

// A project's wiki as a graph: its pages, and what they link to (a mounted
// page too), supersede, name and came from.
func TestHub_WikiGraph(t *testing.T) {
	l, _ := wikiLoop(t)
	l.mount(acmeRetail)
	margin := "/@acme-retail/metrics/gross-margin.md"
	writer := l.member("Writer", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, map[string]any{
			"type": "Module", "slug": "config", "title": "Config", "description": "Where settings come from.",
			"body": "Reads internal/config/load.go; the defaults are in internal/config/defaults.go.",
		}),
		call(runtime.WikiToolWrite, map[string]any{
			"type": "Fact", "slug": "port", "title": "Port", "description": "The port the hub listens on.", "topics": []any{1},
			"body": "The hub listens on 7788. The port is set in [the config](/modules/config.md). Margins are in [gross margin](" + margin + "). See internal/config/load.go.",
		}),
		call(runtime.WikiToolWrite, map[string]any{"type": "Decision", "slug": "old-port", "title": "Old port", "description": "7700.", "body": "We listened on 7700."}),
		call(runtime.WikiToolWrite, map[string]any{"type": "Decision", "slug": "new-port", "title": "New port", "description": "7788.", "body": "We listen on 7788."}),
		call(runtime.WikiToolDeprecate, map[string]any{"path": "/decisions/old-port.md", "successor": "/decisions/new-port.md", "reason": "moved"}),
	}})
	asked := l.say("@Writer 把端口和配置记下来", "", writer)
	l.waitTurns(1, store.TurnDone, "Writer's turn")
	topic := l.topic(asked)

	g, err := l.h.WikiGraph(l.ctx, l.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	ix := indexGraph(g)
	for _, id := range []string{"/modules/config.md", "/facts/port.md", "/decisions/old-port.md", "/decisions/new-port.md"} {
		if n := ix.nodes[id]; n.Kind != nodePage || n.Page == nil || n.Page.Path != id {
			t.Errorf("page %s: %+v", id, n)
		}
	}
	if n := ix.nodes["/decisions/old-port.md"]; n.Page == nil || n.Page.Status != "deprecated" {
		t.Errorf("a deprecated page is there, marked: %+v", n.Page)
	}
	if n := ix.nodes[margin]; n.Kind != nodeExternal || n.Page == nil || n.Page.Mount != "acme-retail" || n.Page.Title == "" {
		t.Errorf("the mounted page linked to: %+v", n)
	}
	if n := ix.nodes["file:internal/config/load.go"]; n.Kind != nodeFile || n.Pages != 2 {
		t.Errorf("a file two pages name: %+v", n)
	}
	id := "topic:" + topic.ID
	// Titled like every topic, by the message it hangs from.
	title := excerpt(topicTitleOf(l.root(topic).Body), topicTitleExcerpt)
	if n := ix.nodes[id]; n.Kind != nodeTopic || n.Topic == nil || n.Topic.Number != topic.Number || n.Topic.RoomID != l.room.ID || n.Topic.Title != title {
		t.Errorf("the topic the pages came from: %+v, want the title %q", n.Topic, title)
	}
	if e, ok := ix.edges["link /facts/port.md > /modules/config.md"]; !ok || e.Context != "The port is set in [the config]." {
		t.Errorf("the link and its sentence: %+v", e)
	}
	for _, want := range []string{
		"link /facts/port.md > " + margin,
		"supersedes /decisions/new-port.md > /decisions/old-port.md",
		"names /modules/config.md > file:internal/config/load.go",
		"names /facts/port.md > file:internal/config/load.go",
		// Came from the topic both by the turn that wrote them and, for the
		// fact, by its topics: one edge.
		"from /facts/port.md > " + id,
		"from /modules/config.md > " + id,
	} {
		if _, ok := ix.edges[want]; !ok {
			t.Errorf("no edge %s in %+v", want, g.Edges)
		}
	}
	for key := range ix.edges {
		if strings.HasPrefix(key, "link /decisions/") {
			t.Errorf("the lines a deprecation writes are no plain links: %s", key)
		}
	}
}

// In the skill library a skill's folder is one node: what its other pages
// link to, the skill links to.
func TestHub_LibraryGraph(t *testing.T) {
	l, _ := wikiLoop(t)
	lib, err := l.h.wikis.library(l.ctx)
	if err != nil {
		t.Fatal(err)
	}
	w, err := lib.Writer("human:alice")
	if err != nil {
		t.Fatal(err)
	}
	pattern := okf.New("Pattern")
	pattern.SetString(okf.KeyTitle, "Copy-pasted tests")
	pattern.SetString(okf.KeyDescription, "Tests that repeat each other.")
	pattern.SetBody("Fold them into a table, as [the skill](/skills/table-tests/SKILL.md) does, in internal/hub/brief_test.go and `go.mod`'s module.")
	if _, err := w.Create("/patterns/copy-paste-tests.md", pattern); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(l.ctx, "test"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "table-tests")
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, text := range map[string]string{
		"SKILL.md":            "---\nname: table-tests\ndescription: Write Go tests as tables.\n---\n\nOne table per behaviour, as in internal/hub/brief_test.go.\n",
		"references/usage.md": "---\ntype: Reference\ntitle: Usage\ndescription: When to use it.\n---\n\nWhen tests repeat, see [the pattern](/patterns/copy-paste-tests.md).\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, p), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := l.h.ImportSkill(l.ctx, dir, "", l.user.ID); err != nil {
		t.Fatal(err)
	}

	g, err := l.h.LibraryGraph(l.ctx)
	if err != nil {
		t.Fatal(err)
	}
	ix := indexGraph(g)
	skill := wiki.SkillPath("table-tests")
	if _, ok := ix.nodes["/skills/table-tests/references/usage.md"]; ok {
		t.Error("a page of a skill's folder is the skill's")
	}
	if n := ix.nodes[skill]; n.Kind != nodePage {
		t.Errorf("the skill: %+v", n)
	}
	for _, want := range []string{"link /patterns/copy-paste-tests.md > " + skill, "link " + skill + " > /patterns/copy-paste-tests.md"} {
		if _, ok := ix.edges[want]; !ok {
			t.Errorf("no edge %s in %+v", want, g.Edges)
		}
	}
	// Both name internal/hub/brief_test.go, of whichever repository: the
	// library is every project's.
	for _, n := range g.Nodes {
		if n.Kind == nodeTopic || n.Kind == nodeFile {
			t.Errorf("the library's pages come from every project's chat, and name no topic nor path of one repository: %+v", n)
		}
	}
}
