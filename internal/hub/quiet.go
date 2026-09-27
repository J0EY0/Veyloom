package hub

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// A turn that may be stuck (docs/design.md 5.23.8): on its machine,
// waiting on no person, it showed no sign of life for TurnQuietAfter. The
// hub says so and nothing more; a long build is quiet too, and a person
// decides whether to cancel it.

// quietCheck is how often the hub looks for turns gone quiet, at most.
const quietCheck = 30 * time.Second

// RunQuietWatch looks for turns gone quiet until ctx ends.
func (h *Hub) RunQuietWatch(ctx context.Context) {
	if h.cfg.TurnQuietAfter <= 0 {
		return
	}
	tick := time.NewTicker(min(quietCheck, h.cfg.TurnQuietAfter/2))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			h.turns.checkQuiet(time.Now())
		}
	}
}

// stir notes that a turn showed a sign of life, or began or ended waiting
// on a person: its quiet starts over, and one said to be quiet is not any
// more.
func (m *TurnManager) stir(at *activeTurn) {
	at.mu.Lock()
	woke := m.stirLocked(at)
	at.mu.Unlock()
	if woke {
		m.publishQuiet(at, nil)
	}
}

// stirLocked is stir under at.mu held; it reports a turn said to be quiet
// that is not any more, which the caller tells once it lets go.
func (m *TurnManager) stirLocked(at *activeTurn) bool {
	at.activeAt = time.Now()
	woke := !at.quietSince.IsZero()
	at.quietSince = time.Time{}
	return woke
}

// checkQuiet says which turns went quiet as of now: on their machine, not
// over, waiting on nobody, with no sign of life for quietAfter.
func (m *TurnManager) checkQuiet(now time.Time) {
	if m.quietAfter <= 0 {
		return
	}
	m.mu.Lock()
	turns := make([]*activeTurn, 0, len(m.active))
	for _, at := range m.active {
		turns = append(turns, at)
	}
	m.mu.Unlock()
	for _, at := range turns {
		at.mu.Lock()
		quiet := at.dispatched && !at.closed && !at.activeAt.IsZero() && at.quietSince.IsZero() && len(at.pending) == 0 &&
			now.Sub(at.activeAt) >= m.quietAfter
		if quiet {
			at.quietSince = at.activeAt
		}
		since := at.quietSince
		at.mu.Unlock()
		if quiet {
			m.logger.Info("a turn went quiet", "turn", at.turn.ID, "member", at.member.DisplayName, "since", since)
			m.publishQuiet(at, &since)
		}
	}
}

// publishQuiet tells the room a turn went quiet, since since, or showed a
// sign of life again, since nil.
func (m *TurnManager) publishQuiet(at *activeTurn, since *time.Time) {
	m.publish(Event{Kind: EventTurnQuiet, RoomID: at.thread.RoomID, At: time.Now(), TurnID: at.turn.ID, QuietSince: since})
}

// QuietSince says, of the turns given, which are quiet and since when.
func (m *TurnManager) QuietSince(turnIDs []string) map[string]time.Time {
	m.mu.Lock()
	turns := make([]*activeTurn, 0, len(turnIDs))
	for _, id := range turnIDs {
		if at := m.active[id]; at != nil {
			turns = append(turns, at)
		}
	}
	m.mu.Unlock()
	out := map[string]time.Time{}
	for _, at := range turns {
		at.mu.Lock()
		if !at.quietSince.IsZero() {
			out[at.turn.ID] = at.quietSince
		}
		at.mu.Unlock()
	}
	return out
}

// sentRun notes a run of at sent to its machine: its quiet counts from
// now, and a turn said to be quiet, its run that went quiet over, is not
// any more. A person who cancelled the turn meanwhile, the cancel perhaps
// reaching the machine ahead of the run and so lost, is heard again.
func (m *TurnManager) sentRun(ctx context.Context, conn protocol.Conn, at *activeTurn) {
	at.mu.Lock()
	woke := m.stirLocked(at)
	cancel := at.cancelAsked
	at.mu.Unlock()
	if woke {
		m.publishQuiet(at, nil)
	}
	if cancel {
		if err := conn.Send(ctx, protocol.CancelTurn{TurnID: at.turn.ID}); err != nil {
			m.logger.Warn("cancel a turn sent after its cancel", "turn", at.turn.ID, "err", err)
		}
	}
}

