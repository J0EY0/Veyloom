package hub

import (
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// A person's message that names nobody goes to one member (design.md 4.2):
// in the room, the leader when there are several; in a topic, the member
// the person is talking with there, and the leader when that one is gone.

func TestLoop_AnUnaddressedMessageToTheRoomGoesToTheLeader(t *testing.T) {
	l := newLoop(t)
	lead := l.member("Lead", map[string]any{"reply": "I will see to it."})
	l.member("Coder", map[string]any{"reply": "on it"})
	l.say("tidy up the README", "")
	turn := l.waitTurns(1, store.TurnDone, "the leader's turn")[0]
	time.Sleep(200 * time.Millisecond)
	if turns := l.turns(); len(turns) != 1 || turn.MemberID != lead.ID {
		t.Errorf("the leader alone takes it: %+v", turns)
	}
}

func TestLoop_AnUnaddressedReplyGoesToWhomThePersonTalksWith(t *testing.T) {
	l := newLoop(t)
	lead := l.member("Lead", map[string]any{"reply": "noted"})
	coder := l.member("Coder", map[string]any{"reply": "fixed"})
	l.say("@Coder fix the bug", "", coder)
	first := l.waitTurns(1, store.TurnDone, "Coder's turn")[0]
	// The leader has had a word there since, to nobody in particular: the
	// last to speak, but not the one alice talks with.
	if _, err := l.s.CreateMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, ThreadID: first.ThreadID, SenderKind: store.SenderAgent, MemberID: lead.ID, Body: "looks right to me"}); err != nil {
		t.Fatal(err)
	}
	l.say("and add a test for it", first.ThreadID)
	second := l.waitTurns(2, store.TurnDone, "the follow-up")[0]
	if second.MemberID != coder.ID || second.ThreadID != first.ThreadID {
		t.Errorf("the follow-up goes to Coder, whom alice talks with: %+v", second)
	}

	// Coder switched off, the topic's next word goes to the leader.
	off := false
	if _, err := l.s.UpdateMember(l.ctx, coder.ID, store.MemberPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	l.say("anyone there?", first.ThreadID)
	third := l.waitTurns(3, store.TurnDone, "the leader's turn")[0]
	if third.MemberID != lead.ID || third.ThreadID != first.ThreadID {
		t.Errorf("Coder off, the leader takes it: %+v", third)
	}
}

// Naming a member in a topic makes it the one the person talks with there
// at once, before its turn has begun: a word without an @ that follows,
// while the member is still busy elsewhere, goes to it too, not to the
// member the person talked with before.
func TestLoop_AWordAfterNamingAMemberGoesToIt(t *testing.T) {
	l := newLoop(t)
	lead := l.member("Lead", map[string]any{"reply": "noted"})
	coder := l.member("Coder", map[string]any{"reply": "done", "delay_ms": 1500})
	l.say("@Lead look into the failing migration", "", lead)
	asked := l.waitTurns(1, store.TurnDone, "Lead's turn")[0]
	// Coder is busy with something else.
	l.say("@Coder build the release", "", coder)
	eventually(t, func() bool {
		turns := l.turns()
		return len(turns) == 2 && turns[0].MemberID == coder.ID && turns[0].Status == store.TurnRunning
	}, "Coder at work")
	l.say("@Coder take this over", asked.ThreadID, coder)
	l.say("it is in db/migrate.go", asked.ThreadID)
	// Both wait for Coder, and it takes them up in the topic once free.
	eventuallyWithin(t, 15*time.Second, func() bool {
		taken := false
		for _, turn := range l.turns() {
			if turn.Status == store.TurnRunning {
				return false
			}
			taken = taken || (turn.MemberID == coder.ID && turn.ThreadID == asked.ThreadID)
		}
		return taken
	}, "Coder's turn in the topic")
	time.Sleep(300 * time.Millisecond)
	for _, turn := range l.turns() {
		if turn.MemberID == lead.ID && turn.ID != asked.ID {
			t.Errorf("Lead was woken again, the word after naming Coder going to it: %+v", turn)
		}
	}
}

// The leader, given a word to the room that names no member, is told so,
// and hands it on to the member it fits, who takes it up in the same piece
// of work; the leader sums up for the person.
func TestLoop_TheLeaderHandsOnWhatNamesNoOne(t *testing.T) {
	l := newLoop(t)
	lead := l.member("Lead", map[string]any{"tool_calls": []any{sendCall("@Coder the login page is slow, please profile it", "")},
		"reply": "Handed to Coder.", "summary_reply": "Coder found the slow query."})
	coder := l.member("Coder", map[string]any{"reply": "Profiled: the session query."})
	msg := l.say("the login page is slow, can someone look into it?", "")
	turns := l.settle(3, "Lead, Coder, and Lead summing up")
	leadTurn := firstTurnOf(turns, lead)
	coderTurn, _ := turnOf(turns, coder, store.TurnChat)
	if leadTurn.TriggerMessageID != msg.ID || coderTurn.WokenByTurnID != leadTurn.ID || coderTurn.ChainMessageID != msg.ID {
		t.Errorf("Lead took it and woke Coder in its piece of work: %+v then %+v", leadTurn, coderTurn)
	}
	spec := specOf(t, leadTurn)
	if !strings.Contains(spec.Prompt, "The person addressed this to no member, so it came to you as the project's leader") ||
		!strings.Contains(spec.SystemPrompt, "As the project's leader, you get a person's message to the room that names no member") {
		t.Errorf("the leader is told why it has this:\n%s\n%s", spec.SystemPrompt, spec.Prompt)
	}
	if coderSpec := specOf(t, coderTurn); strings.Contains(coderSpec.Prompt, "came to you as the project's leader") || strings.Contains(coderSpec.SystemPrompt, "As the project's leader") {
		t.Errorf("Coder is no leader:\n%s", coderSpec.Prompt)
	}
}
