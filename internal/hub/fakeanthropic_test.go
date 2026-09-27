package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAnthropic is a scripted stand-in for the Anthropic Messages API, so
// the real Claude Code CLI can be driven through the hub without an
// account: point ANTHROPIC_BASE_URL at it. The newest user message decides
// the reply. A line holding `USE_TOOL <name> <json input>` makes the model
// call that tool with that input (the last such line counts); tool results
// come back as `RESULT <json>`; anything else gets "ok". `WAIT_MS <n>` in
// the newest message holds the answer back n milliseconds, as a model
// thinking would; `API_ERROR <status> <type> <message>` answers with that
// HTTP status and Anthropic error instead. It keeps how many messages each request carried, which
// shows whether a turn resumed the conversation before it.
type fakeAnthropic struct {
	*httptest.Server
	mu       sync.Mutex
	tools    int
	replies  int
	messages []int
	// held counts the answers held back by WAIT_MS so far.
	held int
	// shapes are the last requests, one line each, told when a test fails.
	shapes []string
}

func newFakeAnthropic(t *testing.T) *fakeAnthropic {
	t.Helper()
	f := &fakeAnthropic{}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(func() {
		f.Close()
		if t.Failed() {
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, shape := range f.shapes {
				t.Logf("fake API request: %s", shape)
			}
		}
	})
	return f
}

// shapeOf is a request's messages in one line: each role with its blocks,
// text abridged.
func shapeOf(messages []fakeAnthropicMessage) string {
	parts := make([]string, 0, len(messages))
	for _, m := range messages {
		var blocks []fakeAnthropicBlock
		kinds := []string{}
		if json.Unmarshal(m.Content, &blocks) != nil {
			var text string
			json.Unmarshal(m.Content, &text)
			blocks = []fakeAnthropicBlock{{Type: "text", Text: text}}
		}
		for _, b := range blocks {
			kind := b.Type
			if b.Type == "text" {
				kind += fmt.Sprintf("(%.40q…%.60q)", b.Text, b.Text[max(0, len(b.Text)-60):])
			}
			if b.Type == "tool_result" {
				kind += fmt.Sprintf("(%.60q)", fakeResultText(b.Content))
			}
			kinds = append(kinds, kind)
		}
		parts = append(parts, m.Role+": "+strings.Join(kinds, " + "))
	}
	return strings.Join(parts, " | ")
}

// longest is the most messages any request carried since the last call.
func (f *fakeAnthropic) longest() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	most := 0
	for _, n := range f.messages {
		most = max(most, n)
	}
	f.messages = nil
	return most
}

type fakeAnthropicMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type fakeAnthropicBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Content json.RawMessage `json:"content"`
	IsError bool            `json:"is_error"`
}

// fakeToolResult is one tool result as the fake repeats it.
type fakeToolResult struct {
	IsError bool   `json:"is_error"`
	Content string `json:"content"`
}

