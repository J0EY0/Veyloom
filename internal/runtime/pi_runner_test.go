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

const piFixture = `{"type":"session","version":3,"id":"pi-sess-1","timestamp":"2026-09-13T00:00:00Z","cwd":"/tmp"}
{"type":"agent_start"}
{"type":"turn_start"}
{"type":"message_start","message":{"role":"assistant","content":[]}}
{"type":"message_update","message":{"role":"assistant","content":[]},"assistantMessageEvent":{"type":"text_delta","delta":"Let me look."}}
{"type":"message_update","message":{"role":"assistant","content":[]},"assistantMessageEvent":{"type":"thinking_delta","delta":"hmm"}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Let me look."},{"type":"toolCall","id":"tc1","name":"read","arguments":{"path":"README.md"}}],"usage":{"input":10,"output":4,"cacheRead":1,"cacheWrite":0,"totalTokens":15,"cost":{"total":0.001}},"stopReason":"toolUse"}}
{"type":"tool_execution_start","toolCallId":"tc1","toolName":"read","args":{"path":"README.md"}}
{"type":"tool_execution_end","toolCallId":"tc1","toolName":"read","result":{"content":[{"type":"text","text":"# Veyloom"}]},"isError":false}
{"type":"tool_execution_start","toolCallId":"tc2","toolName":"write","args":{"path":"notes.md","content":"hi"}}
{"type":"tool_execution_end","toolCallId":"tc2","toolName":"write","result":"ok","isError":false}
{"type":"tool_execution_start","toolCallId":"tc3","toolName":"bash","args":{"command":"false"}}
{"type":"tool_execution_end","toolCallId":"tc3","toolName":"bash","result":{"content":[{"type":"text","text":"exit 1"}]},"isError":true}
{"type":"turn_end","message":{},"toolResults":[]}
{"type":"turn_start"}
{"type":"message_update","message":{"role":"assistant","content":[]},"assistantMessageEvent":{"type":"text_delta","delta":" It is Veyloom."}}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":" It is Veyloom."}],"usage":{"input":20,"output":5,"cacheRead":0,"cacheWrite":0,"totalTokens":25,"cost":{"total":0.002}},"stopReason":"stop"}}
{"type":"turn_end","message":{},"toolResults":[]}
{"type":"agent_end","messages":[]}
`

func runPi(t *testing.T, cfg PiConfig, spec TurnSpec) ([]Event, Result, error) {
	t.Helper()
	turn, err := NewPiRunner(cfg).StartTurn(context.Background(), spec)
	if err != nil {
		t.Fatalf("StartTurn: %v", err)
	}
	events := drain(t, turn)
	res, err := turn.Result()
	return events, res, err
}

func TestPi_ParsesEventStream(t *testing.T) {
	fakePiCLI(t, piFixture, 0, "")

	events, res, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}

	// The reply is the last assistant message, not the running narration.
	if res.Output != " It is Veyloom." || res.SessionRef != "pi-sess-1" {
		t.Errorf("unexpected result: %+v", res)
	}
	if want := (Usage{InputTokens: 30, CacheReadTokens: 1, OutputTokens: 9}); res.Usage != want {
		t.Errorf("usage should be summed over assistant messages: %+v, want %+v", res.Usage, want)
	}

	var kinds []string
	var text strings.Builder
	byKind := map[EventKind][]Event{}
	for _, ev := range events {
		kinds = append(kinds, string(ev.Kind))
		byKind[ev.Kind] = append(byKind[ev.Kind], ev)
		if ev.Kind == EventText {
			text.WriteString(ev.Text)
		}
	}
	want := "session,status,text,tool_call,tool_result,tool_call,tool_result,file_changed,tool_call,tool_result,text"
	if strings.Join(kinds, ",") != want {
		t.Errorf("events = %v\n   want %s", kinds, want)
	}
	if byKind[EventSession][0].SessionRef != "pi-sess-1" {
		t.Errorf("session event = %+v, want pi's session id", byKind[EventSession])
	}
	if text.String() != "Let me look. It is Veyloom." {
		t.Errorf("streamed text = %q", text.String())
	}
	if calls := byKind[EventToolCall]; calls[0].Tool != "read" || calls[0].Input != `{"path":"README.md"}` {
		t.Errorf("tool call: %+v", calls[0])
	}
	if results := byKind[EventToolResult]; results[0].Text != "# Veyloom" || results[1].Text != "ok" || results[2].Text != "error: exit 1" {
		t.Errorf("tool results: %+v", results)
	}
	checkCallIDs(t, events)
	if byKind[EventFileChanged][0].Path != "notes.md" {
		t.Errorf("file change: %+v", byKind[EventFileChanged])
	}
}

