package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeClaudeMain plays Claude Code in print mode with stream-json both
// ways: this test binary re-executed with VEYLOOM_FAKE_CLAUDE=1 (see
// TestMain). It writes its arguments, one per line, to the file named by
// VEYLOOM_FAKE_CLAUDE_ARGS and every line it reads to the one named by
// VEYLOOM_FAKE_CLAUDE_RECORD.
//
// With VEYLOOM_FAKE_CLAUDE_OUTPUT naming a file it replays that file as its
// output once the prompt is in, writes VEYLOOM_FAKE_CLAUDE_STDERR to stderr
// and exits with VEYLOOM_FAKE_CLAUDE_EXIT. Otherwise it plays the exchanges
// the prompt names, in order (fakeClaudeExchanges), and ends with a result
// whose text is every answer it got, as JSON by exchange.
//
// Like the real CLI, once it has written a result it waits for its input
// to close before it exits.
func fakeClaudeMain() {
	if path := os.Getenv("VEYLOOM_FAKE_CLAUDE_ARGS"); path != "" {
		os.WriteFile(path+".tmp", []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o600)
		os.Rename(path+".tmp", path)
	}
	var record io.Writer = io.Discard
	if path := os.Getenv("VEYLOOM_FAKE_CLAUDE_RECORD"); path != "" {
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			defer f.Close()
			record = f
		}
	}
	lines := make(chan []byte)
	go func() {
		in := bufio.NewReader(os.Stdin)
		for {
			line, err := in.ReadBytes('\n')
			if len(line) > 0 {
				record.Write(line)
				lines <- line
			}
			if err != nil {
				close(lines)
				return
			}
		}
	}()
	var first struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}
	if raw, ok := <-lines; !ok || json.Unmarshal(raw, &first) != nil || first.Type != "user" || first.Message.Role != "user" {
		fmt.Fprintln(os.Stderr, "fake claude: the first line is not a user message")
		os.Exit(2)
	}

	f := &claudeScript{in: lines, answers: map[string]json.RawMessage{}, replay: slices.Contains(os.Args[1:], "--replay-user-messages")}
	if path := os.Getenv("VEYLOOM_FAKE_CLAUDE_OUTPUT"); path != "" {
		data, _ := os.ReadFile(path)
		os.Stdout.Write(data)
		if s := os.Getenv("VEYLOOM_FAKE_CLAUDE_STDERR"); s != "" {
			fmt.Fprintln(os.Stderr, s)
		}
		if strings.Contains(string(data), `"type":"result"`) {
			f.drain()
		}
		code, _ := strconv.Atoi(os.Getenv("VEYLOOM_FAKE_CLAUDE_EXIT"))
		os.Exit(code)
	}
	f.play(first.Message.Content, flagOf(os.Args[1:], "--permission-mode"))
}

// fakeClaudeExchanges are the requests the fake can make, by the keyword
// that asks for them in the prompt: a command, a question, a plan, an MCP
// server's form and link, a write to the wiki, and a request of a kind
// the runner does not answer. [withdraw] asks about rm -rf build and takes the request back;
// [both] asks [bash] and [rm] at once; [wait] holds the turn open until the
// file named by VEYLOOM_FAKE_CLAUDE_GO appears. [steer] runs a tool until
// a user message comes in, which it takes in with the tool's result, and
// [deaf] makes the fake lose user messages that come once its answer is
// in; others it answers as turns of their own, as the CLI does, unless
// [crash] has it die in the midst of the first such turn.
var fakeClaudeExchanges = map[string]string{
	"[bash]":   `{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"make test","description":"run the tests"},"permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"make test"}],"behavior":"allow","destination":"localSettings"}],"decision_reason":"This command requires approval","tool_use_id":"tu-bash"}`,
	"[rm]":     `{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"rm -rf build"},"tool_use_id":"tu-rm"}`,
	"[ask]":    `{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"Which colour?","header":"Colour","options":[{"label":"red","description":"warm"},{"label":"blue","description":"cool"}],"multiSelect":false}]},"tool_use_id":"tu-ask","requires_user_interaction":true}`,
	"[plan]":   `{"subtype":"can_use_tool","tool_name":"ExitPlanMode","input":{"plan":"# Tidy up\n1. Remove dead code"},"tool_use_id":"tu-plan","requires_user_interaction":true}`,
	"[form]":   `{"subtype":"elicitation","mcp_server_name":"deploy","message":"Where to?","mode":"form","requested_schema":{"type":"object","properties":{"region":{"type":"string","enum":["eu","us"]},"count":{"type":"integer"}},"required":["region"]}}`,
	"[link]":   `{"subtype":"elicitation","mcp_server_name":"deploy","message":"Sign in","mode":"url","url":"https://example.com/device","elicitation_id":"e1"}`,
	"[hookcb]": `{"subtype":"hook_callback","callback_id":"hook_0","input":{}}`,
	"[wiki]":   `{"subtype":"can_use_tool","tool_name":"mcp__veyloom__write_wiki","input":{"type":"Fact","slug":"go-version","title":"Go","description":"d","body":"b"},"tool_use_id":"tu-wiki"}`,
}

