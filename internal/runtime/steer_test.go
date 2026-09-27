package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// awaitEvent reads events until one of kind comes and returns it with the
// events before it.
func awaitEvent(t *testing.T, turn Turn, kind EventKind) (Event, []Event) {
	t.Helper()
	var before []Event
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-turn.Events():
			if !ok {
				t.Fatalf("turn ended without a %s event; saw %+v", kind, before)
			}
			if ev.Kind == kind {
				return ev, before
			}
			before = append(before, ev)
		case <-timeout:
			t.Fatalf("no %s event came", kind)
		}
	}
}

// startScriptedClaude starts a turn on the scripted fake CLI, prompt
// naming its exchanges.
func startScriptedClaude(t *testing.T, cfg ClaudeConfig, prompt string) Turn {
	t.Helper()
	scriptedClaudeCLI(t)
	runner := NewClaudeRunner(cfg)
	t.Cleanup(func() { runner.Close() })
	turn, err := runner.StartTurn(context.Background(), TurnSpec{Prompt: prompt, Permission: PermissionFullAuto})
	if err != nil {
		t.Fatal(err)
	}
	return turn
}

// A message passed while a tool runs reaches the agent with the tool's
// result, and the CLI echoing it says so.
func TestClaude_SteerGoesInWithTheNextToolResult(t *testing.T) {
	turn := startScriptedClaude(t, ClaudeConfig{}, "[steer]")
	awaitEvent(t, turn, EventToolCall)
	if err := turn.Steer("s1", "use the blue one"); err != nil {
		t.Fatal(err)
	}
	took, before := awaitEvent(t, turn, EventSteer)
	if took.SteerID != "s1" || took.Text != "use the blue one" {
		t.Errorf("steer event = %+v", took)
	}
	if len(eventsOf(before, EventToolResult)) != 1 {
		t.Errorf("the message is taken in with the tool's result: %+v", before)
	}
	rest := drain(t, turn)
	if dropped := eventsOf(rest, EventSteerDropped); len(dropped) != 0 {
		t.Errorf("a message taken in is not dropped: %+v", dropped)
	}
	res, err := turn.Result()
	if err != nil {
		t.Fatal(err)
	}
	answerIs(t, fakeClaudeAnswers(t, res.Output), "[steer]", `"use the blue one"`)
}

// A message that comes too late for the turn's answer is answered as a
// turn of its own before the CLI ends: the turn's output is the last
// answer, its usage both.
func TestClaude_SteerTooLateForTheAnswerIsAnsweredOnItsOwn(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "go")
	t.Setenv("VEYLOOM_FAKE_CLAUDE_GO", gate)
	turn := startScriptedClaude(t, ClaudeConfig{}, "[wait]")
	awaitEvent(t, turn, EventStatus)
	if err := turn.Steer("s1", "one more thing"); err != nil {
		t.Fatal(err)
	}
	if err := turn.Steer("s2", "and another"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	steers := eventsOf(events, EventSteer)
	if len(steers) != 2 || steers[0].SteerID != "s1" || steers[1].SteerID != "s2" || steers[1].Text != "and another" {
		t.Errorf("steer events = %+v", steers)
	}
	// Each is taken in where the CLI's turn for it begins, before what it
	// says to it; the session, announced as the turn began (and awaited
	// above), is not announced again.
	var order []string
	for _, ev := range events {
		switch ev.Kind {
		case EventSteer:
			order = append(order, ev.SteerID)
		case EventText:
			order = append(order, ev.Text)
		case EventStatus:
			order = append(order, "status")
		}
	}
	if want := []string{"s1", "extra", "s2", "extra"}; !slices.Equal(order, want) {
		t.Errorf("events in the order %q, want %q", order, want)
	}
	res, err := turn.Result()
	if err != nil {
		t.Fatal(err)
	}
	answerIs(t, fakeClaudeAnswers(t, res.Output), "[extra]", `"and another"`)
	if res.Usage.InputTokens != 3 || res.Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v, want the three answers' together", res.Usage)
	}
	// Once all is answered, what comes next waits for the next turn.
	if err := turn.Steer("s3", "too late"); !errors.Is(err, ErrSteerRefused) {
		t.Errorf("steer once the turn is over: %v, want ErrSteerRefused", err)
	}
}

