package hub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// dispatched waits until turnID is on its machine, which is when it can
// be passed what people say.
func (l *loop) dispatched(turnID string) {
	l.t.Helper()
	eventually(l.t, func() bool {
		l.h.turns.mu.Lock()
		at := l.h.turns.active[turnID]
		l.h.turns.mu.Unlock()
		if at == nil {
			return false
		}
		at.mu.Lock()
		defer at.mu.Unlock()
		return at.dispatched
	}, "the turn to reach its machine")
}

// busyWith starts a turn of member for body, in a topic of its own, and
// waits until it runs on its machine: the turn and its topic.
func (l *loop) busyWith(member store.Member, body string) (store.Turn, store.Thread) {
	l.t.Helper()
	asked := l.say(body, "", member)
	thread := l.topic(asked)
	turn := l.waitTurns(1, store.TurnRunning, member.DisplayName+"'s turn")[0]
	l.dispatched(turn.ID)
	return turn, thread
}

// steersOf are the steer events of a turn's transcript as it stands, the
// turn running or not.
func (l *loop) steersOf(turn store.Turn) []string {
	l.t.Helper()
	l.h.turns.TranscriptSoFar(turn.ID)
	data, err := os.ReadFile(filepath.Join(l.cfg.TranscriptDir, turn.ID+".jsonl"))
	if err != nil {
		l.t.Fatal(err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `"kind":"steer"`) {
			out = append(out, line)
		}
	}
	return out
}

// What a person says in the topic of a member's running turn reaches that
// turn at once: no second turn, the reply takes it in and is addressed to
// them, and the next brief does not tell it again.
func TestLoop_AMessageInTheTopicReachesTheRunningTurn(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"delay_ms": float64(600)})
	turn, thread := l.busyWith(slow, "@Slow build the parser")
	// Said in the topic, to nobody in particular.
	l.h.turns.postSystem(l.ctx, thread, "", "bob joined the topic")
	steered := l.say("@Slow use the new grammar", thread.ID, slow)

	done := l.waitTurns(1, store.TurnDone, "Slow's one turn")[0]
	if done.ID != turn.ID {
		t.Fatalf("one turn answers both: %+v", done)
	}
	reply := l.root(thread).Body
	if !strings.HasPrefix(reply, "@alice ") || !strings.HasSuffix(reply, "Steered: >> [alice] @Slow use the new grammar") {
		t.Errorf("the reply takes the message in and is addressed to alice: %q", reply)
	}
	steers := l.steersOf(done)
	if len(steers) != 1 || !strings.Contains(steers[0], fmt.Sprintf(`New in topic #%d while you were at work on it:\n   [system] bob joined the topic\n>> [alice] @Slow use the new grammar\n`, thread.Number)) {
		t.Errorf("the turn was passed what was said in its topic since its brief, the message to answer marked:\n%s", strings.Join(steers, "\n"))
	}
	if q := l.queued(); len(q) != 0 {
		t.Errorf("answered, it waits no longer: %+v", q)
	}

	// The next turn in the topic is not told it again.
	l.setOptions(slow, nil)
	l.say("@Slow and now?", thread.ID, slow)
	next := l.waitTurns(2, store.TurnDone, "Slow's next turn")[0]
	brief := promptOf(t, next)
	if strings.Contains(brief, "use the new grammar") || strings.Contains(brief, "bob joined") || !strings.Contains(brief, ">> [alice] @Slow and now?") {
		t.Errorf("the next brief tells only what is new since the steer:\n%s", brief)
	}
	if steered.ThreadID != thread.ID {
		t.Fatalf("the steered message is in the topic: %+v", steered)
	}
}

