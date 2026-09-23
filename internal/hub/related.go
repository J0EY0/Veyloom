package hub

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// related_wiki (docs/design.md 5.17): how a wiki's pages bear on each other,
// walked out from a page or from a path of the repository, and told the way
// an agent reads it. A link is told with the sentence it sits in, which is
// where the relation's kind is written; every relation names the page it
// is told from.

const (
	// relatedMax caps the pages told.
	relatedMax = 40
	// relatedCommon is how many pages may name a path, or come from a
	// topic, for that to relate them: a path many pages name says little of
	// any two of them.
	relatedCommon = 12
)

// relatedWiki answers related_wiki from the turn's wiki.
func (m *TurnManager) relatedWiki(ctx context.Context, tw *turnWiki, args wikiArgs, mounts []mountedWiki) (string, error) {
	start, file := strings.TrimSpace(args.Path), strings.TrimSpace(args.File)
	if (start == "") == (file == "") {
		return "", errors.New("give either path, a page of the wiki, or file, a path of the repository")
	}
	depth := args.Depth
	if depth == 0 {
		depth = 1
	}
	if depth < 1 || depth > 2 {
		return "", errors.New("depth is 1 or 2")
	}
	r := wikiRef{scope: tw.scope, project: tw.project, bundle: tw.bundle}
	if file != "" && r.scope == store.WikiLibrary {
		return "", errors.New("the skill library is shared by every project, so a path of one repository leads nowhere in it: give path, a page of the library")
	}
	rs := newRelations(buildGraph(ctx, m.store, m.logger, r, mounts, m.wikis.now()))
	if file != "" {
		return rs.aroundFile(file, depth, tw.what()), nil
	}
	if p, err := wiki.CleanPath(start); err == nil {
		start = p
	}
	if name := wiki.SkillOfFile(start); name != "" && r.scope == store.WikiLibrary {
		// A page of a skill's folder is the skill's.
		start = wiki.SkillPath(name)
	}
	if n, ok := rs.nodes[start]; !ok || n.Kind != nodePage {
		return "", fmt.Errorf("%s has no page %s: search_wiki finds pages by their words", tw.what(), start)
	}
	return rs.aroundPage(start, depth, tw.what()), nil
}

// relations is a wiki's graph as walked from a page.
type relations struct {
	nodes map[string]GraphNode
	out   map[string][]GraphEdge
	in    map[string][]GraphEdge
}

func newRelations(g WikiGraph) *relations {
	rs := &relations{nodes: map[string]GraphNode{}, out: map[string][]GraphEdge{}, in: map[string][]GraphEdge{}}
	for _, n := range g.Nodes {
		rs.nodes[n.ID] = n
	}
	for _, e := range g.Edges {
		rs.out[e.From] = append(rs.out[e.From], e)
		rs.in[e.To] = append(rs.in[e.To], e)
	}
	return rs
}

// related is a page, with how it relates to the ones it was reached from.
type related struct {
	page string
	why  []string
	// rank orders them: an edge between the two first, then a path both
	// name, then a topic both came from.
	rank int
}

// step lists the pages one step from page, self being how that page is
// named in what is told: "this page", or its path.
func (rs *relations) step(page, self string) []related {
	var out []related
	at := map[string]int{}
	note := func(p string, rank int, why string) {
		if n := rs.nodes[p]; p == page || (n.Kind != nodePage && n.Kind != nodeExternal) {
			return
		}
		i, ok := at[p]
		if !ok {
			i = len(out)
			at[p] = i
			out = append(out, related{page: p, rank: rank})
		}
		if !slices.Contains(out[i].why, why) {
			out[i].why = append(out[i].why, why)
		}
		out[i].rank = min(out[i].rank, rank)
	}
	for _, e := range rs.out[page] {
		switch e.Kind {
		case string(wiki.EdgeLink):
			note(e.To, 0, self+" links to it"+quoted(e.Context))
		case string(wiki.EdgeSupersedes):
			note(e.To, 0, self+" supersedes it")
		case string(wiki.EdgeSource):
			note(e.To, 0, self+" rests on it as a source")
		case edgeNames:
			if f := rs.nodes[e.To]; f.Pages <= relatedCommon {
				for _, other := range rs.in[e.To] {
					note(other.From, 1, fmt.Sprintf("it names %s, as %s does", f.File, self))
				}
			}
		case edgeFrom:
			if t := rs.nodes[e.To].Topic; t != nil && len(rs.in[e.To]) <= relatedCommon {
				for _, other := range rs.in[e.To] {
					note(other.From, 2, fmt.Sprintf("it came from topic #%d, as %s did", t.Number, self))
				}
			}
		}
	}
	for _, e := range rs.in[page] {
		switch e.Kind {
		case string(wiki.EdgeLink):
			note(e.From, 0, "it links to "+self+quoted(e.Context))
		case string(wiki.EdgeSupersedes):
			note(e.From, 0, "it supersedes "+self)
		case string(wiki.EdgeSource):
			note(e.From, 0, "it rests on "+self+" as a source")
		}
	}
	return out
}

