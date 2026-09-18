package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrTurnCancelled is the Result error of a turn stopped by Cancel or by its
// context.
var ErrTurnCancelled = errors.New("runtime: turn cancelled")

// Fake is a runtime for tests and demos. It needs no CLI: a turn emits a few
// events and then replies. Behaviour is steered through TurnSpec.Options:
//
//	reply    string  reply text; default echoes the last line of the prompt
//	delay_ms number  how long the turn takes before replying
//	fail     bool    end the turn with an error instead of a reply
//	preamble string  with tool: text emitted before the tool call
//	tool     bool    emit a tool_call / tool_result pair first
//	approval bool    ask permission to run a command before replying; if
//	                 denied, the reply reports the denial message instead
//	fail_on_resume bool  fail at once, before any event, whenever the turn
//	                 resumes a session: a session that will not resume
//	failure  string  the FailureKind reported with either kind of failure
//	compact  bool    compact the session before replying
//	changes  []string report these files as written, before replying
//	room_tool string  call this room tool first, with room_topic (number)
//	                 and room_text (string) as its arguments, and reply
//	                 with what it answered
type Fake struct{}

// NewFake returns the fake runtime.
func NewFake() *Fake { return &Fake{} }

// Name implements Runner and Detector.
func (*Fake) Name() string { return "fake" }

// Detect implements Detector; the fake runtime is always ready.
func (*Fake) Detect(context.Context) Info {
	return Info{Name: "fake", Binary: "-", Version: "builtin", Status: StatusReady, Detail: "built-in test runtime"}
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
	// A session that will not resume: the turn fails before it says a
	// thing, the way a CLI does that cannot find the session it was given.
	if spec.Session.Resume && optBool(spec.Options, "fail_on_resume") {
		t.finish(ctx, Result{Failure: FailureKind(optString(spec.Options, "failure"))}, errors.New("fake runtime: cannot resume the session"))
		return
	}
	session := spec.Session.resumeID()
	if session == "" {
		session = "fake-" + randomHex(4)
	}
	if !t.emit(ctx, Event{Kind: EventSession, SessionRef: session}) || !t.emit(ctx, Event{Kind: EventStatus, Text: "thinking"}) {
		t.finish(ctx, Result{}, ErrTurnCancelled)
		return
	}
	if optBool(spec.Options, "tool") {
		// A preamble is text said before the tool runs, so a turn has two
		// text segments around a tool call, as real agents do.
		if pre := optString(spec.Options, "preamble"); pre != "" && !t.emit(ctx, Event{Kind: EventText, Text: pre}) {
			t.finish(ctx, Result{}, ErrTurnCancelled)
			return
		}
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: "read_file", Input: "README.md"}) ||
			!t.emit(ctx, Event{Kind: EventToolResult, Tool: "read_file", Text: "# Veyloom"}) {
			t.finish(ctx, Result{}, ErrTurnCancelled)
			return
		}
	}
	if optBool(spec.Options, "compact") {
		if !t.emit(ctx, Event{Kind: EventCompaction, Phase: CompactionStart}) || !t.emit(ctx, Event{Kind: EventCompaction, Phase: CompactionEnd}) {
			t.finish(ctx, Result{}, ErrTurnCancelled)
			return
		}
	}
	for _, path := range optStrings(spec.Options, "changes") {
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: "write_file", Input: path}) ||
			!t.emit(ctx, Event{Kind: EventFileChanged, Path: path}) ||
			!t.emit(ctx, Event{Kind: EventToolResult, Tool: "write_file", Text: "ok"}) {
			t.finish(ctx, Result{}, ErrTurnCancelled)
			return
		}
	}
	reply := optString(spec.Options, "reply")
	if tool := optString(spec.Options, "room_tool"); tool != "" {
		// Reads the room as a real agent would, through the turn's host.
		q := RoomQuery{Tool: tool, Topic: int(optFloat(spec.Options, "room_topic")), Text: optString(spec.Options, "room_text")}
		input, _ := json.Marshal(q)
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: tool, Input: string(input)}) {
			t.finish(ctx, Result{}, ErrTurnCancelled)
			return
		}
		answer := runRoomTool(ctx, spec.Host, tool, input)
		if !t.emit(ctx, Event{Kind: EventToolResult, Tool: tool, Text: answer}) {
			t.finish(ctx, Result{}, ErrTurnCancelled)
			return
		}
		if reply == "" {
			reply = answer
		}
	}
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
		t.finish(ctx, Result{Failure: FailureKind(optString(spec.Options, "failure"))}, errors.New("fake runtime: scripted failure"))
		return
	}

	// Two chunks so consumers see that text arrives incrementally.
	half := len(reply) / 2
	if !t.emit(ctx, Event{Kind: EventText, Text: reply[:half]}) || !t.emit(ctx, Event{Kind: EventText, Text: reply[half:]}) {
		t.finish(ctx, Result{}, ErrTurnCancelled)
		return
	}

	// Characters stand in for tokens: the prompt as input, the reply as output.
	usage := Usage{InputTokens: int64(len([]rune(spec.Prompt))), OutputTokens: int64(len([]rune(reply)))}
	t.finish(ctx, Result{Output: reply, SessionRef: session, Usage: usage}, nil)
}

// What the fake runtime asks permission for when the approval option is set.
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
		panic(fmt.Sprintf("runtime: random: %v", err))
	}
	return hex.EncodeToString(b)
}
