package runtime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// piEvent is the union of the JSON records the runner reads from pi in RPC
// mode, other than its extensions' requests (pi_ui.go): the agent's events,
// which are those of pi's JSON mode, and the responses to commands.
type piEvent struct {
	Type string `json:"type"`
	// session header, which older pis printed first
	ID string `json:"id"`
	// responses to commands: which, whether it went through, and the
	// state get_state reports.
	Command string `json:"command"`
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Data    *struct {
		SessionID string `json:"sessionId"`
	} `json:"data"`
	// message events
	Message               *piMessage `json:"message"`
	AssistantMessageEvent *struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	} `json:"assistantMessageEvent"`
	// tool events
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Args       json.RawMessage `json:"args"`
	Result     json.RawMessage `json:"result"`
	IsError    bool            `json:"isError"`
	// compaction_end, auto_retry_start
	Aborted      bool   `json:"aborted"`
	ErrorMessage string `json:"errorMessage"`
	// auto_retry_start and auto_retry_end (which reuses Success)
	Attempt     int     `json:"attempt"`
	MaxAttempts int     `json:"maxAttempts"`
	DelayMs     float64 `json:"delayMs"`
	FinalError  string  `json:"finalError"`
	// extension_error (which reuses Error)
	ExtensionPath string `json:"extensionPath"`
	Event         string `json:"event"`
}

type piMessage struct {
	Role         string    `json:"role"`
	Content      []piBlock `json:"content"`
	StopReason   string    `json:"stopReason"`
	ErrorMessage string    `json:"errorMessage"`
	Usage        *piUsage  `json:"usage"`
}

type piBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// piUsage is one assistant message's tokens. Pi reports cached input apart
// from fresh input, as Anthropic does.
type piUsage struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// piParser turns pi's event stream into events and collects the result:
// the session id, the last assistant message as the reply, usage summed
// over every assistant message, and the tail of any plain-text lines pi
// printed instead of JSON.
type piParser struct {
	cfg       PiConfig
	emit      func(Event)
	sessionID string
	streamed  strings.Builder
	last      *piMessage
	usage     piUsage
	notes     []string
	ended     bool
	// rejected is why pi turned the prompt down, if it did.
	rejected string
	// edits are the files edit tools are about to write, by tool call id:
	// written once the tool reports it went through.
	edits map[string]string
}

func newPiParser(cfg PiConfig, emit func(Event)) *piParser {
	return &piParser{cfg: cfg, emit: emit, edits: make(map[string]string)}
}

// session reports the session's id, once.
func (p *piParser) session(id string) {
	if id == "" || id == p.sessionID {
		return
	}
	p.sessionID = id
	p.emit(Event{Kind: EventSession, SessionRef: id})
	p.emit(Event{Kind: EventStatus, Text: "session " + id})
}

