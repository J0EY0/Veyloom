package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/protocol"
)

// connectedWorker starts a worker with the fake engine and completes the
// handshake, returning the hub's end of the pipe.
func connectedWorker(t *testing.T, cfg Config) (protocol.Conn, context.CancelFunc) {
	t.Helper()
	cfg.Name = "laptop"
	w := New(cfg, NewDiscovery(nil, time.Second), &MemoryIdentity{}, engine.BuiltinRunners())
	hubEnd, _, cancel := startWorker(t, w)
	recvKind[protocol.Hello](t, hubEnd)
	welcome := protocol.Welcome{WorkerID: "w1", HeartbeatInterval: protocol.Duration(time.Hour)}
	if err := hubEnd.Send(context.Background(), welcome); err != nil {
		t.Fatal(err)
	}
	return hubEnd, cancel
}

// collectTurn reads events for turnID until its TurnDone arrives.
func collectTurn(t *testing.T, hubEnd protocol.Conn, turnID string) ([]engine.Event, protocol.TurnDone) {
	t.Helper()
	var events []engine.Event
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		m, err := hubEnd.Recv(ctx)
		cancel()
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		switch m := m.(type) {
		case protocol.TurnEvent:
			if m.TurnID == turnID {
				events = append(events, m.Event)
			}
		case protocol.TurnDone:
			if m.TurnID == turnID {
				return events, m
			}
		}
	}
	t.Fatalf("turn %s did not finish", turnID)
	return nil, protocol.TurnDone{}
}

func startFakeTurn(t *testing.T, hubEnd protocol.Conn, turnID string, options map[string]any) {
	t.Helper()
	req := protocol.StartTurn{TurnID: turnID, Engine: "fake", Spec: engine.TurnSpec{Prompt: "hello", Options: options}}
	if err := hubEnd.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestTurn_StreamsEventsAndFinishes(t *testing.T) {
	hubEnd, _ := connectedWorker(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"reply": "hi there", "tool": true})
	events, done := collectTurn(t, hubEnd, "t1")

	if done.Error != "" || done.Result.Output != "hi there" || done.Result.SessionRef == "" {
		t.Errorf("unexpected TurnDone: %+v", done)
	}
	var kinds []engine.EventKind
	var text strings.Builder
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == engine.EventText {
			text.WriteString(ev.Text)
		}
	}
	// status, tool_call, tool_result, then the two text chunks coalesced
	// into one event by the 50ms default window.
	if len(kinds) != 4 || kinds[0] != engine.EventStatus || kinds[3] != engine.EventText {
		t.Errorf("unexpected event kinds: %v", kinds)
	}
	if text.String() != "hi there" {
		t.Errorf("text = %q", text.String())
	}
}

func TestTurn_NoCoalescingWhenDisabled(t *testing.T) {
	hubEnd, _ := connectedWorker(t, Config{EventFlushInterval: -1})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"reply": "ab"})
	events, _ := collectTurn(t, hubEnd, "t1")

	textEvents := 0
	for _, ev := range events {
		if ev.Kind == engine.EventText {
			textEvents++
		}
	}
	if textEvents != 2 {
		t.Errorf("got %d text events, want the fake engine's 2 chunks unmerged", textEvents)
	}
}

func TestTurn_UnknownEngineReportsFailure(t *testing.T) {
	hubEnd, _ := connectedWorker(t, Config{})

	req := protocol.StartTurn{TurnID: "t1", Engine: "codex", Spec: engine.TurnSpec{Prompt: "x"}}
	if err := hubEnd.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	_, done := collectTurn(t, hubEnd, "t1")

	if done.Error == "" || !strings.Contains(done.Error, "codex") {
		t.Errorf("expected an error naming the engine, got %+v", done)
	}
}