type claudeScript struct {
	in      chan []byte
	answers map[string]json.RawMessage
	denials []map[string]any
	n       int
	// replay echoes what is taken in from the input, as
	// --replay-user-messages asks.
	replay bool
	// said are user messages read while waiting for something else, to be
	// taken in at the next chance.
	said []string
	// deaf loses user messages that come once the answer is in; crash dies
	// answering the first.
	deaf, crash bool
}

func (f *claudeScript) send(v any) {
	data, _ := json.Marshal(v)
	os.Stdout.Write(append(data, '\n'))
}

// ask sends the request for keyword and returns its request id.
func (f *claudeScript) ask(keyword string) string {
	f.n++
	id := fmt.Sprintf("req-%d", f.n)
	f.send(map[string]any{"type": "control_request", "request_id": id, "request": json.RawMessage(fakeClaudeExchanges[keyword])})
	return id
}

// await reads until the answers to ids are in, or the wait runs out. It
// records each answer by keyword: the response, or {"error": ...}.
func (f *claudeScript) await(wait time.Duration, keywords map[string]string) {
	deadline := time.After(wait)
	for len(keywords) > 0 {
		select {
		case raw, ok := <-f.in:
			if !ok {
				return
			}
			var msg struct {
				Type     string `json:"type"`
				Response struct {
					Subtype   string          `json:"subtype"`
					RequestID string          `json:"request_id"`
					Response  json.RawMessage `json:"response"`
					Error     string          `json:"error"`
				} `json:"response"`
			}
			if json.Unmarshal(raw, &msg) != nil || msg.Type != "control_response" {
				if text, ok := userMessage(raw); ok {
					f.said = append(f.said, text)
				}
				continue
			}
			keyword, ok := keywords[msg.Response.RequestID]
			if !ok {
				f.answers["unexpected "+msg.Response.RequestID] = raw
				continue
			}
			delete(keywords, msg.Response.RequestID)
			answer := msg.Response.Response
			if msg.Response.Subtype == "error" {
				answer, _ = json.Marshal(map[string]string{"error": msg.Response.Error})
			}
			f.answers[keyword] = answer
			f.noteDenial(keyword, answer)
		case <-deadline:
			return
		}
	}
}

// noteDenial lists a tool use that was answered with a denial, as the
// real CLI lists them on its result.
func (f *claudeScript) noteDenial(keyword string, answer json.RawMessage) {
	var req struct {
		ToolName  string          `json:"tool_name"`
		ToolUseID string          `json:"tool_use_id"`
		Input     json.RawMessage `json:"input"`
	}
	var resp struct {
		Behavior string `json:"behavior"`
	}
	json.Unmarshal([]byte(fakeClaudeExchanges[keyword]), &req)
	if json.Unmarshal(answer, &resp) == nil && resp.Behavior == "deny" {
		f.denials = append(f.denials, map[string]any{"tool_name": req.ToolName, "tool_use_id": req.ToolUseID, "tool_input": req.Input})
	}
}

func (f *claudeScript) play(prompt, mode string) {
	f.send(map[string]any{"type": "system", "subtype": "init", "session_id": "sess-1", "model": "claude-test", "permissionMode": mode, "mcp_servers": []any{}})
	f.takeIn(prompt)
	for _, keyword := range strings.Fields(prompt) {
		switch keyword {
		case "[steer]":
			f.send(map[string]any{"type": "assistant", "message": map[string]any{"id": "m-steer", "role": "assistant",
				"content": []any{map[string]any{"type": "tool_use", "id": "tu-steer", "name": "Bash", "input": map[string]any{"command": "sleep 1"}}}}})
			// The CLI echoes it right after the tool's result, which it
			// is handed over with.
			text, _ := f.heard(10 * time.Second)
			f.send(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "tu-steer",
				"content": "slept\n\n<system-reminder>\nThe user sent a new message while you were working:\n" + text + "\n</system-reminder>"}}}})
			f.takeIn(text)
			f.answers["[steer]"], _ = json.Marshal(text)
		case "[deaf]":
			f.deaf = true
		case "[crash]":
			f.crash = true
		case "[both]":
			f.await(10*time.Second, map[string]string{f.ask("[bash]"): "[bash]", f.ask("[rm]"): "[rm]"})
		case "[wait]":
			// Holds the turn open until the test says go.
			for i := 0; i < 400; i++ {
				if _, err := os.Stat(os.Getenv("VEYLOOM_FAKE_CLAUDE_GO")); err == nil {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		case "[withdraw]":
			f.n++
			id := fmt.Sprintf("req-%d", f.n)
			f.send(map[string]any{"type": "control_request", "request_id": id, "request": json.RawMessage(fakeClaudeExchanges["[rm]"])})
			time.Sleep(300 * time.Millisecond)
			f.send(map[string]any{"type": "control_cancel_request", "request_id": id})
			// An answer now would be one nobody asked for.
			f.await(300*time.Millisecond, map[string]string{id: "[withdraw]"})
		default:
			if _, ok := fakeClaudeExchanges[keyword]; ok {
				f.await(10*time.Second, map[string]string{f.ask(keyword): keyword})
			}
		}
	}
	text, _ := json.Marshal(f.answers)
	f.send(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": string(text), "session_id": "sess-1",
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 1}, "permission_denials": append([]map[string]any{}, f.denials...)})
	f.idle()
}

