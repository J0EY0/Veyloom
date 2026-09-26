package hub

import (
	"regexp"
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

// mentionsName says whether markdown names name with an @ in its prose.
func mentionsName(markdown, name string) bool {
	return name != "" && strings.Contains(prose(markdown), "@"+name)
}
