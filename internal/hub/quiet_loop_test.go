package hub

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// A turn that may be stuck is said to be so, and a person may cancel it
// with a new session for the member's next turn (docs/design.md 5.23.8).

// runningTurn waits for the room's one turn to be on its machine and to
// have said something, and returns it.
func (l *loop) runningTurn(sub Subscription) store.Turn {
	l.t.Helper()
	collectUntil(l.t, sub, EventTurnEvent)
	turns := l.turns()
	if len(turns) == 0 || turns[0].Status != store.TurnRunning {
		l.t.Fatalf("no turn running: %+v", turns)
	}
	return turns[0]
}

// A turn with nothing from its runtime for TurnQuietAfter is said to be
// quiet, once, until it stirs.
func TestLoop_AQuietTurnIsSaidToBeSoUntilItStirs(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"delay_ms": 1500})
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()
	l.say("@Slow take your time", "", slow)
	turn := l.runningTurn(sub)

	l.h.turns.checkQuiet(time.Now().Add(9 * time.Minute))
	if quiet := l.h.QuietSince([]string{turn.ID}); len(quiet) != 0 {
		t.Fatalf("quiet for less than TurnQuietAfter: %v", quiet)
	}
	l.h.turns.checkQuiet(time.Now().Add(11 * time.Minute))
	said := collectUntil(t, sub, EventTurnQuiet)
	quiet := said[len(said)-1]
	since := l.h.QuietSince([]string{turn.ID})[turn.ID]
	if quiet.TurnID != turn.ID || quiet.RoomID != l.room.ID || quiet.QuietSince == nil || !quiet.QuietSince.Equal(since) || since.IsZero() {
		t.Fatalf("quiet: %+v, the hub says since %v", quiet, since)
	}
	// Said once, not every look.
	l.h.turns.checkQuiet(time.Now().Add(12 * time.Minute))
	if again := l.h.QuietSince([]string{turn.ID})[turn.ID]; !again.Equal(since) {
		t.Errorf("quiet since %v, then %v", since, again)
	}

	// Its reply is a sign of life.
	woke := collectUntil(t, sub, EventTurnQuiet)
	if last := woke[len(woke)-1]; last.TurnID != turn.ID || last.QuietSince != nil {
		t.Errorf("stirred: %+v", last)
	}
	for _, ev := range woke[:len(woke)-1] {
		if ev.Kind == EventTurnQuiet {
			t.Errorf("said quiet twice: %+v", ev)
		}
	}
	l.waitTurns(1, store.TurnDone, "the turn")
	if quiet := l.h.QuietSince([]string{turn.ID}); len(quiet) != 0 {
		t.Errorf("over, and still quiet: %v", quiet)
	}
}

// A turn waiting on a person is not quiet: the wait is theirs. Answered,
// its quiet counts from the answer.
func TestLoop_ATurnWaitingOnAPersonIsNotQuiet(t *testing.T) {
	l := newLoop(t)
	asker := l.asking("Asker", map[string]any{"approval": true})
	l.say("@Asker go", "", asker)
	a := l.waitApproval()
	l.h.turns.checkQuiet(time.Now().Add(time.Hour))
	if quiet := l.h.QuietSince([]string{a.TurnID}); len(quiet) != 0 {
		t.Fatalf("waiting on a person, said quiet: %v", quiet)
	}
	if _, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}, ""); err != nil {
		t.Fatal(err)
	}
	l.waitTurns(1, store.TurnDone, "the turn")
}

// Cancelled with a new session, the member's session ends as the turn
// does, before the ask that waited for it starts: that one starts a new
// session, and its brief says a person asked for it.
func TestLoop_CancelWithANewSession(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"delay_ms": 3000})
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()
	l.say("@Slow first", "", slow)
	first := l.runningTurn(sub)
	// Asked again while it works: the ask waits for it.
	l.say("@Slow second", "", slow)
	if err := l.h.CancelTurn(l.ctx, first.ID, true); err != nil {
		t.Fatal(err)
	}
	// The second takes as long as the first would have.
	var turns []store.Turn
	eventuallyWithin(t, 10*time.Second, func() bool {
		turns = l.turns()
		return len(turns) == 2 && turns[0].Status == store.TurnDone && turns[1].Status == store.TurnCancelled
	}, "the first cancelled, the second done")
	second := turns[0]

	if old, err := l.s.GetSession(l.ctx, first.SessionID); err != nil || old.EndReason != store.SessionCancelled {
		t.Errorf("the first turn's session ended as a person asked: %+v %v", old, err)
	}
	// Told which turn was stopped on purpose, it does not take it up again
	// of its own accord: it may have been stuck.
	spec := specOf(t, second)
	where, err := l.s.GetThread(l.ctx, first.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	stopped := fmt.Sprintf("a person cancelled your last turn, in topic #%d, where you were answering %q, and asked for a new session", where.Number, "@Slow first")
	if second.SessionID == first.SessionID || spec.Session.Resume || !strings.Contains(spec.Prompt, stopped) {
		t.Errorf("the next turn, in a new session: %s then %s, %+v", first.SessionID, second.SessionID, spec.Session)
	}
	if notes := l.notes(first.ThreadID); countContaining(notes, "Slow's turn was cancelled; its next turn starts a new session") != 1 {
		t.Errorf("the note: %q", notes)
	}
}

