package okf

import (
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Link is a link in a markdown body: an inline link or image, or a
// reference definition. Start and End locate Target in the body, so a
// caller can rewrite it in place.
type Link struct {
	Target     string
	Start, End int
	Image      bool
}

// Links finds the links in body. Code blocks and code spans are skipped:
// a link written inside them is an example, not a link.
func Links(body string) []Link {
	text := maskCode(body)
	var links []Link
	for i := 0; i < len(text); i++ {
		if text[i] != '[' || escaped(text, i) {
			continue
		}
		close := matchBracket(text, i)
		if close < 0 || close+1 >= len(text) || text[close+1] != '(' || strings.HasPrefix(text[i+1:], "^") {
			continue
		}
		start, end, next, ok := destination(text, close+2)
		if !ok {
			continue
		}
		links = append(links, Link{Target: body[start:end], Start: start, End: end, Image: i > 0 && text[i-1] == '!'})
		i = next - 1
	}
	for _, m := range refDefinition.FindAllStringSubmatchIndex(text, -1) {
		start, end := m[2], m[3]
		if body[start] == '<' {
			start, end = start+1, end-1
		}
		links = append(links, Link{Target: body[start:end], Start: start, End: end})
	}
	return links
}

// refDefinition is a reference-style link definition, "[label]: target".
// Footnote definitions start with ^ and are not links.
var refDefinition = regexp.MustCompile(`(?m)^ {0,3}\[[^\]^\n][^\]\n]*\]:[ \t]*(<[^>\n]*>|[^\s<]+)`)

// RewriteLinks rewrites link targets: rewrite returns the new target and
// whether to change it.
func RewriteLinks(body string, rewrite func(target string) (string, bool)) string {
	links := Links(body)
	slices.SortFunc(links, func(a, b Link) int { return a.Start - b.Start })
	var b strings.Builder
	last := 0
	for _, l := range links {
		to, ok := rewrite(l.Target)
		if !ok {
			continue
		}
		b.WriteString(body[last:l.Start])
		b.WriteString(to)
		last = l.End
	}
	b.WriteString(body[last:])
	return b.String()
}

// Footnotes returns the labels body cites as [^label] and the ones it
// defines as "[^label]: text", each once, in order of appearance.
func Footnotes(body string) (cited, defined []string) {
	text := maskCode(body)
	seenCited, seenDefined := map[string]bool{}, map[string]bool{}
	for _, m := range footnote.FindAllStringSubmatchIndex(text, -1) {
		label := text[m[2]:m[3]]
		lineStart := strings.LastIndexByte(text[:m[0]], '\n') + 1
		isDef := m[1] < len(text) && text[m[1]] == ':' && strings.TrimLeft(text[lineStart:m[0]], " ") == "" && m[0]-lineStart <= 3
		switch {
		case isDef && !seenDefined[label]:
			seenDefined[label] = true
			defined = append(defined, label)
		case !isDef && !seenCited[label]:
			seenCited[label] = true
			cited = append(cited, label)
		}
	}
	return cited, defined
}

var footnote = regexp.MustCompile(`\[\^([^\]\s]+)\]`)

// Resolve turns a link target written in the concept at from into a path
// in the bundle, like "/decisions/x.md". Targets with a scheme (https:,
// mailto:, veyloom:) and in-page anchors are not bundle paths.
func Resolve(from, target string) (string, bool) {
	if IsExternal(target) || target == "" || strings.HasPrefix(target, "#") {
		return "", false
	}
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		target = target[:i]
	}
	if t, err := url.PathUnescape(target); err == nil {
		target = t
	}
	if target == "" {
		return "", false
	}
	if !strings.HasPrefix(target, "/") {
		target = path.Join(path.Dir(from), target)
	}
	return path.Clean("/" + strings.TrimPrefix(target, "/")), true
}

// IsExternal reports whether a link target names something outside the
// bundle: it has a URL scheme, or is protocol-relative.
func IsExternal(target string) bool {
	return scheme.MatchString(target) || strings.HasPrefix(target, "//")
}

var scheme = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

// maskCode blanks out fenced code blocks and code spans, keeping every
// other byte where it was, so offsets found in the result hold for the
// original.
func maskCode(body string) string {
	b := []byte(body)
	var fence string
	for start := 0; start < len(b); {
		end := strings.IndexByte(body[start:], '\n')
		if end < 0 {
			end = len(b)
		} else {
			end += start
		}
		line := body[start:end]
		trimmed := strings.TrimLeft(line, " ")
		switch {
		case fence != "":
			blank(b, start, end)
			if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]+" \t\r") == "" {
				fence = ""
			}
		case len(line)-len(trimmed) <= 3 && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")):
			fence = trimmed[:len(trimmed)-len(strings.TrimLeft(trimmed, trimmed[:1]))]
			blank(b, start, end)
		}
		start = end + 1
	}
	maskSpans(b)
	return string(b)
}

// maskSpans blanks code spans: a run of backticks up to the next run of
// the same length, within one paragraph.
func maskSpans(b []byte) {
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(b) && b[i+n] == '`' {
			n++
		}
		closeAt := -1
		for j := i + n; j < len(b); {
			if b[j] == '\n' && j+1 < len(b) && b[j+1] == '\n' {
				break
			}
			if b[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(b) && b[j+m] == '`' {
				m++
			}
			if m == n {
				closeAt = j + m
				break
			}
			j += m
		}
		if closeAt < 0 {
			i += n
			continue
		}
		blank(b, i, closeAt)
		i = closeAt
	}
}

func blank(b []byte, from, to int) {
	for i := from; i < to; i++ {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
}

func escaped(text string, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && text[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

// matchBracket finds the ] closing the [ at open, within one paragraph.
func matchBracket(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch {
		case text[i] == '\n' && i+1 < len(text) && text[i+1] == '\n':
			return -1
		case text[i] == '[' && !escaped(text, i):
			depth++
		case text[i] == ']' && !escaped(text, i):
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// destination reads a link destination and optional title starting just
// after "(", returning where the destination is and where the link ends.
func destination(text string, i int) (start, end, next int, ok bool) {
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	if i < len(text) && text[i] == '<' {
		close := strings.IndexAny(text[i+1:], ">\n")
		if close < 0 || text[i+1+close] != '>' {
			return 0, 0, 0, false
		}
		start, end = i+1, i+1+close
		i = end + 1
	} else {
		start = i
		depth := 0
		for ; i < len(text); i++ {
			c := text[i]
			if c == ' ' || c == '\t' || c == '\n' || (c == ')' && depth == 0 && !escaped(text, i)) {
				break
			}
			if c == '(' && !escaped(text, i) {
				depth++
			} else if c == ')' && !escaped(text, i) {
				depth--
			}
		}
		end = i
		if start == end {
			return 0, 0, 0, false
		}
	}
	// An optional title, then the closing parenthesis.
	for i < len(text) && text[i] != ')' {
		if text[i] == '\n' && i+1 < len(text) && text[i+1] == '\n' {
			return 0, 0, 0, false
		}
		i++
	}
	if i >= len(text) {
		return 0, 0, 0, false
	}
	return start, end, i + 1, true
}