// cancelledMeanwhile ends a turn a person cancelled while it waited to run
// again, in a new session or to be asked for its reply: cancelled, with
// what its first run spent. It reports whether it did.
func (m *TurnManager) cancelledMeanwhile(at *activeTurn, first protocol.TurnDone) bool {
	at.mu.Lock()
	asked := at.cancelAsked
	at.mu.Unlock()
	if !asked {
		return false
	}
	m.OnDone(at.turn.ID, protocol.TurnDone{
		TurnID: at.turn.ID, Error: runtime.ErrTurnCancelled.Error(), Cancelled: true,
		Result: runtime.Result{Usage: first.Result.Usage, SessionRef: first.Result.SessionRef},
	})
	return true
}

// startOver ends the member's session once a person cancelled its turn
// asking for a new one, before anything wakes the member again: its next
// turn starts a new session, whose brief says a person asked for it. A
// turn over before the cancel reached it gets the new session all the
// same, and a note says so; a cancelled one's note says it already.
func (m *TurnManager) startOver(ctx context.Context, at *activeTurn, cancelled bool) {
	if err := m.store.EndOpenSession(ctx, at.member.ID, store.SessionCancelled); err != nil {
		m.logger.Error("end the session a person asked to start over", "member", at.member.ID, "err", err)
		return
	}
	if !cancelled {
		m.postSystem(ctx, at.thread, at.turn.ID, fmt.Sprintf("%s's next turn starts a new session, as a person asked.", at.member.DisplayName))
	}
}

// stoppedTurn is a turn a person cancelled asking for a new session with
// it: the topic it was in, and what it was answering, cut short. over says
// it ended by itself before the cancel reached it, and nothing was stopped.
type stoppedTurn struct {
	topic int
	ask   string
	over  bool
}

// stoppedAsk cuts what a stopped turn was answering short.
const stoppedAsk = 200

// stoppedIn finds the turn a person cancelled as the member's session
// ended so: the member's turn going then, in that session or yet to get
// one, getting its worktree ready say; nil when that cannot be told.
func (m *TurnManager) stoppedIn(ctx context.Context, member store.Member, sessionID string) *stoppedTurn {
	turn, err := m.store.TurnAtSessionEnd(ctx, member.ID, sessionID)
	if err != nil {
		m.logger.Warn("find the turn a person cancelled", "member", member.ID, "session", sessionID, "err", err)
		return nil
	}
	stopped := &stoppedTurn{over: turn.Status != store.TurnCancelled}
	if thread, err := m.store.GetThread(ctx, turn.ThreadID); err == nil {
		stopped.topic = thread.Number
	}
	if turn.TriggerMessageID != "" {
		if msg, err := m.store.GetMessage(ctx, turn.TriggerMessageID); err == nil {
			stopped.ask = excerpt(strings.Join(strings.Fields(msg.Body), " "), stoppedAsk)
		}
	}
	return stopped
}

// stoppedLine tells a new session its last turn was cancelled on purpose,
// and where: an agent that sees the ask unanswered in the chat, and does
// not remember being stopped, takes it up again of its own accord, even
// the command it was stuck in (design.md 5.23.8).
func stoppedLine(stopped *stoppedTurn) string {
	const memory = "You do not remember your earlier turns here; what follows is the hub's record, so rely on that, and on the repository, rather than on memory.\n"
	if stopped != nil && stopped.over {
		// Over before the cancel reached it: nothing was stopped.
		return "\nThis is a new session: a person asked for one as your last turn ended. " + memory
	}
	last := "your last turn"
	if stopped != nil && stopped.topic > 0 {
		last = fmt.Sprintf("your last turn, in topic #%d", stopped.topic)
		if stopped.ask != "" {
			last += fmt.Sprintf(", where you were answering %q", stopped.ask)
		}
	}
	return "\nThis is a new session: a person cancelled " + last + ", and asked for a new session. " +
		"What that turn was doing was stopped on purpose, perhaps because it was stuck: do not take it up again, nor run what it ran, unless someone asks you to. " +
		memory
}
