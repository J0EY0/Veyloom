package hub

import (
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// setRelayLimit sets the project's relay limit.
func (l *loop) setRelayLimit(n int) {
	l.t.Helper()
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{RelayLimit: &n}); err != nil {
		l.t.Fatal(err)
	}
}

// holds are the notes in the topic that tell of held wakes.
func (l *loop) holds(threadID string) []store.Message {
	l.t.Helper()
	var out []store.Message
	for _, m := range l.replies(threadID, store.SenderSystem) {
		if strings.Contains(m.Body, "it waits for a person now") {
			out = append(out, m)
		}
	}
	return out
}

// settle waits for the room to have n turns, all done, and a little more
// to be sure no other starts.
func (l *loop) settle(n int, what string) []store.Turn {
	l.t.Helper()
	turns := l.waitTurns(n, store.TurnDone, what)
	time.Sleep(300 * time.Millisecond)
	if got := len(l.turns()); got != n {
		l.t.Fatalf("%s: want %d turns, got %d", what, n, got)
	}
	return turns
}

// Agents waking one another stop at the project's relay limit, the person
// told in their inbox; letting the held wake go on, or speaking, starts a
// piece of work of its own (docs/design.md 5.22).
func TestLoop_RelayStopsAtTheLimitAndGoesOnWhenLet(t *testing.T) {
	l := newLoop(t)
	l.setRelayLimit(2)
	// Starter hands on to A; A, B, C and D hand on round and round, at
	// work. None names the member a person asked, which would only report.
	cycle := func(name, next string) store.Member {
		return l.member(name, map[string]any{"reply": "@" + next + " your turn", "tool": true, "summary_reply": "Summed up."})
	}
	starter := cycle("Starter", "A")
	a, b, c, d := cycle("A", "B"), cycle("B", "C"), cycle("C", "D"), cycle("D", "A")

	msg := l.say("@Starter go", "", starter)
	// Starter (asked), A (woken 1), B (woken 2); waking C is held.
	turns := l.settle(3, "the piece of work to reach its limit")
	thread := l.topic(msg)
	first, second, third := turns[2], turns[1], turns[0]
	if first.MemberID != starter.ID || second.MemberID != a.ID || third.MemberID != b.ID {
		t.Errorf("the order: %s %s %s", first.MemberID, second.MemberID, third.MemberID)
	}
	for _, turn := range turns {
		if turn.ChainMessageID != msg.ID || turn.ThreadID != thread.ID || !turn.Worked {
			t.Errorf("a turn of the piece of work: %+v", turn)
		}
	}
	if first.WokenByTurnID != "" || second.WokenByTurnID != first.ID || third.WokenByTurnID != second.ID {
		t.Errorf("who woke whom: %q %q %q", first.WokenByTurnID, second.WokenByTurnID, third.WokenByTurnID)
	}
	holds := l.holds(thread.ID)
	if len(holds) != 1 || !strings.HasPrefix(holds[0].Body, "@alice B mentioned C, but agents have woken 2 turns") || len(holds[0].Mentions) != 1 || holds[0].Mentions[0].Kind != store.MentionUser {
		t.Fatalf("the note of the held wake: %+v", holds)
	}

	// Let go on: C is woken in a piece of work of its own, which runs to
	// the limit again: C, D (woken 1), A (woken 2); waking B is held.
	if err := l.h.ContinueRelay(l.ctx, holds[0].ID); err != nil {
		t.Fatal(err)
	}
	turns = l.settle(6, "the piece of work let go on")
	if turns[2].MemberID != c.ID || turns[2].ChainMessageID != holds[0].ID || turns[2].WokenByTurnID != "" {
		t.Errorf("the turn let go on: %+v", turns[2])
	}
	if err := l.h.ContinueRelay(l.ctx, holds[0].ID); err == nil {
		t.Error("a wake let go on twice")
	}
	if n := len(l.holds(thread.ID)); n != 2 {
		t.Errorf("want a second held wake, got %d", n)
	}

	// A person speaking starts one too: D, A (woken 1), B (woken 2).
	l.say("keep going", thread.ID, d)
	l.settle(9, "a piece of work after the person spoke")
}

// Agents that only talk stop after three wakes in a row; agents at work do
// not, up to the limit (docs/design.md 5.22).
func TestLoop_RelayStopsWhenAgentsOnlyTalk(t *testing.T) {
	l := newLoop(t)
	// Ping, Pong and Pang talk round and round: naming the member that
	// handed one the work would only report back to it.
	starter := l.member("Starter", map[string]any{"reply": "@Ping over to you"})
	ping := l.member("Ping", map[string]any{"reply": "@Pong your turn"})
	pong := l.member("Pong", map[string]any{"reply": "@Pang your turn"})
	pang := l.member("Pang", map[string]any{"reply": "@Ping your turn"})
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()
	msg := l.say("@Starter go", "", starter)
	// The note is announced once the held wake is kept, so a UI that
	// offers to let it go on finds it.
	note := awaitNote(t, sub, "it waits for a person now")
	if _, err := l.s.GetRelayHold(l.ctx, note.ID); err != nil {
		t.Errorf("the held wake as its note is announced: %v", err)
	}
	// Starter (asked), then three wakes that only talk; the fourth is held,
	// and what was handed on is not summed up.
	turns := l.settle(4, "talk to stop")
	for _, turn := range turns {
		if turn.Worked {
			t.Errorf("a turn that only talked worked: %+v", turn)
		}
	}
	holds := l.holds(l.topic(msg).ID)
	if len(holds) != 1 || !strings.Contains(holds[0].Body, "only talked") || !strings.Contains(holds[0].Body, "Pang mentioned Ping") {
		t.Errorf("the note of the held wake: %+v", holds)
	}
	hold, err := l.s.GetRelayHold(l.ctx, holds[0].ID)
	if err != nil || hold.Reason != store.HoldIdle || hold.MemberID != ping.ID {
		t.Errorf("the held wake: %+v %v", hold, err)
	}
	_, _ = pong, pang
}

// awaitNote is the first note of the system's announced to sub that says
// what.
func awaitNote(t *testing.T, sub Subscription, what string) store.Message {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-sub.Events():
			if !ok {
				t.Fatal("the subscription closed")
			}
			if ev.Kind == EventMessage && ev.Message.SenderKind == store.SenderSystem && strings.Contains(ev.Message.Body, what) {
				return *ev.Message
			}
		case <-deadline:
			t.Fatalf("no note saying %q", what)
		}
	}
}

func TestTalkTool(t *testing.T) {
	for name, want := range map[string]bool{
		"read_topic": true, "mcp__veyloom__read_room": true, "veyloom/search_messages": true,
		"read_file": false, "mcp__veyloom__write_wiki": false, "Bash": false, "veyloom/set_workspace_setup": false,
	} {
		if got := talkTool(name); got != want {
			t.Errorf("talkTool(%q) = %v", name, got)
		}
	}
	for name, want := range map[string]bool{"ToolSearch": false, "mcp__veyloom__send_message": false, "Bash": true, "Write": true} {
		if got := workTool(name); got != want {
			t.Errorf("workTool(%q) = %v", name, got)
		}
	}
}
