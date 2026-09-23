package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CodexConfig tunes the Codex runner. Zero fields take the defaults from
// DefaultCodexConfig.
type CodexConfig struct {
	// Binary is the executable to run; empty resolves "codex" on PATH at
	// each start.
	Binary string
	// MaxEventBytes caps tool inputs and results copied into events.
	MaxEventBytes int
	// StderrBytes is how much of the CLI's stderr is kept for errors.
	StderrBytes int
	// WaitDelay is how long a stopped app-server gets to exit after
	// SIGTERM before it is killed.
	WaitDelay time.Duration
	// SetupTimeout bounds everything before the turn itself: starting the
	// app-server, the initialize handshake and thread start or resume.
	SetupTimeout time.Duration
	// ProxyBinary is the veyloom executable Codex runs as `mcp-proxy`, the
	// MCP bridge to the turn's room tools. Empty means there is none, and
	// turns run without the room tools.
	ProxyBinary string
}

// DefaultCodexConfig returns the defaults every CodexConfig is completed
// with.
func DefaultCodexConfig() CodexConfig {
	return CodexConfig{
		MaxEventBytes: 4096,
		StderrBytes:   4096,
		WaitDelay:     5 * time.Second,
		SetupTimeout:  60 * time.Second,
	}
}

func (c CodexConfig) withDefaults() CodexConfig {
	def := DefaultCodexConfig()
	if c.MaxEventBytes <= 0 {
		c.MaxEventBytes = def.MaxEventBytes
	}
	if c.StderrBytes <= 0 {
		c.StderrBytes = def.StderrBytes
	}
	if c.WaitDelay <= 0 {
		c.WaitDelay = def.WaitDelay
	}
	if c.SetupTimeout <= 0 {
		c.SetupTimeout = def.SetupTimeout
	}
	return c
}

// codexPolicy is how a permission preset maps onto the app-server's two
// knobs: when it asks (approval policy) and what the sandbox lets it touch.
type codexPolicy struct {
	approval string // untrusted, on-request, never
	sandbox  string // read-only, workspace-write, danger-full-access
}

// codexPolicies maps the presets. edit_with_approval keeps the sandbox on
// the workspace and lets the agent ask for anything beyond it; full_auto
// keeps the same sandbox but never asks.
var codexPolicies = map[string]codexPolicy{
	PermissionReadOnly:         {approval: "never", sandbox: "read-only"},
	PermissionEditWithApproval: {approval: "on-request", sandbox: "workspace-write"},
	PermissionFullAuto:         {approval: "never", sandbox: "workspace-write"},
}

// codexSandboxTypes maps a sandbox mode onto the tagged form turn/start
// takes for sandboxPolicy.
var codexSandboxTypes = map[string]string{
	"read-only":          "readOnly",
	"workspace-write":    "workspaceWrite",
	"danger-full-access": "dangerFullAccess",
}

// CodexRunner runs turns on OpenAI's Codex through `codex app-server`, a
// JSON-RPC conversation over the process's standard streams: one line per
// message, `{id, method, params}` requests, `{id, result}` or `{id, error}`
// replies, `{method, params}` notifications, and the same shapes in the
// other direction when the server asks the client for an approval.
//
// One app-server process serves one turn and is stopped afterwards; the
// thread it created or resumed is the session reference, and lives in
// Codex's own session store. Options honoured from the agent:
//
//	approval_policy string    overrides the preset's: untrusted, on-request, never
//	sandbox         string    overrides the preset's: read-only, workspace-write, danger-full-access
//	extra_args      []string  appended to `codex app-server` verbatim
type CodexRunner struct {
	cfg   CodexConfig
	tools *toolEndpoint
}

// NewCodexRunner creates a runner with cfg.
func NewCodexRunner(cfg CodexConfig) *CodexRunner {
	return &CodexRunner{cfg: cfg.withDefaults(), tools: newToolEndpoint()}
}

// Close releases the tool endpoint. Turns still running lose it.
func (r *CodexRunner) Close() error { return r.tools.close() }

// Name implements Runner.
func (*CodexRunner) Name() string { return "codex" }

