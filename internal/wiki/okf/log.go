package okf

import (
	"regexp"
	"strings"
)

// LogEntry is one line of a log.md (§9): what kind of change it was, and
// what it touched. The spec's examples use kinds like Creation, Update and
// Deprecation.
type LogEntry struct {
	Kind string
	Text string
}

// Log kinds Veyloom writes.
const (
	LogInitialization = "Initialization"
	LogCreation       = "Creation"
	LogUpdate         = "Update"
	LogDeprecation    = "Deprecation"
	LogRename         = "Rename"
	LogRevert         = "Revert"
	LogVerification   = "Verification"
	LogRejection      = "Rejection"
)

// NewLog starts an empty log.md with a title.
func NewLog(title string) []byte { return []byte("# " + oneLine(title) + "\n") }

// AppendLog adds entries under the date heading (YYYY-MM-DD). Dates go
// newest first, so a new date goes above the others; entries of a date
// already there follow the ones it has. Everything else is kept as it is:
// the log is a history, and nothing in it is rewritten.
func AppendLog(data []byte, date string, entries []LogEntry) []byte {
	if len(entries) == 0 {
		return data
	}
	var bullets strings.Builder
	for _, e := range entries {
		bullets.WriteString("* **" + oneLine(e.Kind) + "**: " + oneLine(e.Text) + "\n")
	}
	lines := bullets.String()
	text := string(data)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	newest := dateHeading.FindStringSubmatchIndex(text)
	switch {
	case newest == nil:
		if text != "" && !strings.HasSuffix(text, "\n\n") {
			text += "\n"
		}
		return []byte(text + "## " + date + "\n\n" + lines)
	case text[newest[2]:newest[3]] != date:
		return []byte(text[:newest[0]] + "## " + date + "\n\n" + lines + "\n" + text[newest[0]:])
	}
	// Today is already the newest date: add after its last entry.
	end := len(text)
	if next := dateHeading.FindStringIndex(text[newest[1]:]); next != nil {
		end = newest[1] + next[0]
	}
	at := newest[1] + len(strings.TrimRight(text[newest[1]:end], "\n"))
	lead := "\n"
	if at == newest[1] {
		lead = "\n\n"
	}
	return []byte(text[:at] + lead + strings.TrimSuffix(lines, "\n") + text[at:])
}

// dateHeading matches a level-two heading, capturing its text.
var dateHeading = regexp.MustCompile(`(?m)^## (.*?)[ \t]*$`)

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// checkLog checks a log.md (§9): every date heading is YYYY-MM-DD. Veyloom
// writes its logs without frontmatter.
func checkLog(p string, data []byte, profile Profile) []Problem {
	c := checker{path: p}
	body := data
	if _, rest, err := split(data); err == nil {
		if profile == Strict {
			c.add("log", "Veyloom writes log.md without frontmatter")
		}
		body = rest
	}
	for _, m := range dateHeading.FindAllStringSubmatch(string(body), -1) {
		if !isoDate.MatchString(m[1]) {
			c.add("log", "log heading %q should be a date written YYYY-MM-DD", m[1])
		}
	}
	return c.problems
}
