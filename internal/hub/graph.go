package hub

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// A wiki as a graph (docs/design.md 5.17): its pages, the paths of the
// repository they name and the topics of the chat they came from, and how
// they bear on each other. The wiki package works out what the pages say;
// the hub adds what only it knows: the pages of mounted bundles that links
// lead to, the topics sources name, and which pages are due to be checked
// again.

// WikiGraph is a wiki as the relation graph shows it.
type WikiGraph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

// Kinds of node.
const (
	nodePage     = "page"
	nodeExternal = "external"
	nodeFile     = "file"
	nodeTopic    = "topic"
)

// Kinds of edge, beyond how pages bear on each other (wiki.EdgeKind).
const (
	// edgeNames runs from a page to a path of the repository it names.
	edgeNames = "names"
	// edgeFrom runs from a page to a topic it came from.
	edgeFrom = "from"
	// edgeContains runs from a directory to a path in it.
	edgeContains = "contains"
)

// GraphNode is one node of a wiki's graph. Its id is a page's path (a
// mounted page's as the project addresses it), "file:" and the path for a
// path of the repository, "topic:" and the thread for a topic.
type GraphNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Page is a page's, a mounted one's too, as lists show it.
	Page *WikiPageInfo `json:"page,omitempty"`
	// File is a path of the repository, the fullest way the pages write
	// it; Dir says other paths sit in it; Pages is how many pages name it.
	File  string `json:"file,omitempty"`
	Dir   bool   `json:"dir,omitempty"`
	Pages int    `json:"pages,omitempty"`
	// Topic is a topic's.
	Topic *GraphTopic `json:"topic,omitempty"`
}

// GraphTopic is a topic of the chat pages came from.
type GraphTopic struct {
	RoomID   string `json:"room_id"`
	ThreadID string `json:"thread_id"`
	Number   int    `json:"number"`
	// Title is what the topic's first message says, cut short.
	Title string `json:"title"`
}

// GraphEdge runs between two nodes, by their ids. Context is, for a link,
// the sentence it sits in, which says what the relation is; for a source,
// the source's title.
type GraphEdge struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Kind    string `json:"kind"`
	Context string `json:"context,omitempty"`
}

// graphStore is what a wiki's graph needs from the database: the topics
// sources name.
type graphStore interface {
	ListTurnTopics(ctx context.Context, turnIDs []string) ([]store.TopicRef, error)
	ListTopicsByNumber(ctx context.Context, roomID string, numbers []int) ([]store.TopicRef, error)
}

// graphSource is what putting a wiki's graph together reads: the topics,
// and which pages are due to be checked again.
type graphSource interface {
	graphStore
	reviewStore
}

// WikiGraph is a project's wiki as a graph.
func (h *Hub) WikiGraph(ctx context.Context, projectID string) (WikiGraph, error) {
	r, err := h.openWiki(ctx, projectID)
	if err != nil {
		return WikiGraph{}, err
	}
	return buildGraph(ctx, h.store, h.logger, r, h.wikis.mounts(ctx, r.project), h.now()), nil
}

// LibraryGraph is the skill library as a graph.
func (h *Hub) LibraryGraph(ctx context.Context) (WikiGraph, error) {
	r, err := h.openLibrary(ctx)
	if err != nil {
		return WikiGraph{}, err
	}
	return buildGraph(ctx, h.store, h.logger, r, nil, h.now()), nil
}

// buildGraph puts a wiki's graph together. In the skill library a skill's
// folder is one node, its SKILL.md: what links to or from its other pages
// links to or from the skill.
func buildGraph(ctx context.Context, st graphSource, logger *slog.Logger, r wikiRef, mounts []mountedWiki, now time.Time) WikiGraph {
	g := r.bundle.Graph()
	nodeOf := func(p string) string {
		if r.scope == store.WikiLibrary {
			if name := wiki.SkillOfFile(p); name != "" {
				return wiki.SkillPath(name)
			}
		}
		return p
	}
	b := &graphBuilder{out: WikiGraph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}, nodes: map[string]bool{}, edges: map[GraphEdge]bool{}}
	var pages []wiki.Summary
	for _, p := range g.Pages {
		if nodeOf(p.Path) == p.Path {
			pages = append(pages, p)
		}
	}
	infos := make([]WikiPageInfo, len(pages))
	for i, p := range pages {
		infos[i] = pageInfo(p, now)
	}
	markReviews(ctx, st, logger, r, pages, infos, now)
	for i := range infos {
		b.node(GraphNode{ID: infos[i].Path, Kind: nodePage, Page: &infos[i]})
	}
	for _, e := range g.Edges {
		from, to := nodeOf(e.From), nodeOf(e.To)
		if !b.nodes[to] && !b.external(mounts, to, now) {
			continue
		}
		b.edge(GraphEdge{From: from, To: to, Kind: string(e.Kind), Context: e.Context})
	}
	// The library is shared by every project, and its pages come from
	// every project's chat: a path it names is of no one repository, and
	// the topics are elsewhere. Only a project's wiki shows the paths of
	// its repository and the topics its pages came from. The wiki topic,
	// where the maintainer works, is not one: every page it writes names it.
	if r.scope != store.WikiProject {
		return b.out
	}
	for _, f := range g.Files {
		id := "file:" + f.Path
		b.node(GraphNode{ID: id, Kind: nodeFile, File: f.Path, Dir: f.Dir, Pages: len(f.Pages)})
		for _, p := range f.Pages {
			b.edge(GraphEdge{From: nodeOf(p), To: id, Kind: edgeNames})
		}
	}
	for _, f := range g.Files {
		if f.In != "" {
			b.edge(GraphEdge{From: "file:" + f.In, To: "file:" + f.Path, Kind: edgeContains})
		}
	}
	graphTopics(ctx, st, logger, b, g.Refs, r.project.WikiThreadID)
	return b.out
}