func TestPi_Args(t *testing.T) {
	argsPath := fakePiCLI(t, piFixture, 0, "")
	spec := TurnSpec{
		SystemPrompt: "You test things.",
		Prompt:       "@Tester run the suite",
		Model:        "anthropic/claude-sonnet-5",
		Permission:   PermissionEditWithApproval,
		Session:      Session{Ref: "pi-sess-prev", Resume: true},
		Options:      map[string]any{"provider": "anthropic", "thinking": "low", "extra_args": []any{"--no-extensions"}},
	}

	if _, _, err := runPi(t, PiConfig{}, spec); err != nil {
		t.Fatal(err)
	}

	args, _ := os.ReadFile(argsPath)
	got := strings.Split(strings.TrimSpace(string(args)), "\n")
	want := []string{
		"--mode", "rpc",
		"--append-system-prompt", "You test things.",
		"--provider", "anthropic",
		"--model", "anthropic/claude-sonnet-5",
		"--thinking", "low",
		"--session", "pi-sess-prev",
		"--tools", "read,grep,find,ls,edit,write",
		"--no-extensions",
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args = %q\n  want %q", got, want)
	}
}

func TestPi_PermissionToolSets(t *testing.T) {
	cases := map[string]string{
		PermissionReadOnly:         "--tools\nread,grep,find,ls\n",
		PermissionEditWithApproval: "--tools\nread,grep,find,ls,edit,write\n",
		// No reviewer of pi's own to hand commands to.
		PermissionAutoReview: "--tools\nread,grep,find,ls,edit,write\n",
	}
	for preset, want := range cases {
		argsPath := fakePiCLI(t, piFixture, 0, "")
		runPi(t, PiConfig{}, TurnSpec{Prompt: "x", Permission: preset})
		if args, _ := os.ReadFile(argsPath); !strings.Contains(string(args), want) {
			t.Errorf("%s: expected %q in %q", preset, want, args)
		}
	}
	// full_auto and unknown presets leave every tool enabled.
	for _, preset := range []string{PermissionFullAuto, ""} {
		argsPath := fakePiCLI(t, piFixture, 0, "")
		runPi(t, PiConfig{}, TurnSpec{Prompt: "x", Permission: preset})
		if args, _ := os.ReadFile(argsPath); strings.Contains(string(args), "--tools") {
			t.Errorf("%q: no --tools flag expected", preset)
		}
	}
}

func TestPi_MissingAPIKeyIsReportedFromStdout(t *testing.T) {
	// What pi prints without credentials: the header, then plain text.
	fakePiCLI(t, `{"type":"session","version":3,"id":"s","timestamp":"","cwd":"/tmp"}
No API key found for the selected model.

Use /login to log into a provider via OAuth or API key.
`, 0, "")

	_, _, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "No API key found") {
		t.Errorf("got %v, want the plain-text reason", err)
	}
}

func TestPi_ErrorStopReason(t *testing.T) {
	fakePiCLI(t, `{"type":"session","version":3,"id":"s","timestamp":"","cwd":"/tmp"}
{"type":"message_end","message":{"role":"assistant","content":[],"usage":{"input":12,"output":3,"cacheRead":0,"cacheWrite":0},"stopReason":"toolUse"}}
{"type":"message_end","message":{"role":"assistant","content":[],"stopReason":"error","errorMessage":"rate limited"}}
{"type":"agent_end","messages":[]}
`, 0, "")

	_, res, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("got %v", err)
	}
	// The messages before the error spent tokens all the same.
	if want := (Usage{InputTokens: 12, OutputTokens: 3}); res.Usage != want {
		t.Errorf("usage = %+v, want %+v", res.Usage, want)
	}
}

func TestPi_ProcessFailure(t *testing.T) {
	fakePiCLI(t, "", 2, "pi: unknown option --bogus")

	_, _, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Errorf("got %v, want stderr in the error", err)
	}
}

func TestPi_Cancel(t *testing.T) {
	fakeBinary(t, "pi", "exec /bin/sleep 30")

	turn, err := NewPiRunner(PiConfig{WaitDelay: 200 * time.Millisecond}).StartTurn(context.Background(), TurnSpec{Prompt: "x"})
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

func TestPi_MissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := NewPiRunner(PiConfig{}).StartTurn(context.Background(), TurnSpec{Prompt: "x"}); err == nil {
		t.Error("a missing pi binary must fail at start")
	}
}