// The turn a person cancelled with a new session is the one going as the
// session ended, though it had no session yet, its worktree still getting
// ready say; one over by itself before the cancel came was not stopped.
func TestLoop_TheTurnStoppedIsTheOneGoingAsTheSessionEnded(t *testing.T) {
	for _, status := range []store.TurnStatus{store.TurnCancelled, store.TurnDone} {
		t.Run(string(status), func(t *testing.T) {
			l := newLoop(t)
			slow := l.member("Slow", map[string]any{"reply": "ok"})
			l.say("@Slow first", "", slow)
			first := l.waitTurns(1, store.TurnDone, "the first turn")[0]
			// Stored as it is, lest it wake Slow: the ask of a turn on record
			// in the first one's topic.
			ask, err := l.s.CreateMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, ThreadID: first.ThreadID, SenderKind: store.SenderUser, UserID: l.user.ID, Body: "@Slow second, then"})
			if err != nil {
				t.Fatal(err)
			}
			next, err := l.s.CreateTurn(l.ctx, store.NewTurn{
				MemberID: slow.ID, RoomID: l.room.ID, ThreadID: first.ThreadID, TriggerMessageID: ask.ID, MachineID: l.machineID, Runtime: "fake",
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := l.s.FinishTurn(l.ctx, next.ID, store.TurnOutcome{Status: status}); err != nil {
				t.Fatal(err)
			}
			if err := l.s.EndOpenSession(l.ctx, slow.ID, store.SessionCancelled); err != nil {
				t.Fatal(err)
			}
			// Asked after the session ended: not the one stopped.
			if _, err := l.s.CreateTurn(l.ctx, store.NewTurn{
				MemberID: slow.ID, RoomID: l.room.ID, ThreadID: first.ThreadID, TriggerMessageID: first.TriggerMessageID, MachineID: l.machineID, Runtime: "fake",
			}); err != nil {
				t.Fatal(err)
			}
			if turns := l.turns(); len(turns) != 3 {
				t.Fatalf("nothing else woke Slow: %+v", turns)
			}
			thread, err := l.s.GetThread(l.ctx, first.ThreadID)
			if err != nil {
				t.Fatal(err)
			}
			member, _ := l.s.GetMember(l.ctx, slow.ID)
			stopped := l.h.turns.stoppedIn(l.ctx, member, first.SessionID)
			line := stoppedLine(stopped)
			if status == store.TurnCancelled {
				want := fmt.Sprintf("a person cancelled your last turn, in topic #%d, where you were answering %q", thread.Number, ask.Body)
				if stopped == nil || stopped.over || !strings.Contains(line, want) {
					t.Errorf("the turn stopped: %+v\n%s", stopped, line)
				}
				return
			}
			if stopped == nil || !stopped.over || strings.Contains(line, "cancelled") || !strings.Contains(line, "a person asked for one as your last turn ended") {
				t.Errorf("a turn over before the cancel: %+v\n%s", stopped, line)
			}
		})
	}
}

// A plain cancel keeps the session.
func TestLoop_CancelKeepsTheSession(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"delay_ms": 3000})
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()
	l.say("@Slow first", "", slow)
	first := l.runningTurn(sub)
	if err := l.h.CancelTurn(l.ctx, first.ID, false); err != nil {
		t.Fatal(err)
	}
	l.waitTurns(1, store.TurnCancelled, "the turn cancelled")
	if open, err := l.s.GetOpenSession(l.ctx, slow.ID); err != nil || open.ID != first.SessionID {
		t.Errorf("the session stays: %+v %v", open, err)
	}
	if notes := l.notes(first.ThreadID); countContaining(notes, "Slow's turn was cancelled") != 1 || countContaining(notes, "new session") != 0 {
		t.Errorf("the note: %q", notes)
	}
}

// A person's cancel is heard whenever it comes: a turn it caught between
// two runs gets neither its second run in a new session nor the asking
// for its reply.
func TestCancelAskedStopsTheRunsToCome(t *testing.T) {
	m := &TurnManager{}
	failed := protocol.TurnDone{TurnID: "t1", Error: "session not found"}
	at := &activeTurn{resumed: true, cancelAsked: true, work: make(chan func(), 1)}
	if m.retryFresh(at, failed) || len(at.work) != 0 {
		t.Error("run again in a new session after a person cancelled it")
	}
	if m.askForReply(at, protocol.TurnDone{TurnID: "t1"}) || len(at.work) != 0 {
		t.Error("asked for its reply after a person cancelled it")
	}
}

// sentConn keeps what was sent on it.
type sentConn struct {
	mu   sync.Mutex
	sent []protocol.Message
}

func (c *sentConn) Send(_ context.Context, m protocol.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, m)
	return nil
}

func (c *sentConn) Recv(ctx context.Context) (protocol.Message, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *sentConn) Close() error { return nil }

// A run sent after a person cancelled the turn is cancelled too: their
// cancel may have reached the machine first, which knew no such run then.
func TestSentRunHearsACancelFromBefore(t *testing.T) {
	m := &TurnManager{logger: slog.Default()}
	conn := &sentConn{}
	at := &activeTurn{turn: store.Turn{ID: "t1"}}
	m.sentRun(context.Background(), conn, at)
	if len(conn.sent) != 0 || at.activeAt.IsZero() {
		t.Fatalf("a run nobody cancelled: sent %v, active at %v", conn.sent, at.activeAt)
	}
	at.cancelAsked = true
	m.sentRun(context.Background(), conn, at)
	if len(conn.sent) != 1 {
		t.Fatalf("sent %v", conn.sent)
	}
	if c, ok := conn.sent[0].(protocol.CancelTurn); !ok || c.TurnID != "t1" {
		t.Errorf("sent %#v", conn.sent[0])
	}
}
