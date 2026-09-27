package hub

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// What a person says in the topic a member's turn runs in, while it runs,
// is passed to the turn at once, the runtime's own way, rather than left
// for the next turn (design.md 5.23.2). The messages it carries stay in
// the member's queue in the store until the turn has answered them; what
// the turn does not get to goes back to wait, just as it would have
// waited without steering. The runtime's events tell what became of each
// steer: taken in (runtime.EventSteer), or not reaching the agent in this
// turn (runtime.EventSteerDropped).

// turnSteer is what was passed to a running turn at once.
type turnSteer struct {
	id string
	// triggers are the person's messages it carries, oldest first.
	triggers []trigger
	// to is how far into the topic it went, as messages.seq.
	to    int64
	state steerState
}

type steerState int

const (
	// steerSent is passed and not yet taken in.
	steerSent steerState = iota
	// steerTaken is taken in by the agent.
	steerTaken
	// steerOff is gone back to wait, or settled with the turn.
	steerOff
)

// steerable reports whether t can be passed to the member's running turn
// rather than wait for the next: a person's message in the topic a chat
// turn runs in, on a runtime that takes input while a turn runs. What
// else the turn will say to is the turn's own business (steer). Callers
// hold m.mu.
func steerable(st *memberState, t trigger) bool {
	at := st.running
	return at != nil && t.upkeep == nil && t.setup == nil && t.anchor == "" &&
		t.msg.SenderKind == store.SenderUser && t.thread.ID != "" && t.thread.ID == at.thread.ID &&
		at.upkeep == nil && at.setup == nil && runtime.TraitsOf(at.agent.Runtime).Steer
}

// steerAlong takes out of st's queue what waits there that can go with t
// to the running turn: the person's messages of the same topic, asked
// while the turn was getting ready. They come first, being older. Callers
// hold m.mu.
func steerAlong(st *memberState, t trigger) []trigger {
	var along, rest []trigger
	for _, p := range st.pending {
		if p.msg.ID != t.msg.ID && steerable(st, p) {
			along = append(along, p)
		} else {
			rest = append(rest, p)
		}
	}
	st.pending = rest
	return append(along, t)
}

// steer passes triggers to at, running: what was said in its topic since
// its brief, or its last steer, up to the newest of them. It reports false
// when at takes nothing more, and nothing was passed; once passed, the
// triggers are the turn's until it takes them in, or they go back to wait.
func (m *TurnManager) steer(ctx context.Context, at *activeTurn, triggers []trigger) bool {
	var to int64
	for _, t := range triggers {
		to = max(to, t.msg.Seq)
	}
	at.mu.Lock()
	if !at.dispatched || at.closed || at.finished || at.cancelled || at.noSteer {
		at.mu.Unlock()
		return false
	}
	from := max(at.position, at.steeredTo)
	s := &turnSteer{id: store.NewID(), triggers: triggers, to: to}
	at.steers = append(at.steers, s)
	at.steeredTo = max(at.steeredTo, to)
	session := at.turn.SessionID
	at.mu.Unlock()

	text, err := m.brief.steerText(ctx, at.thread, from, to, session, triggers)
	if err == nil {
		if conn, ok := m.connFor(at.member.MachineID); ok {
			err = conn.Send(ctx, protocol.SteerTurn{TurnID: at.turn.ID, SteerID: s.id, Text: text})
		} else {
			err = errors.New("machine is offline")
		}
	}
	if err != nil {
		m.logger.Warn("pass a message to a running turn", "turn", at.turn.ID, "err", err)
		m.steerDropped(ctx, at, s.id)
	}
	return true
}

// steerTaken notes that the agent took in the steer id.
func (m *TurnManager) steerTaken(at *activeTurn, id string) {
	at.mu.Lock()
	defer at.mu.Unlock()
	for _, s := range at.steers {
		if s.id == id && s.state == steerSent {
			s.state = steerTaken
		}
	}
}

// steerDropped takes back what the steer id carried, which will not reach
// the agent in this turn: it goes back to wait, and the turn, about to
// end, is passed nothing more. A steer the turn no longer holds, one
// passed to a run it has since started over, is no business of it.
func (m *TurnManager) steerDropped(ctx context.Context, at *activeTurn, id string) {
	at.mu.Lock()
	var back []trigger
	for _, s := range at.steers {
		if s.id == id && s.state == steerSent {
			s.state, back = steerOff, s.triggers
			at.noSteer = true
		}
	}
	at.mu.Unlock()
	m.requeue(at.member.ID, back)
}

