package wiki

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// graphPage writes a page of the given type, with sources.
func graphPage(t *testing.T, w *Writer, p, typ, title, body string, sources ...okf.Source) {
	t.Helper()
	d := okf.New(typ)
	d.SetString(okf.KeyTitle, title)
	d.SetString(okf.KeyDescription, title+".")
	d.SetBody(body)
	for _, s := range sources {
		d.AddSource(s)
	}
	if _, err := w.Create(p, d); err != nil {
		t.Fatal(err)
	}
}

func TestBundle_Graph(t *testing.T) {
	b := openTest(t, Options{Layout: ProjectLayout})
	w := writer(t, b, "codex/default")
	graphPage(t, w, "/modules/config.md", "Module", "Config", "Reads `internal/config/load.go`; the defaults are in internal/config/defaults.go.")
	graphPage(t, w, "/facts/port.md", "Fact", "Port",
		"The hub listens on 7788. The port is set in [the config](/modules/config.md), see also [shared](/@acme/metrics/x.md). "+
			"A link [gone](/facts/nowhere.md) and a picture ![map](/files/port/map.png).\n\n"+
			"```\n[not a link](/modules/config.md)\n```\n"+
			"Its code is in hub/brief.go.",
		okf.Source{ID: "cfg", Resource: "/modules/config.md", Title: "The config page"},
		okf.Source{ID: "veyloom-turn", Resource: "veyloom://turns/t1", Title: "Pi in topic #3"},
		okf.Source{ID: "web", Resource: "https://example.com/ports", Title: "Ports"})
	graphPage(t, w, "/decisions/old-port.md", "Decision", "Old port", "We listen on 7700; see [the config](/modules/config.md).")
	graphPage(t, w, "/decisions/new-port.md", "Decision", "New port", "We listen on 7788, set in internal/hub/brief.go.")
	if _, err := w.Deprecate("/decisions/old-port.md", "/decisions/new-port.md", "moved"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.PutMemory("Project memory", "How to work here.", []MemoryEntry{{Text: "See [the config](/modules/config.md)."}}, ""); err != nil {
		t.Fatal(err)
	}

	g := b.Graph()
	var pages []string
	for _, p := range g.Pages {
		pages = append(pages, p.Path)
	}
	// Every page but the memory, deprecated ones too.
	if want := []string{"/decisions/new-port.md", "/decisions/old-port.md", "/facts/port.md", "/modules/config.md"}; !slices.Equal(pages, want) {
		t.Errorf("pages %q, want %q", pages, want)
	}
	edges := map[string]Edge{}
	for _, e := range g.Edges {
		edges[string(e.Kind)+" "+e.From+" > "+e.To] = e
	}
	for _, want := range []string{
		"link /facts/port.md > /modules/config.md",
		// Kept for the caller: a mounted page, and one that is nowhere.
		"link /facts/port.md > /@acme/metrics/x.md",
		"link /facts/port.md > /facts/nowhere.md",
		"source /facts/port.md > /modules/config.md",
		"link /decisions/old-port.md > /modules/config.md",
		// Written once each way by Deprecate, one edge from the newer.
		"supersedes /decisions/new-port.md > /decisions/old-port.md",
	} {
		if _, ok := edges[want]; !ok {
			t.Errorf("no edge %s in %v", want, g.Edges)
		}
	}
	if len(edges) != 6 {
		t.Errorf("%d edges, want 6: pictures, links in code and the memory's links are none: %v", len(edges), g.Edges)
	}
	if e := edges["link /facts/port.md > /modules/config.md"]; e.Context != "The port is set in [the config], see also [shared]." {
		t.Errorf("the sentence the link sits in: %q", e.Context)
	}
	if e := edges["source /facts/port.md > /modules/config.md"]; e.Context != "The config page" {
		t.Errorf("a source's title: %q", e.Context)
	}
	if want := []Ref{{Page: "/facts/port.md", Resource: "veyloom://turns/t1"}}; !slices.Equal(g.Refs, want) {
		t.Errorf("refs %+v", g.Refs)
	}

	files := map[string]File{}
	for _, f := range g.Files {
		files[f.Path] = f
	}
	// hub/brief.go is internal/hub/brief.go, which another page names in full.
	if f := files["internal/hub/brief.go"]; !slices.Equal(f.Pages, []string{"/decisions/new-port.md", "/facts/port.md"}) {
		t.Errorf("one file however it is written: %+v in %+v", f, g.Files)
	}
	if _, ok := files["hub/brief.go"]; ok {
		t.Errorf("the short way stays a way of writing it: %+v", g.Files)
	}
	if f := files["internal/config/load.go"]; f.Dir || !slices.Equal(f.Pages, []string{"/modules/config.md"}) {
		t.Errorf("a file: %+v", f)
	}
	if _, ok := files["/files/port/map.png"]; ok {
		t.Errorf("a file the wiki keeps is none of the repository's: %+v", g.Files)
	}
}

func TestNamedFiles_Directories(t *testing.T) {
	files := namedFiles([]Summary{
		{Path: "/modules/hub.md", Mentions: []string{"internal/hub", "internal"}},
		{Path: "/facts/brief.md", Mentions: []string{"internal/hub/brief.go", "hub/turns.go"}},
	})
	got := map[string]File{}
	for _, f := range files {
		got[f.Path] = f
	}
	// A path other paths sit in is a directory; each sits in the nearest.
	if f := got["internal/hub"]; !f.Dir || f.In != "internal" {
		t.Errorf("internal/hub: %+v", f)
	}
	if f := got["internal/hub/brief.go"]; f.Dir || f.In != "internal/hub" {
		t.Errorf("brief.go: %+v", f)
	}
	// Named from further in, turns.go sits in the directory named whole.
	if f := got["hub/turns.go"]; f.In != "internal/hub" {
		t.Errorf("turns.go: %+v", f)
	}
	if f := got["internal"]; !f.Dir || f.In != "" {
		t.Errorf("internal: %+v", f)
	}
}

func TestSentenceAt(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"- 端口在 [配置](/modules/config.md) 里改；别处不写死。", "端口在 [配置] 里改；"},
		{"> First. It depends on [the queue](/modules/queue.md)[^q]! Then more.", "It depends on [the queue]!"},
		{"## See [the config](/modules/config.md)", "See [the config]"},
		{"Fixtures sit in `testdata/`, **apart** from [the pitfall](/p.md).", "Fixtures sit in testdata/, apart from [the pitfall]."},
		{strings.Repeat("word ", 60) + "then [the target](/a.md) " + strings.Repeat("more ", 60), ""},
	} {
		links := okf.Links(tc.body)
		got := sentenceAt(tc.body, links[0])
		if tc.want == "" {
			if n := len([]rune(got)); n > contextMax+2 || !strings.Contains(got, "[the target]") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
				t.Errorf("a long sentence is cut about the link: %d %q", n, got)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("sentenceAt(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}

// BenchmarkBundle_Graph builds the graph of a wiki of 500 pages, each
// linking to three others and naming five paths.
func BenchmarkBundle_Graph(b *testing.B) {
	bundle, err := Open(b.Context(), b.TempDir(), Options{Layout: ProjectLayout})
	if err != nil {
		b.Fatal(err)
	}
	w, err := bundle.Writer("codex/default")
	if err != nil {
		b.Fatal(err)
	}
	for i := range 500 {
		var body strings.Builder
		for j := 1; j <= 3; j++ {
			fmt.Fprintf(&body, "It depends on [page %d](/facts/p%d.md). ", (i+j)%500, (i+j)%500)
		}
		for j := range 5 {
			fmt.Fprintf(&body, "See internal/pkg%d/file%d.go. ", (i+j)%40, j)
		}
		d := okf.New("Fact")
		d.SetString(okf.KeyTitle, fmt.Sprintf("Page %d", i))
		d.SetString(okf.KeyDescription, "A page.")
		d.SetBody(body.String())
		if _, err := w.Create(fmt.Sprintf("/facts/p%d.md", i), d); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for b.Loop() {
		if g := bundle.Graph(); len(g.Pages) != 500 || len(g.Edges) != 1500 {
			b.Fatalf("%d pages, %d edges", len(g.Pages), len(g.Edges))
		}
	}
}
