package hub

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// The memories (docs/design.md 5.16): how to work in a project, the page
// /memory.md of its wiki, and what the person wants in every project, the
// same page in a bundle of its own. Every turn of every member carries
// both whole, the personal one first: where they differ, the project's
// wins. People keep them in the UI, a member when a person asks it to,
// and the wiki maintainer from what people said; each change is a commit
// a person can undo.

// memoryBudgets cap the memories, in characters.
type memoryBudgets struct{ Personal, Project int }

// memoryEntryMax bounds one entry: a memory is short lines, not pages.
const memoryEntryMax = 400

// memoryKind is one of the two memories: its scope, and its page's title
// and description.
type memoryKind struct {
	scope       store.WikiScope
	title       string
	description string
	// what names it to the agent.
	what string
}

var (
	projectMemory = memoryKind{
		scope: store.WikiProject, title: "Project memory", what: "the project memory",
		description: "How to work in this project: preferences, agreements and corrections, one to a line, each with the day it was noted and who noted it.",
	}
	personalMemory = memoryKind{
		scope: store.WikiPersonal, title: "Personal memory", what: "the personal memory",
		description: "What the person wants in every project: habits, rules and ways of working, one to a line, each with the day it was noted and who noted it.",
	}
)

// memoryOf is the memory kept in a wiki of the scope.
func memoryOf(scope store.WikiScope) memoryKind {
	if scope == store.WikiPersonal {
		return personalMemory
	}
	return projectMemory
}

// budget is how many characters the memory of the scope may take.
func (s *wikiShelf) budget(scope store.WikiScope) int {
	if scope == store.WikiPersonal {
		return s.budgets.Personal
	}
	return s.budgets.Project
}

// today dates a memory's new entries.
func (s *wikiShelf) today() string { return s.now().Local().Format("2006-01-02") }

// memoryText is what an entry says, however it was given: one line, and
// without the day and source of the line it was copied from.
func memoryText(text string) string {
	entries := wiki.ParseMemory("- " + wiki.CleanMemoryText(text))
	if len(entries) == 0 {
		return ""
	}
	return entries[0].Text
}

// checkMemoryText refuses an entry that is not one short line.
func checkMemoryText(text string) error {
	switch n := utf8.RuneCountInString(text); {
	case n == 0:
		return store.Invalid("memoryEntryEmpty", nil, "an entry says nothing")
	case n > memoryEntryMax:
		return store.Invalid("memoryEntryTooLong", store.Params{"max": strconv.Itoa(memoryEntryMax), "chars": strconv.Itoa(n)},
			"an entry is one short line, at most %d characters, and this one has %d", memoryEntryMax, n)
	}
	return nil
}

// checkMemoryBudget refuses a memory that would not fit its budget.
func checkMemoryBudget(kind memoryKind, entries []wiki.MemoryEntry, budget int) error {
	if n := wiki.MemoryChars(entries); n > budget {
		return store.Invalid("memoryOverBudget", store.Params{"chars": strconv.Itoa(n), "budget": strconv.Itoa(budget)},
			"%s would take %d characters, more than the %d it may: fold entries that say the same into one, or forget ones that no longer hold, first", kind.what, n, budget)
	}
	return nil
}

// remember adds entries a member noted to a memory, those it has not got
// already. Past the memory's budget, nothing is added.
func remember(kind memoryKind, have []wiki.MemoryEntry, texts []string, date, source string, budget int) (next []wiki.MemoryEntry, added, already []string, err error) {
	next = append([]wiki.MemoryEntry(nil), have...)
	for _, raw := range texts {
		text := memoryText(raw)
		if err := checkMemoryText(text); err != nil {
			return nil, nil, nil, err
		}
		if i := findMemory(next, text); i >= 0 {
			already = append(already, next[i].Text)
			continue
		}
		next = append(next, wiki.MemoryEntry{Text: text, Date: date, Source: source})
		added = append(added, text)
	}
	if err := checkMemoryBudget(kind, next, budget); err != nil {
		return nil, nil, nil, err
	}
	return next, added, already, nil
}