// StartTurn implements Runner.
func (r *CodexRunner) StartTurn(ctx context.Context, spec TurnSpec) (Turn, error) {
	bin := r.cfg.Binary
	if bin == "" {
		path, err := exec.LookPath("codex")
		if err != nil {
			return nil, fmt.Errorf("codex: %w", err)
		}
		bin = path
	}

	// The room tools reach Codex as an MCP server named in the thread's
	// config; it runs the proxy pointed at this turn's endpoint.
	var mcpServer map[string]any
	var token string
	if spec.Host != nil && r.cfg.ProxyBinary != "" {
		ep, err := r.tools.register(spec.Host, spec.ExtraTools, nil)
		if err != nil {
			return nil, fmt.Errorf("codex: tool endpoint: %w", err)
		}
		token = ep.token
		command, args := mcpProxyServer(r.cfg.ProxyBinary, ep.MCP)
		mcpServer = map[string]any{
			"command": command,
			"args":    args,
			// Veyloom's own tools: the room tools only read the turn's
			// room, and the hub decides which wiki changes wait for a person.
			"default_tools_approval_mode": "approve",
		}
	}
	release := func() {
		if token != "" {
			r.tools.unregister(token)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	// The server runs under its own context so it can be stopped once the
	// turn is over without that looking like a cancelled turn.
	procCtx, stopProc := context.WithCancel(ctx)
	proc, err := startCLI(procCtx, cliOptions{
		Binary:      bin,
		Args:        append([]string{"app-server"}, optStrings(spec.Options, "extra_args")...),
		Dir:         spec.WorkDir,
		StdinPipe:   true,
		StderrBytes: r.cfg.StderrBytes,
		WaitDelay:   r.cfg.WaitDelay,
	})
	if err != nil {
		stopProc()
		cancel()
		release()
		return nil, fmt.Errorf("codex: %w", err)
	}

	t := &codexTurn{
		mcpServer: mcpServer,
		release:   release,
		turnBase:  newTurnBase(ctx, cancel),
		ctx:       ctx,
		stopProc:  stopProc,
		cfg:       r.cfg,
		proc:      proc,
		pending:   make(map[int64]chan codexMessage),
		eof:       make(chan struct{}),
		completed: make(chan codexTurnEnd, 1),
		items:     make(map[string]codexItem),
		mcpFailed: make(map[string]bool),
		tokens:    make(map[string]*codexThreadTokens),
	}
	go t.run(spec)
	return t, nil
}

// codexMessage is any line of the conversation; which fields are set says
// what it is. IDs are kept raw because the server may use numbers or
// strings for its own requests and expects them echoed back as sent.
type codexMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *codexRPCError  `json:"error,omitempty"`
}

type codexRPCError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// codexTurnEnd is what turn/completed reported.
type codexTurnEnd struct {
	status string
	err    string
}

// codexItem is what the runner remembers about a started item, to name
// its result and describe approval requests for it.
type codexItem struct {
	tool  string
	paths []string
}

type codexTurn struct {
	*turnBase
	// mcpServer is the turn's entry for the thread's mcp_servers config,
	// nil without room tools; release gives the turn's endpoint back.
	mcpServer map[string]any
	release   func()
	ctx       context.Context
	stopProc  context.CancelFunc
	cfg       CodexConfig
	proc      *cliProcess

	writeMu sync.Mutex
	nextID  atomic.Int64

	mu        sync.Mutex
	pending   map[int64]chan codexMessage
	items     map[string]codexItem
	text      strings.Builder
	final     string
	malformed int
	// thread is the turn's thread. Codex runs the subagents it starts in
	// threads of their own on the same app-server, which tells of their
	// items and turns as well: what they do is part of the turn, but what
	// they say is not its reply, and their turns ending is not its end.
	thread string
	// tokens is what each thread's token reports came to.
	tokens map[string]*codexThreadTokens
	// reviewing counts Codex's automatic reviews under way; the warning it
	// sends a person during one is that review's reasoning, held in
	// reviewNote until the review completes. mcpFailed remembers the MCP
	// servers already reported as failing to start, which Codex retries.
	reviewing  int
	reviewNote string
	mcpFailed  map[string]bool

	eof       chan struct{}
	completed chan codexTurnEnd
}

// run holds the conversation and, once the turn is over, stops the server
// before finishing so no event can be emitted after the channel closes.
func (t *codexTurn) run(spec TurnSpec) {
	go func() {
		t.proc.lines(t.handleLine)
		close(t.eof)
	}()

	res, err := t.converse(spec)

	t.stopProc()
	<-t.eof
	_ = t.proc.wait()
	t.release()
	t.finish(t.ctx, res, err)
}

// converse performs the handshake, sets up the thread, starts the turn and
// waits for it to complete.
func (t *codexTurn) converse(spec TurnSpec) (Result, error) {
	setupCtx, cancel := context.WithTimeout(t.ctx, t.cfg.SetupTimeout)
	defer cancel()

	if _, err := t.call(setupCtx, "initialize", map[string]any{
		"clientInfo": map[string]any{"name": "veyloom", "title": "Veyloom", "version": "dev"},
	}); err != nil {
		return Result{}, err
	}
	if err := t.write(codexMessage{Method: "initialized"}); err != nil {
		return Result{}, err
	}
	// The skill library's skills, as one more folder of skills for this
	// app-server; the person's own configuration is left as it is. A Codex
	// that cannot take them runs the turn without.
	if spec.SkillDir != "" {
		roots := map[string]any{"extraRoots": []string{filepath.Join(spec.SkillDir, "skills")}}
		if _, err := t.call(setupCtx, "skills/extraRoots/set", roots); err != nil {
			t.emit(t.ctx, Event{Kind: EventNotice, Level: NoticeWarning, Text: "Codex did not take the skill library's skills: " + err.Error()})
		}
	}

	policy := t.policy(spec)
	var thread struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model string `json:"model"`
	}
	params := map[string]any{"approvalPolicy": policy.approval, "sandbox": policy.sandbox}
	if t.mcpServer != nil {
		// A config override, keyed like `-c mcp_servers.veyloom=…` on the
		// command line: it adds this server to whatever the user has set
		// up, for this thread. Sent on resume too, since the endpoint is
		// new with every turn.
		params["config"] = map[string]any{"mcp_servers." + toolServerName: t.mcpServer}
	}
	if spec.WorkDir != "" {
		params["cwd"] = spec.WorkDir
	}
	if spec.Model != "" {
		params["model"] = spec.Model
	}
	method := "thread/start"
	if spec.Session.Resume && spec.Session.Ref != "" {
		// Codex names its threads itself, so only its own reference resumes.
		method = "thread/resume"
		params["threadId"] = spec.Session.Ref
		// Only the thread's id is read back; without this the reply carries
		// the thread's whole history, which grows with every turn.
		params["excludeTurns"] = true
	} else if spec.SystemPrompt != "" {
		// The role card is fixed at creation; a resumed thread keeps the
		// instructions it was started with.
		params["developerInstructions"] = spec.SystemPrompt
	}
	raw, err := t.call(setupCtx, method, params)
	if err != nil {
		// The app-server answering no to thread/resume is its verdict on
		// the thread: whatever the reason, this thread will not carry the
		// turn. A server that died or timed out said nothing about it.
		var refused *codexRefusal
		if method == "thread/resume" && errors.As(err, &refused) {
			return Result{Failure: FailureSessionNotFound}, err
		}
		return Result{}, err
	}
	if err := json.Unmarshal(raw, &thread); err != nil || thread.Thread.ID == "" {
		return Result{}, fmt.Errorf("codex: %s returned no thread id: %s", method, raw)
	}
	t.mu.Lock()
	t.thread = thread.Thread.ID
	t.mu.Unlock()
	t.emit(t.ctx, Event{Kind: EventSession, SessionRef: thread.Thread.ID})
	status := "session started"
	if thread.Model != "" {
		status += " (model " + thread.Model + ")"
	}
	t.emit(t.ctx, Event{Kind: EventStatus, Text: status})

	turnParams := map[string]any{
		"threadId":       thread.Thread.ID,
		"input":          []map[string]any{{"type": "text", "text": spec.Prompt}},
		"approvalPolicy": policy.approval,
		"sandboxPolicy":  map[string]any{"type": codexSandboxTypes[policy.sandbox]},
	}
	if spec.WorkDir != "" {
		turnParams["cwd"] = spec.WorkDir
	}
	if _, err := t.call(t.ctx, "turn/start", turnParams); err != nil {
		return Result{}, err
	}

	var end codexTurnEnd
	select {
	case end = <-t.completed:
	case <-t.eof:
		return Result{Usage: t.usage()}, fmt.Errorf("codex: app-server exited before the turn completed: %s", strings.TrimSpace(t.proc.stderrTail()))
	case <-t.ctx.Done():
		return Result{Usage: t.usage()}, ErrTurnCancelled
	}

	t.mu.Lock()
	output := t.final
	if output == "" {
		output = t.text.String()
	}
	t.mu.Unlock()
	usage := t.usage()

	switch end.status {
	case "completed":
	case "interrupted":
		return Result{Usage: usage}, errors.New("codex: turn interrupted")
	default:
		reason := end.err
		if reason == "" {
			reason = "turn " + end.status
		}
		return Result{Usage: usage}, fmt.Errorf("codex: %s", reason)
	}
	return Result{Output: output, SessionRef: thread.Thread.ID, Usage: usage}, nil
}

