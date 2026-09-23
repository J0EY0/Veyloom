package hub

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Checking the pages of a project's wiki again (docs/design.md 5.16): which
// are due, and why. The UI marks them; each upkeep lists the first few for
// the maintainer, who sets each right, deprecates it or confirms it as it
// is (confirm_wiki), which counts as checking it.

// WikiReview says why a page is due to be checked again.
type WikiReview struct {
	// Why is "changed": a turn that started after the page was last
	// checked changed a file the page names; "period": as long has passed
	// since as pages of its type hold; or "stale": its stale_after has come.
	Why string `json:"why"`
	// For "changed", the file as the turn recorded it, and the last turn to
	// change it: where, and when it started.
	File        string     `json:"file,omitempty"`
	TurnID      string     `json:"turn_id,omitempty"`
	RoomID      string     `json:"room_id,omitempty"`
	ThreadID    string     `json:"thread_id,omitempty"`
	TopicNumber int        `json:"topic_number,omitempty"`
	ChangedAt   *time.Time `json:"changed_at,omitempty"`
	// For "period", how many days pages like it hold.
	Every int `json:"every,omitempty"`
}

const (
	reviewChanged = "changed"
	reviewPeriod  = "period"
	reviewStale   = "stale"
)

// changedFilesMax bounds the files looked at for the paths pages name.
const changedFilesMax = 5000

// reviewStore is what finding the pages due needs from the database.
type reviewStore interface {
	ListChangedFiles(ctx context.Context, projectID string, since time.Time, limit int) ([]store.ChangedFile, error)
}

// wikiReviews finds the pages of a project's wiki due to be checked again,
// by path. Deprecated pages are not, nor a topic's write-up for a file
// changed since: it says what the topic came to.
func wikiReviews(ctx context.Context, st reviewStore, projectID string, pages []wiki.Summary, now time.Time) (map[string]WikiReview, error) {
	var since time.Time
	for _, p := range pages {
		if p.ReviewEvery() > 0 && len(p.Mentions) > 0 && (since.IsZero() || p.CheckedAt().Before(since)) {
			since = p.CheckedAt()
		}
	}
	var files []store.ChangedFile
	if !since.IsZero() {
		var err error
		if files, err = st.ListChangedFiles(ctx, projectID, since, changedFilesMax); err != nil {
			return nil, err
		}
	}
	out := map[string]WikiReview{}
	for _, p := range pages {
		if p.Status == okf.Deprecated {
			continue
		}
		every, checked := p.ReviewEvery(), p.CheckedAt()
		if review, ok := changedSince(p, files, checked); ok && every > 0 {
			out[p.Path] = review
			continue
		}
		switch {
		case p.Stale(now):
			out[p.Path] = WikiReview{Why: reviewStale}
		case every > 0 && now.Sub(checked) >= every:
			out[p.Path] = WikiReview{Why: reviewPeriod, Every: int(every / (24 * time.Hour))}
		}
	}
	return out, nil
}

// changedSince finds the last change to a file the page names by a turn
// that started after the page was checked.
func changedSince(p wiki.Summary, files []store.ChangedFile, checked time.Time) (WikiReview, bool) {
	var last *store.ChangedFile
	for i := range files {
		f := &files[i]
		if !f.At.After(checked) || (last != nil && !f.At.After(last.At)) {
			continue
		}
		if slices.ContainsFunc(p.Mentions, func(m string) bool { return wiki.NamesFile(m, f.Path) }) {
			last = f
		}
	}
	if last == nil {
		return WikiReview{}, false
	}
	at := last.At
	return WikiReview{
		Why: reviewChanged, File: last.Path, TurnID: last.TurnID, RoomID: last.RoomID, ThreadID: last.ThreadID,
		TopicNumber: last.TopicNumber, ChangedAt: &at,
	}, true
}

// reviewOrder is the order pages due are gone over in: those a change
// calls for first, then the resident ones every turn carries, then the
// longest unchecked.
func reviewOrder(pages []wiki.Summary, reviews map[string]WikiReview) []wiki.Summary {
	var due []wiki.Summary
	for _, p := range pages {
		if _, ok := reviews[p.Path]; ok {
			due = append(due, p)
		}
	}
	rank := func(p wiki.Summary) int {
		switch {
		case reviews[p.Path].Why == reviewChanged:
			return 0
		case p.Carried():
			return 1
		}
		return 2
	}
	slices.SortStableFunc(due, func(a, b wiki.Summary) int {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra - rb
		}
		return a.CheckedAt().Compare(b.CheckedAt())
	})
	return due
}

// confirmWiki stamps a page of the project's wiki as confirmed by the
// maintainer, who checked it and found it right: machine-confirmed, in
// OKF's words, and checked as of now. It goes into the wiki's history
// with the rest of the upkeep.
func (m *TurnManager) confirmWiki(ctx context.Context, at *activeTurn, path string) (string, error) {
	if _, _, ok := splitMount(path); ok {
		return "", readOnlyMount(path)
	}
	if p, err := wiki.CleanPath(path); err == nil && p == wiki.MemoryPath {
		return "", errors.New("the project memory is kept entry by entry with remember and forget, not confirmed")
	}
	tw, err := m.turnWiki(ctx, at, store.WikiProject)
	if err != nil {
		return "", err
	}
	page, err := tw.bundle.Page(path)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("the wiki has no page %s", path)
	}
	if err != nil {
		return "", err
	}
	if page.Status == okf.Deprecated {
		return "", fmt.Errorf("%s is deprecated: there is nothing to confirm", page.Path)
	}
	if _, err := tw.writer.Verify(page.Path); err != nil {
		return "", err
	}
	return fmt.Sprintf("Confirmed %s as it is: it counts as checked from now. It goes into the wiki's history as part of this turn when the turn ends.", page.Path), nil
}
