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
	"sync"
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
//	steer    string  "refuse": Steer is refused; "drop": what Steer passes
//	                 is taken but never got to, and reported dropped
//	fail     bool    end the turn with an error instead of a reply
//	preamble string  with tool: text emitted before the tool call
//	tool     bool    emit a tool_call / tool_result pair first
//	approval bool    ask permission to run a command before replying; if
//	                 denied, the reply reports the denial message instead
//	approvals number  with approval: ask this many times over, and reply
//	                 with how many were allowed
//	together bool    with approvals: ask them all at once, as a runtime
//	                 making tool calls side by side
//	similar  []string with approval: offer these rules with the request,
//	                 as Claude Code does; with one of them among the turn's
//	                 allowed rules, the request goes through unasked
//	prefix   []string with approval: offer whatever starts with these
//	                 words, as Codex does; allowed always or for the rest of
//	                 the turn, the request is settled as the rule's
//	fail_on_resume bool  fail at once, before any event, whenever the turn
//	                 resumes a session: a session that will not resume
//	failure  string  the FailureKind reported with either kind of failure
//	retry_in_ms number  with failure: report it passing this long from now
//	                 (Result.RetryAt)
//	quota    object  report the account's standing before replying: limited
//	                 (bool), used_percent (number), window (string) and
//	                 resets_in_ms (number)
//	compact  bool    compact the session before replying
//	changes  []string report these files as written, before replying
//	write    []string write these files for real, relative to the working
//	                 directory, each with a line of its own, and report
//	                 them as written, before replying
//	content  string  with write: what the files hold instead, such as code
//	                 that builds, for a reader that checks it
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
//	quiet    bool    say nothing after the tools: the turn's last text is
//	                 its preamble
//	summary_reply string  in a turn summing up the work it handed on, reply
//	                 with this and do nothing else
//	when_asked string  asked for the reply a turn did not give (ReplyAsk),
//	                 give this; without it, say nothing again
//	withdraw_ms number  ask permission to run a command, take the request
//	                 back after this long unless answered, and reply with
//	                 what became of it
//
// What Steer passes before the reply is taken in, at once while the turn
// waits out its delay, and the reply ends with the last line of each.
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
	t := &fakeTurn{turnBase: newTurnBase(ctx, cancel), steerMode: optString(spec.Options, "steer"), working: true, heard: make(chan struct{}, 1)}
	go t.run(ctx, spec)
	return t, nil
}

type fakeTurn struct {
	*turnBase
	steerMode string
	// While working, the turn takes steering: said holds what Steer
	// passed until the turn gets to it, and heard tells it there is some.
	steerMu sync.Mutex
	working bool
	said    []fakeSteer
	heard   chan struct{}
}

// fakeSteer is text Steer passed.
type fakeSteer struct{ id, text string }

// Steer implements Turn: the turn takes text while it works.
func (t *fakeTurn) Steer(id, text string) error {
	t.steerMu.Lock()
	defer t.steerMu.Unlock()
	if !t.working || t.steerMode == "refuse" {
		return ErrSteerRefused
	}
	t.said = append(t.said, fakeSteer{id: id, text: text})
	select {
	case t.heard <- struct{}{}:
	default:
	}
	return nil
}

// work takes as long as delay, taking in what Steer passes meanwhile. It
// returns the texts taken in, and false when the turn was cancelled.
func (t *fakeTurn) work(ctx context.Context, delay time.Duration) ([]string, bool) {
	var took []string
	done := time.After(delay)
	for {
		select {
		case <-t.heard:
			texts, ok := t.takeIn(ctx, false)
			if !ok {
				return nil, false
			}
			took = append(took, texts...)
		case <-done:
			return took, true
		case <-ctx.Done():
			return nil, false
		}
	}
}

// takeIn takes in what Steer passed so far, and with stop, stops taking
// more: the reply is on its way. It returns the texts taken in, and false
// when the turn was cancelled.
func (t *fakeTurn) takeIn(ctx context.Context, stop bool) ([]string, bool) {
	t.steerMu.Lock()
	said := t.said
	if t.steerMode == "drop" {
		said = nil
	} else {
		t.said = nil
	}
	if stop {
		t.working = false
	}
	t.steerMu.Unlock()
	var took []string
	for _, s := range said {
		if !t.emit(ctx, Event{Kind: EventSteer, SteerID: s.id, Text: s.text}) {
			return nil, false
		}
		took = append(took, s.text)
	}
	return took, true
}

