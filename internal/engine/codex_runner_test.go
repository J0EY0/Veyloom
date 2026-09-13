package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fake app-server is this test binary re-executed with an environment
// variable set: a shell script cannot hold a JSON-RPC conversation, and
// the fake must answer requests, raise requests of its own and wait for
// the replies. TestMain diverts into it before any test runs.
func TestMain(m *testing.M) {
	if os.Getenv("VEYLOOM_FAKE_CODEX") == "1" {
		fakeCodexMain()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeCodexMain plays `codex app-server`. Every line it receives is
// appended to the file named by VEYLOOM_FAKE_CODEX_RECORD so tests can
// assert on what the runner sent. The turn's script is chosen by keywords
// in the prompt, written as [hang], [fail], [approve], [filechange] or
// [unsupported]; any other prompt plays the default turn.
func fakeCodexMain() {
	if len(os.Args) < 2 || os.Args[1] != "app-server" {
		fmt.Fprintf(os.Stderr, "fake codex: unexpected args %v\n", os.Args[1:])
		os.Exit(2)
	}
	if os.Getenv("VEYLOOM_FAKE_CODEX_EXIT") != "" {
		fmt.Fprintln(os.Stderr, "fake codex: refusing to start")
		os.Exit(3)
	}
	var record io.Writer = io.Discard
	if path := os.Getenv("VEYLOOM_FAKE_CODEX_RECORD"); path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err == nil {
			defer f.Close()
			record = f
		}
	}
	srv := &fakeAppServer{in: bufio.NewReader(os.Stdin), out: os.Stdout, record: record}
	srv.serve()
}

type fakeAppServer struct {
	in     *bufio.Reader
	out    io.Writer
	record io.Writer
}

func (f *fakeAppServer) send(v any) {
	data, _ := json.Marshal(v)
	f.out.Write(append(data, '\n'))
}

func (f *fakeAppServer) notify(method string, params any) {
	f.send(map[string]any{"method": method, "params": params})
}

// next reads one client line, recording it. ok is false at EOF.
func (f *fakeAppServer) next() (codexMessage, bool) {
	line, err := f.in.ReadBytes('\n')
	if len(line) > 0 {
		f.record.Write(line)
	}
	if err != nil && len(line) == 0 {
		return codexMessage{}, false
	}
	var msg codexMessage
	json.Unmarshal(line, &msg)
	return msg, true
}

// awaitReply reads until the client answers request id, returning the
// reply; other lines are consumed and recorded.
func (f *fakeAppServer) awaitReply(id int) (codexMessage, bool) {
	for {
		msg, ok := f.next()
		if !ok {
			return codexMessage{}, false
		}
		var got int
		if msg.Method == "" && json.Unmarshal(msg.ID, &got) == nil && got == id {
			return msg, true
		}
	}
}

func (f *fakeAppServer) serve() {
	for {
		msg, ok := f.next()
		if !ok {
			return
		}
		switch msg.Method {
		case "initialize":
			f.send(map[string]any{"id": msg.ID, "result": map[string]any{"userAgent": "fake-codex", "codexHome": "/tmp", "platformFamily": "unix", "platformOs": "macos"}})
		case "initialized":
		case "thread/start", "thread/resume":
			id := "thr-new"
			if msg.Method == "thread/resume" {
				var p struct {
					ThreadID string `json:"threadId"`
				}
				json.Unmarshal(msg.Params, &p)
				id = p.ThreadID
			}
			f.send(map[string]any{"id": msg.ID, "result": map[string]any{"thread": map[string]any{"id": id}, "model": "gpt-5-codex", "modelProvider": "openai", "cwd": "/tmp"}})
			f.notify("thread/started", map[string]any{"thread": map[string]any{"id": id}})
		case "turn/start":
			var p struct {
				ThreadID string `json:"threadId"`
				Input    []struct {
					Text string `json:"text"`
				} `json:"input"`
			}
			json.Unmarshal(msg.Params, &p)
			f.send(map[string]any{"id": msg.ID, "result": map[string]any{"turn": map[string]any{"id": "turn-1", "status": "inProgress", "items": []any{}}}})
			prompt := ""
			if len(p.Input) > 0 {
				prompt = p.Input[0].Text
			}
			if !f.play(p.ThreadID, prompt) {
				return
			}
		default:
			if len(msg.ID) > 0 {
				f.send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32601, "message": "unknown method " + msg.Method}})
			}
		}
	}
}

