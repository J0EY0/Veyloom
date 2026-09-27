package hub

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// What the chat's turns look up in the wikis goes back to the maintainer
// (docs/design.md 5.23.7): the searches that found nothing, with what their
// turns read next, are the words a page should carry; the pages no turn
// reads may be hard to find, or no longer needed.

// lookupStore is what the wiki's use needs of the store.
type lookupStore interface {
	RecordWikiLookup(ctx context.Context, l store.WikiLookup) error
	ListTurnWikiLookups(ctx context.Context, turnIDs []string) ([]store.WikiLookup, error)
	WikiLookedUpSince(ctx context.Context, projectID string, since time.Time) (store.WikiUse, error)
	PruneWikiLookups(ctx context.Context, before time.Time) (int64, error)
}

const (
	// lookupsKept is how long lookups are kept (RunSweep).
	lookupsKept = 90 * 24 * time.Hour
	// unreadAfter is how long a page goes unread, and unchanged, before an
	// upkeep lists it.
	unreadAfter = 30 * 24 * time.Hour
	// upkeepMisses caps the searches an upkeep lists, upkeepUnread the
	// pages, and upkeepMissTurns the turns named for a search.
	upkeepMisses    = 15
	upkeepUnread    = 10
	upkeepMissTurns = 5
)

// noteLookup records a chat turn's search or read in a wiki. Upkeeps and
// setups look the wiki up to keep it, not to use it. A lookup that cannot
// be recorded is only logged: the agent's answer does not wait on it.
func (m *TurnManager) noteLookup(ctx context.Context, at *activeTurn, tw *turnWiki, l store.WikiLookup) {
	if at.turn.Kind != store.TurnChat {
		return
	}
	l.ProjectID, l.TurnID, l.Scope = tw.project.ID, at.turn.ID, tw.scope
	if err := m.store.RecordWikiLookup(ctx, l); err != nil {
		m.logger.Warn("record a wiki lookup", "turn", at.turn.ID, "err", err)
	}
}

// searchMiss is a search that found no page as written, asked in one turn
// or more: its words, the wiki searched, the turns that asked it and the
// pages they read next.
type searchMiss struct {
	query string
	scope store.WikiScope
	turns []string
	then  []string
	// first orders searches asked as often: the first asked first.
	first int64
}

// searchMisses are the searches of the turns an upkeep goes over that
// found nothing, the first upkeepMisses of them, and how many there are.
func (m *TurnManager) searchMisses(ctx context.Context, turns []store.UpkeepTurn) ([]searchMiss, int) {
	if len(turns) == 0 {
		return nil, 0
	}
	ids := make([]string, len(turns))
	for i, t := range turns {
		ids[i] = t.ID
	}
	lookups, err := m.store.ListTurnWikiLookups(ctx, ids)
	if err != nil {
		m.logger.Warn("read what the turns looked up in the wiki", "err", err)
		return nil, 0
	}
	misses := missesIn(lookups)
	return misses[:min(len(misses), upkeepMisses)], len(misses)
}

// missesIn folds lookups, in the order made, into the searches that found
// nothing. The same words in the same wiki, whatever their case and
// spacing, are one search, with every turn that asked them and the pages
// each read next: what a turn reads after a search that found nothing is
// what it went on to find, often the page that should have been found.
// Asked by the most turns first, then the first asked first; turns and
// pages in the order they came.
func missesIn(lookups []store.WikiLookup) []searchMiss {
	byKey := map[string]*searchMiss{}
	var misses []*searchMiss
	// open are each turn's searches that found nothing since it last read:
	// turns running at once look the wiki up in between one another.
	open := map[string][]*searchMiss{}
	for _, l := range lookups {
		if l.Search() {
			if l.Hits > 0 {
				continue
			}
			words := strings.Fields(l.Query)
			key := string(l.Scope) + "\x00" + strings.ToLower(strings.Join(words, " "))
			miss := byKey[key]
			if miss == nil {
				miss = &searchMiss{query: strings.Join(words, " "), scope: l.Scope, first: l.ID}
				byKey[key] = miss
				misses = append(misses, miss)
			}
			if !slices.Contains(miss.turns, l.TurnID) {
				miss.turns = append(miss.turns, l.TurnID)
			}
			if !slices.Contains(open[l.TurnID], miss) {
				open[l.TurnID] = append(open[l.TurnID], miss)
			}
			continue
		}
		for _, miss := range open[l.TurnID] {
			read := l.Path
			if l.Scope != miss.scope {
				read += " (" + wikiName(l.Scope) + ")"
			}
			if !slices.Contains(miss.then, read) {
				miss.then = append(miss.then, read)
			}
		}
		delete(open, l.TurnID)
	}
	out := make([]searchMiss, len(misses))
	for i, miss := range misses {
		out[i] = *miss
	}
	slices.SortStableFunc(out, func(a, b searchMiss) int {
		if n := len(b.turns) - len(a.turns); n != 0 {
			return n
		}
		return int(a.first - b.first)
	})
	return out
}

