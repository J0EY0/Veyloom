package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
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
	// ProxyBinary is the veyloom executable the CLI runs as `mcp-proxy` to
	// reach the turn's MCP endpoint, which serves the room tools. Empty
	// means there is none, and turns run without the room tools.
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
// --permission-mode values. In every one of them, whatever the CLI would
// ask a person comes to the runner over the control protocol (see
// claude_control.go): read_only is plan mode, where the member may ask
// questions and put up a plan but not do more; edit_with_approval is
// acceptEdits, where edits go through and people decide on commands;
// full_auto is bypassPermissions, where only questions and plans ask.
var claudePermissionModes = map[string]string{
	PermissionReadOnly:         "plan",
	PermissionEditWithApproval: "acceptEdits",
	PermissionFullAuto:         "bypassPermissions",
}

// claudeEditTools are the tools whose use means a file changed.
var claudeEditTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

// ClaudeRunner runs turns on Claude Code in print mode, talking stream-json
// both ways: the prompt goes in as a user message, the CLI's output and its
// requests to people come out, and the answers go back in. Options honoured
// from the agent:
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
	t := &claudeTurn{
		turnBase:      newTurnBase(ctx, cancel),
		ctx:           ctx,
		preset:        spec.Permission,
		maxEventBytes: r.cfg.MaxEventBytes,
		inflight:      make(map[string]context.CancelFunc),
	}

	// The room tools come through the turn's own MCP server, which the CLI
	// reaches through the proxy; without one the turn runs without them.
	var toolArgs []string
	if spec.Host != nil && r.cfg.ProxyBinary != "" {
		ep, err := r.tools.register(spec.Host, nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("claude: tool endpoint: %w", err)
		}
		t.token = ep.token
		// Allowed up front, in every preset: they only read the room.
		// Marked read-only, they also pass plan mode, which turns down
		// tools that change things.
		allowed := make([]string, 0, len(RoomToolNames))
		for _, name := range RoomToolNames {
			allowed = append(allowed, claudeToolName(name))
		}
		toolArgs = append(toolArgs, "--mcp-config", claudeMCPConfig(r.cfg.ProxyBinary, ep.MCP), "--allowedTools", strings.Join(allowed, ","))
	}

	proc, err := startCLI(ctx, cliOptions{
		Binary:      bin,
		Args:        append(r.args(spec), toolArgs...),
		Dir:         spec.WorkDir,
		StdinPipe:   true,
		StderrBytes: r.cfg.StderrBytes,
		WaitDelay:   r.cfg.WaitDelay,
	})
	if err != nil {
		r.tools.unregister(t.token)
		cancel()
		return nil, fmt.Errorf("claude: %w", err)
	}
	t.in = newJSONLines(proc.stdin)

	go t.run(proc, r)
	// Written while run reads, so a long prompt cannot stall against the
	// CLI's output. Should the write fail, the CLI has gone, and run says
	// why.
	go t.in.send(claudeUserMessage(spec.Prompt))
	return t, nil
}

// args builds the command line for spec.
func (r *ClaudeRunner) args(spec TurnSpec) []string {
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"}
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
	args = append(args, "--permission-prompt-tool", "stdio")
	if budget := optFloat(spec.Options, "max_budget_usd"); budget > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(budget, 'f', -1, 64))
	}
	return append(args, optStrings(spec.Options, "extra_args")...)
}

// claudeUserMessage is the prompt as the stream-json user message the CLI
// reads from stdin.
func claudeUserMessage(prompt string) map[string]any {
	return map[string]any{
		"type":               "user",
		"message":            map[string]any{"role": "user", "content": prompt},
		"parent_tool_use_id": nil,
		"session_id":         "",
	}
}

type claudeTurn struct {
	*turnBase
	// ctx is the turn's lifetime; requests to people wait on it.
	ctx context.Context
	// token names the turn's tool endpoint; empty when it has none.
	token string
	// preset is the permission preset the turn runs under.
	preset        string
	maxEventBytes int

	// in carries the prompt and the answers to the CLI's requests. It is
	// closed once the result is in: the CLI then exits.
	in *jsonLines

	// inflight holds, by request id, what ends the wait of each control
	// request still being answered.
	controlMu sync.Mutex
	inflight  map[string]context.CancelFunc
}

// run reads the CLI's output until it ends, then reconciles the exit
// status with what was parsed. Control requests are answered as they come;
// the result record closes the input, which ends the CLI.
func (t *claudeTurn) run(proc *cliProcess, r *ClaudeRunner) {
	parser := newClaudeParser(r.cfg, func(ev Event) { t.emit(t.ctx, ev) })
	proc.lines(func(raw []byte) {
		var line claudeLine
		if err := json.Unmarshal(raw, &line); err != nil {
			// Not every stdout line is JSON: the CLI may print warnings.
			// Count them, keep going.
			parser.malformed++
			return
		}
		switch line.Type {
		case "control_request":
			var req struct {
				ToolUseID string `json:"tool_use_id"`
			}
			_ = json.Unmarshal(line.Request, &req)
			parser.asked(req.ToolUseID)
			t.serve(line.RequestID, line.Request)
		case "control_cancel_request":
			t.withdraw(line.RequestID)
		case "control_response":
			// Replies to requests of the runner's own; it sends none.
		default:
			parser.handle(line)
			if line.Type == "result" {
				t.in.close()
			}
		}
	})
	t.in.close()
	waitErr := proc.wait()

	res, err := parser.finish(waitErr, proc.stderrTail())
	t.finish(t.ctx, res, err)
	if t.token != "" {
		r.tools.unregister(t.token)
	}
}
