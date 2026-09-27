package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
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
	// SteerWait is how long, once a turn's answer is in, the runner waits
	// for the CLI to take in text Steer passed it before giving up on the
	// text and letting the CLI end. The CLI takes such text in the moment
	// its answer is done, so this only bounds one that lost it.
	SteerWait time.Duration
	// RecordDir keeps the CLI's output as printed, a file a turn, for the
	// replay tests (docs/design.md 5.23.9); empty records nothing.
	RecordDir string
}

// DefaultClaudeConfig returns the defaults every ClaudeConfig is completed
// with.
func DefaultClaudeConfig() ClaudeConfig {
	return ClaudeConfig{
		StreamPartials: true,
		MaxEventBytes:  4096,
		StderrBytes:    4096,
		WaitDelay:      5 * time.Second,
		SteerWait:      15 * time.Second,
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
	if c.SteerWait <= 0 {
		c.SteerWait = def.SteerWait
	}
	return c
}

// claudePermissionModes maps the permission presets onto Claude Code's
// --permission-mode values. In every one of them, whatever the CLI would
// ask a person comes to the runner over the control protocol (see
// claude_control.go): read_only is plan mode, where the member may ask
// questions and put up a plan but not do more; edit_with_approval is
// acceptEdits, where edits go through and people decide on commands;
// auto_review is auto mode, where Claude Code's own classifier decides on
// commands and people only on what it will not; full_auto is
// bypassPermissions, where only questions and plans ask.
var claudePermissionModes = map[string]string{
	PermissionReadOnly:         "plan",
	PermissionEditWithApproval: "acceptEdits",
	PermissionAutoReview:       "auto",
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
		steerWait:     r.cfg.SteerWait,
		prompted:      make(chan struct{}),
		inflight:      make(map[string]context.CancelFunc),
	}

	// The room tools come through the turn's own MCP server, which the CLI
	// reaches through the proxy; without one the turn runs without them.
	var toolArgs []string
	if spec.Host != nil && r.cfg.ProxyBinary != "" {
		ep, err := r.tools.register(spec.Host, spec.ExtraTools, nil)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("claude: tool endpoint: %w", err)
		}
		t.token = ep.token
		// Allowed up front, in every preset: they are Veyloom's own, the
		// room tools only read and the hub decides which wiki changes wait
		// for a person. The read-only ones also pass plan mode; the wiki
		// writes, should plan mode ask about them, are let through in
		// canUseTool.
		names := turnToolNames(spec.ExtraTools)
		allowed := make([]string, 0, len(names))
		for _, name := range names {
			allowed = append(allowed, claudeToolName(name))
		}
		toolArgs = append(toolArgs, "--mcp-config", claudeMCPConfig(r.cfg.ProxyBinary, ep.MCP), "--allowedTools", strings.Join(allowed, ","))
	}

	proc, err := startCLI(ctx, cliOptions{
		Binary:      bin,
		Args:        append(r.args(spec), toolArgs...),
		Dir:         spec.WorkDir,
		StdinPipe:   true,
		Env:         spec.Env,
		StderrBytes: r.cfg.StderrBytes,
		WaitDelay:   r.cfg.WaitDelay,
		RecordDir:   r.cfg.RecordDir,
		RecordName:  "claude",
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
	go func() {
		t.in.send(claudeUserMessage(spec.Prompt))
		close(t.prompted)
	}()
	return t, nil
}

// args builds the command line for spec.
func (r *ClaudeRunner) args(spec TurnSpec) []string {
	// The CLI echoes what it takes in from its input, which says when text
	// Steer passed it reached the agent (design.md 5.23.2).
	args := []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--replay-user-messages"}
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
	// The skill library's skills, as a plugin for this run only.
	if spec.SkillDir != "" {
		args = append(args, "--plugin-dir", spec.SkillDir)
	}
	if budget := optFloat(spec.Options, "max_budget_usd"); budget > 0 {
		args = append(args, "--max-budget-usd", strconv.FormatFloat(budget, 'f', -1, 64))
	}
	// What people allowed the member always, as the permission rules Claude
	// Code suggested; it matches them itself (docs/design.md 4.6). They go
	// as settings, a JSON list added to the person's own, not on
	// --allowedTools: the CLI splits that on commas and spaces outside
	// parentheses, and loses count in a rule with parentheses of its own.
	// A --settings among the agent's extra_args comes later and wins.
	if len(spec.AllowedRules) > 0 {
		settings, _ := json.Marshal(map[string]any{"permissions": map[string]any{"allow": spec.AllowedRules}})
		args = append(args, "--settings", string(settings))
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
	// closed once the result is in: the CLI then exits. prompted is closed
	// once the prompt is written: what Steer passes goes after it.
	in       *jsonLines
	prompted chan struct{}

	// steers are the texts Steer wrote that the CLI has not taken in yet,
	// oldest first; steerMu also keeps a result from closing the input
	// between Steer's look and its write. took says one was taken in since
	// the last result. unheard, while set, closes the input should the CLI
	// not take any of them in within steerWait of a result (resultIn).
	steerMu   sync.Mutex
	steers    []claudeSteer
	took      bool
	unheard   *time.Timer
	steerWait time.Duration

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
	answered := false
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
			if line.Type == "user" && line.IsReplay {
				// Text of the input echoed back, not a tool result.
				t.tookIn(line.Message)
				return
			}
			if line.Type == "system" && line.Subtype == "init" && answered {
				// A turn of the CLI's own for text Steer passed, the
				// session long announced.
				parser.nextTurn()
				t.takingNext()
				return
			}
			parser.handle(line)
			if line.Type == "result" {
				answered = true
				t.resultIn()
			}
		}
	})
	t.in.close()
	waitErr := proc.wait()
	for _, s := range t.untaken() {
		t.emit(t.ctx, Event{Kind: EventSteerDropped, SteerID: s.id})
	}

	res, err := parser.finish(waitErr, proc.stderrTail())
	t.finish(t.ctx, res, err)
	if t.token != "" {
		r.tools.unregister(t.token)
	}
}

