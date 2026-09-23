package wiki

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// A bundle's pages as a graph (docs/design.md 5.17): which page links to
// which, and in what sentence; which supersedes which; which rests on which;
// and which paths of the repository each names. It is worked out from what
// the pages say, and nothing of it is written anywhere: OKF v0.2's links
// carry no type, so the sentence a link sits in is what says the relation.

// EdgeKind is how one page bears on another.
type EdgeKind string

const (
	// EdgeLink is a link in a page's text to another page.
	EdgeLink EdgeKind = "link"
	// EdgeSupersedes runs from a page to the one it took over from, as the
	// lines Deprecate writes say ("Superseded by …", "Supersedes …").
	EdgeSupersedes EdgeKind = "supersedes"
	// EdgeSource runs from a page to another its sources name.
	EdgeSource EdgeKind = "source"
)

// Edge is one page bearing on another. To may lie outside the bundle, in a
// bundle the wiki mounts or nowhere at all: the caller resolves it or drops
// it.
type Edge struct {
	From, To string
	Kind     EdgeKind
	// Context is, for a link, the sentence it sits in, links written as
	// their text in brackets; for a source, the source's title.
	Context string
}

// File is a path of the repository the pages name, once however it is
// written.
type File struct {
	// Path is the fullest way it is written: a path named from further in
	// ("hub/brief.go") is taken for one written in full elsewhere
	// ("internal/hub/brief.go").
	Path string
	// Pages name it, by path.
	Pages []string
	// Dir says other paths the pages name sit in it.
	Dir bool
	// In is the directory the pages name that it sits in nearest.
	In string
}

// Ref is a source of a page naming something of Veyloom's (veyloom://…):
// a turn, a topic, a message, for the caller to resolve.
type Ref struct {
	Page     string
	Resource string
}

// Graph is a bundle's pages and how they bear on each other.
type Graph struct {
	// Pages are every page but the memory, deprecated ones too, by path.
	Pages []Summary
	Edges []Edge
	Files []File
	Refs  []Ref
}

// contextMax bounds the sentence kept with a link, in characters.
const contextMax = 200

var (
	supersededLine = regexp.MustCompile(`^\s*(?:>\s*)?Superseded by\b`)
	supersedesLine = regexp.MustCompile(`^\s*(?:>\s*)?Supersedes\b`)
)

