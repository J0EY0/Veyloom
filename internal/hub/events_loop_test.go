package hub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// collectUntil reads events until one of kind arrives, returning them all.
func collectUntil(t *testing.T, sub Subscription, kind EventKind) []Event {
	t.Helper()
	var got []Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-sub.Events():
			if !ok {
				t.Fatalf("subscription closed (lagged %v) before %s; got %v", sub.Lagged(), kind, kinds(got))
			}
			got = append(got, ev)
			if ev.Kind == kind {
				return got
			}
		case <-deadline:
			t.Fatalf("no %s event within 5s; got %v", kind, kinds(got))
		}
	}
}

func kinds(events []Event) []EventKind {
	out := make([]EventKind, len(events))
	for i, ev := range events {
		out[i] = ev.Kind
	}
	return out
}

// A running turn's transcript reads as far as it is written whole, and its
// events carry the numbers the room heard them by, from 1: what a page
// opened late lays under the live events.
func TestLoop_ARunningTurnsTranscriptReadsAsFarAsItIsWritten(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"tool": true, "reply": "done", "delay_ms": 1500})
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()
	l.say("@Slow go", "", slow)

	// The call and its result are heard while the turn waits.
	var turnID string
	var heard []runtime.Event
	for !slices.ContainsFunc(heard, func(ev runtime.Event) bool { return ev.Kind == runtime.EventToolResult }) {
		for _, ev := range collectUntil(t, sub, EventTurnEvent) {
			switch ev.Kind {
			case EventTurnStarted:
				turnID = ev.Turn.ID
			case EventTurnEvent:
				heard = append(heard, *ev.TurnEvent)
			}
		}
	}

	n, ok := l.h.TranscriptSoFar(turnID)
	if !ok {
		t.Fatal("a running turn has a transcript so far")
	}
	data, err := os.ReadFile(filepath.Join(l.h.cfg.TranscriptDir, turnID+".jsonl"))
	if err != nil || n == 0 || int64(len(data)) < n || data[n-1] != '\n' {
		t.Fatalf("the transcript is whole records up to %d: %q %v", n, data, err)
	}
	var kinds []runtime.EventKind
	var numbers []int64
	for line := range strings.SplitSeq(strings.TrimSpace(string(data[:n])), "\n") {
		var rec transcriptLine
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("a torn record %q: %v", line, err)
		}
		if rec.Event != nil {
			kinds, numbers = append(kinds, rec.Event.Kind), append(numbers, rec.Event.Seq)
		}
	}
	// The session, which only the transcript keeps, then what the room heard.
	if !slices.Equal(numbers, []int64{1, 2, 3, 4}) || kinds[0] != runtime.EventSession {
		t.Errorf("the transcript's events: %v numbered %v", kinds, numbers)
	}
	for i, ev := range heard {
		if ev.Seq != int64(i+2) || ev.Kind != kinds[i+1] {
			t.Errorf("the room heard %s as %d", ev.Kind, ev.Seq)
		}
	}
	l.waitTurns(1, store.TurnDone, "the slow turn")
	if _, ok := l.h.TranscriptSoFar(turnID); ok {
		t.Error("a finished turn is read whole, as it was written")
	}
}

