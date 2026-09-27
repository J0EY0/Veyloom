package hub

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
)

// Every turn leaves a word in its topic (docs/design.md 5.24). What it
// streams is its word, and what it sends there with send_message
// (sayInTopic). One that ended without either is asked for it once, in the
// session it ran in; one that still says nothing gets no word made up for
// it: a note says so, addressed to no one.

// askForReply decides whether a turn that just ended well, having said
// nothing in its topic, is asked for its reply, and if so sets that going
// and reports true: the turn is not over. A turn is asked once.
func (m *TurnManager) askForReply(at *activeTurn, done protocol.TurnDone) bool {
	if done.Error != "" || done.Cancelled {
		return false
	}
	at.mu.Lock()
	said := at.segments > 0 || at.output.Len() > 0 || strings.TrimSpace(done.Result.Output) != ""
	ok := !said && !at.askedReply && !at.closed && !at.cancelled && !at.cancelAsked
	if ok {
		// Passed nothing until the asking is under way.
		at.askedReply, at.noSteer = true, true
	}
	at.mu.Unlock()
	if !ok {
		return false
	}
	return at.enqueue(func() { m.askReply(at, done) })
}

// askReply runs the turn once more in the session it ran in, asking for
// the reply it did not give. Runs on the turn's executor. When that cannot
// be started the turn ends as the first run did.
func (m *TurnManager) askReply(at *activeTurn, first protocol.TurnDone) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()
	// Cancelled while it waited to be asked: it ends so.
	if m.cancelledMeanwhile(at, first) {
		return
	}

	at.mu.Lock()
	spec := at.spec
	spec.Prompt = runtime.ReplyAsk
	// The session the first run went on, as the runtime last named it.
	spec.Session.Resume = true
	if at.sessionRef != "" {
		spec.Session.Ref = at.sessionRef
	}
	if ref := first.Result.SessionRef; ref != "" {
		spec.Session.Ref = ref
	}
	at.resumed = true
	if at.transcript != nil {
		if err := at.transcript.write(transcriptLine{Kind: "reply_asked", TurnID: at.turn.ID, Runtime: at.agent.Runtime, Spec: transcriptSpec(spec)}); err != nil {
			m.logger.Warn("transcript", "turn", at.turn.ID, "err", err)
		}
	}
	at.beforeAsk = &first
	at.mu.Unlock()
	m.logger.Info("a turn said nothing in its topic; asking it for its reply", "member", at.member.DisplayName, "turn", at.turn.ID)

	err := errors.New("machine is offline")
	conn, online := m.connFor(at.member.MachineID)
	if online {
		err = conn.Send(ctx, protocol.StartTurn{TurnID: at.turn.ID, Runtime: at.agent.Runtime, Spec: spec})
	}
	if err != nil {
		m.logger.Error("ask a turn for its reply", "turn", at.turn.ID, "err", err)
		at.mu.Lock()
		at.beforeAsk = nil
		at.mu.Unlock()
		m.OnDone(at.turn.ID, first)
		return
	}
	m.sentRun(ctx, conn, at)
	// What the first run spent is the turn's, with what the asking spends.
	at.mu.Lock()
	at.spent = at.spent.Plus(first.Result.Usage)
	at.noSteer = false
	at.mu.Unlock()
}

// askedAnswer is how a turn asked for its reply ended: as the asking ended,
// unless that failed, when the turn ends as it did before it was asked,
// done and silent, with what the asking spent besides. What the asking ran
// into still tells how its account stands: a limit it reached holds the
// account's next turns up (turnSettled).
func (m *TurnManager) askedAnswer(at *activeTurn, done protocol.TurnDone) protocol.TurnDone {
	at.mu.Lock()
	before := at.beforeAsk
	at.beforeAsk = nil
	at.mu.Unlock()
	if before == nil || done.Error == "" || done.Cancelled {
		return done
	}
	m.logger.Warn("asking a turn for its reply failed; it ends without one", "turn", at.turn.ID, "err", done.Error)
	at.mu.Lock()
	at.askFailed = done.Error
	at.mu.Unlock()
	return protocol.TurnDone{TurnID: done.TurnID, Result: runtime.Result{
		Usage: done.Result.Usage, SessionRef: before.Result.SessionRef, Failure: done.Result.Failure, RetryAt: done.Result.RetryAt,
	}}
}

// saidNothing is the end of a turn that said nothing in its topic, even
// asked to: no word of its is made up. The head of a topic it opened stays
// empty, which the chat shows as no reply; in a topic it was asked in, a
// note says so.
func (m *TurnManager) saidNothing(ctx context.Context, at *activeTurn) {
	at.mu.Lock()
	opened := at.root != nil
	at.mu.Unlock()
	if !opened {
		m.postSystem(ctx, at.thread, at.turn.ID, noReplyNote(at.member.DisplayName))
	}
}

// noReplyNote says, in the hub's words, that member's turn said nothing in
// the topic; the UI says it its own way (systemNote.ts).
func noReplyNote(member string) string {
	return fmt.Sprintf("%s ended its turn without a word in this topic.", member)
}