func TestTurn_DuplicateIDIsRefused(t *testing.T) {
	hubEnd, _ := connectedWorker(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"delay_ms": float64(300)})
	startFakeTurn(t, hubEnd, "t1", nil)

	_, first := collectTurn(t, hubEnd, "t1")
	if first.Error == "" || !strings.Contains(first.Error, "already running") {
		t.Errorf("second start should be refused, got %+v", first)
	}
	_, second := collectTurn(t, hubEnd, "t1")
	if second.Error != "" {
		t.Errorf("original turn should still finish normally, got %+v", second)
	}
}

func TestTurn_Cancel(t *testing.T) {
	hubEnd, _ := connectedWorker(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"delay_ms": float64(10000)})
	// Give the turn a moment to start before cancelling it.
	time.Sleep(20 * time.Millisecond)
	if err := hubEnd.Send(context.Background(), protocol.CancelTurn{TurnID: "t1"}); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, done := collectTurn(t, hubEnd, "t1")
	if !done.Cancelled || done.Error == "" {
		t.Errorf("expected a cancelled TurnDone, got %+v", done)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("cancel took too long; the 10s delay should have been cut short")
	}
	// Cancelling again, or an unknown turn, is harmless.
	if err := hubEnd.Send(context.Background(), protocol.CancelTurn{TurnID: "t404"}); err != nil {
		t.Fatal(err)
	}
}

func TestTurn_RunConcurrently(t *testing.T) {
	hubEnd, _ := connectedWorker(t, Config{})

	// Each turn takes 200ms; run three. Sequential execution would need
	// 600ms, concurrent well under that.
	for _, id := range []string{"a", "b", "c"} {
		startFakeTurn(t, hubEnd, id, map[string]any{"delay_ms": float64(200), "reply": id})
	}
	start := time.Now()
	got := map[string]string{}
	for len(got) < 3 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		m, err := hubEnd.Recv(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if done, ok := m.(protocol.TurnDone); ok {
			got[done.TurnID] = done.Result.Output
		}
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("three 200ms turns took %v; they should overlap", elapsed)
	}
	for _, id := range []string{"a", "b", "c"} {
		if got[id] != id {
			t.Errorf("turn %s replied %q", id, got[id])
		}
	}
}

func TestTurn_ShutdownCancelsRunningTurns(t *testing.T) {
	hubEnd, cancel := connectedWorker(t, Config{})
	startFakeTurn(t, hubEnd, "t1", map[string]any{"delay_ms": float64(10000)})
	time.Sleep(20 * time.Millisecond)

	start := time.Now()
	cancel()

	// Run must return promptly rather than wait out the 10s turn. The
	// worker closes its end of the pipe on the way out, which we observe
	// as ErrClosed on the hub's end.
	ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	for {
		if _, err := hubEnd.Recv(ctx); err != nil {
			break
		}
	}
	if time.Since(start) > 2*time.Second {
		t.Error("shutdown waited for the running turn instead of cancelling it")
	}
}

// awaitApprovalRequest reads messages until the turn asks for approval.
func awaitApprovalRequest(t *testing.T, hubEnd protocol.Conn, turnID string) protocol.ApprovalRequest {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		m, err := hubEnd.Recv(ctx)
		cancel()
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		switch m := m.(type) {
		case protocol.ApprovalRequest:
			if m.TurnID == turnID {
				return m
			}
		case protocol.TurnDone:
			if m.TurnID == turnID {
				t.Fatalf("turn finished without asking for approval: %+v", m)
			}
		}
	}
	t.Fatal("no approval request arrived")
	return protocol.ApprovalRequest{}
}

