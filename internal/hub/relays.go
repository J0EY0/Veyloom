package hub

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Agents waking one another (docs/design.md 5.22). Every turn belongs to a
// piece of work: the person's message it started from. A member's message
// that names another wakes it within the same piece, as long as two limits
// allow: the last turns agents woke in the piece did work rather than only
// talk, and the piece has not reached the project's relay limit. A wake a
// limit holds back is told to the person, who may let it go on, starting a
// piece of its own.

// idleWakes is how many turns agents woke in a row, doing no work, stop
// agents waking one another in a piece of work.
const idleWakes = 3

// talkTools are the tools a member follows and talks in the chat with: a
// turn that called no other did no work.
var talkTools = map[string]bool{
	runtime.RoomToolListTopics:  true,
	runtime.RoomToolReadTopic:   true,
	runtime.RoomToolReadTurn:    true,
	runtime.RoomToolReadMessage: true,
	runtime.RoomToolReadRoom:    true,
	runtime.RoomToolSearch:      true,
	runtime.MessageToolSend:     true,
	// A reminder only waits; what the turn it makes does is its own. A
	// draft is a person's to run.
	runtime.MessageToolRemind:         true,
	runtime.MessageToolCancelReminder: true,
	runtime.MessageToolDraft:          true,
}

// talkTool says a tool call, by the name its runtime gave it, is one that
// follows or talks in the chat: Claude Code names Veyloom's tools
// mcp__veyloom__<name>, Codex veyloom/<name>, Pi by the bare name.
func talkTool(name string) bool {
	for _, prefix := range []string{"mcp__veyloom__", "veyloom/", "veyloom."} {
		name = strings.TrimPrefix(name, prefix)
	}
	return talkTools[name]
}

// reachTools are what a runtime calls to reach its tools rather than to
// use one: Claude Code's ToolSearch loads the tools it defers, Veyloom's
// among them, before it calls them. Calling one is no work, and does not
// make an answer more than one breath (finishReply).
var reachTools = map[string]bool{"ToolSearch": true}

// workTool says a tool call does work: it neither talks nor reaches for
// a tool.
func workTool(name string) bool {
	return !talkTool(name) && !reachTools[name]
}

// pieceOfWork says which piece of work a turn answering triggers belongs
// to, and the turn that woke it when an agent did. A person's message, or
// a person letting a held wake go on (anchor), starts a piece of its own;
// an agent's message carries on the piece its turn was part of, and so
// does a reminder a turn of the member's set, coming due as a system
// message of that turn's (reminders.go).
func (m *TurnManager) pieceOfWork(ctx context.Context, triggers []store.Message, anchor string) (chain, wokenBy string) {
	if anchor != "" {
		return anchor, ""
	}
	for i := len(triggers) - 1; i >= 0; i-- {
		t := triggers[i]
		if t.SenderKind == store.SenderUser || t.SenderKind == store.SenderSystem && t.TurnID == "" {
			return t.ID, ""
		}
	}
	last := triggers[len(triggers)-1]
	if last.TurnID == "" {
		return last.ID, ""
	}
	waker, err := m.store.GetTurn(ctx, last.TurnID)
	if err != nil || waker.ChainMessageID == "" {
		return last.ID, last.TurnID
	}
	return waker.ChainMessageID, last.TurnID
}

// waker is what asks for a wake, as the limits on agents waking one
// another weigh it: the member whose turn asks, in the topic that turn ran
// in, the turn, and the person it worked for. at is the turn while it
// runs: a wake it has held back tells on the work it hands on
// (handedon.go). A reminder coming due asks as the turn that set it
// (reminders.go).
type waker struct {
	member    store.Member
	thread    store.Thread
	turn      store.Turn
	initiator string
	at        *activeTurn
	reminder  bool
}

// asWaker is the running turn asking for a wake.
func (at *activeTurn) asWaker() waker {
	return waker{member: at.member, thread: at.thread, turn: at.turn, initiator: at.initiator, at: at}
}

// mayWake says whether msg, said by w, may wake member now, in thread.
// When a limit holds the wake back, the person is told in w's topic and
// the wake is kept for them to let go on. Two wakes at once may both pass
// the limit by one: it is a brake, not a count to the turn.
func (m *TurnManager) mayWake(ctx context.Context, w waker, member store.Member, msg store.Message, thread store.Thread) bool {
	project, err := m.store.RoomProject(ctx, w.member.RoomID)
	if err != nil {
		m.logger.Error("the relay limit", "turn", w.turn.ID, "err", err)
		return false
	}
	limit := project.RelayLimit
	if limit < 0 {
		// Agents wake no one in this project: the mention stays a hand-off,
		// and a reminder asks the person.
		if w.reminder {
			m.hold(ctx, w, member, msg, thread, store.HoldPeople, limit)
		}
		return false
	}
	chain := w.turn.ChainMessageID
	if chain == "" {
		return true
	}
	woken, worked, err := m.store.ChainWakes(ctx, chain, idleWakes)
	if err != nil {
		m.logger.Error("the wakes of a piece of work", "turn", w.turn.ID, "err", err)
		return false
	}
	var reason store.HoldReason
	switch {
	case len(worked) == idleWakes && !slices.Contains(worked, true):
		reason = store.HoldIdle
	case limit > 0 && woken >= limit:
		reason = store.HoldLimit
	default:
		return true
	}
	m.hold(ctx, w, member, msg, thread, reason, limit)
	if w.at != nil {
		m.heldBack(w.at)
	}
	return false
}