// settleSteers settles what was passed to at as it ends, and stops it being
// passed more. When the turn went well, the steers the agent took in, in
// the order passed and up to the first it did not, are answered: their
// messages leave the store, and the session has read the topic as far as
// they went (steerRead). All else goes back to wait. It returns the
// messages answered so.
func (m *TurnManager) settleSteers(ctx context.Context, at *activeTurn, ok bool) []store.Message {
	at.mu.Lock()
	at.noSteer = true
	var answered, back []trigger
	answering := ok
	for _, s := range at.steers {
		switch {
		case s.state == steerOff:
			// Gone back to wait already: what it went over is unread.
			answering = false
		case answering && s.state == steerTaken:
			answered = append(answered, s.triggers...)
			at.steerRead = max(at.steerRead, s.to)
		default:
			answering = false
			back = append(back, s.triggers...)
		}
		s.state = steerOff
	}
	at.steers = nil
	at.mu.Unlock()
	m.unqueue(ctx, at.member.ID, answered)
	m.requeue(at.member.ID, back)
	msgs := make([]store.Message, len(answered))
	for i, t := range answered {
		msgs[i] = t.msg
	}
	return msgs
}

// restartSteers sends what was passed to at's run back to wait, as the
// run starts over in a new session: the new run's brief is its own. What
// comes meanwhile waits too, until the new run is under way (retryFresh,
// rerun).
func (m *TurnManager) restartSteers(ctx context.Context, at *activeTurn) {
	at.mu.Lock()
	var back []trigger
	for _, s := range at.steers {
		if s.state != steerOff {
			back = append(back, s.triggers...)
		}
		s.state = steerOff
	}
	at.steers, at.steeredTo = nil, 0
	at.mu.Unlock()
	m.requeue(at.member.ID, back)
}

// requeue puts triggers back in the member's queue, among what waits there
// in the order asked: they were asked while its turn ran, and waited in
// the store all along. A member no longer busy takes them up at once.
func (m *TurnManager) requeue(memberID string, triggers []trigger) {
	if len(triggers) == 0 {
		return
	}
	m.mu.Lock()
	st := m.state(memberID)
	for _, t := range triggers {
		if slices.ContainsFunc(st.pending, func(p trigger) bool { return p.msg.ID == t.msg.ID }) {
			continue
		}
		i := slices.IndexFunc(st.pending, func(p trigger) bool { return p.msg.Seq > t.msg.Seq })
		if i < 0 {
			i = len(st.pending)
		}
		st.pending = slices.Insert(st.pending, i, t)
	}
	m.mu.Unlock()
	m.resumeMember(memberID)
}

// steerText is what a turn is passed while it runs: what was said in its
// topic after from, up to to, what its session said itself left out, the
// messages it is to answer marked as in a brief and told whole.
func (b *briefBuilder) steerText(ctx context.Context, thread store.Thread, from, to int64, session string, triggers []trigger) (string, error) {
	news, total, err := b.store.ThreadNews(ctx, thread.ID, store.NewsQuery{After: from, UpTo: to, SessionID: session, Limit: b.limits.Thread})
	if err != nil {
		return "", fmt.Errorf("steer: %w", err)
	}
	w := &briefWriter{
		names:         newNameResolver(b.store),
		attachmentDir: b.attachmentDir,
		addressed:     make(map[string]bool, len(triggers)),
		written:       make(map[string]bool),
	}
	for _, t := range triggers {
		w.addressed[t.msg.ID] = true
	}
	shown := make(map[string]bool, len(news))
	for _, msg := range news {
		shown[msg.ID] = true
	}
	fmt.Fprintf(&w.sb, "New in topic #%d while you were at work on it%s:\n", thread.Number, leftOut(total-len(news), "message"))
	// Asked as the turn got ready, after its brief was put together: older
	// than what is new since.
	for _, t := range triggers {
		if !shown[t.msg.ID] {
			w.message(ctx, t.msg, "")
		}
	}
	for _, msg := range news {
		w.message(ctx, msg, "")
	}
	return w.sb.String(), nil
}