// play runs the turn's script. It returns false when stdin closed.
func (f *fakeAppServer) play(threadID, prompt string) bool {
	turn := map[string]any{"id": "turn-1", "status": "inProgress", "items": []any{}}
	f.notify("turn/started", map[string]any{"threadId": threadID, "turn": turn})
	complete := func(status, errMsg, final string) {
		if final != "" {
			f.notify("item/completed", map[string]any{"threadId": threadID, "turnId": "turn-1", "item": map[string]any{"type": "agentMessage", "id": "msg-final", "text": final}})
		}
		done := map[string]any{"id": "turn-1", "status": status, "items": []any{}}
		if errMsg != "" {
			done["error"] = map[string]any{"message": errMsg}
		}
		f.notify("turn/completed", map[string]any{"threadId": threadID, "turn": done})
	}
	item := func(kind string, extra map[string]any) map[string]any {
		extra["threadId"], extra["turnId"] = threadID, "turn-1"
		return extra
	}

	switch {
	case strings.Contains(prompt, "[hang]"):
		_, ok := f.next()
		return ok
	case strings.Contains(prompt, "[fail]"):
		complete("failed", "model exploded", "")
	case strings.Contains(prompt, "[approve]"):
		f.send(map[string]any{"id": 100, "method": "item/commandExecution/requestApproval", "params": item("", map[string]any{
			"itemId": "cmd-1", "startedAtMs": 0, "command": "rm -rf build", "cwd": "/repo", "reason": "cleanup",
		})})
		reply, ok := f.awaitReply(100)
		if !ok {
			return false
		}
		var res struct {
			Decision string `json:"decision"`
		}
		json.Unmarshal(reply.Result, &res)
		f.notify("item/started", item("", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "cmd-1", "command": "rm -rf build", "cwd": "/repo", "status": "inProgress", "commandActions": []any{}}}))
		if res.Decision == "accept" {
			f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "cmd-1", "command": "rm -rf build", "cwd": "/repo", "status": "completed", "aggregatedOutput": "removed\n", "exitCode": 0, "commandActions": []any{}}}))
			complete("completed", "", "ran it")
		} else {
			f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "cmd-1", "command": "rm -rf build", "cwd": "/repo", "status": "declined", "commandActions": []any{}}}))
			complete("completed", "", "declined")
		}
	case strings.Contains(prompt, "[filechange]"):
		changes := []map[string]any{{"path": "notes.md", "kind": map[string]any{"type": "add"}, "diff": "+hi"}}
		f.notify("item/started", item("", map[string]any{"item": map[string]any{"type": "fileChange", "id": "fc-1", "changes": changes, "status": "inProgress"}}))
		f.send(map[string]any{"id": "req-101", "method": "item/fileChange/requestApproval", "params": item("", map[string]any{"itemId": "fc-1", "startedAtMs": 0, "reason": "outside sandbox"})})
		reply, ok := f.awaitReplyString("req-101")
		if !ok {
			return false
		}
		var res struct {
			Decision string `json:"decision"`
		}
		json.Unmarshal(reply.Result, &res)
		if res.Decision == "accept" {
			f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "fileChange", "id": "fc-1", "changes": changes, "status": "completed"}}))
			complete("completed", "", "patched")
		} else {
			f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "fileChange", "id": "fc-1", "changes": changes, "status": "declined"}}))
			complete("completed", "", "not patched")
		}
	case strings.Contains(prompt, "[unsupported]"):
		f.send(map[string]any{"id": 102, "method": "item/tool/requestUserInput", "params": item("", map[string]any{"itemId": "q-1"})})
		reply, ok := f.awaitReply(102)
		if !ok {
			return false
		}
		if reply.Error != nil {
			complete("completed", "", "asked")
		} else {
			complete("completed", "", "wrong")
		}
	default:
		f.notify("item/started", item("", map[string]any{"item": map[string]any{"type": "agentMessage", "id": "msg-1", "text": ""}}))
		f.notify("item/agentMessage/delta", item("", map[string]any{"itemId": "msg-1", "delta": "Hel"}))
		f.notify("item/agentMessage/delta", item("", map[string]any{"itemId": "msg-1", "delta": "lo"}))
		f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "agentMessage", "id": "msg-1", "text": "Hello"}}))
		f.notify("item/started", item("", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "cmd-1", "command": "ls", "cwd": "/repo", "status": "inProgress", "commandActions": []any{}}}))
		f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "cmd-1", "command": "ls", "cwd": "/repo", "status": "completed", "aggregatedOutput": "a.txt\n", "exitCode": 1, "commandActions": []any{}}}))
		f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "fileChange", "id": "fc-1", "changes": []map[string]any{{"path": "notes.md", "kind": map[string]any{"type": "add"}, "diff": "+hi"}}, "status": "completed"}}))
		f.notify("item/started", item("", map[string]any{"item": map[string]any{"type": "mcpToolCall", "id": "mcp-1", "server": "github", "tool": "search", "status": "inProgress", "arguments": map[string]any{"q": "veyloom"}}}))
		f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "mcpToolCall", "id": "mcp-1", "server": "github", "tool": "search", "status": "completed", "arguments": map[string]any{"q": "veyloom"}, "result": map[string]any{"hits": 1}}}))
		f.notify("error", item("", map[string]any{"error": map[string]any{"message": "rate limited"}, "willRetry": true}))
		f.notify("thread/tokenUsage/updated", item("", map[string]any{"tokenUsage": map[string]any{
			"total": map[string]any{"totalTokens": 99},
			"last":  map[string]any{"totalTokens": 12, "inputTokens": 10, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "outputTokens": 2, "reasoningOutputTokens": 0},
		}}))
		f.notify("item/agentMessage/delta", item("", map[string]any{"itemId": "msg-final", "delta": " Done."}))
		complete("completed", "", "Hello Done.")
	}
	return true
}

