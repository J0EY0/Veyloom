package hub

import (
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Every turn leaves a word in its topic (docs/design.md 5.24).

// saidIn is what agents said in a topic, its head first: what a turn sends
// to a topic it opened heads it (sayInTopic).
func (l *loop) saidIn(thread store.Thread) []store.Message {
	l.t.Helper()
	said := l.replies(thread.ID, store.SenderAgent)
	if root := l.root(thread); root.SenderKind == store.SenderAgent && root.Body != "" {
		said = append([]store.Message{root}, said...)
	}
	return said
}

// What a turn sends to the topic it opened, having said nothing else, is
// its word: the topic's head, addressed to the person, with no "(no reply)"
// made up for it.
func TestLoop_WhatATurnSendsIsItsWord(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"tool_calls": []any{sendCall("PINEAPPLE", "")}, "quiet": true})
	msg := l.say("@Slow say the word", "", slow)
	turn := l.waitTurns(1, store.TurnDone, "Slow's turn")[0]
	thread := l.topic(msg)
	root := l.root(thread)
	if root.Body != "@alice PINEAPPLE" || !slices.Contains(root.Mentions, store.Mention{Kind: store.MentionUser, ID: l.user.ID}) || turn.ReplyMessageID != root.ID {
		t.Errorf("the head of the topic: %+v (the turn's reply %s)", root, turn.ReplyMessageID)
	}
	if said := l.replies(thread.ID, store.SenderAgent); len(said) != 0 {
		t.Errorf("nothing more is said: %+v", said)
	}
	if tx := transcriptOf(t, turn); strings.Contains(tx, `"reply_asked"`) {
		t.Errorf("a turn that said its word is not asked for one:\n%s", tx)
	}

	// In a topic it was asked in: under the question, addressed.
	l.setOptions(slow, map[string]any{"tool_calls": []any{sendCall("MANGO", "")}, "quiet": true})
	l.say("@Slow and again", thread.ID, slow)
	second := l.waitTurns(2, store.TurnDone, "Slow's second turn")[0]
	said := l.replies(thread.ID, store.SenderAgent)
	if len(said) != 1 || said[0].Body != "@alice MANGO" || said[0].TurnID != second.ID || second.ReplyMessageID != said[0].ID {
		t.Errorf("what it sent is its word: %+v", said)
	}
	for _, m := range append(said, root) {
		if strings.Contains(m.Body, "(no reply)") {
			t.Errorf("a reply made up: %+v", m)
		}
	}
}

// A turn that said nothing in its topic is asked for its reply, once, in its
// session: what it says then is its word.
func TestLoop_ASilentTurnIsAskedForItsReply(t *testing.T) {
	l := newLoop(t)
	quiet := l.member("Quiet", map[string]any{"tool": true, "quiet": true, "when_asked": "I read the README; nothing needs changing."})
	msg := l.say("@Quiet look at the README", "", quiet)
	turn := l.waitTurns(1, store.TurnDone, "the turn")[0]
	root := l.root(l.topic(msg))
	if root.Body != "@alice I read the README; nothing needs changing." || turn.ReplyMessageID != root.ID {
		t.Errorf("the reply it was asked for: %+v", root)
	}
	tx := transcriptOf(t, turn)
	if strings.Count(tx, `"reply_asked"`) != 1 || !strings.Contains(tx, "without a word in this topic") {
		t.Errorf("asked once, as the transcript says:\n%s", tx)
	}
	// Both runs' spending is the turn's: more read than the asking alone.
	asked := int64(len([]rune(runtime.ReplyAsk)))
	if turn.Usage.InputTokens <= asked || turn.Usage.OutputTokens != int64(len([]rune("I read the README; nothing needs changing."))) {
		t.Errorf("what both runs spent: %+v", turn.Usage)
	}
}

// A turn that says nothing even asked gets no word made up for it: the head
// of a topic it opened stays empty, a note says so in a topic it was asked
// in, and nobody is addressed.
func TestLoop_ATurnThatSaysNothingEvenAsked(t *testing.T) {
	l := newLoop(t)
	mute := l.member("Mute", map[string]any{"tool": true, "quiet": true})
	msg := l.say("@Mute go", "", mute)
	turn := l.waitTurns(1, store.TurnDone, "the turn")[0]
	thread := l.topic(msg)
	if root := l.root(thread); root.Body != "" || len(root.Mentions) != 0 || turn.ReplyMessageID != "" {
		t.Errorf("the head stays empty: %+v (the turn's reply %q)", root, turn.ReplyMessageID)
	}

	l.say("@Mute and again", thread.ID, mute)
	l.waitTurns(2, store.TurnDone, "the second turn")
	if said := l.replies(thread.ID, store.SenderAgent); len(said) != 0 {
		t.Errorf("no word made up: %+v", said)
	}
	if notes := l.notes(thread.ID); countContaining(notes, "Mute ended its turn without a word in this topic.") != 1 {
		t.Errorf("the note: %q", notes)
	}
}

// Asking fails, the session not resuming: the turn ends as it was, done
// and silent, not failed.
func TestLoop_AskingForAReplyThatFails(t *testing.T) {
	l := newLoop(t)
	mute := l.member("Mute", map[string]any{"tool": true, "quiet": true, "fail_on_resume": true})
	l.say("@Mute go", "", mute)
	turn := l.waitTurns(1, store.TurnDone, "the turn, done all the same")[0]
	if turn.Error != "" || turn.ReplyMessageID != "" || !strings.Contains(transcriptOf(t, turn), `"reply_asked"`) {
		t.Errorf("turn = %+v", turn)
	}
}