// stopSteering stops the turn taking steering and returns what it never
// got to.
func (t *fakeTurn) stopSteering() []fakeSteer {
	t.steerMu.Lock()
	defer t.steerMu.Unlock()
	t.working = false
	left := t.said
	t.said = nil
	return left
}

// run plays the scripted turn and finishes it, which always closes the
// channels and unblocks Result. What Steer passed that the turn never got
// to is reported dropped first.
func (t *fakeTurn) run(ctx context.Context, spec TurnSpec) {
	res, err := t.play(ctx, spec)
	for _, s := range t.stopSteering() {
		t.emit(ctx, Event{Kind: EventSteerDropped, SteerID: s.id})
	}
	t.finish(ctx, res, err)
}

// play plays the scripted turn. Every emit checks ctx so Cancel takes
// effect at the next step.
func (t *fakeTurn) play(ctx context.Context, spec TurnSpec) (Result, error) {
	// A session that will not resume: the turn fails before it says a
	// thing, the way a CLI does that cannot find the session it was given.
	if spec.Session.Resume && optBool(spec.Options, "fail_on_resume") {
		return fakeFailure(spec), errors.New("fake runtime: cannot resume the session")
	}
	session := spec.Session.resumeID()
	if session == "" {
		session = "fake-" + randomHex(4)
	}
	if !t.emit(ctx, Event{Kind: EventSession, SessionRef: session}) || !t.emit(ctx, Event{Kind: EventStatus, Text: "thinking"}) {
		return Result{}, ErrTurnCancelled
	}
	if spec.Prompt == ReplyAsk {
		// Asked for the reply it did not give: what when_asked says, or
		// nothing again.
		text := optString(spec.Options, "when_asked")
		if text != "" && !t.emit(ctx, Event{Kind: EventText, Text: text}) {
			return Result{}, ErrTurnCancelled
		}
		return Result{Output: text, SessionRef: session, Usage: fakeUsage(spec.Prompt, text)}, nil
	}
	if text := optString(spec.Options, "summary_reply"); text != "" && strings.Contains(spec.Prompt, HandedOnHeading) {
		// Summing up the work it handed on: said, and nothing more done.
		if !t.emit(ctx, Event{Kind: EventText, Text: text}) {
			return Result{}, ErrTurnCancelled
		}
		return Result{Output: text, SessionRef: session}, nil
	}
	if optBool(spec.Options, "tool") {
		// A preamble is text said before the tool runs, so a turn has two
		// text segments around a tool call, as real agents do.
		if pre := optString(spec.Options, "preamble"); pre != "" && !t.emit(ctx, Event{Kind: EventText, Text: pre}) {
			return Result{}, ErrTurnCancelled
		}
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: "read_file", Input: "README.md"}) ||
			!t.emit(ctx, Event{Kind: EventToolResult, Tool: "read_file", Text: "# Veyloom"}) {
			return Result{}, ErrTurnCancelled
		}
	}
	if optBool(spec.Options, "compact") {
		if !t.emit(ctx, Event{Kind: EventCompaction, Phase: CompactionStart}) || !t.emit(ctx, Event{Kind: EventCompaction, Phase: CompactionEnd}) {
			return Result{}, ErrTurnCancelled
		}
	}
	for _, path := range optStrings(spec.Options, "changes") {
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: "write_file", Input: path}) ||
			!t.emit(ctx, Event{Kind: EventFileChanged, Path: path}) ||
			!t.emit(ctx, Event{Kind: EventToolResult, Tool: "write_file", Text: "ok"}) {
			return Result{}, ErrTurnCancelled
		}
	}
	for _, path := range optStrings(spec.Options, "write") {
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: "write_file", Input: path}) {
			return Result{}, ErrTurnCancelled
		}
		result := "ok"
		if err := writeInto(spec.WorkDir, path, optString(spec.Options, "content")); err != nil {
			result = "error: " + err.Error()
		} else if !t.emit(ctx, Event{Kind: EventFileChanged, Path: path}) {
			return Result{}, ErrTurnCancelled
		}
		if !t.emit(ctx, Event{Kind: EventToolResult, Tool: "write_file", Text: result}) {
			return Result{}, ErrTurnCancelled
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
			return Result{}, ErrTurnCancelled
		}
		reply = text
	}
	if tool := optString(spec.Options, "room_tool"); tool != "" {
		// Reads the room as a real agent would, through the turn's host.
		q := RoomQuery{Tool: tool, Topic: int(optFloat(spec.Options, "room_topic")), Text: optString(spec.Options, "room_text")}
		input, _ := json.Marshal(q)
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: tool, Input: string(input)}) {
			return Result{}, ErrTurnCancelled
		}
		answer := runRoomTool(ctx, spec.Host, tool, input)
		if !t.emit(ctx, Event{Kind: EventToolResult, Tool: tool, Text: answer}) {
			return Result{}, ErrTurnCancelled
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
			return Result{}, ErrTurnCancelled
		}
		// A runtime has only the tools its turn was given.
		answer := "error: this turn has no tool " + call.tool
		if !isOptionalTool(call.tool) || slices.Contains(spec.ExtraTools, call.tool) {
			answer = runRoomTool(ctx, spec.Host, call.tool, call.args)
		}
		if !t.emit(ctx, Event{Kind: EventToolResult, Tool: call.tool, Text: answer}) {
			return Result{}, ErrTurnCancelled
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
		return Result{}, ErrTurnCancelled
	}
	if verdict := optString(spec.Options, "reviewed"); verdict != "" &&
		!t.reviewed(ctx, fakeApprovalTool, fakeReviewedInput, fakeReviewer, verdict, "the fake reviewer's reasons", json.RawMessage(`{"risk":"low"}`)) {
		return Result{}, ErrTurnCancelled
	}
	if q := optString(spec.Options, "question"); q != "" {
		d, err := t.askQuestions(ctx, "AskUserQuestion", []Question{
			{ID: "1", Header: "Colour", Question: q, Options: []QuestionOption{{Label: "red"}, {Label: "blue"}}, Other: true},
			{ID: "2", Question: "The passphrase?", Other: true, Secret: true},
		})
		if err != nil {
			return Result{}, err
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
			return Result{}, err
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
			return Result{}, err
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
			return Result{}, err
		case err != nil:
			reply = "Withdrawn"
		default:
			reply = fmt.Sprintf("Answered before withdrawing: %v", d.Allow)
		}
	}
	if optBool(spec.Options, "approval") {
		var err error
		if reply, err = t.approve(ctx, spec, reply); err != nil {
			return Result{}, err
		}
	}
	var steered []string
	if delay := optDuration(spec.Options, "delay_ms"); delay > 0 {
		var ok bool
		if steered, ok = t.work(ctx, delay); !ok {
			return Result{}, ErrTurnCancelled
		}
	}
	if q, ok := spec.Options["quota"].(map[string]any); ok {
		quota := Quota{Limited: optBool(q, "limited"), Window: optString(q, "window")}
		if _, known := q["used_percent"]; known {
			used := int(optFloat(q, "used_percent"))
			quota.UsedPercent = &used
		}
		if in := optDuration(q, "resets_in_ms"); in > 0 {
			quota.ResetsAt = time.Now().Add(in).Truncate(time.Second)
		}
		if !t.emit(ctx, Event{Kind: EventQuota, Quota: &quota}) {
			return Result{}, ErrTurnCancelled
		}
	}
	if optBool(spec.Options, "fail") {
		t.emit(ctx, Event{Kind: EventError, Text: "scripted failure"})
		return fakeFailure(spec), errors.New("fake runtime: scripted failure")
	}

	if optBool(spec.Options, "quiet") {
		return Result{SessionRef: session, Usage: fakeUsage(spec.Prompt, "")}, nil
	}

	late, ok := t.takeIn(ctx, true)
	if !ok {
		return Result{}, ErrTurnCancelled
	}
	for _, text := range append(steered, late...) {
		reply += "\nSteered: " + lastLine(text)
	}

	// Two chunks so consumers see that text arrives incrementally.
	half := len(reply) / 2
	if !t.emit(ctx, Event{Kind: EventText, Text: reply[:half]}) || !t.emit(ctx, Event{Kind: EventText, Text: reply[half:]}) {
		return Result{}, ErrTurnCancelled
	}

	return Result{Output: reply, SessionRef: session, Usage: fakeUsage(spec.Prompt, reply)}, nil
}