func TestLoop_SubscribersSeeTheWholeTurn(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", map[string]any{"tool": true, "reply": "hi"})
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()

	msg := l.say("@Echo hello", "", echo)
	events := collectUntil(t, sub, EventTurnFinished)

	if events[0].Kind != EventMessage || events[0].Message == nil || events[0].Message.ID != msg.ID {
		t.Errorf("first event should be the user's message, got %+v", events[0])
	}
	// The topic root arrives empty, together with the thread it heads.
	root := events[1]
	// The topic opens with its number, for the chat to call it by.
	if root.Kind != EventMessage || root.Message == nil || root.Message.SenderKind != store.SenderAgent || root.Message.Body != "" || root.Thread == nil || root.Thread.ID == "" || root.Thread.Number == 0 {
		t.Fatalf("second event should be the empty topic root with its thread, got %+v", root)
	}
	if events[2].Kind != EventTurnStarted || events[2].Turn == nil || events[2].Turn.Status != store.TurnRunning || events[2].Turn.MemberID != echo.ID || events[2].Turn.ThreadID != root.Thread.ID {
		t.Errorf("third event should be the turn starting in that thread, got %+v", events[2])
	}
	var turnEvents []runtime.EventKind
	var filled *store.Message
	for _, ev := range events[3 : len(events)-1] {
		switch ev.Kind {
		case EventTurnEvent:
			if ev.TurnID != events[2].Turn.ID || ev.TurnEvent == nil {
				t.Errorf("turn event should name the turn: %+v", ev)
			}
			turnEvents = append(turnEvents, ev.TurnEvent.Kind)
		case EventMessage:
			switch {
			case ev.Message.ID == root.Message.ID:
				filled = ev.Message
				if ev.Thread == nil || ev.Thread.ID != root.Thread.ID {
					t.Errorf("the filled-in root should still name its thread: %+v", ev)
				}
			default:
				t.Errorf("unexpected message %+v", ev.Message)
			}
		default:
			t.Errorf("unexpected event %+v", ev)
		}
	}
	// The fake runtime's status, tool call, tool result and text, in order.
	if len(turnEvents) < 4 || turnEvents[0] != runtime.EventStatus || turnEvents[1] != runtime.EventToolCall || turnEvents[2] != runtime.EventToolResult || turnEvents[len(turnEvents)-1] != runtime.EventText {
		t.Errorf("turn events = %v", turnEvents)
	}
	// A turn that used a tool addresses its answer to whoever asked, so the
	// root is re-sent with the mention: no other message follows it.
	if filled == nil || filled.Body != "@alice hi" || filled.TurnID != events[2].Turn.ID || len(filled.Mentions) != 1 || filled.Mentions[0].ID != l.user.ID {
		t.Errorf("the root should be re-sent once the reply text is known, addressed to the asker, got %+v", filled)
	}
	last := events[len(events)-1]
	if last.Turn == nil || last.Turn.Status != store.TurnDone || last.Turn.ReplyMessageID != root.Message.ID || last.Turn.EndedAt == nil {
		t.Errorf("turn_finished should carry the final turn, got %+v", last.Turn)
	}
	for _, ev := range events {
		if ev.RoomID != l.room.ID || ev.At.IsZero() {
			t.Errorf("every event names the room and is timestamped: %+v", ev)
		}
	}
}

func TestLoop_SubscribersSeeApprovals(t *testing.T) {
	l := newLoop(t)
	careful := l.member("Careful", map[string]any{"approval": true, "reply": "built"})
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()

	l.say("@Careful build", "", careful)
	events := collectUntil(t, sub, EventApprovalRequested)
	requested := events[len(events)-1].Approval
	if requested == nil || requested.Status != store.ApprovalPending || requested.Tool != "Bash" {
		t.Fatalf("approval_requested should carry the pending approval, got %+v", requested)
	}
	// The announcement in the thread precedes the approval event, so a UI
	// that renders the card in place of the note has the note already.
	if note := events[len(events)-2]; note.Kind != EventMessage || note.Message.SenderKind != store.SenderSystem || note.Message.ID != requested.MessageID {
		t.Errorf("expected the announcement right before the approval, got %+v", note)
	}

	if _, err := l.h.DecideApproval(l.ctx, requested.ID, l.user.ID, runtime.Decision{Allow: true}, ""); err != nil {
		t.Fatal(err)
	}
	events = collectUntil(t, sub, EventTurnFinished)
	if events[0].Kind != EventApprovalDecided || events[0].Approval.Status != store.ApprovalAllowed || events[0].Approval.DecidedBy != l.user.ID {
		t.Errorf("the decision should be pushed first, got %+v", events[0])
	}
	if events[len(events)-1].Turn.Status != store.TurnDone {
		t.Errorf("turn should finish after approval, got %+v", events[len(events)-1].Turn)
	}
}

func TestLoop_CancelledTurnResolvesApprovalsLive(t *testing.T) {
	l := newLoop(t)
	careful := l.member("Careful", map[string]any{"approval": true})
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()

	l.say("@Careful build", "", careful)
	events := collectUntil(t, sub, EventApprovalRequested)
	a := events[len(events)-1].Approval
	if err := l.h.CancelTurn(l.ctx, a.TurnID, false); err != nil {
		t.Fatal(err)
	}
	events = collectUntil(t, sub, EventTurnFinished)
	var decided *store.Approval
	for _, ev := range events {
		if ev.Kind == EventApprovalDecided {
			decided = ev.Approval
		}
	}
	if decided == nil || decided.ID != a.ID || decided.Status != store.ApprovalCancelled {
		t.Errorf("expected the approval to be pushed as cancelled, got %+v", decided)
	}
	if events[len(events)-1].Turn.Status != store.TurnCancelled {
		t.Errorf("turn should be cancelled, got %+v", events[len(events)-1].Turn)
	}
}