func TestBuiltinRunners_CoverRealRuntimes(t *testing.T) {
	runners := BuiltinRunners()
	for _, name := range []string{"fake", "claude", "pi"} {
		if _, ok := runners[name]; !ok {
			t.Errorf("no runner registered for %s", name)
		}
	}
}

func TestPi_SessionArgs(t *testing.T) {
	const key = "0b6f5c1e-8a4d-4c7b-9f2e-3d1a5b7c9e10"
	dir := filepath.Join(t.TempDir(), "sessions")
	file := filepath.Join(dir, key+".jsonl")
	cases := []struct {
		name    string
		dir     string
		session Session
		want    string
	}{
		{"no session", dir, Session{}, ""},
		{"first turn: our own file", dir, Session{Key: key}, file},
		{"later turns: the same file, not pi's id", dir, Session{Key: key, Ref: "pi-sess-1", Resume: true}, file},
		{"no directory: nothing on the first turn", "", Session{Key: key}, ""},
		{"no directory: resume by pi's id", "", Session{Key: key, Ref: "pi-sess-1", Resume: true}, "pi-sess-1"},
		{"a key that is no UUID never names a file", dir, Session{Key: "../escape", Ref: "pi-sess-1", Resume: true}, "pi-sess-1"},
	}
	for _, c := range cases {
		r := NewPiRunner(PiConfig{SessionDir: c.dir})
		args := r.args(TurnSpec{Prompt: "x", Session: c.session})
		if got := flagValue(args, "--session"); got != c.want {
			t.Errorf("%s: --session = %q, want %q (args %q)", c.name, got, c.want, args)
		}
	}
}

func TestPi_CreatesTheSessionDirectory(t *testing.T) {
	const key = "0b6f5c1e-8a4d-4c7b-9f2e-3d1a5b7c9e10"
	argsPath := fakePiCLI(t, piFixture, 0, "")
	dir := filepath.Join(t.TempDir(), "state", "sessions")

	if _, _, err := runPi(t, PiConfig{SessionDir: dir}, TurnSpec{Prompt: "x", Session: Session{Key: key}}); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("session directory: %v", err)
	}
	args, _ := os.ReadFile(argsPath)
	if want := "--session\n" + filepath.Join(dir, key+".jsonl") + "\n"; !strings.Contains(string(args), want) {
		t.Errorf("args = %q, want them to carry %q", args, want)
	}
}

func TestPi_ResumeOfAMissingSessionFileFailsWithoutRunning(t *testing.T) {
	const key = "0b6f5c1e-8a4d-4c7b-9f2e-3d1a5b7c9e10"
	argsPath := fakePiCLI(t, piFixture, 0, "")
	dir := t.TempDir()

	// Told to continue a session whose file is gone, pi would start a new
	// one without a word; the runner says so instead and never starts pi.
	events, res, err := runPi(t, PiConfig{SessionDir: dir}, TurnSpec{Prompt: "x", Session: Session{Key: key, Ref: "pi-sess-1", Resume: true}})
	if err == nil || !strings.Contains(err.Error(), "is gone") {
		t.Fatalf("err = %v, want the missing file named", err)
	}
	if res.Failure != FailureSessionNotFound {
		t.Errorf("Failure = %q, want %q", res.Failure, FailureSessionNotFound)
	}
	if len(events) != 0 {
		t.Errorf("events = %+v, want none", events)
	}
	if _, err := os.Stat(argsPath); !errors.Is(err, os.ErrNotExist) {
		t.Error("pi must not be started for a session that is gone")
	}

	// The same session on its first turn has no file yet, and that is fine.
	if _, _, err := runPi(t, PiConfig{SessionDir: dir}, TurnSpec{Prompt: "x", Session: Session{Key: key}}); err != nil {
		t.Errorf("first turn: %v", err)
	}
	// With the file there, resuming goes ahead.
	if err := os.WriteFile(filepath.Join(dir, key+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runPi(t, PiConfig{SessionDir: dir}, TurnSpec{Prompt: "x", Session: Session{Key: key, Ref: "pi-sess-1", Resume: true}}); err != nil {
		t.Errorf("resume with the file in place: %v", err)
	}
}

func TestPi_UnknownSessionIDIsNamed(t *testing.T) {
	// pi 0.73 answers an id it does not know with one plain line and exit 0.
	fakePiCLI(t, "No session found matching 'pi-sess-gone'\n", 0, "")
	_, res, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "x", Session: Session{Ref: "pi-sess-gone", Resume: true}})
	if err == nil || !strings.Contains(err.Error(), "No session found matching") {
		t.Fatalf("err = %v, want pi's own words", err)
	}
	if res.Failure != FailureSessionNotFound {
		t.Errorf("Failure = %q, want %q", res.Failure, FailureSessionNotFound)
	}
}

