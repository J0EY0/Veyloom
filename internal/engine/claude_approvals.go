package engine

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Claude Code asks permission through an MCP tool named with
// --permission-prompt-tool. The worker serves that tool itself: each turn
// that may need approvals gets an MCP server bound to it, reachable at a
// URL only that turn knows, and the CLI reaches it through the stdio proxy
// (see internal/mcpproxy). The names below appear on the CLI command line
// as mcp__<server>__<tool>.
const (
	claudeMCPServerName = "veyloom"
	claudeApprovalTool  = "approval_prompt"
)

// claudePermissionPrompt is what Claude Code 2.1.x sends to its permission
// prompt tool.
type claudePermissionPrompt struct {
	ToolName  string         `json:"tool_name"`
	Input     map[string]any `json:"input"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
}

// claudeAllow and claudeDeny are the two replies the CLI accepts, as a
// JSON string in a single text content block. updatedInput is required on
// allow; it is the input the tool then runs with.
type claudeAllow struct {
	Behavior     string         `json:"behavior"`
	UpdatedInput map[string]any `json:"updatedInput"`
}

type claudeDeny struct {
	Behavior string `json:"behavior"`
	Message  string `json:"message"`
}

// claudeApprovals is the HTTP MCP endpoint a ClaudeRunner keeps for its
// turns. One listener on the loopback interface serves every turn; the
// path carries a random token that selects the turn's server.
type claudeApprovals struct {
	once     sync.Once
	startErr error

	mu    sync.Mutex
	srv   *http.Server
	base  string
	turns map[string]*mcp.Server // by token
}

func newClaudeApprovals() *claudeApprovals {
	return &claudeApprovals{turns: make(map[string]*mcp.Server)}
}

// start binds the listener on first use, so a runner that never needs
// approvals never opens a port.
func (a *claudeApprovals) start() error {
	a.once.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			a.startErr = err
			return
		}
		mux := http.NewServeMux()
		mux.Handle("/turns/{token}/mcp", mcp.NewStreamableHTTPHandler(a.serverFor, nil))
		a.mu.Lock()
		a.srv = &http.Server{Handler: mux}
		a.base = "http://" + ln.Addr().String()
		a.mu.Unlock()
		go a.srv.Serve(ln)
	})
	return a.startErr
}

// serverFor picks the turn named by the request path; nil makes the
// handler answer 400.
func (a *claudeApprovals) serverFor(r *http.Request) *mcp.Server {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.turns[r.PathValue("token")]
}

// register gives t its own MCP server and returns the endpoint and token.
func (a *claudeApprovals) register(t *claudeTurn) (endpoint, token string, err error) {
	if err := a.start(); err != nil {
		return "", "", err
	}
	server := mcp.NewServer(&mcp.Implementation{Name: claudeMCPServerName, Version: "dev"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        claudeApprovalTool,
		Description: "Asks the people in the Veyloom room whether the agent may use a tool.",
	}, t.approvalPrompt)

	token = randomHex(16)
	a.mu.Lock()
	a.turns[token] = server
	endpoint = a.base + "/turns/" + token + "/mcp"
	a.mu.Unlock()
	return endpoint, token, nil
}

// unregister forgets a turn's server and drops the CLI's sessions on it.
func (a *claudeApprovals) unregister(token string) {
	a.mu.Lock()
	server := a.turns[token]
	delete(a.turns, token)
	a.mu.Unlock()
	if server == nil {
		return
	}
	for session := range server.Sessions() {
		_ = session.Close()
	}
}

// close stops the listener. Turns still running lose their endpoint.
func (a *claudeApprovals) close() error {
	a.mu.Lock()
	srv := a.srv
	a.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Close()
}

// approvalPrompt is the MCP tool the CLI calls. It raises an approval
// request on the turn and blocks until the hub answers, or until the turn
// or the CLI's call ends, in which case the answer is a denial.
func (t *claudeTurn) approvalPrompt(ctx context.Context, _ *mcp.CallToolRequest, in claudePermissionPrompt) (*mcp.CallToolResult, any, error) {
	if in.Input == nil {
		in.Input = map[string]any{}
	}
	input, err := json.Marshal(in.Input)
	if err != nil {
		return nil, nil, err
	}

	// Wait for the turn's lifetime, but stop if the CLI abandons the call.
	waitCtx, cancel := context.WithCancel(t.ctx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()

	var reply any
	switch d, err := t.requestApproval(waitCtx, in.ToolName, string(input)); {
	case err != nil:
		reply = claudeDeny{Behavior: "deny", Message: "the turn ended before anyone decided"}
	case d.Allow:
		reply = claudeAllow{Behavior: "allow", UpdatedInput: in.Input}
	default:
		msg := d.Message
		if msg == "" {
			msg = "denied by a Veyloom user"
		}
		reply = claudeDeny{Behavior: "deny", Message: msg}
	}
	text, err := json.Marshal(reply)
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(text)}}}, nil, nil
}

// claudeMCPConfig is the document passed to --mcp-config: one stdio server
// that runs the proxy pointed at the turn's endpoint.
func claudeMCPConfig(proxy, endpoint string) string {
	cfg := map[string]any{
		"mcpServers": map[string]any{
			claudeMCPServerName: map[string]any{
				"type":    "stdio",
				"command": proxy,
				"args":    []string{"mcp-proxy", "--url", endpoint},
			},
		},
	}
	data, _ := json.Marshal(cfg)
	return string(data)
}
