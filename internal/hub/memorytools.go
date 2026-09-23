package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// The memory tools (runtime/memorytools.go): a turn notes entries in the
// memories and takes them out. What it writes lands at once, and is one
// commit with the rest of the turn's changes to that wiki when it ends.

type memoryArgs struct {
	Entries []string `json:"entries"`
	Topic   int      `json:"topic"`
	Scope   string   `json:"scope"`
}

// memoryTries is how often a change is made again from a memory another
// turn changed in the meantime.
const memoryTries = 3

// answerMemory answers a memory tool call of a running turn.
func (m *TurnManager) answerMemory(ctx context.Context, at *activeTurn, q runtime.RoomQuery) (string, error) {
	var args memoryArgs
	if len(q.Args) > 0 {
		if err := json.Unmarshal(q.Args, &args); err != nil {
			return "", fmt.Errorf("bad arguments: %v", err)
		}
	}
	scope := store.WikiProject
	switch args.Scope {
	case "", runtime.MemoryScopeProject:
	case runtime.MemoryScopePersonal:
		scope = store.WikiPersonal
	default:
		return "", fmt.Errorf("scope %q is neither project nor personal", args.Scope)
	}
	if err := memoryInUse(m.wikis.memoryPrefs(), scope); err != nil {
		return "", err
	}
	if len(args.Entries) == 0 {
		return "", errors.New("give the entries, one line each")
	}
	tw, err := m.turnWiki(ctx, at, scope)
	if err != nil {
		return "", err
	}
	kind, budget := memoryOf(scope), m.wikis.budget(scope)
	for try := 1; ; try++ {
		answer, err := m.changeMemory(tw, at, q.Tool, kind, budget, args)
		if !errors.Is(err, store.ErrConflict) || try == memoryTries {
			return answer, err
		}
	}
}

// memoryInUse refuses a memory the person turned off (design.md 5.19):
// nothing goes into it, and nothing comes out, until it is on again.
func memoryInUse(prefs store.MemoryPrefs, scope store.WikiScope) error {
	switch {
	case scope == store.WikiPersonal && !prefs.UsesPersonal():
		return errors.New("the person has turned the personal memory off: nothing was changed in it")
	case scope == store.WikiProject && !prefs.UsesProject():
		return errors.New("the person has turned the project memory off: nothing was changed in it")
	}
	return nil
}

// changeMemory makes one change to a memory as it is now.
func (m *TurnManager) changeMemory(tw *turnWiki, at *activeTurn, tool string, kind memoryKind, budget int, args memoryArgs) (string, error) {
	have, hash, err := tw.bundle.Memory()
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	var next []wiki.MemoryEntry
	changed := false
	switch tool {
	case runtime.MemoryToolRemember:
		topic := at.thread.Number
		if args.Topic > 0 {
			topic = args.Topic
		}
		source := fmt.Sprintf("%s in topic #%d", at.member.DisplayName, topic)
		if kind.scope == store.WikiPersonal {
			source += " of " + tw.project.Name
		}
		var added, already []string
		if next, added, already, err = remember(kind, have, args.Entries, m.wikis.today(), source, budget); err != nil {
			return "", err
		}
		changed = len(added) > 0
		memoryList(&sb, "Noted in "+kind.what+":", added)
		memoryList(&sb, "Already in "+kind.what+", left as it is:", already)
	case runtime.MemoryToolForget:
		var gone []string
		if next, gone, err = forget(kind, have, args.Entries); err != nil {
			return "", err
		}
		changed = true
		memoryList(&sb, "Taken out of "+kind.what+":", gone)
	default:
		return "", fmt.Errorf("unknown memory tool %q", tool)
	}
	entries := have
	if changed {
		if _, err := tw.writer.PutMemory(kind.title, kind.description, next, hash); err != nil {
			return "", err
		}
		entries = next
	}
	fmt.Fprintf(&sb, "It has %s now, %d of the %d characters it may take; every turn carries it whole.", countEntries(len(entries)), wiki.MemoryChars(entries), budget)
	return sb.String(), nil
}

// countEntries says how many entries there are.
func countEntries(n int) string {
	if n == 1 {
		return "1 entry"
	}
	return fmt.Sprintf("%d entries", n)
}

// memoryList writes a heading and the entries under it, if there are any.
func memoryList(sb *strings.Builder, heading string, entries []string) {
	if len(entries) == 0 {
		return
	}
	sb.WriteString(heading + "\n")
	for _, e := range entries {
		sb.WriteString("- " + e + "\n")
	}
}
