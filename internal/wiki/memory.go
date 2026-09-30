package wiki

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Memories (docs/design.md 5.16): how to work in a project, /memory.md in
// its wiki, and what the person wants in every project, the same page in a
// bundle of its own. A memory is one page of type Memory, an entry a line,
// each with the day it was noted and who noted it. Every turn carries it
// whole, so it is kept short: a budget in characters bounds it.

const (
	// MemoryPath is where a bundle keeps its memory.
	MemoryPath = "/memory.md"
	// MemoryType is a memory page's type.
	MemoryType = "Memory"
	// ConventionsPath is where a project's wiki keeps its own conventions:
	// what goes into it and how its pages are written, which people set
	// and its maintainer keeps (docs/design.md 5.12), LLM Wiki's schema.
	ConventionsPath = "/conventions/wiki.md"
)

// PersonalLayout is the personal memory's bundle: the memory page alone.
var PersonalLayout = Layout{LogTitle: "Personal memory history"}

// MemoryEntry is one line of a memory.
type MemoryEntry struct {
	Text string `json:"text"`
	// Date is the day it was noted, YYYY-MM-DD.
	Date string `json:"date,omitempty"`
	// Source is who noted it, and where.
	Source string `json:"source,omitempty"`
}

// Line is the entry as the page has it: "- text (date, source)".
func (e MemoryEntry) Line() string {
	switch {
	case e.Date != "" && e.Source != "":
		return fmt.Sprintf("- %s (%s, %s)", e.Text, e.Date, e.Source)
	case e.Date != "":
		return fmt.Sprintf("- %s (%s)", e.Text, e.Date)
	}
	return "- " + e.Text
}

// memoryNote is the day and source at the end of an entry's line.
var memoryNote = regexp.MustCompile(`^(.*\S)\s+\((\d{4}-\d{2}-\d{2})(?:,\s*([^()]*[^()\s]))?\)$`)

// ParseMemory reads a memory page's entries: its list items, in order.
// Other lines are not entries.
func ParseMemory(body string) []MemoryEntry {
	var out []MemoryEntry
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		text, ok := strings.CutPrefix(line, "- ")
		if !ok {
			text, ok = strings.CutPrefix(line, "* ")
		}
		if text = strings.TrimSpace(text); !ok || text == "" {
			continue
		}
		e := MemoryEntry{Text: text}
		if m := memoryNote.FindStringSubmatch(text); m != nil {
			e.Text, e.Date, e.Source = m[1], m[2], strings.TrimSpace(m[3])
		}
		out = append(out, e)
	}
	return out
}

// MemoryText is the memory as a page's body and a brief have it, a line an
// entry. Its length in characters is what a budget counts.
func MemoryText(entries []MemoryEntry) string {
	var sb strings.Builder
	for _, e := range entries {
		sb.WriteString(e.Line())
		sb.WriteString("\n")
	}
	return sb.String()
}

// MemoryChars is how much of a budget the entries take.
func MemoryChars(entries []MemoryEntry) int { return utf8.RuneCountInString(MemoryText(entries)) }

// CleanMemoryText makes text one entry: one line, its spaces collapsed, no
// list marker of its own.
func CleanMemoryText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	for _, marker := range []string{"-", "*"} {
		if text == marker {
			return ""
		}
		text = strings.TrimPrefix(text, marker+" ")
	}
	return strings.TrimSpace(text)
}

// NewMemory is a memory page with the given entries.
func NewMemory(title, description string, entries []MemoryEntry) *okf.Document {
	d := okf.New(MemoryType)
	d.SetString(okf.KeyTitle, title)
	d.SetString(okf.KeyDescription, description)
	d.SetBody(MemoryText(entries))
	return d
}

// Memory is the bundle's memory: its entries and the hash they were read
// at, for a change made from them. A bundle without one has none, and an
// empty hash.
func (b *Bundle) Memory() ([]MemoryEntry, string, error) {
	p, err := b.Page(MemoryPath)
	if errors.Is(err, store.ErrNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return ParseMemory(p.Doc.Body()), p.Hash, nil
}

// PutMemory writes the bundle's memory, creating the page the first time.
// hash is what Memory returned: a memory changed since is ErrConflict.
func (w *Writer) PutMemory(title, description string, entries []MemoryEntry, hash string) (Page, error) {
	if hash == "" {
		return w.Create(MemoryPath, NewMemory(title, description, entries))
	}
	page, err := w.b.Page(MemoryPath)
	if err != nil {
		return Page{}, err
	}
	d := page.Doc.Clone()
	d.SetBody(MemoryText(entries))
	return w.Put(MemoryPath, d, hash)
}