// forget takes entries out of a memory: each one given by what it says,
// whole, or by a piece of it that only one entry has. Unless every one is
// found, nothing is taken out.
func forget(kind memoryKind, have []wiki.MemoryEntry, texts []string) (next []wiki.MemoryEntry, gone []string, err error) {
	drop := map[int]bool{}
	var problems []string
	for _, raw := range texts {
		text := memoryText(raw)
		if text == "" {
			continue
		}
		if i := findMemory(have, text); i >= 0 {
			drop[i] = true
			continue
		}
		var hits []int
		for i, e := range have {
			if strings.Contains(strings.ToLower(e.Text), strings.ToLower(text)) {
				hits = append(hits, i)
			}
		}
		switch len(hits) {
		case 0:
			problems = append(problems, fmt.Sprintf("no entry of %s says %q", kind.what, text))
		case 1:
			drop[hits[0]] = true
		default:
			problems = append(problems, fmt.Sprintf("%q is in %d entries of %s: give the one to forget whole", text, len(hits), kind.what))
		}
	}
	if len(problems) > 0 {
		return nil, nil, fmt.Errorf("%w: %s; nothing was forgotten", store.ErrInvalidInput, strings.Join(problems, "; "))
	}
	if len(drop) == 0 {
		return nil, nil, fmt.Errorf("%w: name the entries to forget", store.ErrInvalidInput)
	}
	for i, e := range have {
		if drop[i] {
			gone = append(gone, e.Text)
		} else {
			next = append(next, e)
		}
	}
	return next, gone, nil
}

// findMemory is where the memory has an entry saying text, ignoring case;
// -1 when it has none.
func findMemory(entries []wiki.MemoryEntry, text string) int {
	for i, e := range entries {
		if strings.EqualFold(e.Text, text) {
			return i
		}
	}
	return -1
}

// personEntries is the memory as a person saved it, entry by entry: those
// they left as they were keep their day and source, the rest are theirs,
// dated today. Blank lines drop out; an entry given twice is kept once.
func personEntries(kind memoryKind, have []wiki.MemoryEntry, texts []string, date, person string, budget int) ([]wiki.MemoryEntry, error) {
	used := map[int]bool{}
	var out []wiki.MemoryEntry
	for _, raw := range texts {
		text := memoryText(raw)
		if text == "" || findMemory(out, text) >= 0 {
			continue
		}
		if err := checkMemoryText(text); err != nil {
			return nil, err
		}
		entry := wiki.MemoryEntry{Text: text, Date: date, Source: person}
		for i, e := range have {
			if !used[i] && e.Text == text {
				entry, used[i] = e, true
				break
			}
		}
		out = append(out, entry)
	}
	return out, checkMemoryBudget(kind, out, budget)
}

// memoryPrefs are the account's memory switches as they are now.
func (s *wikiShelf) memoryPrefs() store.MemoryPrefs {
	if s == nil || s.prefs == nil {
		return store.DefaultMemoryPrefs
	}
	return s.prefs()
}

// writeMemories writes the memories turns use into a brief (design.md
// 5.19), the personal one first; project is the project's wiki, nil when
// there is none to show or it could not be read. A memory past its budget,
// by an edit made outside Veyloom or a budget lowered since, is carried as
// far as it fits. It reports whether every memory in use was read.
func (s *wikiShelf) writeMemories(ctx context.Context, w *briefWriter, project *wiki.Bundle) bool {
	prefs := s.memoryPrefs()
	read := true
	if prefs.UsesPersonal() {
		personal, err := s.personalMemory(ctx)
		if err != nil {
			s.logger.Warn("brief: open the personal memory", "err", err)
			read = false
		} else {
			heading := "Personal memory, what the person you work for wants in every project:"
			if prefs.UsesProject() {
				heading = "Personal memory, what the person you work for wants in every project (where the project memory says otherwise, it wins):"
			}
			read = s.writeMemory(w, personal, heading, s.budgets.Personal)
		}
	}
	if prefs.UsesProject() {
		read = project != nil && s.writeMemory(w, project, "Project memory, how to work in this project:", s.budgets.Project) && read
	}
	return read
}

// memoriesText is the memories a brief carries whole, as writeMemories
// writes them, and whether every one in use was read: one that was not has
// not changed for all the brief knows, and is certainly not gone.
func (s *wikiShelf) memoriesText(ctx context.Context, project *wiki.Bundle) (string, bool) {
	w := &briefWriter{}
	read := s.writeMemories(ctx, w, project)
	return w.sb.String(), read
}

// writeMemory writes one memory, and reports whether it was read.
func (s *wikiShelf) writeMemory(w *briefWriter, b *wiki.Bundle, heading string, budget int) bool {
	entries, _, err := b.Memory()
	if err != nil {
		s.logger.Warn("brief: read a memory", "wiki", b.Dir(), "err", err)
		return false
	}
	if len(entries) == 0 {
		return true
	}
	w.section(heading)
	room := budget
	for i, e := range entries {
		line := e.Line() + "\n"
		n := utf8.RuneCountInString(line)
		if n > room {
			more := "1 more entry"
			if left := len(entries) - i; left > 1 {
				more = fmt.Sprintf("%d more entries", left)
			}
			fmt.Fprintf(&w.sb, "(%s did not fit in the %d characters it may take)\n", more, budget)
			return true
		}
		room -= n
		w.sb.WriteString(line)
	}
	return true
}

