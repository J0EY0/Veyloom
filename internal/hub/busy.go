package hub

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// What the room's other members are doing as a brief is put together
// (design.md 5.2): so an agent does not take up what another is in the
// middle of, or change the files it is changing, and knows who waits on a
// person. It is read off the turns under way, which the hub keeps in
// memory, so a brief costs no query for it.

// busyMember is another member of the room at work.
type busyMember struct {
	Name string
	// Topic is the number of the topic its turn is in.
	Topic int
	Since time.Time
	// Upkeep is set when the turn is a wiki maintainer's upkeep.
	Upkeep bool
	// Files are the files the turn has changed so far.
	Files []string
	// Waiting says what the turn waits on a person to do, one line each:
	// "allow running `make build`".
	Waiting []string
}

// busyIn lists the turns under way in the room of members other than
// memberID, the longest running first.
func (m *TurnManager) busyIn(roomID, memberID string) []busyMember {
	m.mu.Lock()
	var running []*activeTurn
	for _, at := range m.active {
		if at.thread.RoomID == roomID && at.member.ID != memberID {
			running = append(running, at)
		}
	}
	m.mu.Unlock()

	busy := make([]busyMember, 0, len(running))
	for _, at := range running {
		at.mu.Lock()
		b := busyMember{
			Name: at.member.DisplayName, Topic: at.thread.Number, Since: at.turn.StartedAt,
			Upkeep: at.upkeep != nil, Files: slices.Clone(at.files),
		}
		for _, p := range at.pending {
			b.Waiting = append(b.Waiting, waitingFor(p))
		}
		at.mu.Unlock()
		slices.Sort(b.Waiting)
		busy = append(busy, b)
	}
	slices.SortFunc(busy, func(x, y busyMember) int { return x.Since.Compare(y.Since) })
	return busy
}

// waitingFor says what a pending request asks a person to do.
func waitingFor(p *pendingApproval) string {
	switch p.kind {
	case store.ApprovalQuestion:
		return "answer " + describeQuestions(p.input)
	case store.ApprovalForm:
		return "fill in a form: " + describeElicitation(p.input)
	case store.ApprovalLink:
		return "open a link: " + describeElicitation(p.input)
	}
	switch p.tool {
	case planTool:
		return "approve its plan " + describePlan(p.input)
	case confirmTool:
		return "confirm " + describeConfirm(p.input)
	}
	return "allow running " + describeToolUse(p.tool, p.input)
}

// Caps of what a brief says of the members at work.
const (
	busyFiles   = 8
	busyWaiting = 160
)

// atWork writes what the room's other members are doing now, if any is.
func (b *briefBuilder) atWork(w *briefWriter, busy []busyMember) {
	if len(busy) == 0 {
		return
	}
	w.section("At work right now:")
	now := b.now()
	for _, m := range busy {
		line := "- " + m.Name
		if m.Upkeep {
			line += " is tidying the wiki"
		} else {
			line += fmt.Sprintf(", in topic #%d", m.Topic)
		}
		line += ", " + runningFor(now.Sub(m.Since))
		if len(m.Files) > 0 {
			line += "; has changed " + joinCapped(m.Files, busyFiles)
		}
		for _, wait := range m.Waiting {
			line += "; waits for a person to " + excerpt(wait, busyWaiting)
		}
		w.sb.WriteString(line + "\n")
	}
}

// runningFor says how long a turn has run, to the minute.
func runningFor(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "started just now"
	case d < time.Hour:
		return fmt.Sprintf("for %d min", int(d.Minutes()))
	default:
		return fmt.Sprintf("for %d h %d min", int(d.Hours()), int(d.Minutes())%60)
	}
}

// joinCapped lists up to n items and counts the rest.
func joinCapped(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:n], ", "), len(items)-n)
}
