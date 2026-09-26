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
	runtime.RoomToolListTopics: true,
	runtime.RoomToolReadTopic:  true,
	runtime.RoomToolReadTurn:   true,
	runtime.RoomToolReadRoom:   true,
	runtime.RoomToolSearch:     true,
	runtime.MessageToolSend:    true,
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
// an agent's message carries on the piece its turn was part of.
func (m *TurnManager) pieceOfWork(ctx context.Context, triggers []store.Message, anchor string) (chain, wokenBy string) {
	if anchor != "" {
		return anchor, ""
	}
	for i := len(triggers) - 1; i >= 0; i-- {
		if triggers[i].SenderKind != store.SenderAgent {
			return triggers[i].ID, ""
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

// mayWake says whether msg, said in at, may wake member now, in thread.
// When a limit holds the wake back, the person is told in at's topic and
// the wake is kept for them to let go on. Two wakes at once may both pass
// the limit by one: it is a brake, not a count to the turn.
func (m *TurnManager) mayWake(ctx context.Context, at *activeTurn, member store.Member, msg store.Message, thread store.Thread) bool {
	project, err := m.store.RoomProject(ctx, at.member.RoomID)
	if err != nil {
		m.logger.Error("the relay limit", "turn", at.turn.ID, "err", err)
		return false
	}
	limit := project.RelayLimit
	if limit < 0 {
		// Agents wake no one in this project: the mention stays a hand-off.
		return false
	}
	chain := at.turn.ChainMessageID
	if chain == "" {
		return true
	}
	woken, worked, err := m.store.ChainWakes(ctx, chain, idleWakes)
	if err != nil {
		m.logger.Error("the wakes of a piece of work", "turn", at.turn.ID, "err", err)
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
	m.hold(ctx, at, member, msg, thread, reason, limit)
	m.heldBack(at)
	return false
}

// hold tells the person, in at's topic, that the wake of member by msg was
// held back and why, and keeps the wake under that note.
func (m *TurnManager) hold(ctx context.Context, at *activeTurn, member store.Member, msg store.Message, thread store.Thread, reason store.HoldReason, limit int) {
	body := holdNote(at.member.DisplayName, member.DisplayName, reason, limit)
	var mentions []store.Mention
	if user, ok := m.personOf(ctx, at); ok {
		// Addressed to the person, so it reaches their inbox.
		body = "@" + user.Name + " " + body
		mentions = []store.Mention{{Kind: store.MentionUser, ID: user.ID}}
	}
	note, err := m.store.CreateMessage(ctx, store.NewMessage{
		RoomID: at.thread.RoomID, ThreadID: at.thread.ID, SenderKind: store.SenderSystem,
		Body: body, Mentions: mentions, TurnID: at.turn.ID,
	})
	if err != nil {
		m.logger.Error("tell of a held wake", "turn", at.turn.ID, "err", err)
		return
	}
	// Kept before the note is announced, so whoever reads the note finds
	// the wake to let go on.
	if err := m.store.CreateRelayHold(ctx, store.RelayHold{MessageID: note.ID, MemberID: member.ID, ThreadID: thread.ID, TriggerMessageID: msg.ID, Reason: reason}); err != nil {
		m.logger.Error("keep a held wake", "turn", at.turn.ID, "err", err)
	}
	m.publish(messageEvent(note))
}

// holdNote says why a wake was held back.
func holdNote(waker, woken string, reason store.HoldReason, limit int) string {
	why := fmt.Sprintf("agents have woken %d turns in this piece of work since a person last spoke", limit)
	if reason == store.HoldIdle {
		why = fmt.Sprintf("the last %d turns agents woke in this piece of work only talked", idleWakes)
	}
	return fmt.Sprintf("%s mentioned %s, but %s; it waits for a person now.", waker, woken, why)
}

// personOf is the person the piece of work of at started from.
func (m *TurnManager) personOf(ctx context.Context, at *activeTurn) (store.User, bool) {
	id := at.initiator
	if chain := at.turn.ChainMessageID; chain != "" {
		if msg, err := m.store.GetMessage(ctx, chain); err == nil && msg.UserID != "" {
			id = msg.UserID
		}
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

// relaysLine tells a member it can talk and hand work on as it goes, and
// how many more turns agents may wake in the piece of work it is part of.
// A member a person asked hears back from the members it hands work to;
// one woken to take part is told so apart (handedByLine).
func relaysLine(r relaysLeft, woken bool) string {
	line := "\nYou can post while you work with " + runtime.MessageToolSend + ": in this topic, or in the room to start something new. " +
		"Writing @Name of a member there wakes it at once to work alongside you; writing @ and the person's name reaches their inbox."
	if !woken {
		line += " When the members you hand work to are done, you are woken once with what they came to, to see to what they need or sum it up for the person: do not wait for them or ask them to report back."
	}
	line += " Work that needs another's result, such as tests of code not written yet, hand on once that result is in, not alongside it."
	if r.Limit > 0 {
		line += fmt.Sprintf(" Agents may wake %d more turns of one another in the piece of work this turn belongs to before it waits for the person.", max(r.Limit-r.Woken, 0))
	}
	line += fmt.Sprintf(" Waking a member only to chat or to thank it wastes that: %d such turns in a row stop the waking sooner. When you are stuck, say so and mention the person.\n", idleWakes)
	return line
}
