package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// drain collects every event until the channel closes.
func drain(t *testing.T, turn Turn) []Event {
	t.Helper()
	var events []Event
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-turn.Events():
			if !ok {
				return events
			}
			events = append(events, ev)
		case <-timeout:
			t.Fatal("turn did not finish")
		}
	}
}

func TestFake_EchoesPromptAndStreamsText(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "context\n@fake say hi"})
	if err != nil {
		t.Fatal(err)
	}

	events := drain(t, turn)
	res, err := turn.Result()
	if err != nil {
		t.Fatal(err)
	}

	if res.Output != "Echo: @fake say hi" {
		t.Errorf("Output = %q", res.Output)
	}
	var text strings.Builder
	for _, ev := range events {
		if ev.At.IsZero() {
			t.Error("events must be timestamped")
		}
		if ev.Kind == EventText {
			text.WriteString(ev.Text)
		}
	}
	if text.String() != res.Output {
		t.Errorf("text events concatenate to %q, want the output %q", text.String(), res.Output)
	}
	if !strings.HasPrefix(res.SessionRef, "fake-") {
		t.Errorf("a first turn should mint a session ref, got %q", res.SessionRef)
	}
	if events[0].Kind != EventSession || events[0].SessionRef != res.SessionRef {
		t.Errorf("the session should be reported first, as %q: got %+v", res.SessionRef, events[0])
	}
}

func TestFake_NamesItsSessionAfterTheHubsKey(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "hi", Session: Session{Key: "key-1"}})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	res, err := turn.Result()
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionRef != "key-1" {
		t.Errorf("SessionRef = %q, want the key it was given", res.SessionRef)
	}
}

func TestFake_OptionsSteerTheTurn(t *testing.T) {
	turn, _ := NewFake().StartTurn(context.Background(), TurnSpec{
		Prompt:  "x",
		Session: Session{Ref: "fake-existing", Resume: true},
		Options: map[string]any{"reply": "custom", "tool": true},
	})

	events := drain(t, turn)
	res, err := turn.Result()
	if err != nil {
		t.Fatal(err)
	}

	if res.Output != "custom" || res.SessionRef != "fake-existing" {
		t.Errorf("unexpected result: %+v", res)
	}
	kinds := map[EventKind]int{}
	for _, ev := range events {
		kinds[ev.Kind]++
	}
	if kinds[EventToolCall] != 1 || kinds[EventToolResult] != 1 {
		t.Errorf("tool option should emit a call/result pair, got %v", kinds)
	}
}

func TestFake_Fail(t *testing.T) {
	turn, _ := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "x", Options: map[string]any{"fail": true}})

	events := drain(t, turn)
	if _, err := turn.Result(); err == nil {
		t.Fatal("scripted failure should surface as an error")
	}
	if last := events[len(events)-1]; last.Kind != EventError {
		t.Errorf("last event should be the error, got %+v", last)
	}
}

func TestFake_Cancel(t *testing.T) {
	turn, _ := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "x", Options: map[string]any{"delay_ms": float64(5000)}})

	turn.Cancel()

	drain(t, turn) // must close promptly rather than after the 5s delay
	if _, err := turn.Result(); !errors.Is(err, ErrTurnCancelled) {
		t.Errorf("Result error = %v, want ErrTurnCancelled", err)
	}
}

func TestFake_CancelViaContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	turn, _ := NewFake().StartTurn(ctx, TurnSpec{Prompt: "x", Options: map[string]any{"delay_ms": float64(5000)}})

	cancel()

	drain(t, turn)
	if _, err := turn.Result(); !errors.Is(err, ErrTurnCancelled) {
		t.Errorf("Result error = %v, want ErrTurnCancelled", err)
	}
}

// awaitApproval reads events until the approval request arrives and
// returns it along with the events before it.
func awaitApproval(t *testing.T, turn Turn) (Event, []Event) {
	t.Helper()
	var before []Event
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev, ok := <-turn.Events():
			if !ok {
				t.Fatal("turn ended without asking for approval")
			}
			if ev.Kind == EventApprovalRequest {
				return ev, before
			}
			before = append(before, ev)
		case <-timeout:
			t.Fatal("no approval request arrived")
		}
	}
}

func TestFake_ApprovalAllowedRunsTheCommand(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "go", Options: map[string]any{"approval": true, "reply": "built"}})
	if err != nil {
		t.Fatal(err)
	}

	req, _ := awaitApproval(t, turn)
	if req.ApprovalID == "" || req.Tool != "Bash" || req.Input != `{"command":"make test"}` {
		t.Fatalf("unexpected request: %+v", req)
	}
	if err := turn.Answer(req.ApprovalID, Decision{Allow: true}); err != nil {
		t.Fatal(err)
	}

	events := drain(t, turn)
	res, err := turn.Result()
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "built" {
		t.Errorf("Output = %q", res.Output)
	}
	if len(events) < 2 || events[0].Kind != EventToolCall || events[0].Tool != "Bash" || events[1].Kind != EventToolResult {
		t.Errorf("an allowed request should be followed by the tool running, got %+v", events)
	}
	// A request is answered once.
	if err := turn.Answer(req.ApprovalID, Decision{Allow: true}); !errors.Is(err, ErrUnknownApproval) {
		t.Errorf("second answer: got %v, want ErrUnknownApproval", err)
	}
}

func TestFake_ApprovalDeniedIsReported(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "go", Options: map[string]any{"approval": true}})
	if err != nil {
		t.Fatal(err)
	}

	req, _ := awaitApproval(t, turn)
	if err := turn.Answer(req.ApprovalID, Decision{Allow: false, Message: "not on main"}); err != nil {
		t.Fatal(err)
	}

	events := drain(t, turn)
	res, err := turn.Result()
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "Denied: not on main" {
		t.Errorf("Output = %q", res.Output)
	}
	for _, ev := range events {
		if ev.Kind == EventToolCall {
			t.Error("a denied command must not run")
		}
	}
}

func TestFake_CancelWhileApprovalPending(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "go", Options: map[string]any{"approval": true}})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := awaitApproval(t, turn)

	turn.Cancel()

	drain(t, turn)
	if _, err := turn.Result(); !errors.Is(err, ErrTurnCancelled) {
		t.Errorf("Result error = %v, want ErrTurnCancelled", err)
	}
	if err := turn.Answer(req.ApprovalID, Decision{Allow: true}); !errors.Is(err, ErrUnknownApproval) {
		t.Errorf("answer after cancel: got %v, want ErrUnknownApproval", err)
	}
}

func TestFake_AnswerUnknownApproval(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := turn.Answer("nope", Decision{Allow: true}); !errors.Is(err, ErrUnknownApproval) {
		t.Errorf("got %v, want ErrUnknownApproval", err)
	}
	drain(t, turn)
}

func TestFake_CompactsWhenAsked(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "hi", Options: map[string]any{"compact": true}})
	if err != nil {
		t.Fatal(err)
	}
	if got := compactionPhases(drain(t, turn)); got != "start,end" {
		t.Errorf("compaction phases = %q, want start then end", got)
	}
}
