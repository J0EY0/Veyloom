package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// drain collects every event until the channel closes. A turn of a fake
// CLI ends in well under a second; the wait is long because a machine busy
// with the whole suite under the race detector may take seconds to start
// a process.
func drain(t *testing.T, turn Turn) []Event {
	t.Helper()
	var events []Event
	timeout := time.After(15 * time.Second)
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

func TestFake_WritesFiles(t *testing.T) {
	dir := t.TempDir()
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{
		Prompt: "hi", WorkDir: dir,
		Options: map[string]any{"write": []any{"docs/a.md", "../outside.md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var changed, results []string
	for _, ev := range drain(t, turn) {
		switch ev.Kind {
		case EventFileChanged:
			changed = append(changed, ev.Path)
		case EventToolResult:
			results = append(results, ev.Text)
		}
	}
	first, err := os.ReadFile(filepath.Join(dir, "docs", "a.md"))
	if err != nil || !strings.HasPrefix(string(first), "written in a fake turn ") {
		t.Errorf("the file written: %q %v", first, err)
	}
	if len(changed) != 1 || changed[0] != "docs/a.md" || len(results) != 2 || !strings.HasPrefix(results[1], "error:") {
		t.Errorf("changed %v, results %v", changed, results)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "outside.md")); err == nil {
		t.Error("a path outside the working directory was written")
	}

	// Written again, the file changes.
	again, _ := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "hi", WorkDir: dir, Options: map[string]any{"write": []any{"docs/a.md"}}})
	drain(t, again)
	if second, _ := os.ReadFile(filepath.Join(dir, "docs", "a.md")); string(second) == string(first) {
		t.Error("writing again changed nothing")
	}
}

// The fake asks as many times over as it is told, and a rule of the
// member's settles its requests as a runtime's would: one of Claude Code's
// lets the command through unasked, a Codex prefix says it did.
func TestFake_RulesSettleRequests(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options map[string]any
		rules   []string
		ruled   int
	}{
		{"a rule of Claude Code's", map[string]any{"similar": []any{"Bash(make test)"}}, []string{"Bash(make test)"}, 0},
		{"a prefix of Codex's", map[string]any{"prefix": []any{"make", "test"}}, []string{`["make","test"]`}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			options := map[string]any{"approval": true, "approvals": float64(2)}
			for k, v := range tc.options {
				options[k] = v
			}
			turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "go", Options: options, AllowedRules: tc.rules})
			if err != nil {
				t.Fatal(err)
			}
			events := drain(t, turn)
			if res, err := turn.Result(); err != nil || res.Output != "Allowed 2 of 2" {
				t.Fatalf("result = %+v, %v", res, err)
			}
			ruled := 0
			for _, ev := range events {
				if ev.Kind == EventApprovalRequest {
					if ev.Reviewer != ReviewerRule || ev.Verdict != VerdictAllowed {
						t.Errorf("asked: %+v", ev)
					}
					ruled++
				}
			}
			if ruled != tc.ruled {
				t.Errorf("%d settled by the rule, want %d", ruled, tc.ruled)
			}
		})
	}
}

// Allowed with the like of it, the rest of the turn's requests are the
// rule's: asked for once.
func TestFake_AllowingTheLikeOfItCoversTheRest(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "go", Options: map[string]any{"approval": true, "approvals": float64(3), "prefix": []any{"make"}}})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := awaitApproval(t, turn)
	if req.Similar == nil || strings.Join(req.Similar.Prefix, " ") != "make" {
		t.Fatalf("offer: %+v", req.Similar)
	}
	if err := turn.Answer(req.ApprovalID, Decision{Allow: true, Similar: true}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if ev, _ := awaitApproval(t, turn); ev.Reviewer != ReviewerRule {
			t.Fatalf("asked again: %+v", ev)
		}
	}
	drain(t, turn)
	if res, _ := turn.Result(); res.Output != "Allowed 3 of 3" {
		t.Errorf("Output = %q", res.Output)
	}
}

// Asked all at once, every request waits for its own answer.
func TestFake_RequestsTogether(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "go", Options: map[string]any{"approval": true, "approvals": float64(2), "together": true}})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := awaitApproval(t, turn)
	second, _ := awaitApproval(t, turn)
	if first.ApprovalID == second.ApprovalID {
		t.Fatal("the same request twice")
	}
	if err := turn.Answer(second.ApprovalID, Decision{Allow: false}); err != nil {
		t.Fatal(err)
	}
	if err := turn.Answer(first.ApprovalID, Decision{Allow: true}); err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	if res, _ := turn.Result(); res.Output != "Allowed 1 of 2" {
		t.Errorf("Output = %q", res.Output)
	}
}