// policy resolves the preset and the agent's overrides.
func (t *codexTurn) policy(spec TurnSpec) codexPolicy {
	p, ok := codexPolicies[spec.Permission]
	if !ok {
		p = codexPolicies[PermissionReadOnly]
	}
	if v := optString(spec.Options, "approval_policy"); v != "" {
		p.approval = v
	}
	if v := optString(spec.Options, "sandbox"); v != "" {
		if _, known := codexSandboxTypes[v]; known {
			p.sandbox = v
		}
	}
	return p
}

// call sends a request and waits for its reply, the server's exit or ctx.
func (t *codexTurn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := t.nextID.Add(1)
	ch := make(chan codexMessage, 1)
	t.mu.Lock()
	t.pending[id] = ch
	t.mu.Unlock()
	forget := func() {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
	}

	rawParams, err := json.Marshal(params)
	if err != nil {
		forget()
		return nil, fmt.Errorf("codex: encode %s params: %w", method, err)
	}
	if err := t.write(codexMessage{ID: json.RawMessage(strconv.FormatInt(id, 10)), Method: method, Params: rawParams}); err != nil {
		forget()
		return nil, err
	}

	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, &codexRefusal{Method: method, Message: resp.Error.Message, Code: resp.Error.Code}
		}
		return resp.Result, nil
	case <-t.eof:
		forget()
		return nil, fmt.Errorf("codex: app-server exited during %s: %s", method, strings.TrimSpace(t.proc.stderrTail()))
	case <-ctx.Done():
		forget()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("codex: %s timed out after %s", method, t.cfg.SetupTimeout)
		}
		return nil, ErrTurnCancelled
	}
}