// MemoryView is a memory as the UI shows it.
type MemoryView struct {
	Entries []wiki.MemoryEntry `json:"entries"`
	// Chars is how much of its budget it takes, in characters.
	Chars  int `json:"chars"`
	Budget int `json:"budget"`
	// Hash is what a change is made from; empty while there is no memory.
	Hash string `json:"hash"`
	// File is the page on disk, once there is one.
	File string `json:"file,omitempty"`
}

func memoryView(b *wiki.Bundle, entries []wiki.MemoryEntry, hash string, budget int) MemoryView {
	view := MemoryView{Entries: entries, Chars: wiki.MemoryChars(entries), Budget: budget, Hash: hash}
	if view.Entries == nil {
		view.Entries = []wiki.MemoryEntry{}
	}
	if hash != "" {
		view.File, _ = b.File(wiki.MemoryPath)
	}
	return view
}

// memoryRef opens the wiki whose memory is asked for: a project's, or the
// personal one when projectID is empty.
func (h *Hub) memoryRef(ctx context.Context, projectID string) (wikiRef, error) {
	if projectID != "" {
		return h.openWiki(ctx, projectID)
	}
	b, err := h.wikis.personalMemory(ctx)
	if err != nil {
		return wikiRef{}, err
	}
	return wikiRef{scope: store.WikiPersonal, bundle: b}, nil
}

// Memory is a project's memory, or the personal one when projectID is
// empty.
func (h *Hub) Memory(ctx context.Context, projectID string) (MemoryView, error) {
	r, err := h.memoryRef(ctx, projectID)
	if err != nil {
		return MemoryView{}, err
	}
	entries, hash, err := r.bundle.Memory()
	if err != nil {
		return MemoryView{}, err
	}
	return memoryView(r.bundle, entries, hash, h.wikis.budget(r.scope)), nil
}

// SetMemory saves a memory as a person left it (see personEntries), one
// commit a person can undo. hash is the MemoryView's it was edited from.
func (h *Hub) SetMemory(ctx context.Context, projectID string, texts []string, hash, userID string) (MemoryView, error) {
	r, err := h.memoryRef(ctx, projectID)
	if err != nil {
		return MemoryView{}, err
	}
	user, err := h.store.GetUser(ctx, userID)
	if err != nil {
		return MemoryView{}, err
	}
	person, err := h.personActor(ctx, userID)
	if err != nil {
		return MemoryView{}, err
	}
	kind, budget := memoryOf(r.scope), h.wikis.budget(r.scope)
	have, current, err := r.bundle.Memory()
	if err != nil {
		return MemoryView{}, err
	}
	if hash != current {
		return MemoryView{}, store.Conflicting("memoryChanged", nil, "%s changed since it was read; read it again", kind.what)
	}
	entries, err := personEntries(kind, have, texts, h.wikis.today(), user.Name, budget)
	if err != nil {
		return MemoryView{}, err
	}
	if slices.Equal(entries, have) {
		return memoryView(r.bundle, have, current, budget), nil
	}
	w, err := r.bundle.Writer(person)
	if err != nil {
		return MemoryView{}, err
	}
	if _, err := w.PutMemory(kind.title, kind.description, entries, current); err != nil {
		return MemoryView{}, err
	}
	if _, err := w.Commit(ctx, "Changed "+kind.what); err != nil {
		return MemoryView{}, err
	}
	h.changed(r)
	return h.Memory(ctx, projectID)
}

// MemoryHistory lists the latest changes to the personal memory, not the
// setting up of its bundle; a project's memory is in its wiki's history.
func (h *Hub) MemoryHistory(ctx context.Context, limit int) ([]WikiCommit, error) {
	r, err := h.memoryRef(ctx, "")
	if err != nil {
		return nil, err
	}
	return h.history(ctx, r, wiki.MemoryPath, limit)
}

// RevertMemory undoes one change to the personal memory as the person.
func (h *Hub) RevertMemory(ctx context.Context, sha, userID, reason string) (string, error) {
	r, err := h.memoryRef(ctx, "")
	if err != nil {
		return "", err
	}
	return h.revert(ctx, r, sha, userID, reason)
}