// Graph works the bundle's graph out from its index.
func (b *Bundle) Graph() Graph {
	b.mu.RLock()
	defer b.mu.RUnlock()
	paths := make([]string, 0, len(b.pages))
	for p := range b.pages {
		if p != MemoryPath {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	var g Graph
	type key struct {
		from, to string
		kind     EdgeKind
	}
	seen := map[key]bool{}
	add := func(e Edge) {
		k := key{e.From, e.To, e.Kind}
		if e.From == e.To || e.To == MemoryPath || seen[k] {
			return
		}
		seen[k] = true
		g.Edges = append(g.Edges, e)
	}
	for _, p := range paths {
		e := b.pages[p]
		g.Pages = append(g.Pages, e.sum)
		body := e.text.original
		for _, l := range okf.Links(body) {
			to, ok := okf.Resolve(p, l.Target)
			if l.Image || !ok || path.Ext(to) != ".md" {
				continue
			}
			switch line := lineAt(body, l.Start); {
			case supersededLine.MatchString(line):
				add(Edge{From: to, To: p, Kind: EdgeSupersedes})
			case supersedesLine.MatchString(line):
				add(Edge{From: p, To: to, Kind: EdgeSupersedes})
			default:
				add(Edge{From: p, To: to, Kind: EdgeLink, Context: sentenceAt(body, l)})
			}
		}
		for _, s := range e.sources {
			if strings.HasPrefix(s.Resource, "veyloom://") {
				g.Refs = append(g.Refs, Ref{Page: p, Resource: s.Resource})
				continue
			}
			if to, ok := okf.Resolve(p, s.Resource); ok && !okf.IsExternal(s.Resource) && path.Ext(to) == ".md" {
				add(Edge{From: p, To: to, Kind: EdgeSource, Context: s.Title})
			}
		}
	}
	g.Files = namedFiles(g.Pages)
	return g
}

// lineAt is the line of text at offset i.
func lineAt(text string, i int) string {
	start := strings.LastIndexByte(text[:i], '\n') + 1
	end := strings.IndexByte(text[i:], '\n')
	if end < 0 {
		return text[start:]
	}
	return text[start : i+end]
}

var (
	// sentenceBreak ends a sentence: a full stop, question or exclamation
	// mark, or semicolon, in Chinese or followed by a space.
	sentenceBreak = regexp.MustCompile(`[。！？；]|[.!?;](?:\s|$)`)
	// markdownLink is a link or picture, with its text.
	markdownLink = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	// footnoteRef is a footnote cited, which says nothing of the relation.
	footnoteRef = regexp.MustCompile(`\[\^[^\]]+\]`)
	// lineMarker is what starts a list item, a quote or a heading.
	lineMarker = regexp.MustCompile(`^(?:\s*(?:[-*+]|\d+[.)]|>+|#+)\s+)+`)
)

// inlineMarks are the marks of code and emphasis a sentence reads without.
var inlineMarks = strings.NewReplacer("`", "", "**", "", "__", "")

// sentenceAt is the sentence a link sits in, within its line: its links
// written as their text in brackets, at most contextMax characters around
// the link.
func sentenceAt(text string, l okf.Link) string {
	lineStart := strings.LastIndexByte(text[:l.Start], '\n') + 1
	lineEnd := len(text)
	if i := strings.IndexByte(text[l.End:], '\n'); i >= 0 {
		lineEnd = l.End + i
	}
	// The link's text starts at the bracket before its target.
	open := lineStart
	if i := strings.LastIndex(text[lineStart:l.Start], "]("); i >= 0 {
		if j := strings.LastIndexByte(text[lineStart:lineStart+i], '['); j >= 0 {
			open = lineStart + j
		}
	}
	start := lineStart
	for _, m := range sentenceBreak.FindAllStringIndex(text[lineStart:open], -1) {
		start = lineStart + m[1]
	}
	end := lineEnd
	if m := sentenceBreak.FindStringIndex(text[l.End:lineEnd]); m != nil {
		end = l.End + m[1]
	}
	linkText := inlineMarks.Replace(markdownLink.ReplaceAllString(text[open:min(l.End+1, lineEnd)], "[$1]"))
	sentence := markdownLink.ReplaceAllString(text[start:end], "[$1]")
	sentence = footnoteRef.ReplaceAllString(sentence, "")
	sentence = lineMarker.ReplaceAllString(sentence, "")
	sentence = inlineMarks.Replace(sentence)
	sentence = strings.Join(strings.Fields(sentence), " ")
	return around(sentence, strings.Join(strings.Fields(linkText), " "), contextMax)
}

// around cuts text to at most limit characters, keeping the part about
// mark and marking what was cut with an ellipsis.
func around(text, mark string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	at := 0
	if i := strings.Index(text, mark); i >= 0 {
		at = utf8.RuneCountInString(text[:i])
	}
	from := max(0, min(at-limit/2, len(runes)-limit))
	to := from + limit
	cut := string(runes[from:to])
	if from > 0 {
		cut = "…" + cut
	}
	if to < len(runes) {
		cut += "…"
	}
	return cut
}

// namedFiles gathers the paths of the repository the pages name, each once
// however it is written, with the pages naming it and the directories among
// them.
func namedFiles(pages []Summary) []File {
	named := map[string][]string{}
	for _, p := range pages {
		for _, m := range p.Mentions {
			if !slices.Contains(named[m], p.Path) {
				named[m] = append(named[m], p.Path)
			}
		}
	}
	written := make([]string, 0, len(named))
	for m := range named {
		written = append(written, m)
	}
	// The fullest ways first, so a shorter one finds what it stands for.
	slices.SortFunc(written, func(x, y string) int {
		if d := len(y) - len(x); d != 0 {
			return d
		}
		return strings.Compare(x, y)
	})
	fullest := map[string]string{}
	byName := map[string][]string{}
	for _, m := range written {
		f := m
		for _, longer := range byName[path.Base(m)] {
			if strings.HasSuffix(longer, "/"+m) {
				f = longer
				break
			}
		}
		fullest[m] = f
		if f == m {
			byName[path.Base(m)] = append(byName[path.Base(m)], m)
		}
	}
	pagesOf := map[string][]string{}
	for m, ps := range named {
		f := fullest[m]
		for _, p := range ps {
			if !slices.Contains(pagesOf[f], p) {
				pagesOf[f] = append(pagesOf[f], p)
			}
		}
	}
	out := make([]File, 0, len(pagesOf))
	for f, ps := range pagesOf {
		slices.Sort(ps)
		out = append(out, File{Path: f, Pages: ps})
	}
	slices.SortFunc(out, func(x, y File) int { return strings.Compare(x.Path, y.Path) })
	// A directory is a path others sit in, either of them written in full
	// or from further in ("hub" for internal/hub). Each path is in the
	// nearest.
	index := make(map[string]int, len(out))
	byBase := map[string][]int{}
	for i, f := range out {
		index[f.Path] = i
		byBase[path.Base(f.Path)] = append(byBase[path.Base(f.Path)], i)
	}
	dirNamed := func(d string, self int) int {
		if j, ok := index[d]; ok && j != self {
			return j
		}
		for _, j := range byBase[path.Base(d)] {
			if j != self && strings.HasSuffix(out[j].Path, "/"+d) {
				return j
			}
		}
		return -1
	}
	for i := range out {
		parts := strings.Split(out[i].Path, "/")
	nearest:
		for end := len(parts) - 1; end >= 1; end-- {
			for start := 0; start < end; start++ {
				if j := dirNamed(strings.Join(parts[start:end], "/"), i); j >= 0 {
					out[j].Dir = true
					out[i].In = out[j].Path
					break nearest
				}
			}
		}
	}
	return out
}