// quoted is a link's sentence as told after the relation, if it has one.
func quoted(sentence string) string {
	if sentence == "" {
		return ""
	}
	return fmt.Sprintf(": %q", sentence)
}

// aroundPage tells the pages up to depth steps from a page.
func (rs *relations) aroundPage(page string, depth int, what string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "How %s bears on the rest of %s:\n", rs.label(page), what)
	told := map[string]bool{page: true}
	left := relatedMax
	first := rs.tell(&sb, "One step away:", sortRelated(rs.step(page, "this page")), told, &left)
	if len(first) == 0 {
		sb.WriteString("Nothing in the wiki links to or from it, supersedes it or rests on it, and no other page names the paths it names or came from its topics.\n")
		return sb.String()
	}
	if depth > 1 {
		rs.onward(&sb, "Two steps away:", first, told, &left)
	}
	sb.WriteString("Read a page with read_wiki.\n")
	return sb.String()
}

// aroundFile tells the pages that name a path of the repository, or a
// directory it is in, or a path in it, and the pages up to depth steps from
// them.
func (rs *relations) aroundFile(file string, depth int, what string) string {
	file = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(file), "./"), "/")
	var naming []related
	for id, n := range rs.nodes {
		if n.Kind != nodeFile || !(wiki.NamesFile(n.File, file) || wiki.NamesFile(file, n.File)) {
			continue
		}
		how := "it names " + n.File
		switch {
		case n.File == file || strings.HasSuffix(n.File, "/"+file) || strings.HasSuffix(file, "/"+n.File):
		case wiki.NamesFile(n.File, file):
			how += ", which " + file + " is in"
		default:
			how += ", which is in " + file
		}
		for _, e := range rs.in[id] {
			if e.Kind == edgeNames {
				naming = append(naming, related{page: e.From, why: []string{how}})
			}
		}
	}
	var sb strings.Builder
	if len(naming) == 0 {
		fmt.Fprintf(&sb, "No page of %s names %s. search_wiki finds pages by their words.\n", what, file)
		return sb.String()
	}
	told := map[string]bool{}
	left := relatedMax
	pages := rs.tell(&sb, fmt.Sprintf("Pages of %s naming %s:", what, file), sortRelated(naming), told, &left)
	next := rs.onward(&sb, "One step from them:", pages, told, &left)
	if depth > 1 {
		rs.onward(&sb, "Two steps from them:", next, told, &left)
	}
	sb.WriteString("Read a page with read_wiki.\n")
	return sb.String()
}

// onward tells, under heading, the pages one step on from those given that
// are not told yet, and returns them.
func (rs *relations) onward(sb *strings.Builder, heading string, from []string, told map[string]bool, left *int) []string {
	var next []related
	for _, p := range from {
		for _, r := range rs.step(p, p) {
			if !told[r.page] {
				next = append(next, r)
			}
		}
	}
	return rs.tell(sb, heading, sortRelated(next), told, left)
}

// tell writes, under heading, the pages not told yet, up to left of them,
// and returns them.
func (rs *relations) tell(sb *strings.Builder, heading string, pages []related, told map[string]bool, left *int) []string {
	var fresh []related
	for _, r := range pages {
		if !told[r.page] {
			fresh = append(fresh, r)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	sb.WriteString(heading + "\n")
	var shown []string
	for i, r := range fresh {
		if *left <= 0 {
			fmt.Fprintf(sb, "(%d more not listed.)\n", len(fresh)-i)
			break
		}
		told[r.page] = true
		*left--
		shown = append(shown, r.page)
		fmt.Fprintf(sb, "- %s: %s\n", rs.label(r.page), strings.Join(r.why, "; "))
	}
	return shown
}

// sortRelated gathers what is told of each page into one and orders them:
// the closer relation first, then by path.
func sortRelated(rs []related) []related {
	var out []related
	at := map[string]int{}
	for _, r := range rs {
		i, ok := at[r.page]
		if !ok {
			at[r.page] = len(out)
			out = append(out, related{page: r.page, why: slices.Clone(r.why), rank: r.rank})
			continue
		}
		for _, w := range r.why {
			if !slices.Contains(out[i].why, w) {
				out[i].why = append(out[i].why, w)
			}
		}
		out[i].rank = min(out[i].rank, r.rank)
	}
	slices.SortFunc(out, func(a, b related) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		return strings.Compare(a.page, b.page)
	})
	return out
}

// label names a page as told: its path, title and type, and whether it is
// deprecated or in a bundle the wiki mounts.
func (rs *relations) label(page string) string {
	n := rs.nodes[page]
	if n.Page == nil {
		return page
	}
	kind := n.Page.Type
	if n.Page.Status == "deprecated" {
		kind += ", deprecated"
	}
	if n.Page.Mount != "" {
		kind += ", in the mounted bundle " + n.Page.Mount + ", read-only"
	}
	return fmt.Sprintf("%s %q (%s)", page, n.Page.Title, kind)
}