// wikiName names a wiki in an upkeep's brief.
func wikiName(scope store.WikiScope) string {
	if scope == store.WikiLibrary {
		return "the skill library"
	}
	return "the project wiki"
}

// missesPart lists the searches of the turns gone over that found no page
// as written, and what the turns read next.
func missesPart(w *briefWriter, misses []searchMiss, total int) {
	if len(misses) == 0 {
		return
	}
	heading := fmt.Sprintf("Searches that found nothing as written (%d, asked most first):", total)
	if total > len(misses) {
		heading = fmt.Sprintf("Searches that found nothing as written (%d; these %d first, asked most first):", total, len(misses))
	}
	w.section(heading)
	for _, miss := range misses {
		line := fmt.Sprintf("- %q in %s, by %s", miss.query, wikiName(miss.scope), turnsNamed(miss.turns))
		if len(miss.then) > 0 {
			line += "; then read " + strings.Join(miss.then, ", ")
		} else {
			line += "; read nothing after"
		}
		w.sb.WriteString(line + "\n")
	}
}

// turnsNamed names turns by id, the first upkeepMissTurns of them.
func turnsNamed(ids []string) string {
	if len(ids) == 1 {
		return "turn " + ids[0]
	}
	named := "turns " + strings.Join(ids[:min(len(ids), upkeepMissTurns)], ", ")
	if len(ids) > upkeepMissTurns {
		named += fmt.Sprintf(" and %d more", len(ids)-upkeepMissTurns)
	}
	return named
}

// unreadPages lists the project wiki's pages no chat turn has read in
// unreadAfter, nor anyone written or confirmed in that time, the longest
// unchecked first, the first upkeepUnread of them, and how many there are.
// Deprecated pages are history, resident ones are in every brief, the
// project memory is too, and a page due to be checked again is listed
// there. Only while the chat's turns look the wiki up at all, in a wiki
// nobody uses every page going unread and none standing out; and once
// lookups were kept for all of unreadAfter, lest a page read before they
// were be listed as unread.
func (m *TurnManager) unreadPages(ctx context.Context, project store.Project, bundle *wiki.Bundle, checks []pageCheck) ([]wiki.Summary, int) {
	// Lookups are stamped by the database's clock, pages by the wiki's.
	use, err := m.store.WikiLookedUpSince(ctx, project.ID, time.Now().Add(-unreadAfter))
	if err != nil {
		m.logger.Warn("read what the wiki was looked up for", "project", project.ID, "err", err)
		return nil, 0
	}
	if !use.Used || !use.Covered {
		return nil, 0
	}
	checking := make(map[string]bool, len(checks))
	for _, c := range checks {
		checking[c.page.Path] = true
	}
	since := m.wikis.now().Add(-unreadAfter)
	var unread []wiki.Summary
	for _, p := range bundle.Pages() {
		if p.Status == okf.Deprecated || p.Carried() || p.Path == wiki.MemoryPath || checking[p.Path] || use.Read[p.Path] || p.CheckedAt().After(since) {
			continue
		}
		unread = append(unread, p)
	}
	slices.SortStableFunc(unread, func(a, b wiki.Summary) int { return a.CheckedAt().Compare(b.CheckedAt()) })
	return unread[:min(len(unread), upkeepUnread)], len(unread)
}

// unreadPart lists the pages no turn has read lately, each with what it
// says it is about, which is what a search finds it by.
func unreadPart(w *briefWriter, pages []wiki.Summary, total int, now time.Time) {
	if len(pages) == 0 {
		return
	}
	days := int(unreadAfter / (24 * time.Hour))
	heading := fmt.Sprintf("Pages no turn has read in %d days (%d, the longest unchecked first):", days, total)
	if total > len(pages) {
		heading = fmt.Sprintf("Pages no turn has read in %d days (%d; these %d first, the longest unchecked first):", days, total, len(pages))
	}
	w.section(heading)
	for _, p := range pages {
		checked := p.CheckedAt()
		line := fmt.Sprintf("- %s %q (%s), last checked %s (%s)", p.Path, p.Title, p.Type, checked.Local().Format("2006-01-02"), daysAgo(now.Sub(checked)))
		if p.Description != "" {
			line += ": " + excerpt(p.Description, unreadDescription)
		}
		w.sb.WriteString(line + "\n")
	}
}

// unreadDescription cuts what an unread page says it is about.
const unreadDescription = 160

// pruneLookups drops the lookups of every project older than before, those
// of projects nobody maintains included, which no upkeep reads.
func (h *Hub) pruneLookups(ctx context.Context, before time.Time) {
	if _, err := h.store.PruneWikiLookups(ctx, before); err != nil {
		h.logger.Warn("prune the wiki lookups", "err", err)
	}
}
