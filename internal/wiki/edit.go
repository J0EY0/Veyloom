package wiki

import (
	"fmt"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
)

// Edit is one change to a page's text, the way agents patch pages instead
// of rewriting them (WikiSkill does the same): append to the end, or
// replace or insert after an exact piece of the text. Matching on the text
// itself means an edit made from a stale read fails instead of undoing
// someone else's change.
type Edit struct {
	Op      string `json:"op"`
	Target  string `json:"target,omitempty"`
	Content string `json:"content"`
}

// The edit operations.
const (
	OpAppend      = "append"
	OpReplace     = "replace"
	OpInsertAfter = "insert_after"
)

// applyEdits applies edits in order to text, the whole file.
func applyEdits(text string, edits []Edit) (string, error) {
	if len(edits) == 0 {
		return "", fmt.Errorf("%w: no edits", store.ErrInvalidInput)
	}
	for i, e := range edits {
		switch e.Op {
		case OpAppend:
			if !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
			text += e.Content
			if !strings.HasSuffix(text, "\n") {
				text += "\n"
			}
		case OpReplace, OpInsertAfter:
			at, err := findOnce(text, e.Target)
			if err != nil {
				return "", fmt.Errorf("edit %d: %w", i+1, err)
			}
			end := at + len(e.Target)
			if e.Op == OpReplace {
				text = text[:at] + e.Content + text[end:]
				continue
			}
			insert := e.Content
			// Inserting after a whole line puts the new text on lines of
			// its own.
			if (end == len(text) || text[end] == '\n') && !strings.HasPrefix(insert, "\n") {
				insert = "\n" + strings.TrimSuffix(insert, "\n")
			}
			text = text[:end] + insert + text[end:]
		default:
			return "", fmt.Errorf("%w: edit %d: op %q should be append, replace or insert_after", store.ErrInvalidInput, i+1, e.Op)
		}
	}
	return text, nil
}

func findOnce(text, target string) (int, error) {
	if target == "" {
		return 0, fmt.Errorf("%w: the text to find is empty", store.ErrInvalidInput)
	}
	at := strings.Index(text, target)
	if at < 0 {
		return 0, fmt.Errorf("%w: the text to find is not in the page; read the page again and copy it exactly", store.ErrConflict)
	}
	if n := strings.Count(text, target); n > 1 {
		return 0, fmt.Errorf("%w: the text to find appears %d times; give enough of it to pick one", store.ErrInvalidInput, n)
	}
	return at, nil
}
