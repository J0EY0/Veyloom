package wiki

import (
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// What a turn's brief shows of a bundle (docs/design.md 5.2): the pages
// every turn carries, the pages changed since the session last looked, and
// the pages that may bear on what the turn is about.

// ResidentTag marks a page every turn of the project carries in full: a
// convention, a naming rule, what nobody should work without.
const ResidentTag = "resident"

// Resident lists the pages every turn carries: tagged resident and not
// deprecated, whoever tagged them (docs/design.md 5.15). The ones a person
// vouched for come first, the most recently vouched first, then the ones
// only an agent tagged: when they do not all fit a brief, those are the
// ones only named.
func (b *Bundle) Resident() []Page {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []Page
	for _, e := range b.pages {
		if !e.sum.Carried() {
			continue
		}
		if p, err := e.page(); err == nil {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(x, y Page) int {
		if c := y.VouchedAt.Compare(x.VouchedAt); c != 0 {
			return c
		}
		return strings.Compare(x.Path, y.Path)
	})
	return out
}

// Carried reports whether every turn carries the page: see Resident.
func (s Summary) Carried() bool {
	return s.Status != okf.Deprecated && s.Tagged(ResidentTag)
}

// vouchedAt says when a person last stood behind the page as it is: when
// one wrote it, or confirmed it after the last write. A page with no
// author stamp was put there by hand, outside Veyloom's tools, which is a
// person's doing as the wiki's history already has it (see Sync).
func vouchedAt(s Summary, verified []okf.Stamp) time.Time {
	gen := s.Generated
	var at time.Time
	if gen.By == "" || okf.IsHuman(gen.By) {
		at = gen.At
		if at.IsZero() {
			at = s.Modified
		}
	}
	for _, v := range verified {
		if okf.IsHuman(v.By) && !v.At.Before(gen.At) && v.At.After(at) {
			at = v.At
		}
	}
	return at
}

// Latest is when the newest change to any page was made: where a reader
// that has seen the wiki as it is now stands in its history. Changed(Latest())
// is empty until something changes. The zero time means there are no pages.
func (b *Bundle) Latest() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var latest time.Time
	for _, e := range b.pages {
		if e.sum.Modified.After(latest) {
			latest = e.sum.Modified
		}
	}
	return latest
}

// Changed lists the pages that changed after since, newest first. Pages
// removed since are not in it: there is nothing left to read of them.
func (b *Bundle) Changed(since time.Time) []Summary {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []Summary
	for _, e := range b.pages {
		if e.sum.Modified.After(since) {
			out = append(out, e.sum)
		}
	}
	slices.SortFunc(out, func(x, y Summary) int {
		if c := y.Modified.Compare(x.Modified); c != 0 {
			return c
		}
		return strings.Compare(x.Path, y.Path)
	})
	return out
}

// Relevance is what a turn is about: the files its topic touched, from the
// repository's root, and the words it was asked in, in any language.
type Relevance struct {
	Paths []string
	Text  string
}

// Relevant finds pages that may bear on r, best first, at most limit.
// Pages count for naming one of the files or the directories they sit in,
// and for sharing words with the text in their title, description, tags or
// body. Chinese has no spaces to split words at, so it is compared two
// characters at a time, and only with titles, descriptions and tags, where
// a pair that matches is less often a coincidence. Deprecated pages are
// left out; a page needs more than one weak match to count at all.
func (b *Bundle) Relevant(r Relevance, limit int) []Hit {
	paths := pathTerms(r.Paths)
	words := asciiWords(r.Text)
	pairs := hanPairs(r.Text)
	if limit <= 0 || len(paths)+len(words)+len(pairs) == 0 {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	var hits []Hit
	for _, e := range b.pages {
		if e.sum.Status == okf.Deprecated {
			continue
		}
		score := 0
		for _, t := range paths {
			if strings.Contains(e.text.body, t.term) || strings.Contains(e.text.path, t.term) || strings.Contains(e.text.title, t.term) {
				score += t.weight
			}
		}
		for _, w := range words {
			switch {
			case strings.Contains(e.text.title, w):
				score += 4
			case strings.Contains(e.text.description, w) || strings.Contains(e.text.tags, w):
				score += 2
			case strings.Contains(e.text.body, w):
				score++
			}
		}
		pairScore, matched := 0, 0
		for _, p := range pairs {
			if matched == 4 {
				break
			}
			switch {
			case strings.Contains(e.text.title, p):
				pairScore += 3
				matched++
			case strings.Contains(e.text.description, p) || strings.Contains(e.text.tags, p):
				pairScore += 2
				matched++
			}
		}
		score += pairScore
		if score >= relevantScore {
			hits = append(hits, Hit{Summary: e.sum, score: score})
		}
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

// relevantScore is the least a page must score to count: a file it names
// or the directory the file is in, a word in its title, or two Chinese
// pairs in its title or description.
const relevantScore = 4

type weightedTerm struct {
	term   string
	weight int
}

// pathTerms are the files and the two directories above each: a page
// naming the file counts most, one naming its directory (a module's page,
// or a page on a file next to it) enough to count too. A directory right
// under the root (internal/, web/) says too little to count at all.
func pathTerms(paths []string) []weightedTerm {
	weights := map[string]int{}
	add := func(t string, w int) {
		if strings.Count(t, "/") >= 1 && w > weights[t] {
			weights[t] = w
		}
	}
	for _, p := range paths {
		p = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(path.Clean(p), "./"), "/"))
		if p == "" || p == "." {
			continue
		}
		weights[p] = max(weights[p], 6)
		if d := path.Dir(p); d != "." {
			add(d, 4)
			if dd := path.Dir(d); dd != "." {
				add(dd, 2)
			}
		}
	}
	terms := make([]weightedTerm, 0, len(weights))
	for t, w := range weights {
		terms = append(terms, weightedTerm{t, w})
	}
	slices.SortFunc(terms, func(x, y weightedTerm) int { return strings.Compare(x.term, y.term) })
	return terms
}

var asciiWord = regexp.MustCompile(`[a-z0-9_][a-z0-9_./-]{2,}`)

// asciiWords are the words of the text worth looking for: names, paths and
// identifiers rather than the words every sentence has.
func asciiWords(text string) []string {
	var out []string
	for _, w := range asciiWord.FindAllString(strings.ToLower(text), -1) {
		w = strings.Trim(w, "./-")
		if len(w) < 3 || stopWords[w] || strings.Trim(w, "0123456789") == "" || slices.Contains(out, w) {
			continue
		}
		out = append(out, w)
		if len(out) == 20 {
			break
		}
	}
	return out
}

// hanPairs are the text's Chinese characters two at a time, run by run,
// without the pairs every question has.
func hanPairs(text string) []string {
	var out []string
	var run []rune
	flush := func() {
		for i := 0; i+1 < len(run); i++ {
			p := string(run[i : i+2])
			if !stopPairs[p] && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
		run = run[:0]
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			run = append(run, r)
			continue
		}
		flush()
	}
	flush()
	if len(out) > 30 {
		out = out[:30]
	}
	return out
}

var stopWords = toSet("the and for with this that from what when where which how why you your are was were can could should would " +
	"please make use using into about have has had not but all any one two its our out get got let now then than there here also " +
	"just like need want will been being they them their does did done doing see look check fix add new old more less very some " +
	"such only other after before over under again each both most own same too who whom these those upon via per it's don't i'm")

var stopPairs = toSet("什么 怎么 为什 这个 那个 一下 可以 我们 你们 他们 是不 不是 没有 就是 还是 然后 因为 所以 如果 现在 一个 这些 那些 " +
	"看看 时候 需要 应该 能不 不能 已经 一些 这里 那里 是否 还有 以及 或者 但是 而且 这样 那样 怎样 多少 哪里 哪个 的话 帮我 请你 一起 看一 " +
	"的时 了吗 了吧 么用 么做 是什 么是 下这 下那 个问 问题")

func toSet(words string) map[string]bool {
	set := map[string]bool{}
	for _, w := range strings.Fields(words) {
		set[w] = true
	}
	return set
}