// Should the CLI die answering a message it took in, the run failed: the
// message is not answered, whatever the answer before it.
func TestClaude_SteerTheCLIDiesAnsweringFailsTheRun(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "go")
	t.Setenv("VEYLOOM_FAKE_CLAUDE_GO", gate)
	turn := startScriptedClaude(t, ClaudeConfig{}, "[crash] [wait]")
	awaitEvent(t, turn, EventStatus)
	if err := turn.Steer("s1", "one more thing"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	if steers := eventsOf(events, EventSteer); len(steers) != 1 || steers[0].SteerID != "s1" {
		t.Errorf("taken in as its turn began: %+v", steers)
	}
	res, err := turn.Result()
	if err == nil || !strings.Contains(err.Error(), "ended before answering what was passed to it") || !strings.Contains(err.Error(), "heap out of memory") {
		t.Fatalf("the run: %+v, %v", res, err)
	}
	if res.Failure != "" || res.Usage.OutputTokens != 1 {
		t.Errorf("no account's failure, the answer before spent: %+v", res)
	}
}

// A failed model call of the CLI's turn before is not the news of the one
// it began after for text Steer passed.
func TestClaudeParser_EachTurnItsOwnFailure(t *testing.T) {
	p := newClaudeParser(ClaudeConfig{}, func(Event) {})
	p.handle(claudeLine{Type: "assistant", Error: json.RawMessage(`"rate_limit"`)})
	p.handle(claudeLine{Type: "result", Subtype: "success", Result: "done"})
	p.nextTurn()
	p.handle(claudeLine{Type: "result", Subtype: "error_max_turns", IsError: true, Result: "Reached maximum number of turns (3)"})
	res, err := p.finish(nil, "")
	if err == nil || res.Failure != "" {
		t.Errorf("the second turn's failure is its own: %+v %v", res, err)
	}
}

// Should the CLI lose a message, the turn still ends, and the message is
// reported dropped so it waits for the next turn.
func TestClaude_SteerTheCLINeverTakesInIsDropped(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "go")
	t.Setenv("VEYLOOM_FAKE_CLAUDE_GO", gate)
	turn := startScriptedClaude(t, ClaudeConfig{SteerWait: 200 * time.Millisecond}, "[deaf] [wait]")
	awaitEvent(t, turn, EventStatus)
	if err := turn.Steer("s1", "lost"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	if steers := eventsOf(events, EventSteer); len(steers) != 0 {
		t.Errorf("nothing was taken in: %+v", steers)
	}
	if dropped := eventsOf(events, EventSteerDropped); len(dropped) != 1 || dropped[0].SteerID != "s1" {
		t.Errorf("dropped = %+v, want s1", dropped)
	}
	if _, err := turn.Result(); err != nil {
		t.Errorf("the turn itself went well: %v", err)
	}
}

// The prompt the CLI echoes back is no tool result and no steer.
func TestClaude_ThePromptsEchoIsNoEvent(t *testing.T) {
	events, _ := runScripted(t, "hello", PermissionFullAuto, nil)
	for _, ev := range events {
		if ev.Kind == EventToolResult || ev.Kind == EventSteer || ev.Kind == EventSteerDropped {
			t.Errorf("unexpected %+v", ev)
		}
	}
}