// idle answers each user message that comes once the answer is in as a
// turn of its own, as the CLI does, until the input closes: announced
// like the first, and echoed only once the answer is under way (2.1.85,
// streaming partial messages).
func (f *claudeScript) idle() {
	for {
		text, ok := f.heard(time.Hour)
		if !ok {
			return
		}
		if f.deaf {
			continue
		}
		out, _ := json.Marshal(map[string]string{"[extra]": text})
		f.send(map[string]any{"type": "system", "subtype": "init", "session_id": "sess-1", "model": "claude-test", "mcp_servers": []any{}})
		f.send(map[string]any{"type": "assistant", "message": map[string]any{"id": "m-extra", "role": "assistant", "content": []any{map[string]any{"type": "text", "text": "extra"}}}})
		f.takeIn(text)
		if f.crash {
			os.Stderr.WriteString("FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory\n")
			os.Exit(134)
		}
		f.send(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": string(out), "session_id": "sess-1",
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1}})
	}
}

// heard is the next user message: one read before, or the next to come
// within wait. False when none comes, or the input closed.
func (f *claudeScript) heard(wait time.Duration) (string, bool) {
	if len(f.said) > 0 {
		text := f.said[0]
		f.said = f.said[1:]
		return text, true
	}
	deadline := time.After(wait)
	for {
		select {
		case raw, ok := <-f.in:
			if !ok {
				return "", false
			}
			if text, ok := userMessage(raw); ok {
				return text, true
			}
		case <-deadline:
			return "", false
		}
	}
}

// takeIn echoes text as taken in, when asked to.
func (f *claudeScript) takeIn(text string) {
	if f.replay {
		f.send(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}, "session_id": "sess-1", "parent_tool_use_id": nil, "isReplay": true})
	}
}

// userMessage is the text of a user message read from the input.
func userMessage(raw []byte) (string, bool) {
	var msg struct {
		Type    string `json:"type"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(raw, &msg) != nil || msg.Type != "user" {
		return "", false
	}
	return msg.Message.Content, true
}

// drain waits for the input to close.
func (f *claudeScript) drain() {
	for range f.in {
	}
}

func flagOf(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// fakeClaudeCLI puts the fake on PATH as `claude`, replaying output and
// exiting with exitCode after writing stderr. It returns where the fake
// records its arguments and what it reads.
func fakeClaudeCLI(t *testing.T, output string, exitCode int, stderr string) (argsPath, stdinPath string) {
	t.Helper()
	outputPath := filepath.Join(t.TempDir(), "output.jsonl")
	if err := os.WriteFile(outputPath, []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	argsPath, stdinPath = scriptedClaudeCLI(t)
	t.Setenv("VEYLOOM_FAKE_CLAUDE_OUTPUT", outputPath)
	t.Setenv("VEYLOOM_FAKE_CLAUDE_EXIT", strconv.Itoa(exitCode))
	t.Setenv("VEYLOOM_FAKE_CLAUDE_STDERR", stderr)
	return argsPath, stdinPath
}

// scriptedClaudeCLI puts the fake on PATH as `claude`, playing the
// exchanges its prompt names.
func scriptedClaudeCLI(t *testing.T) (argsPath, stdinPath string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	argsPath, stdinPath = filepath.Join(dir, "args"), filepath.Join(dir, "stdin")
	t.Setenv("VEYLOOM_FAKE_CLAUDE", "1")
	t.Setenv("VEYLOOM_FAKE_CLAUDE_ARGS", argsPath)
	t.Setenv("VEYLOOM_FAKE_CLAUDE_RECORD", stdinPath)
	t.Setenv("VEYLOOM_FAKE_CLAUDE_OUTPUT", "")
	fakeBinary(t, "claude", "exec "+exe+" \"$@\"\n")
	return argsPath, stdinPath
}

// fakeClaudeAnswers is what the scripted fake was answered, by exchange,
// read from a turn's output.
func fakeClaudeAnswers(t *testing.T, output string) map[string]json.RawMessage {
	t.Helper()
	answers := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(output), &answers); err != nil {
		t.Fatalf("output %q is not the fake's answers: %v", output, err)
	}
	return answers
}