// What reaches a person's inbox reaches them live, from every room: the
// requests waiting for them and the answers addressed to them, nothing of
// the turns themselves.
func TestLoop_InboxSubscribersSeeWhatReachesThem(t *testing.T) {
	l := newLoop(t)
	careful := l.member("Careful", map[string]any{"approval": true, "reply": "built"})
	sub := l.h.SubscribeInbox(l.user.ID)
	defer sub.Close()

	l.say("@Careful build", "", careful)
	events := collectUntil(t, sub, EventApprovalRequested)
	if got := kinds(events); len(got) != 1 {
		t.Errorf("before the request: %v", got)
	}
	if _, err := l.h.DecideApproval(l.ctx, events[0].Approval.ID, l.user.ID, runtime.Decision{Allow: true}, ""); err != nil {
		t.Fatal(err)
	}
	events = collectUntil(t, sub, EventMessage)
	answer := events[len(events)-1].Message
	if got := kinds(events); len(got) != 2 || got[0] != EventApprovalDecided {
		t.Errorf("after the decision: %v", got)
	}
	if answer.Body != "@alice built" || answer.MemberID != careful.ID {
		t.Errorf("the answer addressed to the person: %+v", answer)
	}
	l.waitTurns(1, store.TurnDone, "the turn")
	select {
	case ev, ok := <-sub.Events():
		if ok {
			t.Errorf("more arrived: %+v", ev)
		}
	case <-time.After(300 * time.Millisecond):
	}
}

// What a person reads of their inbox is told to their inbox streams, so
// every tab counts again, and to nobody else's; reading what was read
// already tells nothing.
func TestLoop_InboxReadIsToldToThePerson(t *testing.T) {
	l := newLoop(t)
	bob, err := l.s.CreateUser(l.ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := l.s.CreateMessage(l.ctx, store.NewMessage{
		RoomID: l.room.ID, SenderKind: store.SenderUser, UserID: bob.ID, Body: "@alice look",
		Mentions: []store.Mention{{Kind: store.MentionUser, ID: l.user.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	sub := l.h.SubscribeInbox(l.user.ID)
	defer sub.Close()
	other := l.h.SubscribeInbox(bob.ID)
	defer other.Close()

	if n, err := l.h.MarkInboxRead(l.ctx, l.user.ID, store.InboxRead{MessageIDs: []string{msg.ID}}); err != nil || n != 1 {
		t.Fatalf("read: %d %v", n, err)
	}
	if read := collectUntil(t, sub, EventInboxRead); read[len(read)-1].UserID != l.user.ID {
		t.Errorf("told: %+v", read[len(read)-1])
	}
	if n, _ := l.h.MarkInboxRead(l.ctx, l.user.ID, store.InboxRead{MessageIDs: []string{msg.ID}}); n != 0 {
		t.Errorf("read again: %d", n)
	}
	for _, s := range []Subscription{sub, other} {
		select {
		case ev, ok := <-s.Events():
			if ok {
				t.Errorf("more arrived: %+v", ev)
			}
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// A topic being worked on says what it was asked, without the @ that
// asked it, for the sidebar to list it by (docs/webui.md §0).
func TestLoop_RunningTopicsSayWhatWasAsked(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"delay_ms": float64(1500)})
	l.say("@Slow 把标签功能加上：要能按标签过滤", "", slow)
	l.waitTurns(1, store.TurnRunning, "the turn to start")
	var topics []store.RunningTopic
	eventually(t, func() bool {
		var err error
		topics, err = l.s.ListRunningTopics(l.ctx)
		return err == nil && len(topics) == 1
	}, "the topic to be listed")
	if topics[0].Ask != "把标签功能加上" || !slices.Equal(topics[0].Members, []string{"Slow"}) {
		t.Errorf("the running topic: %+v", topics[0])
	}
	l.waitTurns(1, store.TurnDone, "the turn")
}