// What the turn will not get to waits for the next turn, as it would have
// without steering: refused, taken and dropped, or taken by a turn that
// failed.
func TestLoop_WhatTheTurnDoesNotGetToWaitsForTheNext(t *testing.T) {
	for _, c := range []struct {
		name    string
		options map[string]any
		first   store.TurnStatus
	}{
		{"refused", map[string]any{"delay_ms": float64(500), "steer": "refuse"}, store.TurnDone},
		{"dropped", map[string]any{"delay_ms": float64(500), "steer": "drop"}, store.TurnDone},
		{"failed", map[string]any{"delay_ms": float64(500), "fail": true}, store.TurnFailed},
	} {
		t.Run(c.name, func(t *testing.T) {
			l := newLoop(t)
			slow := l.member("Slow", c.options)
			first, thread := l.busyWith(slow, "@Slow build the parser")
			steered := l.say("@Slow use the new grammar", thread.ID, slow)
			eventually(t, func() bool { q := l.queued(); return len(q) == 1 && q[0].MessageID == steered.ID }, "the message kept while it waits")

			eventually(t, func() bool {
				turns := l.turns()
				return len(turns) == 2 && turns[0].TriggerMessageID == steered.ID && turns[0].Status != store.TurnRunning
			}, "a turn of its own for the message")
			turns := l.turns()
			if turns[1].ID != first.ID || turns[1].Status != c.first {
				t.Errorf("the first turn: %+v", turns[1])
			}
			if brief := promptOf(t, turns[0]); !strings.Contains(brief, ">> [alice] @Slow use the new grammar") {
				t.Errorf("the next turn answers it:\n%s", brief)
			}
			if c.name == "failed" && len(l.steersOf(first)) != 1 {
				t.Errorf("the failed turn took it in: %v", l.steersOf(first))
			}
			if q := l.queued(); len(q) != 0 {
				t.Errorf("taken up, it waits no longer: %+v", q)
			}
		})
	}
}

// Only a person's message in the topic of the running turn is passed to
// it: one in the room waits for a turn of its own.
func TestLoop_OnlyTheTurnsTopicIsPassedToIt(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"delay_ms": float64(400)})
	first, _ := l.busyWith(slow, "@Slow build the parser")
	elsewhere := l.say("@Slow and the lexer", "", slow)

	turns := l.waitTurns(2, store.TurnDone, "a turn for each")
	if turns[1].ID != first.ID || turns[0].TriggerMessageID != elsewhere.ID {
		t.Errorf("turns: %+v", turns)
	}
	if steers := l.steersOf(turns[1]); len(steers) != 0 {
		t.Errorf("nothing passed to the first turn: %v", steers)
	}
}

// Someone else's message passed to the turn is answered by it too: its
// last word is addressed to them as well.
func TestLoop_TheTurnAnswersEveryoneWhoseMessageItTookIn(t *testing.T) {
	l := newLoop(t)
	bob, err := l.s.CreateUser(l.ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	slow := l.member("Slow", map[string]any{"delay_ms": float64(600)})
	_, thread := l.busyWith(slow, "@Slow build the parser")
	if _, err := l.h.PostUserMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, ThreadID: thread.ID, UserID: bob.ID, Body: "@Slow keep it small",
		Mentions: []store.Mention{{Kind: store.MentionAgent, ID: slow.ID}}}); err != nil {
		t.Fatal(err)
	}
	done := l.waitTurns(1, store.TurnDone, "Slow's turn")[0]
	reply, err := l.s.GetMessage(l.ctx, done.ReplyMessageID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(reply.Body, "@alice @bob ") {
		t.Errorf("the reply is addressed to both: %q", reply.Body)
	}
	users := 0
	for _, m := range reply.Mentions {
		if m.Kind == store.MentionUser && (m.ID == l.user.ID || m.ID == bob.ID) {
			users++
		}
	}
	if users != 2 {
		t.Errorf("both are mentioned, so it reaches both inboxes: %+v", reply.Mentions)
	}
}

// What was passed to a turn the hub stopped under is not lost: the next
// hub answers it.
func TestLoop_WhatWasPassedOutlivesTheHub(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"delay_ms": float64(5000)})
	_, thread := l.busyWith(slow, "@Slow build the parser")
	steered := l.say("@Slow use the new grammar", thread.ID, slow)
	eventually(t, func() bool { return len(l.steersOf(l.turns()[0])) == 1 }, "the turn to take the message in")

	l.setOptions(slow, map[string]any{"reply": "done"})
	l.restart()
	eventually(t, func() bool {
		turns := l.turns()
		return len(turns) == 2 && turns[0].TriggerMessageID == steered.ID && turns[0].Status == store.TurnDone
	}, "the next hub to answer what was passed")
	if q := l.queued(); len(q) != 0 {
		t.Errorf("answered, it waits no longer: %+v", q)
	}
}
