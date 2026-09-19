package runtime

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakePiMain plays `pi --mode rpc` for the tests: this test binary
// re-executed with VEYLOOM_FAKE_PI=1 (see TestMain). It writes its
// arguments, one per line, to the file named by VEYLOOM_FAKE_PI_ARGS and
// every command it reads to the one named by VEYLOOM_FAKE_PI_RECORD. It
// answers get_state with the session id and accepts the prompt, then:
//
// with VEYLOOM_FAKE_PI_OUTPUT naming a file, replays that file as the run,
// its session header (if any) giving the id instead; writes
// VEYLOOM_FAKE_PI_STDERR to stderr and exits with VEYLOOM_FAKE_PI_EXIT;
//
// otherwise plays the extension dialogs the prompt names, in order
// (piScriptDialogs), and ends with a reply whose text is every answer it
// got, as JSON by keyword.
//
// Like pi, once the agent is done it waits for its input to close.
func fakePiMain() {
	if path := os.Getenv("VEYLOOM_FAKE_PI_ARGS"); path != "" {
		os.WriteFile(path+".tmp", []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0o600)
		os.Rename(path+".tmp", path)
	}
	var record io.Writer = io.Discard
	if path := os.Getenv("VEYLOOM_FAKE_PI_RECORD"); path != "" {
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

	s := &piScript{in: lines, answers: map[string]any{}}
	session, output := "pi-sess-1", ""
	if path := os.Getenv("VEYLOOM_FAKE_PI_OUTPUT"); path != "" {
		data, _ := os.ReadFile(path)
		output = string(data)
		if first, rest, _ := strings.Cut(output, "\n"); strings.Contains(first, `"type":"session"`) {
			var header struct {
				ID string `json:"id"`
			}
			json.Unmarshal([]byte(first), &header)
			session, output = header.ID, rest
		}
	}
	// The two commands the runner sends before anything happens.
	var prompt string
	for got := 0; got < 2; {
		raw, ok := <-lines
		if !ok {
			os.Exit(2)
		}
		var cmd struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Message string `json:"message"`
		}
		json.Unmarshal(raw, &cmd)
		switch cmd.Type {
		case "get_state":
			s.send(map[string]any{"id": cmd.ID, "type": "response", "command": "get_state", "success": true, "data": map[string]any{"sessionId": session, "isStreaming": false}})
			got++
		case "prompt":
			prompt = cmd.Message
			if strings.Contains(prompt, "[reject]") {
				s.send(map[string]any{"id": cmd.ID, "type": "response", "command": "prompt", "success": false, "error": "Agent is busy"})
				s.drain()
				os.Exit(0)
			}
			s.send(map[string]any{"id": cmd.ID, "type": "response", "command": "prompt", "success": true})
			got++
		}
	}

	if os.Getenv("VEYLOOM_FAKE_PI_OUTPUT") != "" {
		os.Stdout.WriteString(output)
		if msg := os.Getenv("VEYLOOM_FAKE_PI_STDERR"); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
		if strings.Contains(output, `"type":"agent_end"`) {
			s.drain()
		}
		code, _ := strconv.Atoi(os.Getenv("VEYLOOM_FAKE_PI_EXIT"))
		os.Exit(code)
	}
	s.play(prompt)
}

// piScriptDialogs are the extension requests the fake can make, by the
// keyword that asks for them. [timeout] is a select pi settles itself
// after 300 ms; [notify] and [status] want no answer; [retry] and
// [exterr] are events of their own.
var piScriptDialogs = map[string]string{
	"[confirm]": `{"method":"confirm","title":"Dangerous command","message":"Allow rm -rf build?"}`,
	"[select]":  `{"method":"select","title":"Which environment?","options":["staging","production"]}`,
	"[input]":   `{"method":"input","title":"Release name?","placeholder":"v1.2.3"}`,
	"[editor]":  `{"method":"editor","title":"Edit the changelog","prefill":"- fixed things\n"}`,
	"[timeout]": `{"method":"select","title":"Quick, pick one","options":["a","b"],"timeout":300}`,
	"[notify]":  `{"method":"notify","message":"Command blocked by the permission gate","notifyType":"warning"}`,
	"[status]":  `{"method":"setStatus","statusKey":"gate","statusText":"watching"}`,
}

type piScript struct {
	in      chan []byte
	answers map[string]any
	n       int
}

func (s *piScript) send(v any) {
	data, _ := json.Marshal(v)
	os.Stdout.Write(append(data, '\n'))
}