// awaitReplyString is awaitReply for a string request id.
func (f *fakeAppServer) awaitReplyString(id string) (codexMessage, bool) {
	for {
		msg, ok := f.next()
		if !ok {
			return codexMessage{}, false
		}
		var got string
		if msg.Method == "" && json.Unmarshal(msg.ID, &got) == nil && got == id {
			return msg, true
		}
	}
}

// codexHarness points a runner at the fake and records its traffic.
type codexHarness struct {
	runner *CodexRunner
	record string
}

func newCodexHarness(t *testing.T) *codexHarness {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(t.TempDir(), "record.jsonl")
	t.Setenv("VEYLOOM_FAKE_CODEX", "1")
	t.Setenv("VEYLOOM_FAKE_CODEX_RECORD", record)
	return &codexHarness{runner: NewCodexRunner(CodexConfig{Binary: exe}), record: record}
}

// sent returns the recorded client messages by method.
func (h *codexHarness) sent(t *testing.T) map[string][]codexMessage {
	t.Helper()
	// The fake creates the file on its first line; before that there is
	// nothing to report.
	data, err := os.ReadFile(h.record)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	out := map[string][]codexMessage{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var msg codexMessage
		if json.Unmarshal([]byte(line), &msg) == nil {
			out[msg.Method] = append(out[msg.Method], msg)
		}
	}
	return out
}

func paramsOf(t *testing.T, msg codexMessage) map[string]any {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(msg.Params, &p); err != nil {
		t.Fatalf("params: %v", err)
	}
	return p
}