// Codex takes a message in with turn/steer at the agent's next step; the
// user message it records under the message's id says it did.
func TestCodex_SteerGoesInWithTurnSteer(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[steer]", Permission: PermissionFullAuto})
	if err != nil {
		t.Fatal(err)
	}
	awaitEvent(t, turn, EventToolCall)
	if err := turn.Steer("s1", "use the blue one"); err != nil {
		t.Fatal(err)
	}
	took, _ := awaitEvent(t, turn, EventSteer)
	if took.SteerID != "s1" || took.Text != "use the blue one" {
		t.Errorf("steer event = %+v", took)
	}
	if dropped := eventsOf(drain(t, turn), EventSteerDropped); len(dropped) != 0 {
		t.Errorf("dropped = %+v", dropped)
	}
	res, err := turn.Result()
	if err != nil || res.Output != "noted: use the blue one" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	steers := h.sent(t)["turn/steer"]
	if len(steers) != 1 {
		t.Fatalf("turn/steer sent %d times", len(steers))
	}
	p := paramsOf(t, steers[0])
	if p["threadId"] != "thr-new" || p["expectedTurnId"] != "turn-1" || p["clientUserMessageId"] != "s1" {
		t.Errorf("turn/steer params = %v", p)
	}
	// Once the turn is over, what comes waits for the next.
	if err := turn.Steer("s2", "late"); !errors.Is(err, ErrSteerRefused) {
		t.Errorf("steer once the turn is over: %v", err)
	}
}

// A message passed while the turn is being set up goes as soon as it has
// started.
func TestCodex_SteerBeforeTheTurnStartedGoesOnceItHas(t *testing.T) {
	h := newCodexHarness(t)
	t.Setenv("VEYLOOM_FAKE_CODEX_SLOW_START", "300ms")
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[steer]", Permission: PermissionFullAuto})
	if err != nil {
		t.Fatal(err)
	}
	if err := turn.Steer("s1", "early word"); err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	if took := eventsOf(events, EventSteer); len(took) != 1 || took[0].SteerID != "s1" {
		t.Errorf("steer events = %+v", took)
	}
	if res, err := turn.Result(); err != nil || res.Output != "noted: early word" {
		t.Errorf("result = %+v, %v", res, err)
	}
}

// What Codex will not take is refused, and nothing more is said of it.
// Should the turn end before the refusal is read, it is reported dropped
// instead: either way one outcome, never both.
func TestCodex_SteerCodexRefusesIsRefused(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[steer-refused]", Permission: PermissionFullAuto})
	if err != nil {
		t.Fatal(err)
	}
	awaitEvent(t, turn, EventToolCall)
	steerErr := turn.Steer("s1", "no room")
	events := drain(t, turn)
	took, dropped := eventsOf(events, EventSteer), eventsOf(events, EventSteerDropped)
	switch {
	case len(took) > 0:
		t.Errorf("Codex took nothing in: %+v", took)
	case errors.Is(steerErr, ErrSteerRefused) && len(dropped) == 0:
	case steerErr == nil && len(dropped) == 1 && dropped[0].SteerID == "s1":
		t.Log("the turn ended before the refusal was read")
	default:
		t.Errorf("steer = %v with dropped %+v: want refused, or else dropped once", steerErr, dropped)
	}
}

// A message taken but never got to, as when the turn fails first, is
// reported dropped.
func TestCodex_SteerTheTurnNeverGotToIsDropped(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[steer-failed]", Permission: PermissionFullAuto})
	if err != nil {
		t.Fatal(err)
	}
	awaitEvent(t, turn, EventToolCall)
	if err := turn.Steer("s1", "too late"); err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	if dropped := eventsOf(events, EventSteerDropped); len(dropped) != 1 || dropped[0].SteerID != "s1" {
		t.Errorf("dropped = %+v", dropped)
	}
	if _, err := turn.Result(); err == nil {
		t.Error("the turn failed")
	}
}

// startScriptedPi starts a turn on the scripted fake pi, prompt naming
// what it does.
func startScriptedPi(t *testing.T, prompt string) Turn {
	t.Helper()
	scriptedPiCLI(t)
	turn, err := NewPiRunner(PiConfig{}).StartTurn(context.Background(), TurnSpec{Prompt: prompt, Permission: PermissionFullAuto})
	if err != nil {
		t.Fatal(err)
	}
	return turn
}