func TestPi_ReportsCompaction(t *testing.T) {
	// One that fails and is tried again, then one that takes. Pi says it
	// will run the agent again only after one that took, so the failed one
	// does not.
	out := `{"type":"session","id":"pi-sess-1"}
{"type":"compaction_start","reason":"overflow"}
{"type":"compaction_end","reason":"overflow","aborted":false,"willRetry":false,"errorMessage":"summary request failed"}
{"type":"compaction_start","reason":"threshold"}
{"type":"compaction_end","reason":"threshold","result":{"summary":"## Goal\n...","firstKeptEntryId":"e9","tokensBefore":180000},"aborted":false,"willRetry":false}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"stopReason":"stop"}}
{"type":"agent_end","messages":[]}
`
	fakePiCLI(t, out, 0, "")
	events, _, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := compactionPhases(events); got != "start,failed,start,end" {
		t.Errorf("compaction phases = %q, want start,failed,start,end", got)
	}
	for _, ev := range events {
		if ev.Kind == EventCompaction && ev.Phase == CompactionFailed && ev.Text != "summary request failed" {
			t.Errorf("a failed compaction should say why: %+v", ev)
		}
	}
}

func TestPi_SkillsOneFolderEach(t *testing.T) {
	dir, err := WriteSkills(t.TempDir(), testSkillSet("0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	r := NewPiRunner(PiConfig{})
	args := strings.Join(r.args(TurnSpec{Prompt: "x", SkillDir: dir}), "\n")
	for _, name := range []string{"go-table-tests", "release-notes"} {
		if !strings.Contains(args, "--skill\n"+filepath.Join(dir, "skills", name)) {
			t.Errorf("args lack --skill %s:\n%s", name, args)
		}
	}
}

// A maintainer's upkeep turn gets its tools: whitelisted, and named to
// the extension, which registers its optional tools for the turns that
// name them. A chat turn gets neither.
func TestPi_UpkeepTurnsGetTheMaintainersTools(t *testing.T) {
	cfg := PiConfig{ToolDir: t.TempDir()}
	argsPath := fakePiCLI(t, piFixture, 0, "")
	if _, _, err := runPi(t, cfg, TurnSpec{Prompt: "x", Permission: PermissionReadOnly, Host: &recordingHost{}, ExtraTools: UpkeepToolNames}); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(argsPath)
	want := strings.Join(append(append([]string{"read", "grep", "find", "ls"}, AgentToolNames...), UpkeepToolNames...), ",")
	if !strings.Contains(string(args), "--tools\n"+want+"\n") {
		t.Errorf("args %q, want --tools %s", args, want)
	}
	if extra, _ := os.ReadFile(argsPath + ".extra"); string(extra) != strings.Join(UpkeepToolNames, ",") {
		t.Errorf("the extension is told %q", extra)
	}
	source, _ := os.ReadFile(piExtensionFile(cfg.ToolDir))
	if !strings.Contains(string(source), `"name": "list_turns"`) || !strings.Contains(string(source), `"optional": true`) || !strings.Contains(string(source), piExtraToolsEnv) {
		t.Errorf("the extension does not know the optional tools:\n%s", source)
	}

	argsPath = fakePiCLI(t, piFixture, 0, "")
	if _, _, err := runPi(t, cfg, TurnSpec{Prompt: "x", Permission: PermissionReadOnly, Host: &recordingHost{}}); err != nil {
		t.Fatal(err)
	}
	if args, _ := os.ReadFile(argsPath); strings.Contains(string(args), UpkeepToolListTurns) || !strings.Contains(string(args), RoomToolReadTurn) {
		t.Errorf("a chat turn should get read_turn and not the maintainer's tools: %q", args)
	}
	if _, err := os.Stat(argsPath + ".extra"); err == nil {
		t.Error("a chat turn names optional tools to the extension")
	}
}
