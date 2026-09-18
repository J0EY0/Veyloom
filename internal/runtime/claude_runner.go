package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ClaudeConfig tunes the Claude Code runner. Zero fields take the defaults
// from DefaultClaudeConfig.
type ClaudeConfig struct {
	// Binary is the executable to run; empty resolves "claude" on PATH at
	// each start, so a CLI installed after the machine started is found.
	Binary string
	// StreamPartials asks the CLI for token-level text chunks. When false,
	// reply text arrives one assistant message at a time.
	StreamPartials bool
	// MaxEventBytes caps tool inputs and results copied into events; the
	// rest is elided with a marker, so a 2 MB file read does not flood the
	// hub or the transcript.
	MaxEventBytes int
	// StderrBytes is how much of the CLI's stderr is kept for error
	// messages.
	StderrBytes int
	// WaitDelay is how long a cancelled CLI gets to exit after SIGTERM
	// before it is killed.
	WaitDelay time.Duration
	// ProxyBinary is the executable the CLI spawns to reach the machine's
	// MCP endpoint for approvals: veyloom itself, run as `mcp-proxy`.
	// Empty means the running executable.
	ProxyBinary string
}

// DefaultClaudeConfig returns the defaults every ClaudeConfig is completed
// with.
func DefaultClaudeConfig() ClaudeConfig {
	return ClaudeConfig{
		StreamPartials: true,
		MaxEventBytes:  4096,
		StderrBytes:    4096,
		WaitDelay:      5 * time.Second,
	}
}

func (c ClaudeConfig) withDefaults() ClaudeConfig {
	def := DefaultClaudeConfig()
	if c.MaxEventBytes <= 0 {
		c.MaxEventBytes = def.MaxEventBytes
	}
	if c.StderrBytes <= 0 {
		c.StderrBytes = def.StderrBytes
	}
	if c.WaitDelay <= 0 {
		c.WaitDelay = def.WaitDelay
	}
	return c
}

// claudePermissionModes maps the permission presets onto Claude Code's
// --permission-mode values. edit_with_approval uses acceptEdits: edits go
// through while commands prompt, and the prompts are answered by people
// through the hub (see claude_approvals.go). The other two presets never
// prompt: plan mode refuses, bypassPermissions allows.
var claudePermissionModes = map[string]string{
	PermissionReadOnly:         "plan",
	PermissionEditWithApproval: "acceptEdits",
	PermissionFullAuto:         "bypassPermissions",
}

// claudeEditTools are the tools whose use means a file changed.
var claudeEditTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

// ClaudeRunner runs turns on Claude Code in print mode, reading its
// stream-json output. Options honoured from the agent:
//
//	max_budget_usd number    passed as --max-budget-usd
//	extra_args     []string  appended to the command line verbatim
type ClaudeRunner struct {
	cfg   ClaudeConfig
	tools *toolEndpoint
}

// NewClaudeRunner creates a runner with cfg.
func NewClaudeRunner(cfg ClaudeConfig) *ClaudeRunner {
	return &ClaudeRunner{cfg: cfg.withDefaults(), tools: newToolEndpoint()}
}

// Name implements Runner.
func (*ClaudeRunner) Name() string { return "claude" }

// Close releases the tool endpoint. Turns still running lose it.
func (r *ClaudeRunner) Close() error { return r.tools.close() }

