package okf

import (
	"bytes"
	"regexp"
	"strings"
)

// IndexEntry is one line of an index.md: a link and what is behind it.
type IndexEntry struct {
	Title       string
	Link        string
	Description string
}

// IndexSection groups entries under a heading.
type IndexSection struct {
	Heading string
	Entries []IndexEntry
}

// RenderIndex writes an index.md (§8): sections of "* [Title](link) -
// description" lines. The bundle root's also declares the OKF version, the
// one frontmatter an index may carry.
func RenderIndex(root bool, sections []IndexSection) []byte {
	var b bytes.Buffer
	if root {
		b.WriteString("---\nokf_version: \"" + Version + "\"\n---\n\n")
	}
	for i, s := range sections {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("# " + oneLine(s.Heading) + "\n")
		if len(s.Entries) > 0 {
			b.WriteString("\n")
		}
		for _, e := range s.Entries {
			b.WriteString("* [" + escapeLinkText(oneLine(e.Title)) + "](" + e.Link + ")")
			if d := oneLine(e.Description); d != "" {
				b.WriteString(" - " + d)
			}
			b.WriteString("\n")
		}
	}
	return b.Bytes()
}

// IndexVersion returns the OKF version a root index.md declares.
func IndexVersion(data []byte) string {
	front, _, err := split(data)
	if err != nil {
		return ""
	}
	m, err := parseMapping(front)
	if err != nil {
		return ""
	}
	return scalar(m, "okf_version")
}

var spaces = regexp.MustCompile(`\s+`)

func oneLine(s string) string { return strings.TrimSpace(spaces.ReplaceAllString(s, " ")) }

func escapeLinkText(s string) string {
	return strings.NewReplacer(`\`, `\\`, `[`, `\[`, `]`, `\]`).Replace(s)
}
