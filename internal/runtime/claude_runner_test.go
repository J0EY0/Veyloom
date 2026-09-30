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

const claudeFixture = `{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-opus-5","cwd":"/tmp"}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"I will "}}}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"read it."}}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I will read it."},{"type":"tool_use","id":"tu1","name":"Read","input":{"file_path":"README.md"}}]},"session_id":"sess-1"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"# Veyloom"}]},"session_id":"sess-1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu2","name":"Write","input":{"file_path":"notes.md","content":"hi <b>"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu2","content":[{"type":"text","text":"ok"}]}]}}
this line is a stray warning, not JSON
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":" Done."}}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":" Done."}]}}
{"type":"result","subtype":"success","is_error":false,"duration_ms":1234,"num_turns":3,"result":"I will read it. Done.","session_id":"sess-1","total_cost_usd":0.01,"usage":{"input_tokens":10,"cache_read_input_tokens":7,"cache_creation_input_tokens":3,"output_tokens":5}}
`

func runClaude(t *testing.T, cfg ClaudeConfig, spec TurnSpec) ([]Event, Result, error) {
	t.Helper()
	turn, err := NewClaudeRunner(cfg).StartTurn(context.Background(), spec)
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	events := drain(t, turn)
	res, err := turn.Result()
	return events, res, err
}

func TestClaude_ParsesStreamJSON(t *testing.T) {
	fakeClaudeCLI(t, claudeFixture, 0, "")

	events, res, err := runClaude(t, ClaudeConfig{StreamPartials: true}, TurnSpec{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}

	if res.Output != "I will read it. Done." || res.SessionRef != "sess-1" {
		t.Errorf("unexpected result: %+v", res)
	}
	// Anthropic keeps cached input apart from fresh input; the cost Claude
	// Code estimates is not kept.
	if want := (Usage{InputTokens: 10, CacheReadTokens: 7, CacheWriteTokens: 3, OutputTokens: 5}); res.Usage != want {
		t.Errorf("usage = %+v, want %+v", res.Usage, want)
	}

	var kinds []EventKind
	var text strings.Builder
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == EventText {
			text.WriteString(ev.Text)
		}
	}
	// The session is reported first, before anything can go wrong.
	want := []EventKind{EventSession, EventStatus, EventText, EventText, EventToolCall, EventToolResult, EventToolCall, EventToolResult, EventFileChanged, EventText}
	if strings.Join(kindStrings(kinds), ",") != strings.Join(kindStrings(want), ",") {
		t.Errorf("events = %v\n   want %v", kinds, want)
	}
	if events[0].SessionRef != "sess-1" {
		t.Errorf("session event = %+v, want the CLI's session id", events[0])
	}
	// Text comes from the deltas only; the assistant text blocks are not
	// repeated on top.
	if text.String() != "I will read it. Done." {
		t.Errorf("streamed text = %q", text.String())
	}

	byKind := map[EventKind][]Event{}
	for _, ev := range events {
		byKind[ev.Kind] = append(byKind[ev.Kind], ev)
	}
	if calls := byKind[EventToolCall]; calls[0].Tool != "Read" || calls[0].Input != `{"file_path":"README.md"}` || !strings.Contains(calls[1].Input, "hi <b>") {
		t.Errorf("tool calls: %+v", calls)
	}
	if results := byKind[EventToolResult]; results[0].Tool != "Read" || results[0].Text != "# Veyloom" || results[1].Tool != "Write" || results[1].Text != "ok" {
		t.Errorf("tool results should be attributed by tool_use id: %+v", results)
	}
	checkCallIDs(t, events)
	if byKind[EventFileChanged][0].Path != "notes.md" {
		t.Errorf("file change: %+v", byKind[EventFileChanged])
	}
	if !strings.Contains(byKind[EventStatus][0].Text, "claude-opus-5") {
		t.Errorf("init status should name the model: %+v", byKind[EventStatus])
	}
}

func kindStrings(kinds []EventKind) []string {
	out := make([]string, len(kinds))
	for i, k := range kinds {
		out[i] = string(k)
	}
	return out
}