func (f *fakeAnthropic) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasPrefix(r.URL.Path, "/v1/messages") {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Model    string                 `json:"model"`
		Stream   bool                   `json:"stream"`
		Messages []fakeAnthropicMessage `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/count_tokens") {
		json.NewEncoder(w).Encode(map[string]int{"input_tokens": 10})
		return
	}
	f.mu.Lock()
	// Every reply is a message of its own: the CLI merges the blocks of
	// messages that share an id.
	f.replies++
	id := fmt.Sprintf("msg_%03d", f.replies)
	f.messages = append(f.messages, len(body.Messages))
	f.shapes = append(f.shapes, shapeOf(body.Messages))
	if len(f.shapes) > 12 {
		f.shapes = f.shapes[1:]
	}
	f.mu.Unlock()

	if status, kind, message, ok := apiErrorIn(body.Messages); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]any{"type": kind, "message": message}})
		return
	}
	blocks, stop := f.reply(body.Messages)
	if wait := waitIn(body.Messages); wait > 0 {
		f.mu.Lock()
		f.held++
		f.mu.Unlock()
		time.Sleep(wait)
	}
	msg := map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": body.Model, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 10, "output_tokens": 1},
	}
	if !body.Stream {
		msg["content"], msg["stop_reason"] = blocks, stop
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msg)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	event := func(name string, data any) {
		payload, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, payload)
		if flusher != nil {
			flusher.Flush()
		}
	}
	event("message_start", map[string]any{"type": "message_start", "message": msg})
	for i, b := range blocks {
		switch b["type"] {
		case "text":
			event("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "text", "text": ""}})
			event("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "text_delta", "text": b["text"]}})
		case "tool_use":
			input, _ := json.Marshal(b["input"])
			event("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "tool_use", "id": b["id"], "name": b["name"], "input": map[string]any{}}})
			event("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(input)}})
		}
		event("content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop, "stop_sequence": nil}, "usage": map[string]int{"output_tokens": 5}})
	event("message_stop", map[string]any{"type": "message_stop"})
}

// reply decides the model's answer to messages, by the newest message: a
// directive written after any tool results in it calls a tool; otherwise
// tool results are repeated back. A resumed CLI can put the last turn's
// tool results and the new prompt in one message, results first.
func (f *fakeAnthropic) reply(messages []fakeAnthropicMessage) ([]map[string]any, string) {
	if len(messages) == 0 {
		return []map[string]any{{"type": "text", "text": "ok"}}, "end_turn"
	}
	last := messages[len(messages)-1]
	var blocks []fakeAnthropicBlock
	var text string
	if json.Unmarshal(last.Content, &text) == nil {
		blocks = []fakeAnthropicBlock{{Type: "text", Text: text}}
	} else {
		json.Unmarshal(last.Content, &blocks)
	}
	var results []fakeToolResult
	var directive string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if d := lastDirective(b.Text); d != "" {
				directive, results = d, nil
			}
		case "tool_result":
			results = append(results, fakeToolResult{IsError: b.IsError, Content: fakeResultText(b.Content)})
			directive = ""
		}
	}
	if directive != "" {
		name, raw, _ := strings.Cut(directive, " ")
		var input map[string]any
		if json.Unmarshal([]byte(raw), &input) == nil {
			f.mu.Lock()
			f.tools++
			id := fmt.Sprintf("toolu_%03d", f.tools)
			f.mu.Unlock()
			return []map[string]any{{"type": "tool_use", "id": id, "name": name, "input": input}}, "tool_use"
		}
	}
	if len(results) > 0 {
		data, _ := json.Marshal(results)
		return []map[string]any{{"type": "text", "text": "RESULT " + string(data)}}, "end_turn"
	}
	return []map[string]any{{"type": "text", "text": "ok"}}, "end_turn"
}

// waitIn is how long the newest message's WAIT_MS asks the answer to be
// held back; zero without one.
func waitIn(messages []fakeAnthropicMessage) time.Duration {
	if len(messages) == 0 {
		return 0
	}
	var text string
	if json.Unmarshal(messages[len(messages)-1].Content, &text) != nil {
		var blocks []fakeAnthropicBlock
		json.Unmarshal(messages[len(messages)-1].Content, &blocks)
		for _, b := range blocks {
			if b.Type == "text" {
				text += b.Text
			}
		}
	}
	at := strings.LastIndex(text, "WAIT_MS ")
	if at < 0 {
		return 0
	}
	var ms int
	fmt.Sscanf(text[at+len("WAIT_MS "):], "%d", &ms)
	return time.Duration(ms) * time.Millisecond
}

// apiErrorIn is the error the newest message's API_ERROR asks for.
func apiErrorIn(messages []fakeAnthropicMessage) (status int, kind, message string, ok bool) {
	if len(messages) == 0 {
		return 0, "", "", false
	}
	var text string
	if json.Unmarshal(messages[len(messages)-1].Content, &text) != nil {
		var blocks []fakeAnthropicBlock
		json.Unmarshal(messages[len(messages)-1].Content, &blocks)
		for _, b := range blocks {
			if b.Type == "text" {
				text += b.Text
			}
		}
	}
	at := strings.LastIndex(text, "API_ERROR ")
	if at < 0 {
		return 0, "", "", false
	}
	line, _, _ := strings.Cut(text[at+len("API_ERROR "):], "\n")
	fields := strings.SplitN(strings.TrimSpace(line), " ", 3)
	if len(fields) < 3 {
		return 0, "", "", false
	}
	fmt.Sscanf(fields[0], "%d", &status)
	return status, fields[1], fields[2], status > 0
}

// heldBack is how many answers WAIT_MS has held back so far.
func (f *fakeAnthropic) heldBack() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.held
}

// asked reports whether the newest message of a request so far ended
// with part, as shapeOf shows it.
func (f *fakeAnthropic) asked(part string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, shape := range f.shapes {
		if at := strings.LastIndex(shape, " | user: "); at >= 0 && strings.Contains(shape[at:], part) {
			return true
		}
	}
	return false
}

// lastDirective is what follows the last "USE_TOOL " in text, up to the end
// of its line: a tool's name and its input.
func lastDirective(text string) string {
	at := strings.LastIndex(text, "USE_TOOL ")
	if at < 0 {
		return ""
	}
	line, _, _ := strings.Cut(text[at+len("USE_TOOL "):], "\n")
	return strings.TrimSpace(line)
}

// fakeResultText is a tool result's content as text: a string, or its
// text blocks joined.
func fakeResultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []fakeAnthropicBlock
	json.Unmarshal(raw, &blocks)
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, " ")
}
