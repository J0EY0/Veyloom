package wiki

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// A wiki's health check (docs/design.md 5.12): what a look over the whole
// bundle finds to set right, for the wiki maintainer's brief. Pages that
// contradict each other are not found here; that takes reading them.

// Health is what a check of a bundle found.
type Health struct {
	// Orphans are current pages no other page links to. The memory is none:
	// every turn carries it, and nothing needs to lead there.
	Orphans []Summary
	// Broken are links to pages that are not there.
	Broken []BrokenLink
	// Stale are current pages past their stale_after.
	Stale []Summary
	// ResidentChars is what the pages every turn carries come to, counted
	// as a brief counts them; past the brief's budget, the last of them are
	// only named.
	ResidentChars int
	// Problems are what breaks OKF in the files as they are on disk.
	Problems []okf.Problem
	// Unlinked are current pages that name the same path of the repository
	// while neither links to or otherwise bears on the other (design.md
	// 5.17): the one may well bear on the other.
	Unlinked []UnlinkedPair
}

// UnlinkedPair is two pages naming the same path, neither leading to the
// other.
type UnlinkedPair struct {
	A, B string
	File string
}

// Limits of the pages naming the same path a check lists.
const (
	// unlinkedMax caps the pairs listed.
	unlinkedMax = 10
	// unlinkedCommon is how many pages may name a path for its pairs to
	// count: a path many pages name says little of any two of them.
	unlinkedCommon = 5
)

// BrokenLink is a link from a page to one that is not there.
type BrokenLink struct {
	From, To string
}

// Empty reports whether the check found nothing, with budget the most the
// resident pages may come to.
func (h Health) Empty(budget int) bool {
	return len(h.Orphans) == 0 && len(h.Broken) == 0 && len(h.Stale) == 0 && len(h.Problems) == 0 && len(h.Unlinked) == 0 && h.ResidentChars <= budget
}

// Health checks the bundle as it is.
func (b *Bundle) Health(now time.Time) Health {
	var h Health
	b.mu.RLock()
	for p, e := range b.pages {
		for _, target := range e.links {
			if b.pages[target] == nil && !isReserved(target) {
				h.Broken = append(h.Broken, BrokenLink{From: p, To: target})
			}
		}
		if e.sum.Status == okf.Deprecated {
			continue
		}
		if e.sum.Stale(now) {
			h.Stale = append(h.Stale, e.sum)
		}
		if len(b.backlinks(p)) == 0 && p != MemoryPath {
			h.Orphans = append(h.Orphans, e.sum)
		}
	}
	b.mu.RUnlock()
	h.Unlinked = unlinkedPairs(b.Graph())
	for _, p := range b.Resident() {
		h.ResidentChars += utf8.RuneCountInString(p.ResidentText())
	}
	h.Problems = b.Problems()
	byPath := func(x, y Summary) int { return strings.Compare(x.Path, y.Path) }
	slices.SortFunc(h.Orphans, byPath)
	slices.SortFunc(h.Stale, byPath)
	slices.SortFunc(h.Broken, func(x, y BrokenLink) int { return strings.Compare(x.From+" "+x.To, y.From+" "+y.To) })
	return h
}

// isReserved reports whether p is one of the files every bundle has that
// are not pages: an index or the log.
func isReserved(p string) bool {
	base := path.Base(p)
	return base == okf.IndexFile || base == okf.LogFile
}

// ResidentText is the page as a brief carries it, whole.
func (p Page) ResidentText() string {
	return fmt.Sprintf("\n--- %s: %s\n%s\n", p.Path, p.Title, strings.TrimSpace(p.Doc.Body()))
}

// unlinkedPairs finds current pages naming the same path of the repository
// with no edge between them either way, the first unlinkedMax by path.
func unlinkedPairs(g Graph) []UnlinkedPair {
	current := map[string]bool{}
	for _, p := range g.Pages {
		if p.Status != okf.Deprecated {
			current[p.Path] = true
		}
	}
	linked := map[[2]string]bool{}
	for _, e := range g.Edges {
		linked[[2]string{e.From, e.To}] = true
		linked[[2]string{e.To, e.From}] = true
	}
	var out []UnlinkedPair
	paired := map[[2]string]bool{}
	for _, f := range g.Files {
		var pages []string
		for _, p := range f.Pages {
			if current[p] {
				pages = append(pages, p)
			}
		}
		if len(pages) < 2 || len(pages) > unlinkedCommon {
			continue
		}
		for i, a := range pages {
			for _, b := range pages[i+1:] {
				if pair := [2]string{a, b}; !linked[pair] && !paired[pair] {
					paired[pair] = true
					out = append(out, UnlinkedPair{A: a, B: b, File: f.Path})
				}
			}
		}
	}
	return out[:min(len(out), unlinkedMax)]
}
