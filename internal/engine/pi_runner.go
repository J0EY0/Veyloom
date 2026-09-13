package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// PiConfig tunes the Pi runner. Zero fields take the defaults from
// DefaultPiConfig.
type PiConfig struct {
	// Binary is the executable to run; empty resolves "pi" on PATH at each
	// start.
	Binary string
	// MaxEventBytes caps tool inputs and results copied into events.
	MaxEventBytes int
	// StderrBytes is how much of the CLI's stderr is kept for errors.
	StderrBytes int
	// NoteLines is how many non-JSON stdout lines are kept: pi prints
	// problems such as a missing API key there, in plain text.
	NoteLines int
	// WaitDelay is how long a cancelled CLI gets to exit after SIGTERM
	// before it is killed.
	WaitDelay time.Duration
}

// DefaultPiConfig returns the defaults every PiConfig is completed with.
func DefaultPiConfig() PiConfig {
	return PiConfig{
		MaxEventBytes: 4096,
		StderrBytes:   4096,
		NoteLines:     8,
		WaitDelay:     5 * time.Second,
	}
}

func (c PiConfig) withDefaults() PiConfig {
	def := DefaultPiConfig()
	if c.MaxEventBytes <= 0 {
		c.MaxEventBytes = def.MaxEventBytes
	}
	if c.StderrBytes <= 0 {
		c.StderrBytes = def.StderrBytes
	}
	if c.NoteLines <= 0 {
		c.NoteLines = def.NoteLines
	}
	if c.WaitDelay <= 0 {
		c.WaitDelay = def.WaitDelay
	}
	return c
}

// piToolSets maps the permission presets onto pi's --tools allowlist. Pi
// has no approval prompts, so edit_with_approval becomes "edit but never
// run commands"; full_auto leaves the default of every tool enabled.
var piToolSets = map[string][]string{
	PermissionReadOnly:         {"read", "grep", "find", "ls"},
	PermissionEditWithApproval: {"read", "grep", "find", "ls", "edit", "write"},
}

// piEditTools are the tools whose use means a file changed.
var piEditTools = map[string]bool{"edit": true, "write": true}

// PiRunner runs turns on the pi coding agent in print mode with JSON event
// output. Options honoured from the template:
//
//	provider   string    passed as --provider
//	thinking   string    passed as --thinking (off, minimal, low, medium, high, xhigh)
//	extra_args []string  appended to the command line verbatim
type PiRunner struct {
	cfg PiConfig
}

// NewPiRunner creates a runner with cfg.
func NewPiRunner(cfg PiConfig) *PiRunner {
	return &PiRunner{cfg: cfg.withDefaults()}
}

// Name implements Runner.
func (*PiRunner) Name() string { return "pi" }