// claudeSteer is text Steer wrote to the CLI's input.
type claudeSteer struct{ id, text string }

// Steer implements Turn. The text goes to the CLI as a user message: it
// hands the agent the message with its next tool result, or answers it as
// a turn of its own once the turn's answer is done (2.1.85 and 2.1.281,
// design.md 5.23.2). The CLI echoing it back says it was taken in.
func (t *claudeTurn) Steer(id, text string) error {
	select {
	case <-t.prompted:
	case <-t.ctx.Done():
		return ErrSteerRefused
	}
	t.steerMu.Lock()
	defer t.steerMu.Unlock()
	if err := t.in.send(claudeUserMessage(text)); err != nil {
		return ErrSteerRefused
	}
	t.steers = append(t.steers, claudeSteer{id: id, text: text})
	return nil
}

// tookIn notes that the CLI took in text of its input, which is the
// prompt or one of the texts Steer passed: each is echoed on its own, in
// the order written, word for word (2.1.85 and 2.1.281). The text is
// looked for among all those still untaken, so one the CLI somehow lost
// holds up none after it.
func (t *claudeTurn) tookIn(msg *claudeMessage) {
	text := userText(msg)
	t.steerMu.Lock()
	t.heard()
	i := slices.IndexFunc(t.steers, func(s claudeSteer) bool { return s.text == text })
	var took claudeSteer
	if i >= 0 {
		took = t.steers[i]
		t.steers = slices.Delete(t.steers, i, i+1)
		t.took = true
	}
	t.steerMu.Unlock()
	if i >= 0 {
		t.emit(t.ctx, Event{Kind: EventSteer, SteerID: took.id, Text: took.text})
	}
}

// takingNext notes that the CLI began a turn of its own once its answer
// was done, which answers the oldest text Steer passed not yet taken in:
// streaming partial messages, the CLI echoes that text only once the
// answer to it is under way (2.1.85), too late to tell where it begins.
// When the echo came first, it said so already.
func (t *claudeTurn) takingNext() {
	t.steerMu.Lock()
	t.heard()
	var took *claudeSteer
	if !t.took && len(t.steers) > 0 {
		first := t.steers[0]
		took = &first
		t.steers = slices.Delete(t.steers, 0, 1)
		t.took = true
	}
	t.steerMu.Unlock()
	if took != nil {
		t.emit(t.ctx, Event{Kind: EventSteer, SteerID: took.id, Text: took.text})
	}
}

// resultIn closes the input on a result, which ends the CLI, unless the
// CLI still has text of Steer's to take in: it answers each such text as
// a turn of its own, which may ask people for things on the input, and
// takes the first in the moment the result is out. Should it not within
// steerWait, it lost the text, and the input is closed after all.
func (t *claudeTurn) resultIn() {
	t.steerMu.Lock()
	defer t.steerMu.Unlock()
	t.heard()
	t.took = false
	if len(t.steers) == 0 {
		t.in.close()
		return
	}
	t.unheard = time.AfterFunc(t.steerWait, t.in.close)
}

// heard stops the wait resultIn set going. steerMu is held.
func (t *claudeTurn) heard() {
	if t.unheard != nil {
		t.unheard.Stop()
		t.unheard = nil
	}
}

// untaken ends the turn's steering: it returns the texts Steer passed that
// the CLI never took in.
func (t *claudeTurn) untaken() []claudeSteer {
	t.steerMu.Lock()
	defer t.steerMu.Unlock()
	t.heard()
	left := t.steers
	t.steers = nil
	return left
}

// userText is the text of a user message, whose content is a string or
// blocks.
func userText(msg *claudeMessage) string {
	if msg == nil {
		return ""
	}
	var text string
	if json.Unmarshal(msg.Content, &text) == nil {
		return text
	}
	var parts []string
	for _, b := range blocksOf(msg) {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "")
}