// fakeUsage is what a fake turn spends: characters stand in for tokens,
// the prompt as input and the reply as output.
func fakeUsage(prompt, reply string) Usage {
	return Usage{InputTokens: int64(len([]rune(prompt))), OutputTokens: int64(len([]rune(reply)))}
}

// approve plays the approval option: the same command asked for as many
// times as the approvals option says, each settled by a rule when one
// covers it and by people otherwise. It returns the reply as it then
// stands: the denial message, or how the command was allowed.
func (t *fakeTurn) approve(ctx context.Context, spec TurnSpec, reply string) (string, error) {
	times := max(int(optFloat(spec.Options, "approvals")), 1)
	rules, prefix := optStrings(spec.Options, "similar"), optStrings(spec.Options, "prefix")
	var offer *Similar
	if len(rules) > 0 || len(prefix) > 0 {
		offer = &Similar{Rules: rules, Prefix: prefix}
	}
	standing := make(map[string]bool, len(spec.AllowedRules))
	for _, rule := range spec.AllowedRules {
		standing[rule] = true
	}
	// Like Claude Code, a rule of its own lets the command through unasked;
	// like the Codex runner, a prefix does and says so.
	unasked := false
	for _, rule := range rules {
		unasked = unasked || standing[rule]
	}
	prefixed := false
	if len(prefix) > 0 {
		key, _ := json.Marshal(prefix)
		prefixed = standing[string(key)]
	}

	if optBool(spec.Options, "together") && !unasked && !prefixed {
		return t.approveTogether(ctx, times, offer)
	}
	allowed := 0
	for range times {
		switch {
		case unasked:
			reply = "Allowed by a rule"
		case prefixed:
			detail, _ := json.Marshal(map[string]any{"prefix": prefix})
			if !t.reviewed(ctx, fakeApprovalTool, fakeApprovalInput, ReviewerRule, VerdictAllowed, "", detail) {
				return reply, ErrTurnCancelled
			}
			reply = "Allowed by a rule"
		default:
			d, err := t.requestApprovalOffering(ctx, fakeApprovalTool, fakeApprovalInput, offer)
			if err != nil {
				return reply, err
			}
			if !d.Allow {
				reply = "Denied: " + d.Message
				continue
			}
			if d.Similar {
				reply = "Allowed, and the like of it"
				unasked, prefixed = len(rules) > 0, len(prefix) > 0
			}
		}
		allowed++
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: fakeApprovalTool, Input: fakeApprovalInput}) ||
			!t.emit(ctx, Event{Kind: EventToolResult, Tool: fakeApprovalTool, Text: "ok"}) {
			return reply, ErrTurnCancelled
		}
	}
	if times > 1 {
		reply = fmt.Sprintf("Allowed %d of %d", allowed, times)
	}
	return reply, nil
}