// StartTurn implements Runner. The prompt goes in on stdin, which has no
// length limit and needs no quoting; everything else is a flag.
func (r *ClaudeRunner) StartTurn(ctx context.Context, spec TurnSpec) (Turn, error) {
	bin := r.cfg.Binary
	if bin == "" {
		path, err := exec.LookPath("claude")
		if err != nil {
			return nil, fmt.Errorf("claude: %w", err)
		}
		bin = path
	}

	ctx, cancel := context.WithCancel(ctx)
	t := &claudeTurn{turnBase: newTurnBase(cancel), ctx: ctx}

	// The turn's own MCP server carries what Veyloom gives the agent: the
	// room tools whenever there is a hub to ask, and the permission prompt
	// under edit_with_approval, the one preset that prompts.
	var toolArgs []string
	asks := spec.Permission == PermissionEditWithApproval
	if asks || spec.Host != nil {
		proxy, err := r.proxyBinary()
		if err != nil {
			cancel()
			return nil, fmt.Errorf("claude: %w", err)
		}
		ep, err := r.tools.register(spec.Host, func(server *mcp.Server) {
			if asks {
				t.addApprovalTool(server)
			}
		})
		if err != nil {
			cancel()
			return nil, fmt.Errorf("claude: tool endpoint: %w", err)
		}
		t.token = ep.token
		if asks {
			toolArgs = append(toolArgs, "--permission-prompt-tool", claudeToolName(claudeApprovalTool))
		}
		toolArgs = append(toolArgs, "--mcp-config", claudeMCPConfig(proxy, ep.MCP))
		if spec.Host != nil {
			// Allowed up front, in every preset: they only read the room.
			// Marked read-only, they also pass plan mode, which turns down
			// tools that change things.
			allowed := make([]string, 0, len(RoomToolNames))
			for _, name := range RoomToolNames {
				allowed = append(allowed, claudeToolName(name))
			}
			toolArgs = append(toolArgs, "--allowedTools", strings.Join(allowed, ","))
		}
	}

	proc, err := startCLI(ctx, cliOptions{
		Binary:      bin,
		Args:        append(r.args(spec), toolArgs...),
		Dir:         spec.WorkDir,
		Stdin:       strings.NewReader(spec.Prompt),
		StderrBytes: r.cfg.StderrBytes,
		WaitDelay:   r.cfg.WaitDelay,
	})
	if err != nil {
		r.tools.unregister(t.token)
		cancel()
		return nil, fmt.Errorf("claude: %w", err)
	}

	go t.run(proc, r)
	return t, nil
}

// proxyBinary is the executable the CLI runs as its MCP bridge.
func (r *ClaudeRunner) proxyBinary() (string, error) {
	if r.cfg.ProxyBinary != "" {
		return r.cfg.ProxyBinary, nil
	}
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate own executable for the MCP proxy: %w", err)
	}
	return path, nil
}

// args builds the command line for spec.
func (r *ClaudeRunner) args(spec TurnSpec) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose"}
	if r.cfg.StreamPartials {
		args = append(args, "--include-partial-messages")
	}
	if spec.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", spec.SystemPrompt)
	}
	if spec.Model != "" {
		args = append(args, "--model", spec.Model)
	}
	// The hub names the session, so it exists under a known id from its
	// first turn on, however that turn ends.
	switch id := spec.Session.resumeID(); {
	case spec.Session.Resume && id != "":
		args = append(args, "--resume", id)
	case spec.Session.Key != "":
		args = append(args, "--session-id", spec.Session.Key)
	}
	if mode, ok := claudePermissionModes[spec.Permission]; ok {
		args = append(args, "--permission-mode", mode)
	}
	if budget := optFloat(spec.Options, "max_budget_usd"); budget > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(budget, 'f', -1, 64))
	}
	return append(args, optStrings(spec.Options, "extra_args")...)
}

type claudeTurn struct {
	*turnBase
	// ctx is the turn's lifetime; the approval tool waits on it.
	ctx context.Context
	// token names the turn's tool endpoint; empty when it has none.
	token string
}

// run reads the CLI's output until it ends, then reconciles the exit
// status with what was parsed. Pending approvals are released by finish
// before the endpoint goes away, so their callers get an answer.
func (t *claudeTurn) run(proc *cliProcess, r *ClaudeRunner) {
	parser := newClaudeParser(r.cfg, func(ev Event) { t.emit(t.ctx, ev) })
	proc.lines(parser.feed)
	waitErr := proc.wait()

	res, err := parser.finish(waitErr, proc.stderrTail())
	t.finish(t.ctx, res, err)
	if t.token != "" {
		r.tools.unregister(t.token)
	}
}

