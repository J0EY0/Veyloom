package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
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
//	use_skill string  read this skill's SKILL.md from the turn's skills, as
//	                 a runtime does when a task calls for the skill, and
//	                 reply with its first line after the frontmatter
//	room_tool string  call this room tool first, with room_topic (number)
//	                 and room_text (string) as its arguments, and reply
//	                 with what it answered
//	notice   string  show people this warning before replying
//	reviewed string  report a command its own reviewer settled with this
//	                 verdict (allowed, denied) before replying
//	question string  ask this, with options red and blue, and a secret
//	                 passphrase, then reply with the answer to the first and
//	                 how long the passphrase was
//	form     string  have a form filled in with this message (a name, a
//	                 size, a count and a yes-or-no), then reply with it
//	link     string  have this link opened, then reply with whether it was
//	withdraw_ms number  ask permission to run a command, take the request
//	                 back after this long unless answered, and reply with
//	                 what became of it
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
	t := &fakeTurn{turnBase: newTurnBase(ctx, cancel)}
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
	if name := optString(spec.Options, "use_skill"); name != "" {
		path := filepath.Join(spec.SkillDir, "skills", name, "SKILL.md")
		input, _ := json.Marshal(map[string]string{"path": path})
		data, err := os.ReadFile(path)
		text := firstBodyLine(string(data))
		if spec.SkillDir == "" || err != nil {
			text = "no such skill: " + name
		}
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: "read", Input: string(input)}) ||
			!t.emit(ctx, Event{Kind: EventToolResult, Tool: "read", Text: text}) {
			t.finish(ctx, Result{}, ErrTurnCancelled)
			return
		}
		reply = text
	}
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
	// tool_calls: [{"tool": ..., "args": {...}}], each made in order through
	// the turn's host with its arguments as given, the way an agent calls
	// the wiki tools. The answers make the reply unless one is set.
	var answers []string
	for _, call := range optCalls(spec.Options, "tool_calls") {
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: call.tool, Input: string(call.args)}) {
			t.finish(ctx, Result{}, ErrTurnCancelled)
			return
		}
		// A runtime has only the tools its turn was given.
		answer := "error: this turn has no tool " + call.tool
		if !isOptionalTool(call.tool) || slices.Contains(spec.ExtraTools, call.tool) {
			answer = runRoomTool(ctx, spec.Host, call.tool, call.args)
		}
		if !t.emit(ctx, Event{Kind: EventToolResult, Tool: call.tool, Text: answer}) {
			t.finish(ctx, Result{}, ErrTurnCancelled)
			return
		}
		answers = append(answers, answer)
	}
	if reply == "" && len(answers) > 0 {
		reply = strings.Join(answers, "\n---\n")
	}
	if reply == "" {
		reply = "Echo: " + lastLine(spec.Prompt)
	}
	if text := optString(spec.Options, "notice"); text != "" && !t.notice(ctx, NoticeWarning, text) {
		t.finish(ctx, Result{}, ErrTurnCancelled)
		return
	}
	if verdict := optString(spec.Options, "reviewed"); verdict != "" &&
		!t.reviewed(ctx, fakeApprovalTool, fakeReviewedInput, fakeReviewer, verdict, "the fake reviewer's reasons", json.RawMessage(`{"risk":"low"}`)) {
		t.finish(ctx, Result{}, ErrTurnCancelled)
		return
	}
	if q := optString(spec.Options, "question"); q != "" {
		d, err := t.askQuestions(ctx, "AskUserQuestion", []Question{
			{ID: "1", Header: "Colour", Question: q, Options: []QuestionOption{{Label: "red"}, {Label: "blue"}}, Other: true},
			{ID: "2", Question: "The passphrase?", Other: true, Secret: true},
		})
		if err != nil {
			t.finish(ctx, Result{}, err)
			return
		}
		if a := d.Answers(); len(a["1"]) > 0 {
			reply = "Answer: " + answerText(a["1"])
			// Says what reached it without repeating a secret.
			if pass := a["2"]; len(pass) > 0 {
				reply += fmt.Sprintf(" (passphrase of %d characters)", utf8.RuneCountInString(pass[0]))
			}
		} else {
			reply = "No answer: " + d.Message
		}
	}
	if message := optString(spec.Options, "form"); message != "" {
		d, err := t.askForm(ctx, "elicitation", FormRequest{Server: "fake", Message: message, Schema: json.RawMessage(fakeFormSchema)})
		if err != nil {
			t.finish(ctx, Result{}, err)
			return
		}
		if content := d.Content(); content != nil {
			reply = "Form: " + compactJSON(content)
		} else {
			reply = "No form: " + d.Message
		}
	}
	if url := optString(spec.Options, "link"); url != "" {
		d, err := t.askLink(ctx, "elicitation", LinkRequest{Server: "fake", Message: "Sign in to continue", URL: url})
		if err != nil {
			t.finish(ctx, Result{}, err)
			return
		}
		if d.Allow {
			reply = "Link done"
		} else {
			reply = "No link: " + d.Message
		}
	}
	if _, ok := spec.Options["withdraw_ms"]; ok {
		wait, stop := context.WithTimeout(ctx, time.Duration(optFloat(spec.Options, "withdraw_ms"))*time.Millisecond)
		d, err := t.requestApproval(wait, fakeApprovalTool, fakeApprovalInput)
		stop()
		switch {
		case ctx.Err() != nil:
			t.finish(ctx, Result{}, err)
			return
		case err != nil:
			reply = "Withdrawn"
		default:
			reply = fmt.Sprintf("Answered before withdrawing: %v", d.Allow)
		}
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

// What the fake runtime asks permission for when the approval option is set,
// and what its own reviewer settles when the reviewed option is.
const (
	fakeApprovalTool  = "Bash"
	fakeApprovalInput = `{"command":"make test"}`
	fakeReviewedInput = `{"command":"curl -sI https://example.com"}`
	fakeReviewer      = "fake_review"
)

// fakeFormSchema is the form the form option asks for: one field of every
// kind MCP elicitation allows.
const fakeFormSchema = `{"type":"object","properties":{` +
	`"name":{"type":"string","title":"Name","description":"Who is deploying","minLength":1},` +
	`"size":{"type":"string","title":"Size","oneOf":[{"const":"s","title":"Small"},{"const":"l","title":"Large"}]},` +
	`"regions":{"type":"array","title":"Regions","items":{"type":"string","enum":["eu","us","ap"]},"minItems":1},` +
	`"count":{"type":"integer","title":"Count","minimum":1,"maximum":5,"default":1},` +
	`"notify":{"type":"boolean","title":"Notify me","default":true}},` +
	`"required":["name","size"]}`

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

// fakeCall is one tool call of the fake's tool_calls option.
type fakeCall struct {
	tool string
	args json.RawMessage
}

// optCalls reads a list of {"tool", "args"} objects.
func optCalls(o map[string]any, key string) []fakeCall {
	list, _ := o[key].([]any)
	calls := make([]fakeCall, 0, len(list))
	for _, item := range list {
		m, _ := item.(map[string]any)
		tool, _ := m["tool"].(string)
		if tool == "" {
			continue
		}
		args, _ := json.Marshal(m["args"])
		calls = append(calls, fakeCall{tool: tool, args: args})
	}
	return calls
}

// firstBodyLine is the first line of a markdown file after its frontmatter
// that says something.
func firstBodyLine(text string) string {
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		if i := strings.Index(rest, "\n---\n"); i >= 0 {
			text = rest[i+5:]
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}
