package wiki

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path"
	"regexp"
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
	// entry and word are what the snippet is cut from once the hits are
	// ranked: the page, and the first of the query's words its body holds.
	entry *entry
	word  string
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
		score, matched, word := 0, 0, ""
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
			if strings.Contains(e.text.body, w) {
				s++
				if word == "" {
					word = w
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
		hits = append(hits, Hit{Summary: e.sum, score: score, entry: e, word: word})
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
	// Snippets only for the hits kept: reading a body as prose costs more
	// than finding a word in it.
	for i := range hits {
		if hits[i].word != "" {
			hits[i].Snippet = snippet(withoutTitleHeading(hits[i].entry.text.original, hits[i].Title), hits[i].word)
		}
		hits[i].entry, hits[i].word = nil, ""
	}
	return hits
}

var (
	// titleHeading is a first-level heading opening a page's text.
	titleHeading = regexp.MustCompile(`^\s*#[ \t]+(.+?)[ \t]*#*[ \t]*(?:\n|$)`)
	// fenceLine opens or closes a block of code.
	fenceLine = regexp.MustCompile("^\\s*(?:```|~~~)")
	// ruleLine is the rule under a table's head, or one across the page.
	ruleLine = regexp.MustCompile(`^\s*\|?\s*:?-{3,}`)
)

// snippet is about a hundred and twenty bytes of a page's text around the
// first place word is in it, read as prose on one line (prose), on rune
// boundaries and not through a Latin word; empty when the word is only in
// the page's markup, a link's target say.
func snippet(body, word string) string {
	text := prose(body)
	lower := strings.ToLower(text)
	if len(lower) != len(text) { // lowercasing moved the offsets
		text = lower
	}
	at := strings.Index(lower, word)
	if at < 0 {
		return ""
	}
	start, end := max(at-40, 0), min(at+len(word)+80, len(text))
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	// A word cut in two starts or ends where it is whole, if near.
	for n := 0; start > 0 && n < 16 && wordByte(text[start-1]); n++ {
		start--
	}
	for n := 0; end < len(text) && n < 16 && wordByte(text[end]); n++ {
		end++
	}
	// Nor with the stop that ended the sentence before.
	s := strings.TrimRight(strings.TrimLeft(text[start:end], " 。，；：、！？）,;:!?)"), " ")
	if start > 0 {
		s = "…" + s
	}
	if end < len(text) {
		s += "…"
	}
	return s
}

// withoutTitleHeading drops the heading a page's text opens with when it
// only says the page's title again, which a result shows above its
// snippet, as the page shows it above its text (the web client's
// withoutTitleHeading).
func withoutTitleHeading(body, title string) string {
	m := titleHeading.FindStringSubmatchIndex(body)
	if m == nil || strings.TrimSpace(title) == "" || strings.TrimSpace(body[m[2]:m[3]]) != strings.TrimSpace(title) {
		return body
	}
	return body[m[1]:]
}

// wordByte is part of a Latin word or a number.
func wordByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_'
}

// prose is a page's text as it reads, on one line: headings, list items
// and quotes without their marks, code and emphasis without theirs, links
// as their text and a table's cells side by side. The lines fencing code
// and the rules under a table's head go.
func prose(body string) string {
	var words []string
	for _, line := range strings.Split(body, "\n") {
		if fenceLine.MatchString(line) || ruleLine.MatchString(line) {
			continue
		}
		words = append(words, strings.Fields(readable(line))...)
	}
	return strings.Join(words, " ")
}

// readable is one line of markdown as it reads, the way sentenceAt reads a
// link's sentence, but with links as their text alone.
func readable(line string) string {
	line = lineMarker.ReplaceAllString(line, "")
	line = markdownLink.ReplaceAllString(line, "$1")
	line = footnoteRef.ReplaceAllString(line, "")
	line = inlineMarks.Replace(line)
	return strings.ReplaceAll(line, "|", " ")
}