// graphBuilder gathers a graph's nodes and edges, each once.
type graphBuilder struct {
	out   WikiGraph
	nodes map[string]bool
	edges map[GraphEdge]bool
}

func (b *graphBuilder) node(n GraphNode) {
	if !b.nodes[n.ID] {
		b.nodes[n.ID] = true
		b.out.Nodes = append(b.out.Nodes, n)
	}
}

// edge adds an edge between two nodes there are, once: the same two
// nodes are joined once for each kind, with the first sentence found.
func (b *graphBuilder) edge(e GraphEdge) {
	key := GraphEdge{From: e.From, To: e.To, Kind: e.Kind}
	if e.From == e.To || !b.nodes[e.From] || !b.nodes[e.To] || b.edges[key] {
		return
	}
	b.edges[key] = true
	b.out.Edges = append(b.out.Edges, e)
}

// external adds the node of a mounted page a link leads to, reporting
// whether there is one: a link to a page nowhere to be found leads nowhere.
func (b *graphBuilder) external(mounts []mountedWiki, p string, now time.Time) bool {
	name, inner, ok := splitMount(p)
	if !ok {
		return false
	}
	m, err := mountNamed(mounts, name)
	if err != nil {
		return false
	}
	page, err := m.bundle.Page(inner)
	if err != nil {
		return false
	}
	info := mountedInfo(name, page.Summary, now)
	b.node(GraphNode{ID: p, Kind: nodeExternal, Page: &info})
	return true
}

// topics adds the topics the pages came from: those their sources name,
// and those the turns their sources name ran in. What cannot be looked up
// costs the graph its topics, not the graph.
func graphTopics(ctx context.Context, st graphStore, logger *slog.Logger, b *graphBuilder, refs []wiki.Ref, wikiThread string) {
	byTurn := map[string][]string{}           // turn -> pages
	byNumber := map[string]map[int][]string{} // room -> number -> pages
	for _, ref := range refs {
		rest, _ := strings.CutPrefix(ref.Resource, "veyloom://")
		parts := strings.Split(rest, "/")
		switch {
		case len(parts) == 2 && parts[0] == "turns":
			byTurn[parts[1]] = append(byTurn[parts[1]], ref.Page)
		case len(parts) == 4 && parts[0] == "rooms" && parts[2] == "topics":
			if n, err := strconv.Atoi(parts[3]); err == nil {
				if byNumber[parts[1]] == nil {
					byNumber[parts[1]] = map[int][]string{}
				}
				byNumber[parts[1]][n] = append(byNumber[parts[1]][n], ref.Page)
			}
		}
	}
	add := func(t store.TopicRef, pages []string) {
		if t.ThreadID == wikiThread {
			return
		}
		id := "topic:" + t.ThreadID
		b.node(GraphNode{ID: id, Kind: nodeTopic, Topic: &GraphTopic{
			RoomID: t.RoomID, ThreadID: t.ThreadID, Number: t.Number, Title: excerpt(topicTitleOf(t.RootBody), topicTitleExcerpt),
		}})
		for _, p := range pages {
			b.edge(GraphEdge{From: p, To: id, Kind: edgeFrom})
		}
	}
	if len(byTurn) > 0 {
		ids := make([]string, 0, len(byTurn))
		for id := range byTurn {
			ids = append(ids, id)
		}
		found, err := st.ListTurnTopics(ctx, ids)
		if err != nil {
			logger.Warn("graph: find the topics of turns", "err", err)
		}
		for _, t := range found {
			add(t, byTurn[t.TurnID])
		}
	}
	for room, numbers := range byNumber {
		ns := make([]int, 0, len(numbers))
		for n := range numbers {
			ns = append(ns, n)
		}
		found, err := st.ListTopicsByNumber(ctx, room, ns)
		if err != nil {
			logger.Warn("graph: find topics by number", "room", room, "err", err)
			continue
		}
		for _, t := range found {
			add(t, numbers[t.Number])
		}
	}
}