// handle takes one record other than an extension's request.
func (p *piParser) handle(ev piEvent) {
	switch ev.Type {
	case "session":
		p.session(ev.ID)
	case "response":
		switch {
		case ev.Command == "get_state" && ev.Success && ev.Data != nil:
			p.session(ev.Data.SessionID)
		case ev.Command == "prompt" && !ev.Success:
			p.rejected = orDefault(ev.Error, "the prompt was not accepted")
		}
	case "auto_retry_start":
		delay := time.Duration(ev.DelayMs * float64(time.Millisecond)).Round(100 * time.Millisecond)
		p.emit(Event{Kind: EventNotice, Level: NoticeWarning, Text: fmt.Sprintf("Pi: %s; retrying in %s, attempt %d of %d",
			truncate(ev.ErrorMessage, p.cfg.MaxEventBytes), delay, ev.Attempt, ev.MaxAttempts)})
	case "auto_retry_end":
		if !ev.Success {
			p.emit(Event{Kind: EventNotice, Level: NoticeError, Text: fmt.Sprintf("Pi gave up after %d attempts: %s", ev.Attempt, truncate(ev.FinalError, p.cfg.MaxEventBytes))})
		}
	case "extension_error":
		p.emit(Event{Kind: EventNotice, Level: NoticeError, Text: fmt.Sprintf("Pi extension %s failed in %s: %s",
			filepath.Base(ev.ExtensionPath), orDefault(ev.Event, "an event"), truncate(ev.Error, p.cfg.MaxEventBytes))})
	case "compaction_start":
		p.emit(Event{Kind: EventCompaction, Phase: CompactionStart})
	case "compaction_end":
		// A result means the summary is in place; without one the session
		// is as it was (pi may try again, which it then announces anew).
		if ev.Aborted || ev.ErrorMessage != "" || len(ev.Result) == 0 || string(ev.Result) == "null" {
			p.emit(Event{Kind: EventCompaction, Phase: CompactionFailed, Text: ev.ErrorMessage})
		} else {
			p.emit(Event{Kind: EventCompaction, Phase: CompactionEnd})
		}
	case "message_update":
		if e := ev.AssistantMessageEvent; e != nil && e.Type == "text_delta" && e.Delta != "" {
			p.streamed.WriteString(e.Delta)
			p.emit(Event{Kind: EventText, Text: e.Delta})
		}
	case "message_end":
		if ev.Message != nil && ev.Message.Role == "assistant" {
			msg := *ev.Message
			p.last = &msg
			if msg.Usage != nil {
				p.usage.Input += msg.Usage.Input
				p.usage.Output += msg.Usage.Output
				p.usage.CacheRead += msg.Usage.CacheRead
				p.usage.CacheWrite += msg.Usage.CacheWrite
			}
		}
	case "tool_execution_start":
		p.emit(Event{Kind: EventToolCall, Tool: ev.ToolName, Input: truncate(compactJSON(ev.Args), p.cfg.MaxEventBytes)})
		if piEditTools[ev.ToolName] {
			if path := piArgPath(ev.Args); path != "" {
				p.edits[ev.ToolCallID] = path
			}
		}
	case "tool_execution_end":
		text := truncate(piResultText(ev.Result), p.cfg.MaxEventBytes)
		if ev.IsError {
			text = "error: " + text
		}
		p.emit(Event{Kind: EventToolResult, Tool: ev.ToolName, Text: text})
		if path, ok := p.edits[ev.ToolCallID]; ok {
			delete(p.edits, ev.ToolCallID)
			if !ev.IsError {
				p.emit(Event{Kind: EventFileChanged, Path: path})
			}
		}
	case "agent_end":
		p.ended = true
	}
}

// note keeps a plain-text stdout line; only the last few matter.
func (p *piParser) note(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	p.notes = append(p.notes, line)
	if len(p.notes) > p.cfg.NoteLines {
		p.notes = p.notes[len(p.notes)-p.cfg.NoteLines:]
	}
}

// finish reconciles what was parsed with how the process ended.
func (p *piParser) finish(waitErr error, stderr string) (Result, error) {
	if p.last == nil && p.rejected != "" {
		return Result{Failure: classifyFailure(p.rejected)}, fmt.Errorf("pi: %s", p.rejected)
	}
	if p.last == nil {
		detail := strings.Join(p.notes, " | ")
		if stderr = strings.TrimSpace(stderr); stderr != "" {
			detail = strings.TrimSpace(detail + " " + stderr)
		}
		if detail == "" && waitErr != nil {
			detail = waitErr.Error()
		}
		if detail == "" {
			detail = "output ended without an assistant message"
		}
		return Result{Failure: classifyFailure(detail)}, fmt.Errorf("pi: %s", detail)
	}
	usage := Usage{InputTokens: tokens(p.usage.Input), CacheReadTokens: tokens(p.usage.CacheRead), CacheWriteTokens: tokens(p.usage.CacheWrite), OutputTokens: tokens(p.usage.Output)}
	if p.last.StopReason == "error" || p.last.StopReason == "aborted" {
		reason := p.last.ErrorMessage
		if reason == "" {
			reason = p.last.StopReason
		}
		return Result{Usage: usage, Failure: classifyFailure(reason)}, fmt.Errorf("pi: %s", reason)
	}

	var output strings.Builder
	for _, block := range p.last.Content {
		if block.Type == "text" {
			output.WriteString(block.Text)
		}
	}
	text := output.String()
	if strings.TrimSpace(text) == "" {
		text = p.streamed.String()
	}
	return Result{Output: text, SessionRef: p.sessionID, Usage: usage}, nil
}

// piArgPath reads the path argument of an edit or write call.
func piArgPath(raw json.RawMessage) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return ""
	}
	return args.Path
}

// piResultText flattens a tool result, which is a string, an object with
// text content blocks, or anything else pi's tools return.
func piResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var wrapped struct {
		Content []piBlock `json:"content"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && len(wrapped.Content) > 0 {
		parts := make([]string, 0, len(wrapped.Content))
		for _, b := range wrapped.Content {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return compactJSON(raw)
}
