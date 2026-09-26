package hub

import (
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// sendCall is a fake turn's send_message call.
func sendCall(text, to string) map[string]any {
	args := map[string]any{"text": text}
	if to != "" {
		args["to"] = to
	}
	return map[string]any{"tool": runtime.MessageToolSend, "args": args}
}

// A member posts in its topic while it works, and the member it names is
// woken at once, in the same piece of work (docs/design.md 5.22).
func TestLoop_SendMessageWakesAMemberAtOnce(t *testing.T) {
	l := newLoop(t)
	lead := l.member("Lead", map[string]any{"tool_calls": []any{sendCall("@Coder please build the API", "")}, "reply": "Planned.", "delay_ms": 800, "summary_reply": "Coder built it."})
	coder := l.member("Coder", map[string]any{"reply": "Built."})
	msg := l.say("@Lead plan it", "", lead)
	// Lead, Coder, and Lead summing up.
	turns := l.settle(3, "Lead and the member it woke")
	thread := l.topic(msg)
	leadTurn := firstTurnOf(turns, lead)
	coderTurn, _ := turnOf(turns, coder, store.TurnChat)
	if coderTurn.ThreadID != thread.ID || coderTurn.WokenByTurnID != leadTurn.ID || coderTurn.ChainMessageID != msg.ID {
		t.Errorf("Coder's turn: %+v", coderTurn)
	}
	if leadTurn.EndedAt == nil || !coderTurn.StartedAt.Before(*leadTurn.EndedAt) {
		t.Errorf("Coder should start while Lead still works: started %v, Lead ended %v", coderTurn.StartedAt, leadTurn.EndedAt)
	}
	sent := slices.IndexFunc(l.replies(thread.ID, store.SenderAgent), func(m store.Message) bool {
		return m.MemberID == lead.ID && m.Body == "@Coder please build the API" && slices.Contains(m.Mentions, store.Mention{Kind: store.MentionAgent, ID: coder.ID})
	})
	if sent < 0 {
		t.Errorf("Lead's message is not in the topic: %+v", l.replies(thread.ID, store.SenderAgent))
	}
	if tx := transcriptOf(t, leadTurn); !strings.Contains(tx, "Posted in this topic. Woken, working alongside you now: Coder.") {
		t.Errorf("what Lead was told:\n%s", tx)
	}
	spec := specOf(t, leadTurn)
	if !slices.Contains(spec.ExtraTools, runtime.MessageToolSend) || !strings.Contains(spec.Prompt, "Agents may wake 30 more turns") {
		t.Errorf("a chat turn has the tool and is told of it: %v", spec.ExtraTools)
	}
	if !strings.Contains(specOf(t, coderTurn).Prompt, "Agents may wake 29 more turns") {
		t.Error("the woken turn is told what is left")
	}
}

// A member posts in the room: naming members starts a topic at the
// message, where they work; naming no one wakes no one.
func TestLoop_SendMessageToTheRoomStartsATopic(t *testing.T) {
	l := newLoop(t)
	lead := l.member("Lead", map[string]any{"tool_calls": []any{
		sendCall("@Coder @Tester split the login work", "room"),
		sendCall("Heads up: the build is green", "room"),
	}, "reply": "Sent.", "summary_reply": "Both are done."})
	coder := l.member("Coder", map[string]any{"reply": "Mine."})
	tester := l.member("Tester", map[string]any{"reply": "Tests."})
	msg := l.say("@Lead organise it", "", lead)
	turns := l.settle(4, "Lead, the two it woke, and Lead summing up")
	leadTurn := firstTurnOf(turns, lead)

	var split, note store.Message
	for _, m := range l.topLevel() {
		if m.MemberID == lead.ID && m.Body == "@Coder @Tester split the login work" {
			split = m
		}
		if m.MemberID == lead.ID && m.Body == "Heads up: the build is green" {
			note = m
		}
	}
	if split.ID == "" || note.ID == "" {
		t.Fatalf("Lead's messages in the room: %+v", l.topLevel())
	}
	topic, err := l.s.ThreadOfMessage(l.ctx, split.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range []store.Member{coder, tester} {
		turn, _ := turnOf(turns, member, store.TurnChat)
		if turn.ThreadID != topic.ID || turn.WokenByTurnID != leadTurn.ID || turn.ChainMessageID != msg.ID {
			t.Errorf("%s's turn: %+v", member.DisplayName, turn)
		}
	}
	if _, err := l.s.ThreadOfMessage(l.ctx, note.ID); err == nil {
		t.Error("a message naming no one started a topic")
	}
	tx := transcriptOf(t, leadTurn)
	for _, want := range []string{"Posted in the room, starting topic #", "Woken, working alongside you now: Coder, Tester.", "It names no member, so it wakes no one."} {
		if !strings.Contains(tx, want) {
			t.Errorf("Lead was not told %q:\n%s", want, tx)
		}
	}
}

// A turn posts ten messages at most; one that names the person reaches
// their inbox.
func TestLoop_SendMessageLimitsAndReachesThePerson(t *testing.T) {
	l := newLoop(t)
	calls := []any{sendCall("@alice have a look", "")}
	for range 10 {
		calls = append(calls, sendCall("one more note", ""))
	}
	lead := l.member("Lead", map[string]any{"tool_calls": calls, "reply": "Done."})
	msg := l.say("@Lead go", "", lead)
	turns := l.waitTurns(1, store.TurnDone, "Lead's turn")
	thread := l.topic(msg)
	var sent []store.Message
	for _, m := range l.replies(thread.ID, store.SenderAgent) {
		if m.Body == "@alice have a look" || m.Body == "one more note" {
			sent = append(sent, m)
		}
	}
	if len(sent) != 10 {
		t.Errorf("want 10 messages sent, got %d", len(sent))
	}
	if len(sent) > 0 && !slices.Contains(sent[0].Mentions, store.Mention{Kind: store.MentionUser, ID: l.user.ID}) {
		t.Errorf("the message naming the person: %+v", sent[0])
	}
	if tx := transcriptOf(t, turns[0]); !strings.Contains(tx, "a turn sends at most 10 messages") {
		t.Errorf("the eleventh message should be refused:\n%s", tx)
	}
}

// A reply that names a member the turn sent a message to only tells of it:
// the member is not woken again (docs/design.md 5.22).
func TestLoop_SendMessageThenNamingInTheReplyWakesOnce(t *testing.T) {
	l := newLoop(t)
	lead := l.member("Lead", map[string]any{"tool_calls": []any{sendCall("@Coder please build the API", "")}, "reply": "I asked @Coder to build the API.", "summary_reply": "Coder built it."})
	coder := l.member("Coder", map[string]any{"reply": "Built."})
	l.say("@Lead plan it", "", lead)
	// Lead, Coder once, and Lead summing up.
	turns := l.settle(3, "Lead and the member it woke, once")
	if work, ok := turnOf(turns, coder, store.TurnChat); !ok || work.WokenByTurnID == "" {
		t.Errorf("Coder's turn: %+v", turns)
	}
}

// firstTurnOf is member's earliest turn among turns, newest first.
func firstTurnOf(turns []store.Turn, member store.Member) store.Turn {
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].MemberID == member.ID {
			return turns[i]
		}
	}
	return store.Turn{}
}
