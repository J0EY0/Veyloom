package hub

import (
	"regexp"
	"slices"
	"strings"
)

// What an agent writes is markdown, and an @ in its code, `@Tester` or a
// fenced block, is shown as written: it names no one, wakes no one and
// reaches nobody's inbox. So mentions are looked for in its prose alone.

// fenceLine opens or closes a fenced code block: three backticks or
// tildes or more, indented three spaces at most.
var fenceLine = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")

// inlineCode is a code span on one line.
var inlineCode = regexp.MustCompile("`+[^`\n]*`+")

// prose is markdown with its code taken out, fenced blocks and spans on a
// line alike; a block left open runs to the end, as it is shown.
func prose(markdown string) string {
	if !strings.Contains(markdown, "`") && !strings.Contains(markdown, "~~~") {
		return markdown
	}
	var b strings.Builder
	fence := ""
	for i, line := range strings.Split(markdown, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}
		mark := fenceLine.FindStringSubmatch(line)
		switch {
		case fence != "":
			if mark != nil && mark[1][0] == fence[0] && len(mark[1]) >= len(fence) {
				fence = ""
			}
		case mark != nil:
			fence = mark[1]
		default:
			b.WriteString(inlineCode.ReplaceAllString(line, " "))
		}
	}
	return b.String()
}

// namedAt finds which of names the prose text @-mentions. At each @ the
// longest of names that follows is the one meant: with Coder and Coder2
// both given, "@Coder2" names Coder2 and not Coder; with Coder alone it
// names Coder, as "@Coder请看" does, since nothing marks where a name
// ends. So every name an @ may mean must be given, the name of a member
// taken out of the project too.
func namedAt(text string, names []string) map[string]bool {
	if !strings.Contains(text, "@") {
		return nil
	}
	longest := slices.Clone(names)
	slices.SortFunc(longest, func(a, b string) int { return len(b) - len(a) })
	found := make(map[string]bool)
	for rest := text; ; {
		i := strings.IndexByte(rest, '@')
		if i < 0 {
			return found
		}
		rest = rest[i+1:]
		if j := slices.IndexFunc(longest, func(n string) bool { return n != "" && strings.HasPrefix(rest, n) }); j >= 0 {
			found[longest[j]] = true
		}
	}
}
