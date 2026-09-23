package wiki

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Summary is what listings, search results and the brief's catalog show of
// a page, read from its frontmatter.
type Summary struct {
	Path        string
	Type        string
	Title       string // the file name when the page has none
	Description string
	Tags        []string
	Status      okf.Status
	Tier        okf.Tier
	Generated   okf.Stamp
	Verified    time.Time // the last confirmation, zero when there is none
	StaleAfter  time.Time // zero when not set
	// Modified is when the file last changed, by whatever hand, to the
	// microsecond as a database keeps time: a place in the wiki's history
	// a reader can store and come back to (see Changed).
	Modified time.Time
	// VouchedAt is when a person last stood behind the page as it is:
	// wrote it, or confirmed it since it was last written. Zero when no
	// person has, which is where an agent's writes start.
	VouchedAt time.Time
	// Team is the project that owns a skill, by its wiki folder name;
	// empty for a page that is no skill and for a skill nobody owns.
	Team string
	// Mentions are the paths of the repository the page names (see
	// Mentions): a change to one is a reason to check the page again.
	Mentions []string
}

// Tagged reports whether the page carries tag, ignoring case.
func (s Summary) Tagged(tag string) bool {
	return slices.ContainsFunc(s.Tags, func(t string) bool { return strings.EqualFold(t, tag) })
}

// Stale reports whether the page's content is past its stale_after.
func (s Summary) Stale(now time.Time) bool {
	return !s.StaleAfter.IsZero() && !now.Before(s.StaleAfter)
}

// Page is one page: its summary, a copy of its document and a hash of its
// bytes, which a Writer checks so a change made from a stale read fails.
type Page struct {
	Summary
	Doc  *okf.Document
	Hash string
}

// entry is a page as the index keeps it.
type entry struct {
	sum   Summary
	data  []byte
	hash  string
	size  int64
	mtime time.Time
	links []string // pages it links to
	// sources are the page's sources, for the graph (see Graph).
	sources []okf.Source
	text    searchText
}

// searchText is a page's text, lowercased once for searching. original
// is the body as written, for snippets.
type searchText struct {
	title, description, tags, path, body string
	original                             string
}

func newEntry(p string, data []byte, info fs.FileInfo) (*entry, error) {
	d, err := okf.Parse(data)
	if err != nil {
		return nil, err
	}
	sum := Summary{
		Path:        p,
		Type:        d.Type(),
		Title:       d.Title(),
		Description: d.Description(),
		Tags:        d.Tags(),
		Status:      d.Status(),
	}
	if sum.Title == "" {
		sum.Title = d.Name()
	}
	sum.Team = d.Metadata()[TeamKey]
	if sum.Title == "" {
		sum.Title = stem(p)
	}
	sum.Generated, _ = d.Generated()
	verified := d.Verified()
	sum.Tier = okf.TierOf(verified)
	sum.Verified = okf.LastVerified(verified)
	sum.StaleAfter, _ = d.StaleAfter()
	body := d.Body()
	sum.Mentions = Mentions(body)
	sum256 := sha256.Sum256(data)
	e := &entry{
		sum:     sum,
		data:    data,
		sources: d.Sources(),
		hash:    hex.EncodeToString(sum256[:8]),
		text: searchText{
			title:       strings.ToLower(sum.Title),
			description: strings.ToLower(sum.Description),
			tags:        strings.ToLower(strings.Join(sum.Tags, " ")),
			path:        strings.ToLower(p),
			body:        strings.ToLower(body),
			original:    body,
		},
	}
	if info != nil {
		e.size, e.mtime = info.Size(), info.ModTime()
		e.sum.Modified = e.mtime.Truncate(time.Microsecond)
	}
	e.sum.VouchedAt = vouchedAt(e.sum, verified)
	for _, l := range okf.Links(body) {
		if target, ok := okf.Resolve(p, l.Target); ok && path.Ext(target) == ".md" && !slices.Contains(e.links, target) {
			e.links = append(e.links, target)
		}
	}
	return e, nil
}

func (e *entry) page() (Page, error) {
	d, err := okf.Parse(e.data)
	if err != nil {
		return Page{}, err
	}
	return Page{Summary: e.sum, Doc: d, Hash: e.hash}, nil
}

// Hit is one search result.
type Hit struct {
	Summary
	Snippet string // the body around the first match, when the body matched
	score   int
}

// Score is how well the page matched: hits of several bundles for the
// same query rank against each other by it.
func (h Hit) Score() int { return h.score }

// Search finds pages by their text: every word of query is looked for,
// ignoring case, in the title, description, tags, path and body, and pages
// are ranked by where the words were found and how many of them. There is
// no word splitting, so Chinese, paths, identifiers and error messages all
// match as written (design.md 5.4). Deprecated pages rank lower.
func (b *Bundle) Search(query string, limit int) []Hit {
	words := strings.Fields(strings.ToLower(query))
	if len(words) == 0 || limit <= 0 {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	var hits []Hit
	for _, e := range b.pages {
		score, matched, at := 0, 0, -1
		for _, w := range words {
			s := 0
			if strings.Contains(e.text.title, w) {
				s += 8
			}
			if strings.Contains(e.text.description, w) {
				s += 4
			}
			if strings.Contains(e.text.tags, w) {
				s += 3
			}
			if strings.Contains(e.text.path, w) {
				s += 2
			}
			if i := strings.Index(e.text.body, w); i >= 0 {
				s++
				if at < 0 {
					at = i
				}
			}
			if s > 0 {
				matched++
				score += s
			}
		}
		if matched == 0 {
			continue
		}
		if matched == len(words) {
			score *= 2
		}
		switch phrase := strings.Join(words, " "); {
		case e.text.title == phrase:
			score += 40
		case len(words) > 1 && strings.Contains(e.text.title, phrase):
			score += 10
		}
		if e.sum.Status == okf.Deprecated {
			score = max(score/4, 1)
		}
		hit := Hit{Summary: e.sum, score: score}
		if at >= 0 {
			text := e.text.original
			if len(text) != len(e.text.body) { // lowercasing moved the offsets
				text = e.text.body
			}
			hit.Snippet = snippet(text, at)
		}
		hits = append(hits, hit)
	}
	slices.SortFunc(hits, func(x, y Hit) int {
		if x.score != y.score {
			return y.score - x.score
		}
		return strings.Compare(x.Path, y.Path)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// snippet cuts about eighty bytes of text around at, on rune boundaries,
// on one line.
func snippet(text string, at int) string {
	start, end := max(at-40, 0), min(at+80, len(text))
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	s := strings.Join(strings.Fields(text[start:end]), " ")
	if start > 0 {
		s = "…" + s
	}
	if end < len(text) {
		s += "…"
	}
	return s
}