// checkCallIDs checks that every tool call carries its id and every result
// the id of a call of the same tool made before it.
func checkCallIDs(t *testing.T, events []Event) {
	t.Helper()
	calls := map[string]string{}
	for _, ev := range events {
		switch ev.Kind {
		case EventToolCall:
			if ev.CallID == "" {
				t.Errorf("a tool call without its id: %+v", ev)
			}
			calls[ev.CallID] = ev.Tool
		case EventToolResult:
			if tool, ok := calls[ev.CallID]; !ok || tool != ev.Tool {
				t.Errorf("a tool result not paired with its call: %+v", ev)
			}
		}
	}
}

func TestClaude_ASubagentsWordsAreNotTheReply(t *testing.T) {
	// As Claude Code 2.1.275 forwards an Agent tool's subagent: its lines
	// carry the tool use they belong to.
	const fixture = `{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-haiku","cwd":"/tmp"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu-agent","name":"Agent","input":{"prompt":"Say PLUM"}}]},"parent_tool_use_id":null}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"PLUM"}},"parent_tool_use_id":"tu-agent"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu-read","name":"Read","input":{"file_path":"SKILL.md"}},{"type":"text","text":"PLUM"}]},"parent_tool_use_id":"tu-agent"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu-agent","content":"PLUM"}]},"parent_tool_use_id":null}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"It said plum."}},"parent_tool_use_id":null}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"It said plum."}]},"parent_tool_use_id":null}
{"type":"result","subtype":"success","is_error":false,"num_turns":2,"result":"It said plum.","session_id":"sess-1"}
`
	for _, partials := range []bool{true, false} {
		lines := fixture
		if !partials {
			// Without the flag the CLI sends no stream_event lines.
			var kept []string
			for _, line := range strings.Split(fixture, "\n") {
				if !strings.Contains(line, `"stream_event"`) {
					kept = append(kept, line)
				}
			}
			lines = strings.Join(kept, "\n")
		}
		fakeClaudeCLI(t, lines, 0, "")
		events, res, err := runClaude(t, ClaudeConfig{StreamPartials: partials}, TurnSpec{Prompt: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		var text strings.Builder
		var calls []string
		for _, ev := range events {
			switch ev.Kind {
			case EventText:
				text.WriteString(ev.Text)
			case EventToolCall:
				calls = append(calls, ev.Tool)
			}
		}
		if text.String() != "It said plum." || res.Output != "It said plum." {
			t.Errorf("partials %v: text %q, output %q, want the turn's own words", partials, text.String(), res.Output)
		}
		if strings.Join(calls, ",") != "Agent,Read" {
			t.Errorf("partials %v: calls %q, want the subagent's among the turn's", partials, calls)
		}
	}
}

func TestClaude_WithoutPartialsTextComesFromMessages(t *testing.T) {
	// No stream_event lines, as the CLI would produce without the flag.
	var lines []string
	for _, line := range strings.Split(claudeFixture, "\n") {
		if !strings.Contains(line, `"stream_event"`) {
			lines = append(lines, line)
		}
	}
	argsPath, _ := fakeClaudeCLI(t, strings.Join(lines, "\n"), 0, "")

	events, res, err := runClaude(t, ClaudeConfig{StreamPartials: false}, TurnSpec{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, ev := range events {
		if ev.Kind == EventText {
			text.WriteString(ev.Text)
		}
	}
	if text.String() != "I will read it. Done." || res.Output != "I will read it. Done." {
		t.Errorf("text = %q, output = %q", text.String(), res.Output)
	}
	args, _ := os.ReadFile(argsPath)
	if strings.Contains(string(args), "--include-partial-messages") {
		t.Error("partials flag must be absent when StreamPartials is false")
	}
}

func TestClaude_ArgsAndStdin(t *testing.T) {
	argsPath, stdinPath := fakeClaudeCLI(t, claudeFixture, 0, "")

	spec := TurnSpec{
		SystemPrompt: "You are the architect.",
		Prompt:       "line 1\nline 2 with 'quotes' and $vars",
		Model:        "opus",
		Permission:   PermissionEditWithApproval,
		Session:      Session{Key: "key-1", Ref: "sess-prev", Resume: true},
		Options:      map[string]any{"max_budget_usd": 2.5, "extra_args": []any{"--add-dir", "/extra"}},
	}
	if _, _, err := runClaude(t, ClaudeConfig{StreamPartials: true}, spec); err != nil {
		t.Fatal(err)
	}

	args, _ := os.ReadFile(argsPath)
	got := strings.Split(strings.TrimSpace(string(args)), "\n")
	want := []string{
		"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--replay-user-messages", "--include-partial-messages",
		"--append-system-prompt", "You are the architect.",
		"--model", "opus",
		"--resume", "sess-prev",
		"--permission-mode", "acceptEdits",
		"--permission-prompt-tool", "stdio",
		"--max-budget-usd", "2.5",
		"--add-dir", "/extra",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args = %q\n  want %q", got, want)
	}
	// The prompt is the one user message on stdin, verbatim.
	stdin, _ := os.ReadFile(stdinPath)
	var first struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}
	lines := strings.Split(strings.TrimSpace(string(stdin)), "\n")
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil || len(lines) != 1 || first.Type != "user" || first.Message.Role != "user" || first.Message.Content != spec.Prompt {
		t.Errorf("stdin = %q, want one user message carrying the prompt", stdin)
	}
}

// The spec's environment is the CLI's, and with it the commands it runs:
// here it moves where the fake writes its arguments.
func TestClaude_RunsInTheSpecsEnvironment(t *testing.T) {
	fakeClaudeCLI(t, claudeFixture, 0, "")
	moved := t.TempDir() + "/args"
	if _, _, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "a", Env: []string{"VEYLOOM_FAKE_CLAUDE_ARGS=" + moved}}); err != nil {
		t.Fatal(err)
	}
	if args, err := os.ReadFile(moved); err != nil || !strings.Contains(string(args), "stream-json") {
		t.Errorf("the CLI did not run with the spec's environment: %q %v", args, err)
	}
}

func TestClaude_PermissionModes(t *testing.T) {
	for preset, mode := range map[string]string{
		PermissionReadOnly:         "plan",
		PermissionEditWithApproval: "acceptEdits",
		PermissionAutoReview:       "auto",
		PermissionFullAuto:         "bypassPermissions",
	} {
		argsPath, _ := fakeClaudeCLI(t, claudeFixture, 0, "")
		if _, _, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x", Permission: preset}); err != nil {
			t.Fatal(err)
		}
		args, _ := os.ReadFile(argsPath)
		if !strings.Contains(string(args), "--permission-mode\n"+mode+"\n") {
			t.Errorf("%s: expected --permission-mode %s in %q", preset, mode, args)
		}
	}
	// An unknown preset passes no mode, leaving the CLI's default.
	argsPath, _ := fakeClaudeCLI(t, claudeFixture, 0, "")
	runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x"})
	if args, _ := os.ReadFile(argsPath); strings.Contains(string(args), "--permission-mode") {
		t.Error("no preset should mean no --permission-mode flag")
	}
}