func TestCodex_HandshakeTurnAndEvents(t *testing.T) {
	h := newCodexHarness(t)
	workDir := t.TempDir()
	spec := TurnSpec{SystemPrompt: "You test things.", Prompt: "run the tests", WorkDir: workDir, Model: "gpt-5-codex", Permission: PermissionEditWithApproval}
	turn, err := h.runner.StartTurn(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	res, err := turn.Result()
	if err != nil {
		t.Fatal(err)
	}

	if res.Output != "Hello Done." || res.SessionRef != "thr-new" {
		t.Errorf("unexpected result: %+v", res)
	}
	if res.Usage["inputTokens"] != float64(10) || res.Usage["outputTokens"] != float64(2) {
		t.Errorf("usage should be the turn's token counts, got %v", res.Usage)
	}

	sent := h.sent(t)
	if init := sent["initialize"]; len(init) != 1 || !strings.Contains(string(init[0].Params), `"name":"veyloom"`) {
		t.Errorf("initialize = %+v", init)
	}
	if len(sent["initialized"]) != 1 {
		t.Error("initialized notification should follow initialize")
	}
	start := paramsOf(t, sent["thread/start"][0])
	if start["approvalPolicy"] != "on-request" || start["sandbox"] != "workspace-write" || start["cwd"] != workDir || start["model"] != "gpt-5-codex" || start["developerInstructions"] != "You test things." {
		t.Errorf("thread/start params = %v", start)
	}
	if len(sent["thread/resume"]) != 0 {
		t.Error("a first turn must not resume")
	}
	ts := paramsOf(t, sent["turn/start"][0])
	input := ts["input"].([]any)[0].(map[string]any)
	if ts["threadId"] != "thr-new" || input["type"] != "text" || input["text"] != "run the tests" || ts["approvalPolicy"] != "on-request" {
		t.Errorf("turn/start params = %v", ts)
	}
	if sp := ts["sandboxPolicy"].(map[string]any); sp["type"] != "workspaceWrite" {
		t.Errorf("sandboxPolicy = %v", sp)
	}

	var kinds []EventKind
	byKind := map[EventKind][]Event{}
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
		byKind[ev.Kind] = append(byKind[ev.Kind], ev)
	}
	want := []EventKind{EventStatus, EventText, EventText, EventToolCall, EventToolResult, EventFileChanged, EventToolCall, EventToolResult, EventError, EventText}
	if strings.Join(kindStrings(kinds), ",") != strings.Join(kindStrings(want), ",") {
		t.Errorf("events = %v\n   want %v", kinds, want)
	}
	if !strings.Contains(byKind[EventStatus][0].Text, "gpt-5-codex") {
		t.Errorf("status should name the model: %+v", byKind[EventStatus])
	}
	if calls := byKind[EventToolCall]; calls[0].Tool != "commandExecution" || calls[0].Input != "ls" || calls[1].Tool != "github/search" || calls[1].Input != `{"q":"veyloom"}` {
		t.Errorf("tool calls: %+v", calls)
	}
	if results := byKind[EventToolResult]; results[0].Text != "a.txt\n[exit code 1]" || results[1].Text != `{"hits":1}` {
		t.Errorf("tool results: %+v", results)
	}
	if byKind[EventFileChanged][0].Path != "notes.md" {
		t.Errorf("file change: %+v", byKind[EventFileChanged])
	}
	if !strings.Contains(byKind[EventError][0].Text, "rate limited") || !strings.Contains(byKind[EventError][0].Text, "retrying") {
		t.Errorf("error event: %+v", byKind[EventError])
	}
}

func TestCodex_PoliciesPerPresetAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		name       string
		permission string
		options    map[string]any
		approval   string
		sandbox    string
		policyType string
	}{
		{"read_only", PermissionReadOnly, nil, "never", "read-only", "readOnly"},
		{"edit_with_approval", PermissionEditWithApproval, nil, "on-request", "workspace-write", "workspaceWrite"},
		{"full_auto", PermissionFullAuto, nil, "never", "workspace-write", "workspaceWrite"},
		{"unknown preset falls back to read only", "", nil, "never", "read-only", "readOnly"},
		{"overrides", PermissionEditWithApproval, map[string]any{"approval_policy": "untrusted", "sandbox": "danger-full-access"}, "untrusted", "danger-full-access", "dangerFullAccess"},
		{"bad sandbox override ignored", PermissionReadOnly, map[string]any{"sandbox": "chroot"}, "never", "read-only", "readOnly"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCodexHarness(t)
			turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "hi", Permission: tc.permission, Options: tc.options})
			if err != nil {
				t.Fatal(err)
			}
			drain(t, turn)
			sent := h.sent(t)
			start := paramsOf(t, sent["thread/start"][0])
			ts := paramsOf(t, sent["turn/start"][0])
			if start["approvalPolicy"] != tc.approval || start["sandbox"] != tc.sandbox || ts["approvalPolicy"] != tc.approval || ts["sandboxPolicy"].(map[string]any)["type"] != tc.policyType {
				t.Errorf("thread/start %v, turn/start %v", start, ts)
			}
		})
	}
}