func TestTurn_ApprovalRoundTrip(t *testing.T) {
	hubEnd, _ := connectedWorker(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"approval": true, "reply": "built"})
	req := awaitApprovalRequest(t, hubEnd, "t1")
	if req.ApprovalID == "" || req.Tool != "Bash" || req.Input != `{"command":"make test"}` || req.At.IsZero() {
		t.Fatalf("unexpected request: %+v", req)
	}

	decision := protocol.ApprovalDecision{TurnID: "t1", ApprovalID: req.ApprovalID, Decision: engine.Decision{Allow: true}}
	if err := hubEnd.Send(context.Background(), decision); err != nil {
		t.Fatal(err)
	}
	events, done := collectTurn(t, hubEnd, "t1")
	if done.Error != "" || done.Result.Output != "built" {
		t.Errorf("unexpected TurnDone: %+v", done)
	}
	ran := false
	for _, ev := range events {
		if ev.Kind == engine.EventToolCall && ev.Tool == "Bash" {
			ran = true
		}
		if ev.Kind == engine.EventApprovalRequest {
			t.Error("approval requests must not also be forwarded as TurnEvents")
		}
	}
	if !ran {
		t.Error("the allowed command should have run")
	}
}

func TestTurn_ApprovalDenied(t *testing.T) {
	hubEnd, _ := connectedWorker(t, Config{})

	startFakeTurn(t, hubEnd, "t1", map[string]any{"approval": true})
	req := awaitApprovalRequest(t, hubEnd, "t1")
	decision := protocol.ApprovalDecision{TurnID: "t1", ApprovalID: req.ApprovalID, Decision: engine.Decision{Allow: false, Message: "no"}}
	if err := hubEnd.Send(context.Background(), decision); err != nil {
		t.Fatal(err)
	}

	_, done := collectTurn(t, hubEnd, "t1")
	if done.Result.Output != "Denied: no" {
		t.Errorf("unexpected TurnDone: %+v", done)
	}
}

func TestTurn_StrayDecisionsAreHarmless(t *testing.T) {
	hubEnd, _ := connectedWorker(t, Config{})

	// Unknown turn, then a running turn with an unknown approval id.
	stray := protocol.ApprovalDecision{TurnID: "t404", ApprovalID: "a", Decision: engine.Decision{Allow: true}}
	if err := hubEnd.Send(context.Background(), stray); err != nil {
		t.Fatal(err)
	}
	startFakeTurn(t, hubEnd, "t1", map[string]any{"approval": true})
	req := awaitApprovalRequest(t, hubEnd, "t1")
	wrong := protocol.ApprovalDecision{TurnID: "t1", ApprovalID: "not-" + req.ApprovalID, Decision: engine.Decision{Allow: true}}
	if err := hubEnd.Send(context.Background(), wrong); err != nil {
		t.Fatal(err)
	}
	// The real request is still pending; cancelling ends the turn.
	if err := hubEnd.Send(context.Background(), protocol.CancelTurn{TurnID: "t1"}); err != nil {
		t.Fatal(err)
	}
	_, done := collectTurn(t, hubEnd, "t1")
	if !done.Cancelled {
		t.Errorf("expected the turn to end by cancellation, got %+v", done)
	}
}

func TestTextBuffer_CoalescesUntilFlush(t *testing.T) {
	buf := newTextBuffer(time.Hour)
	at := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)

	if out := buf.add(engine.Event{Kind: engine.EventText, Text: "ab", At: at}); out != nil {
		t.Errorf("first chunk should be buffered, got %v", out)
	}
	if out := buf.add(engine.Event{Kind: engine.EventText, Text: "cd", At: at.Add(time.Second)}); out != nil {
		t.Errorf("second chunk should be buffered, got %v", out)
	}

	// A non-text event flushes the text ahead of itself.
	out := buf.add(engine.Event{Kind: engine.EventStatus, Text: "done"})
	if len(out) != 2 || out[0].Kind != engine.EventText || out[0].Text != "abcd" || !out[0].At.Equal(at) || out[1].Kind != engine.EventStatus {
		t.Errorf("unexpected flush: %+v", out)
	}
	if _, ok := buf.take(); ok {
		t.Error("buffer should be empty after a flush")
	}
	if buf.deadline() != nil {
		t.Error("no deadline should be armed while the buffer is empty")
	}
}