// claudeLine is the union of the stream-json records the runner reads.
// Fields are filled according to Type.
type claudeLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
	// Status is set on system/status lines: "compacting" while the CLI
	// compacts the session.
	Status  string             `json:"status"`
	Message *claudeMessage     `json:"message"`
	Event   *claudeStreamEvent `json:"event"`
	// result records
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
	// Errors is where the CLI puts what went wrong before the model ran,
	// such as a session it cannot find; Result is empty then.
	Errors     []string     `json:"errors"`
	DurationMS int64        `json:"duration_ms"`
	NumTurns   int          `json:"num_turns"`
	Usage      *claudeUsage `json:"usage"`
}

// claudeUsage is the token count of a whole run, summed by Claude Code over
// every model call in it. Anthropic reports cached input apart from fresh
// input, so the parts map straight across.
type claudeUsage struct {
	InputTokens              float64 `json:"input_tokens"`
	CacheReadInputTokens     float64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens float64 `json:"cache_creation_input_tokens"`
	OutputTokens             float64 `json:"output_tokens"`
}

type claudeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type claudeBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
}

type claudeStreamEvent struct {
	Type  string `json:"type"`
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

// claudeParser turns stream-json records into events and collects what
// the result needs. It keeps the tool_use id → name map so tool results
// can be attributed, and the reply text as a fallback should the result
// record be missing.
type claudeParser struct {
	cfg       ClaudeConfig
	emit      func(Event)
	toolNames map[string]string
	sessionID string
	text      strings.Builder
	result    *claudeLine
	malformed int
}

func newClaudeParser(cfg ClaudeConfig, emit func(Event)) *claudeParser {
	return &claudeParser{cfg: cfg, emit: emit, toolNames: make(map[string]string)}
}

func (p *claudeParser) feed(raw []byte) {
	var line claudeLine
	if err := json.Unmarshal(raw, &line); err != nil {
		// Not every stdout line is JSON: the CLI may print warnings. Count
		// them, keep going.
		p.malformed++
		return
	}
	// The session is reported the moment it is known, and again should
	// the CLI rename it when resuming, as older versions did. Only lines of
	// a session that exists count: a result that says the session could
	// not be found carries a made-up id of its own.
	if line.SessionID != "" && line.SessionID != p.sessionID && !(line.Type == "result" && line.IsError) {
		p.sessionID = line.SessionID
		p.emit(Event{Kind: EventSession, SessionRef: line.SessionID})
	}

	switch line.Type {
	case "system":
		switch line.Subtype {
		case "init":
			text := "session started"
			if line.Model != "" {
				text += " (model " + line.Model + ")"
			}
			p.emit(Event{Kind: EventStatus, Text: text})
		case "status":
			if line.Status == "compacting" {
				p.emit(Event{Kind: EventCompaction, Phase: CompactionStart})
			}
		case "compact_boundary":
			// The line the CLI writes once the summary has replaced the
			// older part of the session.
			p.emit(Event{Kind: EventCompaction, Phase: CompactionEnd})
		}
	case "stream_event":
		if ev := line.Event; ev != nil && ev.Type == "content_block_delta" && ev.Delta != nil && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
			p.text.WriteString(ev.Delta.Text)
			p.emit(Event{Kind: EventText, Text: ev.Delta.Text})
		}
	case "assistant":
		p.assistant(line.Message)
	case "user":
		p.user(line.Message)
	case "result":
		result := line
		p.result = &result
	}
}

