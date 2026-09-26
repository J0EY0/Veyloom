package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// The fake app-server, and the fake Claude Code and pi, are this test binary
// re-executed with an environment variable set: a shell script cannot hold
// a JSON-RPC conversation, and the fakes must answer requests, raise
// requests of their own and wait for the replies. TestMain diverts into
// them before any test runs.
func TestMain(m *testing.M) {
	if os.Getenv("VEYLOOM_FAKE_CODEX") == "1" {
		fakeCodexMain()
		os.Exit(0)
	}
	if os.Getenv("VEYLOOM_FAKE_CLAUDE") == "1" {
		fakeClaudeMain()
		os.Exit(0)
	}
	if os.Getenv("VEYLOOM_FAKE_PI") == "1" {
		fakePiMain()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeCodexMain plays `codex app-server`. Every line it receives is
// appended to the file named by VEYLOOM_FAKE_CODEX_RECORD so tests can
// assert on what the runner sent. The turn's script is chosen by keywords
// in the prompt, written as [hang], [fail], [approve], [filechange],
// [permissions], [question], [elicit], [review] or [unsupported]; any other
// prompt plays the default turn.
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
		case "skills/extraRoots/set":
			f.send(map[string]any{"id": msg.ID, "result": map[string]any{}})
		case "thread/start", "thread/resume":
			id := "thr-new"
			if msg.Method == "thread/resume" {
				var p struct {
					ThreadID string `json:"threadId"`
				}
				json.Unmarshal(msg.Params, &p)
				id = p.ThreadID
			}
			if id == "thr-gone" {
				// A thread the server will not resume.
				f.send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32600, "message": "no rollout found for thread id thr-gone"}})
				continue
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
	case strings.Contains(prompt, "[subagent]"):
		// Codex starts a subagent, which works in a thread of its own on the
		// same server: its messages stream among the turn's, and its turn
		// ends before the turn does.
		sub := func(extra map[string]any) map[string]any {
			extra["threadId"], extra["turnId"] = "thr-sub", "turn-sub"
			return extra
		}
		usage := func(in, out int) map[string]any {
			n := map[string]any{"totalTokens": in + out, "inputTokens": in, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "outputTokens": out}
			return map[string]any{"tokenUsage": map[string]any{"total": n, "last": n}}
		}
		f.notify("item/started", item("", map[string]any{"item": map[string]any{"type": "subAgentActivity", "id": "sa-1", "kind": "started", "agentThreadId": "thr-sub", "agentPath": "/root/audit_refunds"}}))
		f.notify("turn/started", map[string]any{"threadId": "thr-sub", "turn": map[string]any{"id": "turn-sub", "status": "inProgress", "items": []any{}}})
		f.notify("item/agentMessage/delta", item("", map[string]any{"itemId": "msg-1", "delta": "Look"}))
		f.notify("item/agentMessage/delta", sub(map[string]any{"itemId": "msg-sub", "delta": "SUB "}))
		f.notify("item/agentMessage/delta", item("", map[string]any{"itemId": "msg-1", "delta": "ing."}))
		f.notify("item/started", sub(map[string]any{"item": map[string]any{"type": "commandExecution", "id": "cmd-sub", "command": "cat SKILL.md", "status": "inProgress", "commandActions": []any{}}}))
		f.notify("item/completed", sub(map[string]any{"item": map[string]any{"type": "commandExecution", "id": "cmd-sub", "command": "cat SKILL.md", "status": "completed", "aggregatedOutput": "45 days\n", "exitCode": 0, "commandActions": []any{}}}))
		f.notify("item/started", sub(map[string]any{"item": map[string]any{"type": "contextCompaction", "id": "cc-sub"}}))
		f.notify("item/completed", sub(map[string]any{"item": map[string]any{"type": "contextCompaction", "id": "cc-sub"}}))
		f.notify("thread/tokenUsage/updated", sub(usage(40, 4)))
		f.notify("item/completed", sub(map[string]any{"item": map[string]any{"type": "agentMessage", "id": "msg-sub", "text": "SUB done"}}))
		f.notify("turn/completed", map[string]any{"threadId": "thr-sub", "turn": map[string]any{"id": "turn-sub", "status": "completed", "items": []any{}}})
		f.notify("thread/tokenUsage/updated", item("", usage(100, 10)))
		f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "agentMessage", "id": "msg-1", "text": "Looking."}}))
		f.notify("item/started", item("", map[string]any{"item": map[string]any{"type": "mcpToolCall", "id": "mcp-1", "server": "veyloom", "tool": "patch_wiki", "arguments": map[string]any{"path": "/skills/refund-window/SKILL.md"}, "status": "inProgress"}}))
		f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "mcpToolCall", "id": "mcp-1", "server": "veyloom", "tool": "patch_wiki", "status": "completed", "result": map[string]any{"content": []map[string]any{{"type": "text", "text": "Proposed"}}}}}))
		complete("completed", "", "Proposed the patch.")
	case strings.Contains(prompt, "[compact]"):
		// The thread outgrew its budget: codex compacts, then answers.
		f.notify("item/started", item("", map[string]any{"item": map[string]any{"type": "contextCompaction", "id": "cc-1"}}))
		f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "contextCompaction", "id": "cc-1"}}))
		complete("completed", "", "compacted and done")
	case strings.Contains(prompt, "[hang]"):
		// One response in before it hangs, so a cancel has spent something.
		f.notify("thread/tokenUsage/updated", item("", map[string]any{"tokenUsage": map[string]any{
			"total": map[string]any{"totalTokens": 85, "inputTokens": 80, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "outputTokens": 5},
			"last":  map[string]any{"totalTokens": 85, "inputTokens": 80, "cachedInputTokens": 0, "cacheWriteInputTokens": 0, "outputTokens": 5},
		}}))
		f.notify("item/started", item("", map[string]any{"item": map[string]any{"type": "agentMessage", "id": "msg-1", "text": ""}}))
		f.notify("item/agentMessage/delta", item("", map[string]any{"itemId": "msg-1", "delta": "Thinking"}))
		_, ok := f.next()
		return ok
	case strings.Contains(prompt, "[fail]"):
		f.notify("thread/tokenUsage/updated", item("", map[string]any{"tokenUsage": map[string]any{
			"total": map[string]any{"totalTokens": 320, "inputTokens": 300, "cachedInputTokens": 100, "cacheWriteInputTokens": 0, "outputTokens": 20},
			"last":  map[string]any{"totalTokens": 320, "inputTokens": 300, "cachedInputTokens": 100, "cacheWriteInputTokens": 0, "outputTokens": 20},
		}}))
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
		if res.Decision == "accept" || res.Decision == "acceptForSession" {
			f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "cmd-1", "command": "rm -rf build", "cwd": "/repo", "status": "completed", "aggregatedOutput": "removed\n", "exitCode": 0, "commandActions": []any{}}}))
			complete("completed", "", map[bool]string{false: "ran it", true: "ran it, and will again"}[res.Decision == "acceptForSession"])
		} else {
			f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "cmd-1", "command": "rm -rf build", "cwd": "/repo", "status": "declined", "commandActions": []any{}}}))
			complete("completed", "", "declined")
		}
	case strings.Contains(prompt, "[prefix]"):
		// Three commands, each proposing go test as the words to allow:
		// two simple ones and one that does more after it.
		var decisions []string
		for i, command := range []string{"/bin/zsh -lc 'go test ./...'", "/bin/zsh -lc 'go test ./cmd/...'", "/bin/zsh -lc 'go test ./... && rm -rf /'"} {
			id := 200 + i
			f.send(map[string]any{"id": id, "method": "item/commandExecution/requestApproval", "params": item("", map[string]any{
				"itemId": fmt.Sprintf("cmd-%d", i), "startedAtMs": 0, "command": command, "cwd": "/repo", "proposedExecpolicyAmendment": []string{"go", "test"},
			})})
			reply, ok := f.awaitReply(id)
			if !ok {
				return false
			}
			var res struct {
				Decision string `json:"decision"`
			}
			json.Unmarshal(reply.Result, &res)
			decisions = append(decisions, res.Decision)
		}
		complete("completed", "", strings.Join(decisions, ","))
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
	case strings.Contains(prompt, "[permissions]"):
		f.send(map[string]any{"id": 103, "method": "item/permissions/requestApproval", "params": item("", map[string]any{
			"itemId": "perm-1", "startedAtMs": 0, "environmentId": nil, "cwd": "/repo", "reason": "fetch deps",
			"permissions": map[string]any{"network": map[string]any{"enabled": true}, "fileSystem": nil},
		})})
		reply, ok := f.awaitReply(103)
		if !ok {
			return false
		}
		var res struct {
			Permissions struct {
				Network *struct {
					Enabled bool `json:"enabled"`
				} `json:"network"`
				FileSystem json.RawMessage `json:"fileSystem"`
			} `json:"permissions"`
			Scope string `json:"scope"`
		}
		json.Unmarshal(reply.Result, &res)
		switch granted := res.Permissions; {
		case granted.Network != nil && granted.Network.Enabled && granted.FileSystem == nil:
			complete("completed", "", "network for the "+res.Scope)
		case granted.Network == nil && granted.FileSystem == nil:
			complete("completed", "", "nothing granted for the "+res.Scope)
		default:
			complete("completed", "", "unexpected grant: "+string(reply.Result))
		}
	case strings.Contains(prompt, "[question]"):
		f.send(map[string]any{"id": 104, "method": "item/tool/requestUserInput", "params": item("", map[string]any{
			"itemId": "q-1", "isBlocking": true, "autoResolutionMs": nil,
			"questions": []map[string]any{
				{"id": "colour", "header": "Colour", "question": "Which colour?", "isOther": false, "isSecret": false,
					"options": []map[string]any{{"label": "red", "description": "warm"}, {"label": "blue", "description": "cool"}}},
				{"id": "token", "header": "Token", "question": "Your token?", "isOther": false, "isSecret": true, "options": nil},
			},
		})})
		reply, ok := f.awaitReply(104)
		if !ok {
			return false
		}
		var res struct {
			Answers map[string]struct {
				Answers []string `json:"answers"`
			} `json:"answers"`
		}
		json.Unmarshal(reply.Result, &res)
		var parts []string
		for _, id := range []string{"colour", "token", "stray"} {
			if a, ok := res.Answers[id]; ok {
				parts = append(parts, id+"="+strings.Join(a.Answers, "|"))
			}
		}
		if len(parts) == 0 {
			complete("completed", "", "no answers")
		} else {
			complete("completed", "", strings.Join(parts, "; "))
		}
	case strings.Contains(prompt, "[elicit]"):
		// An MCP server asks for a form, then for a link to be opened.
		f.send(map[string]any{"id": 105, "method": "mcpServer/elicitation/request", "params": map[string]any{
			"threadId": threadID, "turnId": "turn-1", "serverName": "deploy", "mode": "form", "_meta": nil, "message": "Where to?",
			"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"region": map[string]any{"type": "string", "enum": []string{"eu", "us"}}}, "required": []string{"region"}},
		}})
		form, ok := f.awaitReply(105)
		if !ok {
			return false
		}
		f.send(map[string]any{"id": 106, "method": "mcpServer/elicitation/request", "params": map[string]any{
			"threadId": threadID, "turnId": "turn-1", "serverName": "deploy", "mode": "url", "_meta": nil, "message": "Sign in",
			"url": "https://login.example.com/device", "elicitationId": "el-1",
		}})
		link, ok := f.awaitReply(106)
		if !ok {
			return false
		}
		complete("completed", "", "form="+compactJSON(form.Result)+"; link="+compactJSON(link.Result))
	case strings.Contains(prompt, "[review]"):
		// Codex's own reviewer settles two requests: one approved with its
		// reasoning, one denied whose reasoning only came as the warning
		// sent during the review. Around them, what Codex tells people.
		command := map[string]any{"type": "command", "source": "unifiedExec", "command": "/bin/zsh -lc 'curl -sI https://example.com'", "cwd": "/repo"}
		f.notify("item/autoApprovalReview/started", item("", map[string]any{"reviewId": "rv-1", "targetItemId": "exec-1", "startedAtMs": 1, "review": map[string]any{"status": "inProgress"}, "action": command}))
		f.notify("guardianWarning", item("", map[string]any{"message": "Automatic approval review approved (risk: low)"}))
		f.notify("item/autoApprovalReview/completed", item("", map[string]any{"reviewId": "rv-1", "targetItemId": "exec-1", "startedAtMs": 1, "completedAtMs": 2, "decisionSource": "agent",
			"review": map[string]any{"status": "approved", "riskLevel": "low", "userAuthorization": "high", "rationale": "a public HEAD request"}, "action": command}))
		patch := map[string]any{"type": "applyPatch", "cwd": "/repo", "files": []string{"/etc/hosts"}}
		f.notify("item/autoApprovalReview/started", item("", map[string]any{"reviewId": "rv-2", "startedAtMs": 3, "review": map[string]any{"status": "inProgress"}, "action": patch}))
		f.notify("guardianWarning", item("", map[string]any{"message": "writing outside the workspace"}))
		f.notify("item/autoApprovalReview/completed", item("", map[string]any{"reviewId": "rv-2", "startedAtMs": 3, "completedAtMs": 4, "decisionSource": "agent",
			"review": map[string]any{"status": "denied", "riskLevel": "high", "userAuthorization": nil, "rationale": nil}, "action": patch}))
		f.notify("guardianWarning", item("", map[string]any{"message": "outside any review"}))
		f.notify("autoApprovalReview/strictReviewRequired", item("", map[string]any{"startedAtMs": 5}))
		f.notify("warning", map[string]any{"threadId": threadID, "message": "rate limits are close"})
		f.notify("configWarning", map[string]any{"summary": "unknown key", "details": "foo is not a setting", "path": "/home/me/.codex/config.toml"})
		f.notify("deprecationNotice", map[string]any{"summary": "sandbox_permissions is going away", "details": nil})
		f.notify("model/rerouted", item("", map[string]any{"fromModel": "gpt-6", "toModel": "gpt-6-safe", "reason": "highRiskCyberActivity"}))
		for range 2 { // Codex retries a server that will not start
			f.notify("mcpServer/startupStatus/updated", map[string]any{"threadId": threadID, "name": "node_repl", "status": "starting", "error": nil})
			f.notify("mcpServer/startupStatus/updated", map[string]any{"threadId": threadID, "name": "node_repl", "status": "failed", "error": "handshake failed"})
		}
		complete("completed", "", "reviewed")
	case strings.Contains(prompt, "[unsupported]"):
		f.send(map[string]any{"id": 102, "method": "item/somethingNew/request", "params": item("", map[string]any{"itemId": "q-1"})})
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
		f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "mcpToolCall", "id": "mcp-1", "server": "github", "tool": "search", "status": "completed", "arguments": map[string]any{"q": "veyloom"}, "result": map[string]any{
			"content": []map[string]any{{"type": "text", "text": "1 hit"}, {"type": "text", "text": "J0EY0/veyloom"}}, "structuredContent": nil, "_meta": nil,
		}}}))
		f.notify("item/started", item("", map[string]any{"item": map[string]any{"type": "mcpToolCall", "id": "mcp-2", "server": "github", "tool": "search", "status": "inProgress", "arguments": map[string]any{}}}))
		f.notify("item/completed", item("", map[string]any{"item": map[string]any{"type": "mcpToolCall", "id": "mcp-2", "server": "github", "tool": "search", "status": "failed", "arguments": map[string]any{}, "error": map[string]any{"message": "server gone"}}}))
		f.notify("error", item("", map[string]any{"error": map[string]any{"message": "rate limited"}, "willRetry": true}))
		// Two model responses in the turn, on a thread that had already
		// spent 1000 input (400 of it cached) and 90 output.
		f.notify("thread/tokenUsage/updated", item("", map[string]any{"tokenUsage": map[string]any{
			"total": map[string]any{"totalTokens": 1150, "inputTokens": 1060, "cachedInputTokens": 450, "cacheWriteInputTokens": 0, "outputTokens": 90 + 12, "reasoningOutputTokens": 4},
			"last":  map[string]any{"totalTokens": 72, "inputTokens": 60, "cachedInputTokens": 50, "cacheWriteInputTokens": 0, "outputTokens": 12, "reasoningOutputTokens": 4},
		}}))
		f.notify("thread/tokenUsage/updated", item("", map[string]any{"tokenUsage": map[string]any{
			"total": map[string]any{"totalTokens": 1230, "inputTokens": 1130, "cachedInputTokens": 500, "cacheWriteInputTokens": 0, "outputTokens": 110, "reasoningOutputTokens": 4},
			"last":  map[string]any{"totalTokens": 78, "inputTokens": 70, "cachedInputTokens": 50, "cacheWriteInputTokens": 0, "outputTokens": 8, "reasoningOutputTokens": 0},
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
	// Both responses of this turn, not the thread's total and not only the
	// last response; the cached input split out of the input.
	if want := (Usage{InputTokens: 30, CacheReadTokens: 100, OutputTokens: 20}); res.Usage != want {
		t.Errorf("usage = %+v, want %+v", res.Usage, want)
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
	want := []EventKind{EventSession, EventStatus, EventText, EventText, EventToolCall, EventToolResult, EventFileChanged, EventToolCall, EventToolResult, EventToolCall, EventToolResult, EventError, EventText}
	if strings.Join(kindStrings(kinds), ",") != strings.Join(kindStrings(want), ",") {
		t.Errorf("events = %v\n   want %v", kinds, want)
	}
	if byKind[EventSession][0].SessionRef != "thr-new" {
		t.Errorf("session event = %+v, want the new thread's id", byKind[EventSession])
	}
	if !strings.Contains(byKind[EventStatus][0].Text, "gpt-5-codex") {
		t.Errorf("status should name the model: %+v", byKind[EventStatus])
	}
	if calls := byKind[EventToolCall]; calls[0].Tool != "commandExecution" || calls[0].Input != "ls" || calls[1].Tool != "github/search" || calls[1].Input != `{"q":"veyloom"}` {
		t.Errorf("tool calls: %+v", calls)
	}
	if results := byKind[EventToolResult]; results[0].Text != "a.txt\n[exit code 1]" || results[1].Text != "1 hit\nJ0EY0/veyloom" || results[2].Text != "error: server gone" {
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
		{"auto_review", PermissionAutoReview, nil, "on-request", "workspace-write", "workspaceWrite"},
		{"full_auto", PermissionFullAuto, nil, "never", "danger-full-access", "dangerFullAccess"},
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

// The presets that ask say who is asked: people for edit_with_approval,
// whatever the person's own Codex does, and Codex's automatic review for
// auto_review. The others leave it be, since nothing is asked.
func TestCodex_ApprovalsReviewerPerPreset(t *testing.T) {
	for permission, want := range map[string]any{
		PermissionEditWithApproval: "user",
		PermissionAutoReview:       "auto_review",
		PermissionFullAuto:         nil,
		PermissionReadOnly:         nil,
	} {
		t.Run(permission, func(t *testing.T) {
			h := newCodexHarness(t)
			turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "hi", Permission: permission})
			if err != nil {
				t.Fatal(err)
			}
			drain(t, turn)
			sent := h.sent(t)
			start, ts := paramsOf(t, sent["thread/start"][0]), paramsOf(t, sent["turn/start"][0])
			if start["approvalsReviewer"] != want || ts["approvalsReviewer"] != want {
				t.Errorf("approvalsReviewer: thread/start %v, turn/start %v, want %v", start["approvalsReviewer"], ts["approvalsReviewer"], want)
			}
		})
	}
}

// A command starting with the words of a rule of the member's runs without
// anyone being asked, and people are told which rule let it. One that
// does more than run one command is asked about all the same.
func TestCodex_MemberRulesLetCommandsThrough(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[prefix] test", Permission: PermissionEditWithApproval, AllowedRules: []string{`["go","test"]`, "Bash(make:*)"}})
	if err != nil {
		t.Fatal(err)
	}
	var ruled []Event
	for {
		ev, _ := awaitApproval(t, turn)
		if ev.Reviewer == "" {
			if ev.Input != `{"command":"/bin/zsh -lc 'go test ./... \u0026\u0026 rm -rf /'","cwd":"/repo"}` {
				t.Errorf("asked about %s", ev.Input)
			}
			if err := turn.Answer(ev.ApprovalID, Decision{Allow: false}); err != nil {
				t.Fatal(err)
			}
			break
		}
		ruled = append(ruled, ev)
	}
	drain(t, turn)
	if res, err := turn.Result(); err != nil || res.Output != "accept,accept,decline" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if len(ruled) != 2 || ruled[0].Reviewer != ReviewerRule || ruled[0].Verdict != VerdictAllowed || string(ruled[0].Detail) != `{"prefix":["go","test"]}` ||
		ruled[1].Input != `{"command":"/bin/zsh -lc 'go test ./cmd/...'","cwd":"/repo"}` {
		t.Errorf("settled by the rule: %+v", ruled)
	}
}

// Allowing a command with the like of it takes in, for the rest of the
// turn, whatever starts with the words Codex proposed: the runner keeps
// that, so it goes no further than the turn.
func TestCodex_AllowingTheLikeOfACommandKeepsItsWords(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[prefix] test", Permission: PermissionEditWithApproval})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := awaitApproval(t, turn)
	if first.Reviewer != "" || first.Similar == nil || !first.Similar.Same || strings.Join(first.Similar.Prefix, " ") != "go test" {
		t.Fatalf("the first request: %+v (offer %+v)", first, first.Similar)
	}
	if err := turn.Answer(first.ApprovalID, Decision{Allow: true, Similar: true}); err != nil {
		t.Fatal(err)
	}
	second, _ := awaitApproval(t, turn)
	if second.Reviewer != ReviewerRule {
		t.Fatalf("the second request should be the rule's: %+v", second)
	}
	third, _ := awaitApproval(t, turn)
	if third.Reviewer != "" {
		t.Fatalf("the third request should be asked: %+v", third)
	}
	if err := turn.Answer(third.ApprovalID, Decision{Allow: false}); err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	if res, err := turn.Result(); err != nil || res.Output != "acceptForSession,accept,decline" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if sent := h.sent(t); len(sent["config/batchWrite"])+len(sent["config/value/write"]) != 0 {
		t.Error("nothing of the person's own Codex configuration should change")
	}
}

func TestCodex_ResumesTheThread(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{SystemPrompt: "role", Prompt: "again", Session: Session{Key: "key-1", Ref: "thr-old", Resume: true}, Permission: PermissionReadOnly})
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
	if resume["excludeTurns"] != true {
		t.Error("thread/resume should not ask for the thread's whole history back")
	}
	if _, has := resume["developerInstructions"]; has {
		t.Error("a resumed thread keeps its instructions; none should be sent")
	}
}

func TestCodex_CommandApproval(t *testing.T) {
	for _, tc := range []struct {
		name         string
		allow, again bool
		output       string
		result       string
	}{
		{"allowed", true, false, "ran it", "removed\n"},
		{"allowed for the session", true, true, "ran it, and will again", "removed\n"},
		{"denied", false, true, "declined", "declined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCodexHarness(t)
			turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[approve] this", Permission: PermissionEditWithApproval})
			if err != nil {
				t.Fatal(err)
			}
			req, _ := awaitApproval(t, turn)
			if req.Tool != "commandExecution" || req.Input != `{"command":"rm -rf build","cwd":"/repo","reason":"cleanup"}` || req.Similar == nil || !req.Similar.Same {
				t.Fatalf("unexpected approval request: %+v", req)
			}
			if err := turn.Answer(req.ApprovalID, Decision{Allow: tc.allow, Similar: tc.again, Message: "because"}); err != nil {
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
	events := drain(t, turn)
	res, err := turn.Result()
	if err != nil || res.Output != "asked" {
		t.Errorf("the server should get a JSON-RPC error and carry on; result = %+v, %v", res, err)
	}
	// Not in silence: people are told what was asked and turned down.
	var told bool
	for _, ev := range events {
		if ev.Kind == EventNotice && ev.Level == NoticeError && strings.Contains(ev.Text, "item/somethingNew/request") {
			told = true
		}
	}
	if !told {
		t.Errorf("a refused request should be a notice, events = %+v", events)
	}
}

func TestCodex_AutoReviewsAndNoticesAreShown(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[review] go", Permission: PermissionEditWithApproval})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	if res, err := turn.Result(); err != nil || res.Output != "reviewed" {
		t.Fatalf("result = %+v, %v", res, err)
	}
	var reviews []Event
	var notices []string
	for _, ev := range events {
		switch ev.Kind {
		case EventApprovalRequest:
			reviews = append(reviews, ev)
		case EventNotice:
			notices = append(notices, ev.Level+": "+ev.Text)
		}
	}
	if len(reviews) != 2 {
		t.Fatalf("reviews = %+v, want two", reviews)
	}
	approved, denied := reviews[0], reviews[1]
	if approved.Reviewer != "codex_auto_review" || approved.Verdict != VerdictAllowed || approved.Text != "a public HEAD request" || approved.ApprovalID == "" ||
		approved.Tool != "commandExecution" || approved.Input != `{"command":"/bin/zsh -lc 'curl -sI https://example.com'","cwd":"/repo"}` || string(approved.Detail) != `{"authorization":"high","risk":"low"}` {
		t.Errorf("approved review = %+v (detail %s)", approved, approved.Detail)
	}
	// No reasoning of its own: the warning sent during the review says why.
	if denied.Verdict != VerdictDenied || denied.Text != "writing outside the workspace" || denied.Tool != "fileChange" || denied.Input != `{"paths":["/etc/hosts"]}` || string(denied.Detail) != `{"risk":"high"}` {
		t.Errorf("denied review = %+v (detail %s)", denied, denied.Detail)
	}
	want := []string{
		"warning: outside any review",
		"info: Codex will have every further command in this turn reviewed before it runs",
		"warning: rate limits are close",
		"warning: unknown key: foo is not a setting (/home/me/.codex/config.toml)",
		"info: sandbox_permissions is going away",
		"info: Codex switched this turn from gpt-6 to gpt-6-safe (highRiskCyberActivity)",
		"warning: MCP server node_repl failed to start: handshake failed",
	}
	if strings.Join(notices, "\n") != strings.Join(want, "\n") {
		t.Errorf("notices:\n%s\nwant:\n%s", strings.Join(notices, "\n"), strings.Join(want, "\n"))
	}
}

func TestCodex_FailedTurn(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[fail] now", Permission: PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	res, err := turn.Result()
	if err == nil || !strings.Contains(err.Error(), "model exploded") {
		t.Errorf("Result error = %v, want the server's message", err)
	}
	// What the failed turn spent still counts.
	if want := (Usage{InputTokens: 200, CacheReadTokens: 100, OutputTokens: 20}); res.Usage != want {
		t.Errorf("usage = %+v, want %+v", res.Usage, want)
	}
}

func TestCodex_Cancel(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[hang] forever", Permission: PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the turn to be under way, past its first response, then
	// cancel.
	timeout := time.After(5 * time.Second)
	for started := false; !started; {
		select {
		case ev := <-turn.Events():
			started = ev.Kind == EventText
		case <-timeout:
			t.Fatal("the reply never started")
		}
	}
	start := time.Now()
	turn.Cancel()
	drain(t, turn)
	res, err := turn.Result()
	if !errors.Is(err, ErrTurnCancelled) {
		t.Errorf("Result error = %v, want ErrTurnCancelled", err)
	}
	if want := (Usage{InputTokens: 80, OutputTokens: 5}); res.Usage != want {
		t.Errorf("a cancelled turn should keep what it spent: usage = %+v, want %+v", res.Usage, want)
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

func TestCodex_RefusedResumeIsNamed(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "again", Session: Session{Key: "key-1", Ref: "thr-gone", Resume: true}, Permission: PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	events := drain(t, turn)
	res, err := turn.Result()
	if err == nil || !strings.Contains(err.Error(), "no rollout found") {
		t.Fatalf("err = %v, want the server's refusal", err)
	}
	if res.Failure != FailureSessionNotFound {
		t.Errorf("Failure = %q, want %q: the server gave its verdict on the thread", res.Failure, FailureSessionNotFound)
	}
	for _, ev := range events {
		if ev.Kind == EventSession || ev.Kind == EventText || ev.Kind == EventToolCall {
			t.Errorf("a turn that never started should report nothing, got %+v", ev)
		}
	}
}

func TestCodex_ReportsCompaction(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[compact] go on", Permission: PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	var phases []string
	for _, ev := range drain(t, turn) {
		if ev.Kind == EventCompaction {
			phases = append(phases, ev.Phase)
		}
	}
	if _, err := turn.Result(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(phases, ",") != "start,end" {
		t.Errorf("compaction phases = %v, want start then end", phases)
	}
}

func TestCodex_SubagentsWorkInTheTurnButDoNotEndOrAnswerIt(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[subagent] keep the wiki", Permission: PermissionEditWithApproval})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var calls, notices []string
	compacted := false
	for _, ev := range drain(t, turn) {
		switch ev.Kind {
		case EventText:
			text.WriteString(ev.Text)
		case EventToolCall:
			calls = append(calls, ev.Tool+" "+ev.Input)
		case EventNotice:
			notices = append(notices, ev.Text)
		case EventCompaction:
			compacted = true
		}
	}
	res, err := turn.Result()
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "Proposed the patch." || text.String() != "Looking." {
		t.Errorf("the reply is the turn's own: output %q, streamed %q", res.Output, text.String())
	}
	// The subagent's turn ending did not end this one: the patch came after.
	if want := []string{"commandExecution cat SKILL.md", `veyloom/patch_wiki {"path":"/skills/refund-window/SKILL.md"}`}; !slices.Equal(calls, want) {
		t.Errorf("calls %q, want the subagent's command and then the turn's own call %q", calls, want)
	}
	if compacted {
		t.Error("the subagent's compaction is none of the session's")
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "Codex started a subagent, audit_refunds") {
		t.Errorf("notices %q", notices)
	}
	if want := (Usage{InputTokens: 140, OutputTokens: 14}); res.Usage != want {
		t.Errorf("usage %+v, want both threads' %+v", res.Usage, want)
	}
}

func TestCodex_MCPResultText(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"text blocks", `{"content":[{"type":"text","text":"a"},{"type":"image","data":"x"},{"type":"text","text":"b"}],"_meta":null}`, "a\nb"},
		{"no text blocks", `{"content":[{"type":"image","data":"x"}]}`, `{"content":[{"data":"x","type":"image"}]}`},
		{"no content", `{"hits":1}`, `{"hits":1}`},
		{"nothing", ``, ``},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := mcpResultText(json.RawMessage(tc.raw)); got != tc.want {
				t.Errorf("mcpResultText(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestCodex_PermissionsRequest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		allow  bool
		output string
	}{
		{"allowed", true, "network for the turn"},
		{"denied", false, "nothing granted for the turn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCodexHarness(t)
			turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[permissions] please", Permission: PermissionEditWithApproval})
			if err != nil {
				t.Fatal(err)
			}
			req, _ := awaitApproval(t, turn)
			if req.Tool != "permissions" || req.Input != `{"cwd":"/repo","network":{"enabled":true},"reason":"fetch deps"}` {
				t.Fatalf("unexpected approval request: %+v", req)
			}
			if err := turn.Answer(req.ApprovalID, Decision{Allow: tc.allow}); err != nil {
				t.Fatal(err)
			}
			drain(t, turn)
			res, err := turn.Result()
			if err != nil {
				t.Fatal(err)
			}
			if res.Output != tc.output {
				t.Errorf("Output = %q, want %q", res.Output, tc.output)
			}
		})
	}
}

func TestCodex_QuestionsReachAPerson(t *testing.T) {
	for _, tc := range []struct {
		name   string
		d      Decision
		output string
	}{
		// An answer to something not asked is not passed on.
		{"answered", Decision{Allow: true, Answer: json.RawMessage(`{"answers":{"colour":["blue"],"token":["hunter2"],"stray":["x"]}}`)}, "colour=blue; token=hunter2"},
		{"declined", Decision{Allow: false, Message: "not now"}, "no answers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newCodexHarness(t)
			turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[question] please", Permission: PermissionEditWithApproval})
			if err != nil {
				t.Fatal(err)
			}
			req, _ := awaitApproval(t, turn)
			var set QuestionSet
			if err := json.Unmarshal([]byte(req.Input), &set); err != nil || req.ApprovalKind != ApprovalQuestion || req.Tool != "requestUserInput" {
				t.Fatalf("request = %+v (%v)", req, err)
			}
			want := []Question{
				{ID: "colour", Header: "Colour", Question: "Which colour?", Options: []QuestionOption{{Label: "red", Description: "warm"}, {Label: "blue", Description: "cool"}}},
				// No options: the only way to answer is to write one.
				{ID: "token", Header: "Token", Question: "Your token?", Other: true, Secret: true},
			}
			if !reflect.DeepEqual(set.Questions, want) {
				t.Errorf("questions = %+v\nwant %+v", set.Questions, want)
			}
			if err := turn.Answer(req.ApprovalID, tc.d); err != nil {
				t.Fatal(err)
			}
			drain(t, turn)
			if res, err := turn.Result(); err != nil || res.Output != tc.output {
				t.Errorf("result = %+v, %v; want %q", res, err, tc.output)
			}
		})
	}
}

// What an MCP server asks through Codex, a form or a link, reaches a person;
// what they do comes back as the MCP elicitation result.
func TestCodex_ElicitationsReachAPerson(t *testing.T) {
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "[elicit] deploy", Permission: PermissionEditWithApproval})
	if err != nil {
		t.Fatal(err)
	}
	form, _ := awaitApproval(t, turn)
	var asked FormRequest
	if err := json.Unmarshal([]byte(form.Input), &asked); err != nil || form.ApprovalKind != ApprovalForm || form.Tool != "elicitation" ||
		asked.Server != "deploy" || asked.Message != "Where to?" || !strings.Contains(string(asked.Schema), `"enum":["eu","us"]`) {
		t.Fatalf("form request = %+v (%v)", form, err)
	}
	if err := turn.Answer(form.ApprovalID, Decision{Allow: true, Answer: json.RawMessage(`{"content":{"region":"eu"}}`)}); err != nil {
		t.Fatal(err)
	}
	link, _ := awaitApproval(t, turn)
	var opened LinkRequest
	if err := json.Unmarshal([]byte(link.Input), &opened); err != nil || link.ApprovalKind != ApprovalLink || opened.URL != "https://login.example.com/device" || opened.Message != "Sign in" {
		t.Fatalf("link request = %+v (%v)", link, err)
	}
	if err := turn.Answer(link.ApprovalID, Decision{Allow: false, Message: "not now"}); err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	res, err := turn.Result()
	want := `form={"_meta":null,"action":"accept","content":{"region":"eu"}}; link={"_meta":null,"action":"decline","content":null}`
	if err != nil || res.Output != want {
		t.Errorf("result = %q, %v\nwant %q", res.Output, err, want)
	}
}

func TestCodex_SkillsComeAsAnExtraRoot(t *testing.T) {
	h := newCodexHarness(t)
	dir := t.TempDir()
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "go", SkillDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	sent := h.sent(t)
	if roots := sent["skills/extraRoots/set"]; len(roots) != 1 || paramsOf(t, roots[0])["extraRoots"].([]any)[0] != filepath.Join(dir, "skills") {
		t.Errorf("skills/extraRoots/set = %+v", roots)
	}
	// Without skills Codex is not asked.
	h2 := newCodexHarness(t)
	turn, _ = h2.runner.StartTurn(context.Background(), TurnSpec{Prompt: "go"})
	drain(t, turn)
	if roots := h2.sent(t)["skills/extraRoots/set"]; len(roots) != 0 {
		t.Errorf("no skills, no roots: %+v", roots)
	}
}