func TestClaude_ErrorResult(t *testing.T) {
	fakeClaudeCLI(t, `{"type":"system","subtype":"init","session_id":"s"}
{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Credit balance is too low","session_id":"s","usage":{"input_tokens":40,"output_tokens":2}}
`, 0, "")

	_, res, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "Credit balance is too low") {
		t.Errorf("got %v, want the result's error text", err)
	}
	// What a failed run spent still counts.
	if want := (Usage{InputTokens: 40, OutputTokens: 2}); res.Usage != want {
		t.Errorf("usage = %+v, want %+v", res.Usage, want)
	}
}

func TestClaude_ProcessFailureWithoutResult(t *testing.T) {
	fakeClaudeCLI(t, `{"type":"system","subtype":"init","session_id":"s"}`+"\n", 1, "Not logged in. Run claude login.")

	_, _, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "Not logged in") {
		t.Errorf("got %v, want an error carrying stderr", err)
	}
}

func TestClaude_CleanExitWithoutResult(t *testing.T) {
	fakeClaudeCLI(t, "", 0, "")

	_, _, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "without a result") {
		t.Errorf("got %v", err)
	}
}

func TestClaude_LargeLineAndTruncation(t *testing.T) {
	big := strings.Repeat("x", 300*1024)
	fixture := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"Write","input":{"file_path":"big.txt","content":"` + big + `"}}]}}
{"type":"result","subtype":"success","is_error":false,"result":"wrote it","session_id":"s"}
`
	fakeClaudeCLI(t, fixture, 0, "")

	events, res, err := runClaude(t, ClaudeConfig{MaxEventBytes: 100}, TurnSpec{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "wrote it" {
		t.Errorf("output = %q", res.Output)
	}
	for _, ev := range events {
		if ev.Kind == EventToolCall {
			if len(ev.Input) > 200 || !strings.Contains(ev.Input, "more bytes]") {
				t.Errorf("tool input should be truncated with a marker, got %d bytes", len(ev.Input))
			}
		}
	}
}

func TestClaude_Cancel(t *testing.T) {
	fakeBinary(t, "claude", "exec /bin/sleep 30")

	turn, err := NewClaudeRunner(ClaudeConfig{WaitDelay: 200 * time.Millisecond}).StartTurn(context.Background(), TurnSpec{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	turn.Cancel()

	drain(t, turn)
	if _, err := turn.Result(); !errors.Is(err, ErrTurnCancelled) {
		t.Errorf("Result error = %v, want ErrTurnCancelled", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("cancel should end the turn promptly")
	}
}

func TestClaude_MissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	if _, err := NewClaudeRunner(ClaudeConfig{}).StartTurn(context.Background(), TurnSpec{Prompt: "x"}); err == nil {
		t.Error("a missing claude binary must fail at start")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("héllo wörld", 3); got != "hé… [10 more bytes]" {
		t.Errorf("truncate on a rune boundary: %q", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Errorf("no-op: %q", got)
	}
}

func TestClaude_SessionArgs(t *testing.T) {
	const key = "0b6f5c1e-8a4d-4c7b-9f2e-3d1a5b7c9e10"
	cases := []struct {
		name    string
		session Session
		flag    string
		value   string
	}{
		{"no session", Session{}, "", ""},
		{"first turn names the session", Session{Key: key}, "--session-id", key},
		{"later turns resume what the CLI called it", Session{Key: key, Ref: "sess-renamed", Resume: true}, "--resume", "sess-renamed"},
		{"resume by the key when no ref was kept", Session{Key: key, Resume: true}, "--resume", key},
		{"nothing to resume by", Session{Resume: true}, "", ""},
	}
	r := NewClaudeRunner(ClaudeConfig{})
	for _, c := range cases {
		args := r.args(TurnSpec{Prompt: "x", Session: c.session})
		for _, flag := range []string{"--session-id", "--resume"} {
			want := ""
			if flag == c.flag {
				want = c.value
			}
			if got := flagValue(args, flag); got != want {
				t.Errorf("%s: %s = %q, want %q (args %q)", c.name, flag, got, want, args)
			}
		}
	}
}

// What claude 2.1.85 prints for --resume of a session it does not have: no
// init line, and a result whose reason is in "errors" under a session id it
// made up for the occasion.
const claudeNoConversation = `{"type":"result","subtype":"error_during_execution","duration_ms":0,"duration_api_ms":0,"is_error":true,"num_turns":0,"stop_reason":null,"session_id":"made-up-for-the-error","total_cost_usd":0,"usage":{"input_tokens":0,"output_tokens":0},"modelUsage":{},"permission_denials":[],"uuid":"u1","errors":["No conversation found with session ID: sess-gone"]}
`

func TestClaude_MissingSessionIsNamedAndNotReported(t *testing.T) {
	fakeClaudeCLI(t, claudeNoConversation, 1, "")
	events, res, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x", Session: Session{Key: "key-1", Ref: "sess-gone", Resume: true}})
	if err == nil || !strings.Contains(err.Error(), "No conversation found with session ID: sess-gone") {
		t.Fatalf("err = %v, want the CLI's own reason rather than its subtype", err)
	}
	if res.Failure != FailureSessionNotFound {
		t.Errorf("Failure = %q, want %q", res.Failure, FailureSessionNotFound)
	}
	for _, ev := range events {
		if ev.Kind == EventSession {
			t.Errorf("the id on a failed result is not a session and must not be reported: %+v", ev)
		}
	}
}

func TestClaude_ContextOverflowIsNamed(t *testing.T) {
	out := `{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-opus-5"}
{"type":"result","subtype":"success","is_error":true,"result":"Prompt is too long","session_id":"sess-1","usage":{"input_tokens":40,"output_tokens":0}}
`
	fakeClaudeCLI(t, out, 1, "")
	_, res, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x", Session: Session{Key: "key-1", Ref: "sess-1", Resume: true}})
	if err == nil || res.Failure != FailureContextOverflow {
		t.Errorf("err = %v, Failure = %q; want a failure named %q", err, res.Failure, FailureContextOverflow)
	}
	if res.Usage.InputTokens != 40 {
		t.Errorf("a failed turn still counts what it spent: %+v", res.Usage)
	}
}

func TestClaude_ErrorFallsBackToStderrThenSubtype(t *testing.T) {
	bare := `{"type":"result","subtype":"error_during_execution","is_error":true,"session_id":"s"}` + "\n"

	fakeClaudeCLI(t, bare, 1, "credentials expired")
	if _, _, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x"}); err == nil || !strings.Contains(err.Error(), "credentials expired") {
		t.Errorf("err = %v, want what the CLI wrote to stderr", err)
	}
	fakeClaudeCLI(t, bare, 1, "")
	_, res, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "error_during_execution") {
		t.Errorf("err = %v, want the subtype when there is nothing better", err)
	}
	if res.Failure != "" {
		t.Errorf("Failure = %q for an error nobody can name", res.Failure)
	}
}

// compactionPhases is the phases of the compaction events among events.
func compactionPhases(events []Event) string {
	var phases []string
	for _, ev := range events {
		if ev.Kind == EventCompaction {
			phases = append(phases, ev.Phase)
		}
	}
	return strings.Join(phases, ",")
}

func TestClaude_ReportsCompaction(t *testing.T) {
	out := `{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-opus-5"}
{"type":"system","subtype":"status","status":"compacting","session_id":"sess-1"}
{"type":"system","subtype":"compact_boundary","session_id":"sess-1","compact_metadata":{"trigger":"auto","pre_tokens":167000}}
{"type":"system","subtype":"status","status":null,"session_id":"sess-1"}
{"type":"result","subtype":"success","is_error":false,"result":"done","session_id":"sess-1","usage":{"input_tokens":1,"output_tokens":1}}
`
	fakeClaudeCLI(t, out, 0, "")
	events, _, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := compactionPhases(events); got != "start,end" {
		t.Errorf("compaction phases = %q, want start then end; the status going back to null is no third event", got)
	}
}

// What the CLI says only to whoever reads its stream, a retry and the tool
// uses it turned down on its own, reaches people as notices.
func TestClaude_RetriesAndDenialsAreNotices(t *testing.T) {
	fixture := `{"type":"system","subtype":"init","session_id":"sess-1","model":"claude-opus-5"}
{"type":"system","subtype":"api_retry","attempt":1,"max_retries":10,"retry_delay_ms":2500,"error_status":529,"error":{"type":"overloaded_error","message":"Overloaded"},"session_id":"sess-1"}
{"type":"system","subtype":"api_retry","attempt":2,"max_retries":10,"retry_delay_ms":5000,"error_status":null,"error":"connection reset","session_id":"sess-1"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Could not write."}]},"session_id":"sess-1"}
{"type":"result","subtype":"success","is_error":false,"result":"Could not write.","session_id":"sess-1","usage":{"input_tokens":1,"output_tokens":1},"permission_denials":[{"tool_name":"Write","tool_use_id":"tu1","tool_input":{"file_path":"notes.md","content":"hi"}},{"tool_name":"Bash","tool_use_id":"tu2","tool_input":{"command":"rm -rf build"}}]}
`
	fakeClaudeCLI(t, fixture, 0, "")
	events, res, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x", Permission: PermissionReadOnly})
	if err != nil || res.Output != "Could not write." {
		t.Fatalf("result = %+v, %v", res, err)
	}
	var notices []string
	for _, ev := range events {
		if ev.Kind == EventNotice {
			notices = append(notices, ev.Level+": "+ev.Text)
		}
	}
	want := []string{
		"warning: Claude Code: the API request failed (HTTP 529): Overloaded; retrying in 2.5s, attempt 1 of 10",
		"warning: Claude Code: the API request failed: connection reset; retrying in 5s, attempt 2 of 10",
		`warning: Claude Code turned down Write {"content":"hi","file_path":"notes.md"} by its own rules, without asking anyone`,
		"warning: Claude Code turned down `rm -rf build` by its own rules, without asking anyone",
	}
	if strings.Join(notices, "\n") != strings.Join(want, "\n") {
		t.Errorf("notices:\n%s\nwant:\n%s", strings.Join(notices, "\n"), strings.Join(want, "\n"))
	}
}

// An edit the CLI did not carry out, denied or refused, changed nothing.
func TestClaude_RefusedEditChangesNoFile(t *testing.T) {
	fakeClaudeCLI(t, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"tu1","name":"Write","input":{"file_path":"notes.md","content":"x"}},{"type":"tool_use","id":"tu2","name":"Edit","input":{"file_path":"main.go"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"This member is read-only in Veyloom","is_error":true}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu2","content":"The file main.go has been updated."}]}}
{"type":"result","subtype":"success","is_error":false,"result":"ok","session_id":"s"}
`, 0, "")
	events, _, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var changed []string
	for _, ev := range events {
		if ev.Kind == EventFileChanged {
			changed = append(changed, ev.Path)
		}
	}
	if strings.Join(changed, ",") != "main.go" {
		t.Errorf("files changed = %q, want only the edit that went through", changed)
	}
	// The results say which call failed.
	var failed []bool
	for _, ev := range eventsOf(events, EventToolResult) {
		failed = append(failed, ev.Failed)
	}
	if !slices.Equal(failed, []bool{true, false}) {
		t.Errorf("failed %v, want the refused call's result marked", failed)
	}
}

func TestClaude_SkillsComeAsAPlugin(t *testing.T) {
	r := NewClaudeRunner(DefaultClaudeConfig())
	args := strings.Join(r.args(TurnSpec{Prompt: "x", SkillDir: "/tools/skills/0123456789abcdef"}), " ")
	if !strings.Contains(args, "--plugin-dir /tools/skills/0123456789abcdef") {
		t.Errorf("args %s", args)
	}
	// A person's own skill of a name the plugin has, here the project's:
	// the run is told to use the plugin's, and the person's is refused;
	// what was allowed comes along.
	home, work := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	own := filepath.Join(work, ".claude", "skills", "release-notes")
	os.MkdirAll(own, 0o755)
	os.WriteFile(filepath.Join(own, "SKILL.md"), []byte("---\nname: release-notes\n---\n"), 0o644)
	set := &SkillSet{Hash: "0123456789abcdef", Skills: []Skill{{Name: "release-notes"}, {Name: "team-practices", Builtin: true}}}
	argv := r.args(TurnSpec{Prompt: "x", SystemPrompt: "Be brief.", WorkDir: work, SkillDir: "/tools/skills/0123456789abcdef", Skills: set, AllowedRules: []string{"Bash(make test)"}})
	i := slices.Index(argv, "--settings")
	if i < 0 || argv[i+1] != `{"permissions":{"allow":["Bash(make test)"],"deny":["Skill(release-notes)"]}}` {
		t.Errorf("args %v", argv)
	}
	j := slices.Index(argv, "--append-system-prompt")
	if j < 0 || !strings.HasPrefix(argv[j+1], "Be brief.\n\n") || !strings.Contains(argv[j+1], "veyloom:release-notes") || strings.Contains(argv[j+1], "team-practices") {
		t.Errorf("system prompt %q", argv[j+1])
	}
	// No clash, no rule and no word of it; nor for a set not written.
	if argv := r.args(TurnSpec{Prompt: "x", SystemPrompt: "Be brief.", WorkDir: t.TempDir(), SkillDir: "/tools/skills/0123456789abcdef", Skills: set}); slices.Contains(argv, "--settings") ||
		argv[slices.Index(argv, "--append-system-prompt")+1] != "Be brief." {
		t.Errorf("no clash: %v", argv)
	}
	if argv := r.args(TurnSpec{Prompt: "x", WorkDir: work, Skills: set}); slices.Contains(argv, "--settings") {
		t.Errorf("no plugin, no rules: %v", argv)
	}
	if args := strings.Join(r.args(TurnSpec{Prompt: "x"}), " "); strings.Contains(args, "--plugin-dir") {
		t.Errorf("no skills, no plugin: %s", args)
	}
}