// ask sends the dialog for keyword and waits up to wait for its answer,
// recording the answer (or "no answer") by keyword.
func (s *piScript) ask(keyword string, wait time.Duration) {
	s.n++
	id := fmt.Sprintf("ui-%d", s.n)
	var req map[string]any
	json.Unmarshal([]byte(piScriptDialogs[keyword]), &req)
	req["type"], req["id"] = "extension_ui_request", id
	s.send(req)
	if req["method"] == "notify" || req["method"] == "setStatus" {
		return
	}
	deadline := time.After(wait)
	for {
		select {
		case raw, ok := <-s.in:
			if !ok {
				return
			}
			var resp map[string]any
			if json.Unmarshal(raw, &resp) != nil || resp["type"] != "extension_ui_response" {
				continue
			}
			if resp["id"] != id {
				s.answers["unexpected "+fmt.Sprint(resp["id"])] = resp
				continue
			}
			delete(resp, "type")
			delete(resp, "id")
			s.answers[keyword] = resp
			return
		case <-deadline:
			s.answers[keyword] = "no answer"
			return
		}
	}
}

func (s *piScript) play(prompt string) {
	s.send(map[string]any{"type": "agent_start"})
	for _, keyword := range strings.Fields(prompt) {
		switch keyword {
		case "[retry]":
			s.send(map[string]any{"type": "auto_retry_start", "attempt": 1, "maxAttempts": 3, "delayMs": 2000, "errorMessage": "529 overloaded"})
			s.send(map[string]any{"type": "auto_retry_end", "success": false, "attempt": 3, "finalError": "529 overloaded_error: Overloaded"})
		case "[exterr]":
			s.send(map[string]any{"type": "extension_error", "extensionPath": "/home/me/.pi/agent/extensions/gate.ts", "event": "tool_call", "error": "boom"})
		case "[timeout]":
			s.ask(keyword, 300*time.Millisecond)
			// A late answer would be one nobody asked for.
			s.ask("[status]", 0)
			time.Sleep(300 * time.Millisecond)
		default:
			if _, ok := piScriptDialogs[keyword]; ok {
				s.ask(keyword, 10*time.Second)
			}
		}
	}
	// Anything that came in after its dialog was over.
	for {
		select {
		case raw, ok := <-s.in:
			if ok {
				var resp map[string]any
				if json.Unmarshal(raw, &resp) == nil && resp["type"] == "extension_ui_response" {
					s.answers["unexpected "+fmt.Sprint(resp["id"])] = resp
				}
				continue
			}
		default:
		}
		break
	}
	text, _ := json.Marshal(s.answers)
	s.send(map[string]any{"type": "message_end", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": string(text)}}, "stopReason": "stop"}})
	s.send(map[string]any{"type": "agent_end", "messages": []any{}})
	s.drain()
}

// drain waits for the input to close.
func (s *piScript) drain() {
	for range s.in {
	}
}

// fakePiCLI puts the fake on PATH as `pi`, replaying output as the run and
// exiting with exitCode after writing stderr. It returns where the fake
// records its arguments.
func fakePiCLI(t *testing.T, output string, exitCode int, stderr string) (argsPath string) {
	t.Helper()
	outputPath := filepath.Join(t.TempDir(), "output.jsonl")
	if err := os.WriteFile(outputPath, []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	argsPath, _ = scriptedPiCLI(t)
	t.Setenv("VEYLOOM_FAKE_PI_OUTPUT", outputPath)
	t.Setenv("VEYLOOM_FAKE_PI_EXIT", strconv.Itoa(exitCode))
	t.Setenv("VEYLOOM_FAKE_PI_STDERR", stderr)
	return argsPath
}

// scriptedPiCLI puts the fake on PATH as `pi`, playing the dialogs its
// prompt names. It returns where the fake records its arguments and the
// commands it reads.
func scriptedPiCLI(t *testing.T) (argsPath, stdinPath string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	argsPath, stdinPath = filepath.Join(dir, "args"), filepath.Join(dir, "stdin")
	t.Setenv("VEYLOOM_FAKE_PI", "1")
	t.Setenv("VEYLOOM_FAKE_PI_ARGS", argsPath)
	t.Setenv("VEYLOOM_FAKE_PI_RECORD", stdinPath)
	t.Setenv("VEYLOOM_FAKE_PI_OUTPUT", "")
	fakeBinary(t, "pi", "exec "+exe+" \"$@\"\n")
	return argsPath, stdinPath
}