// Pi takes a message in with its steer command once the tools of the
// agent's step are done; the user message it starts says it did.
func TestPi_SteerGoesInAfterTheStepsTools(t *testing.T) {
	turn := startScriptedPi(t, "[steer]")
	awaitEvent(t, turn, EventToolCall)
	if err := turn.Steer("s1", "use the blue one"); err != nil {
		t.Fatal(err)
	}
	took, before := awaitEvent(t, turn, EventSteer)
	if took.SteerID != "s1" || took.Text != "use the blue one" {
		t.Errorf("steer event = %+v", took)
	}
	if len(eventsOf(before, EventToolResult)) != 1 {
		t.Errorf("taken in once the tool is done: %+v", before)
	}
	if dropped := eventsOf(drain(t, turn), EventSteerDropped); len(dropped) != 0 {
		t.Errorf("dropped = %+v", dropped)
	}
	res, err := turn.Result()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, `"[steer]":"use the blue one"`) {
		t.Errorf("output = %s", res.Output)
	}
	if err := turn.Steer("s2", "late"); !errors.Is(err, ErrSteerRefused) {
		t.Errorf("steer once the turn is over: %v", err)
	}
}

// A message that comes too late for the run is reported dropped when pi
// ends without it.
func TestPi_SteerTooLateForTheRunIsDropped(t *testing.T) {
	turn := startScriptedPi(t, "[late]")
	awaitEvent(t, turn, EventToolCall)
	if err := turn.Steer("s1", "one more thing"); err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	if dropped := eventsOf(events, EventSteerDropped); len(dropped) != 1 || dropped[0].SteerID != "s1" {
		t.Errorf("dropped = %+v", dropped)
	}
	if took := eventsOf(events, EventSteer); len(took) != 0 {
		t.Errorf("nothing was taken in: %+v", took)
	}
}

// Once the agent's run is over, as while pi compacts after it, pi takes
// no more.
func TestPi_SteerOnceTheRunIsOverIsRefused(t *testing.T) {
	turn := startScriptedPi(t, "[compact]")
	awaitEvent(t, turn, EventCompaction)
	if err := turn.Steer("s1", "too late"); !errors.Is(err, ErrSteerRefused) {
		t.Errorf("steer = %v, want ErrSteerRefused", err)
	}
	drain(t, turn)
}

// The fake takes what Steer passes while it works, and its reply says so.
func TestFake_SteerWhileItWorks(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "go", Options: map[string]any{"delay_ms": 300}})
	if err != nil {
		t.Fatal(err)
	}
	awaitEvent(t, turn, EventStatus)
	if err := turn.Steer("s1", "context\n>> use the blue one"); err != nil {
		t.Fatal(err)
	}
	took, _ := awaitEvent(t, turn, EventSteer)
	if took.SteerID != "s1" || took.Text != "context\n>> use the blue one" {
		t.Errorf("steer event = %+v", took)
	}
	drain(t, turn)
	if res, err := turn.Result(); err != nil || res.Output != "Echo: go\nSteered: >> use the blue one" {
		t.Errorf("result = %+v, %v", res, err)
	}
	if err := turn.Steer("s2", "late"); !errors.Is(err, ErrSteerRefused) {
		t.Errorf("steer once the turn is over: %v", err)
	}
}

// The fake can be told to refuse steering, or to take it and never get
// to it.
func TestFake_SteerRefusedOrDropped(t *testing.T) {
	turn, err := NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "go", Options: map[string]any{"delay_ms": 200, "steer": "refuse"}})
	if err != nil {
		t.Fatal(err)
	}
	awaitEvent(t, turn, EventStatus)
	if err := turn.Steer("s1", "x"); !errors.Is(err, ErrSteerRefused) {
		t.Errorf("steer = %v, want ErrSteerRefused", err)
	}
	drain(t, turn)

	turn, err = NewFake().StartTurn(context.Background(), TurnSpec{Prompt: "go", Options: map[string]any{"delay_ms": 300, "steer": "drop"}})
	if err != nil {
		t.Fatal(err)
	}
	awaitEvent(t, turn, EventStatus)
	if err := turn.Steer("s1", "x"); err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	if dropped := eventsOf(events, EventSteerDropped); len(dropped) != 1 || dropped[0].SteerID != "s1" {
		t.Errorf("dropped = %+v", dropped)
	}
	if res, _ := turn.Result(); strings.Contains(res.Output, "Steered") {
		t.Errorf("output = %q", res.Output)
	}
}
