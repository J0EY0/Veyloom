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
	// ProxyBinary is the executable Codex runs as the MCP bridge to the
	// turn's tools: veyloom itself, run as `mcp-proxy`. Empty means the
	// running executable.
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
	if spec.Host != nil {
		proxy := r.cfg.ProxyBinary
		if proxy == "" {
			path, err := os.Executable()
			if err != nil {
				return nil, fmt.Errorf("codex: locate own executable for the MCP proxy: %w", err)
			}
			proxy = path
		}
		ep, err := r.tools.register(spec.Host, nil)
		if err != nil {
			return nil, fmt.Errorf("codex: tool endpoint: %w", err)
		}
		token = ep.token
		command, args := mcpProxyServer(proxy, ep.MCP)
		mcpServer = map[string]any{
			"command": command,
			"args":    args,
			// They only read the turn's own room: nothing to ask about.
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
		turnBase:  newTurnBase(cancel),
		ctx:       ctx,
		stopProc:  stopProc,
		cfg:       r.cfg,
		proc:      proc,
		pending:   make(map[int64]chan codexMessage),
		eof:       make(chan struct{}),
		completed: make(chan codexTurnEnd, 1),
		items:     make(map[string]codexItem),
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
	// The app-server reports the thread's running token total after each
	// model response. base is that total before this turn, found from the
	// first report (its total less its own response); total is the latest.
	// The turn spent the difference, however many responses it took.
	tokensBase  codexTokens
	tokensTotal codexTokens
	tokensSeen  bool

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
}

// notification turns a server notification into events and state.
func (t *codexTurn) notification(msg codexMessage) {
	switch msg.Method {
	case "item/agentMessage/delta":
		var p struct {
			Delta string `json:"delta"`
		}
		if json.Unmarshal(msg.Params, &p) == nil && p.Delta != "" {
			t.mu.Lock()
			t.text.WriteString(p.Delta)
			t.mu.Unlock()
			t.emit(t.ctx, Event{Kind: EventText, Text: p.Delta})
		}
	case "item/started":
		if item, ok := t.item(msg.Params); ok {
			t.itemStarted(item)
		}
	case "item/completed":
		if item, ok := t.item(msg.Params); ok {
			t.itemCompleted(item)
		}
	case "thread/tokenUsage/updated":
		var p struct {
			TokenUsage struct {
				Total *codexTokens `json:"total"`
				Last  *codexTokens `json:"last"`
			} `json:"tokenUsage"`
		}
		if json.Unmarshal(msg.Params, &p) != nil {
			break
		}
		t.mu.Lock()
		switch total, last := p.TokenUsage.Total, p.TokenUsage.Last; {
		case total != nil:
			if !t.tokensSeen && last != nil {
				t.tokensBase = total.minus(*last)
			} else if !t.tokensSeen {
				t.tokensBase = *total
			}
			t.tokensTotal, t.tokensSeen = *total, true
		case last != nil:
			// Without a running total, add up each response instead.
			t.tokensTotal, t.tokensSeen = t.tokensTotal.plus(*last), true
		}
		t.mu.Unlock()
	case "turn/completed":
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

func (t *codexTurn) itemStarted(item codexItemView) {
	switch item.Type {
	case "contextCompaction":
		t.emit(t.ctx, Event{Kind: EventCompaction, Phase: CompactionStart})
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

func (t *codexTurn) itemCompleted(item codexItemView) {
	switch item.Type {
	case "contextCompaction":
		t.emit(t.ctx, Event{Kind: EventCompaction, Phase: CompactionEnd})
	case "agentMessage":
		t.mu.Lock()
		t.final = item.Text
		t.mu.Unlock()
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
		text := compactJSON(item.Result)
		if len(item.Error) > 0 && string(item.Error) != "null" {
			text = "error: " + compactJSON(item.Error)
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
	default:
		t.respondError(msg.ID, -32601, "veyloom does not support "+msg.Method)
	}
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

// usage is what the turn has spent so far: the thread's latest total less
// what it stood at before the turn.
func (t *codexTurn) usage() Usage {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tokensTotal.minus(t.tokensBase).usage()
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
