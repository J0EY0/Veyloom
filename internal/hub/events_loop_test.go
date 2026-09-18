package hub

import (
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
	if root.Kind != EventMessage || root.Message == nil || root.Message.SenderKind != store.SenderAgent || root.Message.Body != "" || root.Thread == nil || root.Thread.ID == "" {
		t.Fatalf("second event should be the empty topic root with its thread, got %+v", root)
	}
	if events[2].Kind != EventTurnStarted || events[2].Turn == nil || events[2].Turn.Status != store.TurnRunning || events[2].Turn.MemberID != echo.ID || events[2].Turn.ThreadID != root.Thread.ID {
		t.Errorf("third event should be the turn starting in that thread, got %+v", events[2])
	}
	var turnEvents []runtime.EventKind
	var filled, closing *store.Message
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
			case ev.Message.SenderKind == store.SenderAgent && ev.Message.ThreadID == "":
				closing = ev.Message
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
	if filled == nil || filled.Body != "hi" || filled.TurnID != events[2].Turn.ID {
		t.Errorf("the root should be re-sent once the reply text is known, got %+v", filled)
	}
	if closing == nil || closing.Body != "@alice hi" || len(closing.Mentions) != 1 || closing.Mentions[0].ID != l.user.ID {
		t.Errorf("a turn that used a tool closes with a message mentioning the asker, got %+v", closing)
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

	if _, err := l.h.DecideApproval(l.ctx, requested.ID, l.user.ID, runtime.Decision{Allow: true}); err != nil {
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
	if err := l.h.CancelTurn(l.ctx, a.TurnID); err != nil {
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
