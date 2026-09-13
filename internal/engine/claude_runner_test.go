package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeClaudeCLI installs a `claude` script on PATH that records its
// arguments and stdin, prints the given stream-json output and exits with
// the given code. It returns the paths of the recorded arguments and stdin.
func fakeClaudeCLI(t *testing.T, output string, exitCode int, stderr string) (argsPath, stdinPath string) {
	t.Helper()
	dir := t.TempDir()
	argsPath = filepath.Join(dir, "args")
	stdinPath = filepath.Join(dir, "stdin")
	outputPath := filepath.Join(dir, "output.jsonl")
	if err := os.WriteFile(outputPath, []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	// PATH is restricted to the fake's directory, so external commands
	// need absolute paths; printf, test and exit are shell builtins.
	script := "printf '%s\\n' \"$@\" > " + argsPath + "\n" +
		"/bin/cat > " + stdinPath + "\n" +
		"[ -n \"" + stderr + "\" ] && echo \"" + stderr + "\" >&2\n" +
		"/bin/cat " + outputPath + "\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	fakeBinary(t, "claude", script)
	return argsPath, stdinPath
}

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
{"type":"result","subtype":"success","is_error":false,"duration_ms":1234,"num_turns":3,"result":"I will read it. Done.","session_id":"sess-1","total_cost_usd":0.01,"usage":{"input_tokens":10,"output_tokens":5}}
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
	if res.Usage["cost_usd"] != 0.01 || res.Usage["num_turns"] != 3 || res.Usage["input_tokens"] != float64(10) {
		t.Errorf("usage not collected: %v", res.Usage)
	}

	var kinds []EventKind
	var text strings.Builder
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == EventText {
			text.WriteString(ev.Text)
		}
	}
	want := []EventKind{EventStatus, EventText, EventText, EventToolCall, EventToolResult, EventToolCall, EventFileChanged, EventToolResult, EventText}
	if strings.Join(kindStrings(kinds), ",") != strings.Join(kindStrings(want), ",") {
		t.Errorf("events = %v\n   want %v", kinds, want)
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
		SessionRef:   "sess-prev",
		Options:      map[string]any{"max_budget_usd": 2.5, "extra_args": []any{"--add-dir", "/extra"}},
	}
	if _, _, err := runClaude(t, ClaudeConfig{StreamPartials: true}, spec); err != nil {
		t.Fatal(err)
	}

	args, _ := os.ReadFile(argsPath)
	got := strings.Split(strings.TrimSpace(string(args)), "\n")
	want := []string{
		"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--append-system-prompt", "You are the architect.",
		"--model", "opus",
		"--resume", "sess-prev",
		"--permission-mode", "acceptEdits",
		"--max-budget-usd", "2.5",
		"--add-dir", "/extra",
	}
	// edit_with_approval also wires up the approval tool; its exact value
	// is covered by the approval tests.
	if len(got) < len(want)+4 || got[len(want)] != "--permission-prompt-tool" || got[len(want)+2] != "--mcp-config" {
		t.Fatalf("args = %q\n  want %q followed by the approval flags", got, want)
	}
	got = got[:len(want)]
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args = %q\n  want %q", got, want)
	}
	stdin, _ := os.ReadFile(stdinPath)
	if string(stdin) != spec.Prompt {
		t.Errorf("stdin = %q, want the prompt verbatim", stdin)
	}
}

func TestClaude_PermissionModes(t *testing.T) {
	for preset, mode := range map[string]string{
		PermissionReadOnly:         "plan",
		PermissionEditWithApproval: "acceptEdits",
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
{"type":"result","subtype":"error_during_execution","is_error":true,"result":"Credit balance is too low","session_id":"s"}
`, 0, "")

	_, _, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "Credit balance is too low") {
		t.Errorf("got %v, want the result's error text", err)
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