func TestCodex_ResumesTheThread(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{SystemPrompt: "role", Prompt: "again", SessionRef: "thr-old", Permission: PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	res, err := turn.Result()
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionRef != "thr-old" {
		t.Errorf("SessionRef = %q, want the resumed thread", res.SessionRef)
	}
	sent := h.sent(t)
	if len(sent["thread/start"]) != 0 || len(sent["thread/resume"]) != 1 {
		t.Fatalf("expected thread/resume only, got start=%d resume=%d", len(sent["thread/start"]), len(sent["thread/resume"]))
	}
	resume := paramsOf(t, sent["thread/resume"][0])
	if resume["threadId"] != "thr-old" || resume["approvalPolicy"] != "never" || resume["sandbox"] != "read-only" {
		t.Errorf("thread/resume params = %v", resume)
	}
	if _, has := resume["developerInstructions"]; has {
		t.Error("a resumed thread keeps its instructions; none should be sent")
	}
}

func TestCodex_CommandApproval(t *testing.T) {
	for _, tc := range []struct {
		name   string
		allow  bool
		output string
		result string
	}{
		{"allowed", true, "ran it", "removed\n"},
		{"denied", false, "declined", "declined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCodexHarness(t)
			turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[approve] this", Permission: PermissionEditWithApproval})
			if err != nil {
				t.Fatal(err)
			}
			req, _ := awaitApproval(t, turn)
			if req.Tool != "commandExecution" || req.Input != `{"command":"rm -rf build","cwd":"/repo","reason":"cleanup"}` {
				t.Fatalf("unexpected approval request: %+v", req)
			}
			if err := turn.Answer(req.ApprovalID, Decision{Allow: tc.allow, Message: "because"}); err != nil {
				t.Fatal(err)
			}
			events := drain(t, turn)
			res, err := turn.Result()
			if err != nil {
				t.Fatal(err)
			}
			if res.Output != tc.output {
				t.Errorf("Output = %q, want %q", res.Output, tc.output)
			}
			var result string
			for _, ev := range events {
				if ev.Kind == EventToolResult {
					result = ev.Text
				}
			}
			if result != tc.result {
				t.Errorf("tool result = %q, want %q", result, tc.result)
			}
		})
	}
}

func TestCodex_FileChangeApprovalNamesThePaths(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[filechange] please", Permission: PermissionEditWithApproval})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := awaitApproval(t, turn)
	if req.Tool != "fileChange" || req.Input != `{"paths":["notes.md"],"reason":"outside sandbox"}` {
		t.Fatalf("unexpected approval request: %+v", req)
	}
	if err := turn.Answer(req.ApprovalID, Decision{Allow: true}); err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	res, err := turn.Result()
	if err != nil || res.Output != "patched" {
		t.Errorf("result = %+v, %v", res, err)
	}
	changed := false
	for _, ev := range events {
		if ev.Kind == EventFileChanged && ev.Path == "notes.md" {
			changed = true
		}
	}
	if !changed {
		t.Error("an applied file change should be reported")
	}
}

func TestCodex_UnsupportedServerRequestIsRefused(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[unsupported] request", Permission: PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	res, err := turn.Result()
	if err != nil || res.Output != "asked" {
		t.Errorf("the server should get a JSON-RPC error and carry on; result = %+v, %v", res, err)
	}
}

func TestCodex_FailedTurn(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[fail] now", Permission: PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	if _, err := turn.Result(); err == nil || !strings.Contains(err.Error(), "model exploded") {
		t.Errorf("Result error = %v, want the server's message", err)
	}
}

func TestCodex_Cancel(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[hang] forever", Permission: PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the turn to be under way, then cancel.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if sent := h.sent(t); len(sent["turn/start"]) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	turn.Cancel()
	drain(t, turn)
	if _, err := turn.Result(); !errors.Is(err, ErrTurnCancelled) {
		t.Errorf("Result error = %v, want ErrTurnCancelled", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("cancel should stop the server promptly")
	}
}

func TestCodex_ServerDiesDuringSetup(t *testing.T) {
	h := newCodexHarness(t)
	t.Setenv("VEYLOOM_FAKE_CODEX_EXIT", "1")
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	_, err = turn.Result()
	if err == nil || !strings.Contains(err.Error(), "exited during initialize") || !strings.Contains(err.Error(), "refusing to start") {
		t.Errorf("Result error = %v, want the exit with stderr", err)
	}
}

func TestCodex_MissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := NewCodexRunner(CodexConfig{}).StartTurn(context.Background(), TurnSpec{Prompt: "hi"}); err == nil {
		t.Error("expected an error when codex is not installed")
	}
}