// codexRefusal is the app-server answering a request with an error, as
// opposed to not answering at all.
type codexRefusal struct {
	Method  string
	Message string
	Code    int64
}

func (e *codexRefusal) Error() string {
	return fmt.Sprintf("codex: %s: %s (code %d)", e.Method, e.Message, e.Code)
}

// write sends one message line. Writes are serialised so concurrent
// approval replies cannot interleave with a request.
func (t *codexTurn) write(msg codexMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("codex: encode message: %w", err)
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if _, err := t.proc.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("codex: write to app-server: %w", err)
	}
	return nil
}

// respond answers a server request.
func (t *codexTurn) respond(id json.RawMessage, result any) {
	raw, err := json.Marshal(result)
	if err != nil {
		return
	}
	_ = t.write(codexMessage{ID: id, Result: raw})
}

func (t *codexTurn) respondError(id json.RawMessage, code int64, message string) {
	_ = t.write(codexMessage{ID: id, Error: &codexRPCError{Code: code, Message: message}})
}

// handleLine classifies one line from the server. Replies to our requests
// are routed to their waiter; notifications become events; requests are
// served on their own goroutine because approvals block.
func (t *codexTurn) handleLine(raw []byte) {
	var msg codexMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.mu.Lock()
		t.malformed++
		t.mu.Unlock()
		return
	}
	switch {
	case msg.Method != "" && len(msg.ID) > 0:
		go t.serveRequest(msg)
	case msg.Method != "":
		t.notification(msg)
	case len(msg.ID) > 0:
		var id int64
		if json.Unmarshal(msg.ID, &id) != nil {
			return
		}
		t.mu.Lock()
		ch := t.pending[id]
		delete(t.pending, id)
		t.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	}
}

// codexItemView is the subset of a thread item the runner reads.
type codexItemView struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
	Text             string          `json:"text"`
	Command          string          `json:"command"`
	Status           string          `json:"status"`
	AggregatedOutput string          `json:"aggregatedOutput"`
	ExitCode         *int            `json:"exitCode"`
	Server           string          `json:"server"`
	Tool             string          `json:"tool"`
	Arguments        json.RawMessage `json:"arguments"`
	Result           json.RawMessage `json:"result"`
	Error            json.RawMessage `json:"error"`
	Changes          []struct {
		Path string `json:"path"`
	} `json:"changes"`
	// A subagent's coming and going: started, interacted, completed.
	Kind      string `json:"kind"`
	AgentPath string `json:"agentPath"`
}