// approveTogether asks for the command times over, all at once, and waits
// for every answer.
func (t *fakeTurn) approveTogether(ctx context.Context, times int, offer *Similar) (string, error) {
	decisions := make([]Decision, times)
	errs := make([]error, times)
	var wg sync.WaitGroup
	for i := range times {
		wg.Go(func() {
			decisions[i], errs[i] = t.requestApprovalOffering(ctx, fakeApprovalTool, fakeApprovalInput, offer)
		})
	}
	wg.Wait()
	allowed := 0
	for i, d := range decisions {
		if errs[i] != nil {
			return "", errs[i]
		}
		if !d.Allow {
			continue
		}
		allowed++
		if !t.emit(ctx, Event{Kind: EventToolCall, Tool: fakeApprovalTool, Input: fakeApprovalInput}) ||
			!t.emit(ctx, Event{Kind: EventToolResult, Tool: fakeApprovalTool, Text: "ok"}) {
			return "", ErrTurnCancelled
		}
	}
	return fmt.Sprintf("Allowed %d of %d", allowed, times), nil
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

// writeInto writes a file at path inside dir holding content or, without
// any, a line unlike any written before, so every write is a change.
func writeInto(dir, path, content string) error {
	if dir == "" || filepath.IsAbs(path) || !filepath.IsLocal(path) {
		return fmt.Errorf("%q is not a path inside the working directory", path)
	}
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if content == "" {
		content = "written in a fake turn " + randomHex(4) + "\n"
	}
	return os.WriteFile(full, []byte(content), 0o644)
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

// fakeFailure is the failure the options script: its kind, and when it
// passes.
func fakeFailure(spec TurnSpec) Result {
	res := Result{Failure: FailureKind(optString(spec.Options, "failure"))}
	if in := optDuration(spec.Options, "retry_in_ms"); in > 0 {
		res.RetryAt = time.Now().Add(in)
	}
	return res
}