// StartTurn implements Runner. The prompt is the positional message; pi
// treats piped stdin as extra context, so stdin is left empty.
func (r *PiRunner) StartTurn(ctx context.Context, spec TurnSpec) (Turn, error) {
	bin := r.cfg.Binary
	if bin == "" {
		path, err := exec.LookPath("pi")
		if err != nil {
			return nil, fmt.Errorf("pi: %w", err)
		}
		bin = path
	}

	ctx, cancel := context.WithCancel(ctx)
	proc, err := startCLI(ctx, cliOptions{
		Binary: bin,
		Args:   r.args(spec),
		Dir:    spec.WorkDir,
		// Startup update checks are noise for a turn; PI_OFFLINE turns only
		// those off, not model calls.
		Env:         []string{"PI_OFFLINE=1"},
		StderrBytes: r.cfg.StderrBytes,
		WaitDelay:   r.cfg.WaitDelay,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("pi: %w", err)
	}

	t := &piTurn{turnBase: newTurnBase(cancel)}
	go t.run(ctx, proc, r.cfg)
	return t, nil
}

// args builds the command line for spec. The prompt comes last so that no
// flag can be mistaken for it.
func (r *PiRunner) args(spec TurnSpec) []string {
	args := []string{"-p", "--mode", "json"}
	if spec.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", spec.SystemPrompt)
	}
	if provider := optString(spec.Options, "provider"); provider != "" {
		args = append(args, "--provider", provider)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	if thinking := optString(spec.Options, "thinking"); thinking != "" {
		args = append(args, "--thinking", thinking)
	}
	if spec.SessionRef != "" {
		args = append(args, "--session", spec.SessionRef)
	}
	if tools, ok := piToolSets[spec.Permission]; ok {
		args = append(args, "--tools", strings.Join(tools, ","))
	}
	args = append(args, optStrings(spec.Options, "extra_args")...)
	return append(args, spec.Prompt)
}

type piTurn struct {
	*turnBase
}

func (t *piTurn) run(ctx context.Context, proc *cliProcess, cfg PiConfig) {
	parser := newPiParser(cfg, func(ev Event) { t.emit(ctx, ev) })
	proc.lines(parser.feed)
	waitErr := proc.wait()

	res, err := parser.finish(waitErr, proc.stderrTail())
	t.finish(ctx, res, err)
}

// piEvent is the union of the JSON event records the runner reads.
type piEvent struct {
	Type string `json:"type"`
	// session header
	ID string `json:"id"`
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

type piUsage struct {
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	CacheRead   float64 `json:"cacheRead"`
	CacheWrite  float64 `json:"cacheWrite"`
	TotalTokens float64 `json:"totalTokens"`
	Cost        struct {
		Total float64 `json:"total"`
	} `json:"cost"`
}

// piParser turns pi's event stream into events and collects the result:
// the session id from the header, the last assistant message as the reply,
// usage summed over every assistant message, and the tail of any plain-text
// lines pi printed instead of JSON.
type piParser struct {
	cfg       PiConfig
	emit      func(Event)
	sessionID string
	streamed  strings.Builder
	last      *piMessage
	usage     piUsage
	notes     []string
	ended     bool
}

func newPiParser(cfg PiConfig, emit func(Event)) *piParser {
	return &piParser{cfg: cfg, emit: emit}
}

func (p *piParser) feed(raw []byte) {
	var ev piEvent
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Type == "" {
		p.note(string(raw))
		return
	}

	switch ev.Type {
	case "session":
		p.sessionID = ev.ID
		p.emit(Event{Kind: EventStatus, Text: "session " + ev.ID})
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
				p.usage.TotalTokens += msg.Usage.TotalTokens
				p.usage.Cost.Total += msg.Usage.Cost.Total
			}
		}
	case "tool_execution_start":
		p.emit(Event{Kind: EventToolCall, Tool: ev.ToolName, Input: truncate(compactJSON(ev.Args), p.cfg.MaxEventBytes)})
		if piEditTools[ev.ToolName] {
			if path := piArgPath(ev.Args); path != "" {
				p.emit(Event{Kind: EventFileChanged, Path: path})
			}
		}
	case "tool_execution_end":
		text := truncate(piResultText(ev.Result), p.cfg.MaxEventBytes)
		if ev.IsError {
			text = "error: " + text
		}
		p.emit(Event{Kind: EventToolResult, Tool: ev.ToolName, Text: text})
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
		return Result{}, fmt.Errorf("pi: %s", detail)
	}
	if p.last.StopReason == "error" || p.last.StopReason == "aborted" {
		reason := p.last.ErrorMessage
		if reason == "" {
			reason = p.last.StopReason
		}
		return Result{}, fmt.Errorf("pi: %s", reason)
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
	usage := map[string]any{
		"cost_usd":      p.usage.Cost.Total,
		"input_tokens":  p.usage.Input,
		"output_tokens": p.usage.Output,
		"cache_read":    p.usage.CacheRead,
		"cache_write":   p.usage.CacheWrite,
		"total_tokens":  p.usage.TotalTokens,
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