// ours reports whether a notification is about the turn's own thread
// rather than a subagent's. One that names no thread is taken as ours.
func (t *codexTurn) ours(params json.RawMessage) bool {
	var p struct {
		ThreadID string `json:"threadId"`
	}
	if json.Unmarshal(params, &p) != nil || p.ThreadID == "" {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.thread == "" || p.ThreadID == t.thread
}

// notification turns a server notification into events and state.
func (t *codexTurn) notification(msg codexMessage) {
	switch msg.Method {
	case "item/agentMessage/delta":
		var p struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(msg.Params, &p) == nil && p.Delta != "" && t.ours(msg.Params) {
			t.mu.Lock()
			t.text.WriteString(p.Delta)
			t.mu.Unlock()
			t.emit(t.ctx, Event{Kind: EventText, Text: p.Delta})
		}
	case "item/started":
		if item, ok := t.item(msg.Params); ok {
			t.itemStarted(item, t.ours(msg.Params))
		}
	case "item/completed":
		if item, ok := t.item(msg.Params); ok {
			t.itemCompleted(item, t.ours(msg.Params))
		}
	case "thread/tokenUsage/updated":
		var p struct {
			ThreadID   string `json:"threadId"`
			TokenUsage struct {
				Total *codexTokens `json:"total"`
				Last  *codexTokens `json:"last"`
			} `json:"tokenUsage"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			break
		}
		t.mu.Lock()
		th := t.tokens[p.ThreadID]
		if th == nil {
			th = &codexThreadTokens{}
			t.tokens[p.ThreadID] = th
		}
		switch total, last := p.TokenUsage.Total, p.TokenUsage.Last; {
		case total != nil:
			if !th.seen && last != nil {
				th.base = total.minus(*last)
			} else if !th.seen {
				th.base = *total
			}
			th.total, th.seen = *total, true
		case last != nil:
			// Without a running total, add up each response instead.
			th.total, th.seen = th.total.plus(*last), true
		}
		t.mu.Unlock()
	case "turn/completed":
		if !t.ours(msg.Params) {
			// A subagent's turn: the turn goes on.
			break
		}
		var p struct {
			Turn struct {
				Status string `json:"status"`
				Error  *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"turn"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			return
		}
		end := codexTurnEnd{status: p.Turn.Status}
		if p.Turn.Error != nil {
			end.err = p.Turn.Error.Message
		}
		select {
		case t.completed <- end:
		default:
		}
	case "item/autoApprovalReview/started":
		t.mu.Lock()
		t.reviewing++
		t.mu.Unlock()
	case "item/autoApprovalReview/completed":
		t.autoReviewed(msg.Params)
	case "guardianWarning":
		var p struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(msg.Params, &p) != nil || p.Message == "" {
			break
		}
		t.mu.Lock()
		held := t.reviewing > 0
		if held {
			t.reviewNote = p.Message
		}
		t.mu.Unlock()
		if !held {
			t.notice(t.ctx, NoticeWarning, p.Message)
		}
	case "autoApprovalReview/strictReviewRequired":
		t.notice(t.ctx, NoticeInfo, "Codex will have every further command in this turn reviewed before it runs")
	case "warning":
		var p struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(msg.Params, &p) == nil && p.Message != "" {
			t.notice(t.ctx, NoticeWarning, p.Message)
		}
	case "configWarning", "deprecationNotice":
		var p struct {
			Summary string  `json:"summary"`
			Details *string `json:"details"`
			Path    string  `json:"path"`
		}
		if json.Unmarshal(msg.Params, &p) != nil || p.Summary == "" {
			break
		}
		text := p.Summary
		if p.Details != nil && *p.Details != "" {
			text += ": " + *p.Details
		}
		if p.Path != "" {
			text += " (" + p.Path + ")"
		}
		level := NoticeWarning
		if msg.Method == "deprecationNotice" {
			level = NoticeInfo
		}
		t.notice(t.ctx, level, text)
	case "model/rerouted":
		var p struct {
			FromModel string `json:"fromModel"`
			ToModel   string `json:"toModel"`
			Reason    string `json:"reason"`
		}
		if json.Unmarshal(msg.Params, &p) == nil && p.ToModel != "" {
			t.notice(t.ctx, NoticeInfo, fmt.Sprintf("Codex switched this turn from %s to %s (%s)", p.FromModel, p.ToModel, p.Reason))
		}
	case "mcpServer/startupStatus/updated":
		var p struct {
			Name   string  `json:"name"`
			Status string  `json:"status"`
			Error  *string `json:"error"`
		}
		if json.Unmarshal(msg.Params, &p) != nil || p.Status != "failed" {
			break
		}
		t.mu.Lock()
		seen := t.mcpFailed[p.Name]
		t.mcpFailed[p.Name] = true
		t.mu.Unlock()
		if !seen {
			text := "MCP server " + p.Name + " failed to start"
			if p.Error != nil && *p.Error != "" {
				text += ": " + *p.Error
			}
			t.notice(t.ctx, NoticeWarning, text)
		}
	case "error":
		var p struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			WillRetry bool `json:"willRetry"`
		}
		if json.Unmarshal(msg.Params, &p) == nil && p.Error.Message != "" {
			text := p.Error.Message
			if p.WillRetry {
				text += " (retrying)"
			}
			t.emit(t.ctx, Event{Kind: EventError, Text: text})
		}
	}
}

func (t *codexTurn) item(params json.RawMessage) (codexItemView, bool) {
	var p struct {
		Item codexItemView `json:"item"`
	}
	if err := json.Unmarshal(params, &p); err != nil || p.Item.Type == "" {
		return codexItemView{}, false
	}
	return p.Item, true
}

// itemStarted and itemCompleted take the items of the turn's own thread,
// ours, and of its subagents'. A subagent's compaction is none of the
// session's, and what it says is none of the reply.
func (t *codexTurn) itemStarted(item codexItemView, ours bool) {
	switch item.Type {
	case "contextCompaction":
		if ours {
			t.emit(t.ctx, Event{Kind: EventCompaction, Phase: CompactionStart})
		}
	case "subAgentActivity":
		if ours && item.Kind == "started" {
			t.notice(t.ctx, NoticeInfo, "Codex started a subagent, "+path.Base(item.AgentPath)+"; its tool calls are among this turn's")
		}
	case "commandExecution":
		t.remember(item.ID, codexItem{tool: "commandExecution"})
		t.emit(t.ctx, Event{Kind: EventToolCall, Tool: "commandExecution", Input: truncate(item.Command, t.cfg.MaxEventBytes)})
	case "mcpToolCall":
		tool := item.Server + "/" + item.Tool
		t.remember(item.ID, codexItem{tool: tool})
		t.emit(t.ctx, Event{Kind: EventToolCall, Tool: tool, Input: truncate(compactJSON(item.Arguments), t.cfg.MaxEventBytes)})
	case "fileChange":
		paths := make([]string, 0, len(item.Changes))
		for _, c := range item.Changes {
			paths = append(paths, c.Path)
		}
		t.remember(item.ID, codexItem{tool: "fileChange", paths: paths})
	}
}

func (t *codexTurn) itemCompleted(item codexItemView, ours bool) {
	switch item.Type {
	case "contextCompaction":
		if ours {
			t.emit(t.ctx, Event{Kind: EventCompaction, Phase: CompactionEnd})
		}
	case "agentMessage":
		if ours {
			t.mu.Lock()
			t.final = item.Text
			t.mu.Unlock()
		}
	case "commandExecution":
		text := item.AggregatedOutput
		switch {
		case item.Status == "declined":
			text = "declined"
		case item.ExitCode != nil && *item.ExitCode != 0:
			text = strings.TrimRight(text, "\n") + fmt.Sprintf("\n[exit code %d]", *item.ExitCode)
		}
		t.emit(t.ctx, Event{Kind: EventToolResult, Tool: "commandExecution", Text: truncate(text, t.cfg.MaxEventBytes)})
	case "mcpToolCall":
		tool := item.Server + "/" + item.Tool
		text := mcpResultText(item.Result)
		if jsonPresent(item.Error) {
			var e struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(item.Error, &e) != nil || e.Message == "" {
				e.Message = compactJSON(item.Error)
			}
			text = "error: " + e.Message
		}
		t.emit(t.ctx, Event{Kind: EventToolResult, Tool: tool, Text: truncate(text, t.cfg.MaxEventBytes)})
	case "fileChange":
		if item.Status != "completed" {
			return
		}
		for _, c := range item.Changes {
			t.emit(t.ctx, Event{Kind: EventFileChanged, Path: c.Path})
		}
	}
}

// mcpResultText is what an MCP tool call returned, as a person reads it:
// the text of its content blocks, or the whole result when there is none.
func mcpResultText(raw json.RawMessage) string {
	var result struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &result) == nil && len(result.Content) > 0 {
		if text := toolResultText(result.Content); text != "" && text != string(result.Content) {
			return text
		}
	}
	return compactJSON(raw)
}

func (t *codexTurn) remember(id string, item codexItem) {
	t.mu.Lock()
	t.items[id] = item
	t.mu.Unlock()
}

// serveRequest answers a request from the server. Approvals go through the
// hub like any other runtime's; anything else is refused with a JSON-RPC
// error so the server never waits on an answer that will not come.
func (t *codexTurn) serveRequest(msg codexMessage) {
	switch msg.Method {
	case "item/commandExecution/requestApproval":
		var p struct {
			ItemID  string `json:"itemId"`
			Command string `json:"command"`
			Cwd     string `json:"cwd"`
			Reason  string `json:"reason"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		input := map[string]any{"command": p.Command}
		if p.Cwd != "" {
			input["cwd"] = p.Cwd
		}
		if p.Reason != "" {
			input["reason"] = p.Reason
		}
		t.respond(msg.ID, map[string]string{"decision": t.decide("commandExecution", input)})
	case "item/fileChange/requestApproval":
		var p struct {
			ItemID    string `json:"itemId"`
			Reason    string `json:"reason"`
			GrantRoot string `json:"grantRoot"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		input := map[string]any{}
		t.mu.Lock()
		if item, ok := t.items[p.ItemID]; ok {
			input["paths"] = item.paths
		}
		t.mu.Unlock()
		if p.Reason != "" {
			input["reason"] = p.Reason
		}
		if p.GrantRoot != "" {
			input["grantRoot"] = p.GrantRoot
		}
		t.respond(msg.ID, map[string]string{"decision": t.decide("fileChange", input)})
	case "item/tool/requestUserInput":
		t.respond(msg.ID, t.answerQuestions(msg.Params))
	case "mcpServer/elicitation/request":
		t.respond(msg.ID, t.elicit(msg.Params))
	case "item/permissions/requestApproval":
		var p struct {
			Cwd         string          `json:"cwd"`
			Reason      string          `json:"reason"`
			Permissions json.RawMessage `json:"permissions"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		t.respond(msg.ID, t.decidePermissions(p.Permissions, p.Reason, p.Cwd))
	default:
		// Never in silence: people see what was asked and that it was
		// turned down, told before the reply lets Codex move on.
		t.notice(t.ctx, NoticeError, "Codex asked for "+msg.Method+", which Veyloom cannot answer yet; it was told no")
		t.respondError(msg.ID, -32601, "veyloom does not support "+msg.Method)
	}
}

// codexQuestionTool names Codex's questions (its request_user_input tool)
// in approvals.
const codexQuestionTool = "requestUserInput"

// answerQuestions puts request_user_input's questions to people and gives
// Codex what they said, by question id. A person who chooses not to answer
// gives it no answers, which is also what Codex falls back to.
func (t *codexTurn) answerQuestions(params json.RawMessage) map[string]any {
	var p struct {
		Questions []struct {
			ID       string `json:"id"`
			Header   string `json:"header"`
			Question string `json:"question"`
			IsOther  bool   `json:"isOther"`
			IsSecret bool   `json:"isSecret"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	_ = json.Unmarshal(params, &p)
	questions := make([]Question, 0, len(p.Questions))
	asked := make(map[string]bool, len(p.Questions))
	for _, q := range p.Questions {
		options := make([]QuestionOption, len(q.Options))
		for i, o := range q.Options {
			options[i] = QuestionOption{Label: o.Label, Description: o.Description}
		}
		asked[q.ID] = true
		questions = append(questions, Question{ID: q.ID, Header: q.Header, Question: q.Question, Options: options, Other: q.IsOther || len(options) == 0, Secret: q.IsSecret})
	}

	answers := map[string]any{}
	if d, err := t.askQuestions(t.ctx, codexQuestionTool, questions); err == nil {
		for id, a := range d.Answers() {
			if asked[id] && len(a) > 0 {
				answers[id] = map[string][]string{"answers": a}
			}
		}
	}
	return map[string]any{"answers": answers}
}

// elicit puts an MCP server's request to people, a form to fill in or a
// page to open, and answers Codex with the MCP elicitation result. OpenAI's
// own form modes carry a schema too and are shown as forms.
func (t *codexTurn) elicit(params json.RawMessage) map[string]any {
	var p struct {
		ServerName      string          `json:"serverName"`
		Mode            string          `json:"mode"`
		Message         string          `json:"message"`
		RequestedSchema json.RawMessage `json:"requestedSchema"`
		URL             string          `json:"url"`
	}
	_ = json.Unmarshal(params, &p)
	var d Decision
	var err error
	if p.Mode == "url" {
		d, err = t.askLink(t.ctx, elicitationTool, LinkRequest{Server: p.ServerName, Message: p.Message, URL: p.URL})
	} else {
		d, err = t.askForm(t.ctx, elicitationTool, FormRequest{Server: p.ServerName, Message: p.Message, Schema: p.RequestedSchema})
	}
	action := elicitationAction(d, err)
	result := map[string]any{"action": action, "content": nil, "_meta": nil}
	if content := d.Content(); action == ElicitAccept && p.Mode != "url" && content != nil {
		result["content"] = content
	}
	return result
}

// codexReviewer names Codex's own approval reviewer (approvals_reviewer set
// to auto_review or guardian_subagent) in approvals it settled.
const codexReviewer = "codex_auto_review"

// codexVerdicts maps an automatic review's outcome onto a verdict.
var codexVerdicts = map[string]string{
	"approved": VerdictAllowed,
	"denied":   VerdictDenied,
	"timedOut": VerdictExpired,
	"aborted":  VerdictCancelled,
}

// autoReviewed tells people about a request Codex's own reviewer settled:
// what it was about, the verdict, the reviewer's reasoning (or the warning
// it sent a person while reviewing) and the risk it saw.
func (t *codexTurn) autoReviewed(params json.RawMessage) {
	var p struct {
		Review struct {
			Status            string  `json:"status"`
			RiskLevel         *string `json:"riskLevel"`
			UserAuthorization *string `json:"userAuthorization"`
			Rationale         *string `json:"rationale"`
		} `json:"review"`
		Action json.RawMessage `json:"action"`
	}
	err := json.Unmarshal(params, &p)
	t.mu.Lock()
	if t.reviewing > 0 {
		t.reviewing--
	}
	held := t.reviewNote
	t.reviewNote = ""
	t.mu.Unlock()
	if err != nil {
		return
	}
	verdict, ok := codexVerdicts[p.Review.Status]
	if !ok {
		t.notice(t.ctx, NoticeWarning, "Codex's automatic review ended as "+p.Review.Status+": "+held)
		return
	}
	why := deref(p.Review.Rationale)
	if why == "" {
		why = held
	}
	findings := map[string]string{}
	if risk := deref(p.Review.RiskLevel); risk != "" {
		findings["risk"] = risk
	}
	if auth := deref(p.Review.UserAuthorization); auth != "" {
		findings["authorization"] = auth
	}
	var detail json.RawMessage
	if len(findings) > 0 {
		detail, _ = json.Marshal(findings)
	}
	tool, input := codexReviewedAction(p.Action)
	t.reviewed(t.ctx, tool, input, codexReviewer, verdict, why, detail)
}

// codexReviewedAction names what an automatic review was about the way the
// room names Codex's requests: a command as commandExecution with its
// command, a patch as fileChange with its paths, a tool by server/tool.
func codexReviewedAction(raw json.RawMessage) (tool, input string) {
	var a struct {
		Type     string   `json:"type"`
		Command  string   `json:"command"`
		Program  string   `json:"program"`
		Argv     []string `json:"argv"`
		Cwd      string   `json:"cwd"`
		Files    []string `json:"files"`
		Server   string   `json:"server"`
		ToolName string   `json:"toolName"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return "autoReview", compactJSON(raw)
	}
	encode := func(v any) string {
		data, _ := json.Marshal(v)
		return string(data)
	}
	switch a.Type {
	case "command":
		return "commandExecution", encode(map[string]string{"command": a.Command, "cwd": a.Cwd})
	case "execve":
		command := strings.Join(a.Argv, " ")
		if command == "" {
			command = a.Program
		}
		return "commandExecution", encode(map[string]string{"command": command, "cwd": a.Cwd})
	case "applyPatch":
		return "fileChange", encode(map[string]any{"paths": a.Files})
	case "mcpToolCall":
		return a.Server + "/" + a.ToolName, compactJSON(raw)
	case "requestPermissions":
		return codexPermissionsTool, compactJSON(raw)
	case "networkAccess":
		return "network", compactJSON(raw)
	}
	return a.Type, compactJSON(raw)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// codexPermissionsTool names, in approvals, a request for more than the
// sandbox allows: network access, or paths outside the workspace.
const codexPermissionsTool = "permissions"

// decidePermissions asks the hub about a request for more permissions and
// answers it: what was asked for, for this turn only, or nothing.
func (t *codexTurn) decidePermissions(requested json.RawMessage, reason, cwd string) map[string]any {
	var profile struct {
		Network    json.RawMessage `json:"network"`
		FileSystem json.RawMessage `json:"fileSystem"`
	}
	_ = json.Unmarshal(requested, &profile)
	asked := map[string]any{}
	if jsonPresent(profile.Network) {
		asked["network"] = profile.Network
	}
	if jsonPresent(profile.FileSystem) {
		asked["fileSystem"] = profile.FileSystem
	}

	input := map[string]any{}
	for k, v := range asked {
		input[k] = v
	}
	if reason != "" {
		input["reason"] = reason
	}
	if cwd != "" {
		input["cwd"] = cwd
	}
	granted := map[string]any{}
	if t.decide(codexPermissionsTool, input) == "accept" {
		granted = asked
	}
	return map[string]any{"permissions": granted, "scope": "turn"}
}

// jsonPresent reports whether a JSON field was given a value.
func jsonPresent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// decide asks the hub and maps its answer onto the app-server's decision
// vocabulary. A turn that ends while the request is pending declines.
func (t *codexTurn) decide(tool string, input map[string]any) string {
	raw, err := json.Marshal(input)
	if err != nil {
		return "decline"
	}
	d, err := t.requestApproval(t.ctx, tool, string(raw))
	if err != nil || !d.Allow {
		return "decline"
	}
	return "accept"
}

// usage is what the turn has spent so far: in each thread, the turn's own
// and its subagents', the latest total less what it stood at before.
func (t *codexTurn) usage() Usage {
	t.mu.Lock()
	defer t.mu.Unlock()
	var spent codexTokens
	for _, th := range t.tokens {
		spent = spent.plus(th.total.minus(th.base))
	}
	return spent.usage()
}

// codexThreadTokens is what one thread's token reports come to. The
// app-server reports a thread's running total after each model response:
// base is that total before this turn, found from the first report (its
// total less its own response), and total is the latest. The thread spent
// the difference, however many responses it took.
type codexThreadTokens struct {
	base, total codexTokens
	seen        bool
}

// codexTokens is one token breakdown from the app-server. Following OpenAI,
// its input counts cached input too and its output counts reasoning.
type codexTokens struct {
	InputTokens           float64 `json:"inputTokens"`
	CachedInputTokens     float64 `json:"cachedInputTokens"`
	CacheWriteInputTokens float64 `json:"cacheWriteInputTokens"`
	OutputTokens          float64 `json:"outputTokens"`
}

func (a codexTokens) plus(b codexTokens) codexTokens {
	return codexTokens{a.InputTokens + b.InputTokens, a.CachedInputTokens + b.CachedInputTokens, a.CacheWriteInputTokens + b.CacheWriteInputTokens, a.OutputTokens + b.OutputTokens}
}

func (a codexTokens) minus(b codexTokens) codexTokens {
	return codexTokens{a.InputTokens - b.InputTokens, a.CachedInputTokens - b.CachedInputTokens, a.CacheWriteInputTokens - b.CacheWriteInputTokens, a.OutputTokens - b.OutputTokens}
}

// usage splits the cached parts out of the input, taking both to be counted
// inside it, so the parts do not overlap.
func (a codexTokens) usage() Usage {
	cached, written := tokens(a.CachedInputTokens), tokens(a.CacheWriteInputTokens)
	return Usage{
		InputTokens:      max(tokens(a.InputTokens)-cached-written, 0),
		CacheReadTokens:  cached,
		CacheWriteTokens: written,
		OutputTokens:     tokens(a.OutputTokens),
	}
}
