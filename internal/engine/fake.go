package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrTurnCancelled is the Result error of a turn stopped by Cancel or by its
// context.
var ErrTurnCancelled = errors.New("engine: turn cancelled")

// Fake is an engine for tests and demos. It needs no CLI: a turn emits a few
// events and then replies. Behaviour is steered through TurnSpec.Options:
//
//	reply    string  reply text; default echoes the last line of the prompt
//	delay_ms number  how long the turn takes before replying
//	fail     bool    end the turn with an error instead of a reply
//	tool     bool    emit a tool_call / tool_result pair first
//	approval bool    ask permission to run a command before replying; if
//	                 denied, the reply reports the denial message instead
type Fake struct{}

// NewFake returns the fake engine.
func NewFake() *Fake { return &Fake{} }

// Name implements Runner and Detector.
func (*Fake) Name() string { return "fake" }

// Detect implements Detector; the fake engine is always ready.
func (*Fake) Detect(context.Context) Info {
	return Info{Name: "fake", Binary: "-", Version: "builtin", Status: StatusReady, Detail: "built-in test engine"}
}

// StartTurn implements Runner.
func (*Fake) StartTurn(ctx context.Context, spec TurnSpec) (Turn, error) {
	ctx, cancel := context.WithCancel(ctx)
	t := &fakeTurn{turnBase: newTurnBase(cancel)}
	go t.run(ctx, spec)
	return t, nil
}

type fakeTurn struct {
	*turnBase
}

// run plays the scripted turn. Every emit checks ctx so Cancel takes effect
// at the next step; finish always closes the channels and unblocks Result.
func (t *fakeTurn) run(ctx context.Context, spec TurnSpec) {
	if !t.emit(ctx, Event{Kind: EventStatus, Text: "thinking"}) {
		t.finish(ctx, Result{}, ErrTurnCancelled)
		return
	}
	if optBool(spec.Options, "tool") {
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: "read_file", Input: "README.md"}) ||
			!t.emit(ctx, Event{Kind: EventToolResult, Tool: "read_file", Text: "# Veyloom"}) {
			t.finish(ctx, Result{}, ErrTurnCancelled)
			return
		}
	}
	reply := optString(spec.Options, "reply")
	if reply == "" {
		reply = "Echo: " + lastLine(spec.Prompt)
	}
	if optBool(spec.Options, "approval") {
		d, err := t.requestApproval(ctx, fakeApprovalTool, fakeApprovalInput)
		if err != nil {
			t.finish(ctx, Result{}, err)
			return
		}
		if d.Allow {
			if !t.emit(ctx, Event{Kind: EventToolCall, Tool: fakeApprovalTool, Input: fakeApprovalInput}) ||
				!t.emit(ctx, Event{Kind: EventToolResult, Tool: fakeApprovalTool, Text: "ok"}) {
				t.finish(ctx, Result{}, ErrTurnCancelled)
				return
			}
		} else {
			reply = "Denied: " + d.Message
		}
	}
	if delay := optDuration(spec.Options, "delay_ms"); delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			t.finish(ctx, Result{}, ErrTurnCancelled)
			return
		}
	}
	if optBool(spec.Options, "fail") {
		t.emit(ctx, Event{Kind: EventError, Text: "scripted failure"})
		t.finish(ctx, Result{}, errors.New("fake engine: scripted failure"))
		return
	}

	// Two chunks so consumers see that text arrives incrementally.
	half := len(reply) / 2
	if !t.emit(ctx, Event{Kind: EventText, Text: reply[:half]}) || !t.emit(ctx, Event{Kind: EventText, Text: reply[half:]}) {
		t.finish(ctx, Result{}, ErrTurnCancelled)
		return
	}

	session := spec.SessionRef
	if session == "" {
		session = "fake-" + randomHex(4)
	}
	t.finish(ctx, Result{Output: reply, SessionRef: session, Usage: map[string]any{"turns": 1}}, nil)
}

// What the fake engine asks permission for when the approval option is set.
const (
	fakeApprovalTool  = "Bash"
	fakeApprovalInput = `{"command":"make test"}`
)

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func optString(o map[string]any, key string) string {
	v, _ := o[key].(string)
	return v
}

func optBool(o map[string]any, key string) bool {
	v, _ := o[key].(bool)
	return v
}

// optDuration reads a millisecond count; JSON numbers arrive as float64.
func optDuration(o map[string]any, key string) time.Duration {
	switch v := o[key].(type) {
	case float64:
		return time.Duration(v) * time.Millisecond
	case int:
		return time.Duration(v) * time.Millisecond
	}
	return 0
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("engine: random: %v", err))
	}
	return hex.EncodeToString(b)
}