// hold tells the person, in w's topic, that the wake of member by msg was
// held back and why, and keeps the wake under that note.
func (m *TurnManager) hold(ctx context.Context, w waker, member store.Member, msg store.Message, thread store.Thread, reason store.HoldReason, limit int) {
	body := holdNote(w.member.DisplayName, member.DisplayName, reason, limit)
	if w.reminder {
		body = reminderHoldNote(w.member.DisplayName, reason, limit)
	}
	var mentions []store.Mention
	if user, ok := m.personOf(ctx, w); ok {
		// Addressed to the person, so it reaches their inbox.
		body = "@" + user.Name + " " + body
		mentions = []store.Mention{{Kind: store.MentionUser, ID: user.ID}}
	}
	note, err := m.store.CreateMessage(ctx, store.NewMessage{
		RoomID: w.thread.RoomID, ThreadID: w.thread.ID, SenderKind: store.SenderSystem,
		Body: body, Mentions: mentions, TurnID: w.turn.ID,
	})
	if err != nil {
		m.logger.Error("tell of a held wake", "turn", w.turn.ID, "err", err)
		return
	}
	// Kept before the note is announced, so whoever reads the note finds
	// the wake to let go on.
	if err := m.store.CreateRelayHold(ctx, store.RelayHold{MessageID: note.ID, MemberID: member.ID, ThreadID: thread.ID, TriggerMessageID: msg.ID, Reason: reason}); err != nil {
		m.logger.Error("keep a held wake", "turn", w.turn.ID, "err", err)
	}
	m.publish(messageEvent(note))
}

// holdNote says why a wake was held back.
func holdNote(waker, woken string, reason store.HoldReason, limit int) string {
	return fmt.Sprintf("%s mentioned %s, but %s; it waits for a person now.", waker, woken, holdWhy(reason, limit))
}

// reminderHoldNote says why the wake a member's reminder asked for as it
// came due was held back.
func reminderHoldNote(member string, reason store.HoldReason, limit int) string {
	return fmt.Sprintf("%s's reminder came due, but %s; it waits for a person now.", member, holdWhy(reason, limit))
}

// holdWhy says which limit held a wake back.
func holdWhy(reason store.HoldReason, limit int) string {
	switch reason {
	case store.HoldIdle:
		return fmt.Sprintf("the last %d turns agents woke in this piece of work only talked", idleWakes)
	case store.HoldPeople:
		return "only people wake members in this project"
	}
	return fmt.Sprintf("agents have woken %d turns in this piece of work since a person last spoke", limit)
}

// personOf is the person the piece of work of w is for.
func (m *TurnManager) personOf(ctx context.Context, w waker) (store.User, bool) {
	id := w.initiator
	if person := m.chainPerson(ctx, w.turn.ChainMessageID); person != "" {
		id = person
	}
	if id == "" {
		return store.User{}, false
	}
	user, err := m.store.GetUser(ctx, id)
	return user, err == nil
}

// ContinueRelay lets a wake a limit held back go on (docs/design.md 5.22):
// the member is woken where it would have been, in a piece of work of its
// own that starts at the note, as though the person had asked it.
func (h *Hub) ContinueRelay(ctx context.Context, noteID string) error {
	hold, err := h.store.GetRelayHold(ctx, noteID)
	if err != nil {
		return err
	}
	member, err := h.store.GetMember(ctx, hold.MemberID)
	if err != nil {
		return err
	}
	if member.Removed() {
		return store.Conflicting("memberRemoved", store.Params{"name": member.DisplayName}, "%s was taken out of the project", member.DisplayName)
	}
	msg, err := h.store.GetMessage(ctx, hold.TriggerMessageID)
	if err != nil {
		return err
	}
	thread, err := h.store.GetThread(ctx, hold.ThreadID)
	if err != nil {
		return err
	}
	if _, err := h.store.ContinueRelayHold(ctx, noteID); err != nil {
		return err
	}
	return h.turns.triggerFrom(ctx, member, msg, thread, noteID)
}

// relaysLeft is how a piece of work stands against the limit on agents
// waking one another, as a turn in it begins: the project's limit, 0 for
// none, and the turns agents woke in it so far.
type relaysLeft struct {
	Limit int
	Woken int
}

// relaysLeftOf is how the piece of work of turn stands, for member's brief;
// nil when agents wake no one in its project.
func (m *TurnManager) relaysLeftOf(ctx context.Context, member store.Member, turn store.Turn) *relaysLeft {
	if turn.Kind != store.TurnChat || turn.ChainMessageID == "" {
		return nil
	}
	project, err := m.store.RoomProject(ctx, member.RoomID)
	if err != nil || project.RelayLimit < 0 {
		return nil
	}
	woken, _, err := m.store.ChainWakes(ctx, turn.ChainMessageID, 0)
	if err != nil {
		return nil
	}
	return &relaysLeft{Limit: project.RelayLimit, Woken: woken}
}