func (p *claudeParser) assistant(msg *claudeMessage) {
	for _, block := range blocksOf(msg) {
		switch block.Type {
		case "text":
			// With partial messages on, this text already streamed as deltas.
			if !p.cfg.StreamPartials && block.Text != "" {
				p.text.WriteString(block.Text)
				p.emit(Event{Kind: EventText, Text: block.Text})
			}
		case "tool_use":
			p.toolNames[block.ID] = block.Name
			p.emit(Event{Kind: EventToolCall, Tool: block.Name, Input: truncate(compactJSON(block.Input), p.cfg.MaxEventBytes)})
			if path := editedPath(block); path != "" {
				p.emit(Event{Kind: EventFileChanged, Path: path})
			}
		}
	}
}

func (p *claudeParser) user(msg *claudeMessage) {
	for _, block := range blocksOf(msg) {
		if block.Type != "tool_result" {
			continue
		}
		p.emit(Event{
			Kind: EventToolResult,
			Tool: p.toolNames[block.ToolUseID],
			Text: truncate(toolResultText(block.Content), p.cfg.MaxEventBytes),
		})
	}
}

// finish reconciles the parsed result with how the process ended.
func (p *claudeParser) finish(waitErr error, stderr string) (Result, error) {
	if p.result == nil {
		if waitErr != nil {
			detail := strings.TrimSpace(stderr)
			return Result{Failure: classifyFailure(detail)}, fmt.Errorf("claude: %w: %s", waitErr, detail)
		}
		return Result{}, errors.New("claude: output ended without a result record")
	}
	res := *p.result
	var usage Usage
	if u := res.Usage; u != nil {
		usage = Usage{InputTokens: tokens(u.InputTokens), CacheReadTokens: tokens(u.CacheReadInputTokens), CacheWriteTokens: tokens(u.CacheCreationInputTokens), OutputTokens: tokens(u.OutputTokens)}
	}
	if res.IsError {
		// The real reason, wherever the CLI put it; the subtype alone
		// ("error_during_execution") tells a person nothing.
		reason := strings.TrimSpace(res.Result)
		if reason == "" {
			reason = strings.TrimSpace(strings.Join(res.Errors, "; "))
		}
		if reason == "" {
			reason = strings.TrimSpace(stderr)
		}
		if reason == "" {
			reason = res.Subtype
		}
		return Result{Usage: usage, Failure: classifyFailure(reason)}, fmt.Errorf("claude: %s", reason)
	}

	output := res.Result
	if output == "" {
		output = p.text.String()
	}
	return Result{Output: output, SessionRef: p.sessionID, Usage: usage}, nil
}

// blocksOf decodes a message's content, which is an array of blocks or, for
// plain user text, a string.
func blocksOf(msg *claudeMessage) []claudeBlock {
	if msg == nil || len(msg.Content) == 0 {
		return nil
	}
	var blocks []claudeBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil
	}
	return blocks
}

// editedPath returns the file an edit-type tool call targets.
func editedPath(block claudeBlock) string {
	if !claudeEditTools[block.Name] {
		return ""
	}
	var input struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	if err := json.Unmarshal(block.Input, &input); err != nil {
		return ""
	}
	if input.FilePath != "" {
		return input.FilePath
	}
	return input.NotebookPath
}

// toolResultText flattens a tool_result content, which is a string or a
// list of text blocks.
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []claudeBlock
	if json.Unmarshal(raw, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buf strings.Builder
	dec := json.NewEncoder(&buf)
	dec.SetEscapeHTML(false)
	var v any
	if err := json.Unmarshal(raw, &v); err != nil || dec.Encode(v) != nil {
		return string(raw)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// truncate cuts s to at most max bytes on a rune boundary and says how
// much was dropped.
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return fmt.Sprintf("%s… [%d more bytes]", s[:cut], len(s)-cut)
}

func optFloat(o map[string]any, key string) float64 {
	switch v := o[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// optStrings reads a list option; JSON arrays arrive as []any.
func optStrings(o map[string]any, key string) []string {
	list, _ := o[key].([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
