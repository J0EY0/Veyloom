package runtime

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Claude Code asks permission through an MCP tool named with
// --permission-prompt-tool. The machine serves that tool itself, on the
// turn's MCP server (see toolEndpoint), which the CLI reaches through the
// stdio proxy (internal/mcpproxy). Tools appear on the CLI command line as
// mcp__<server>__<tool>.
const (
	claudeMCPServerName = toolServerName
	claudeApprovalTool  = "approval_prompt"
)

// claudeToolName is how the CLI names one of the turn's MCP tools.
func claudeToolName(tool string) string {
	return "mcp__" + claudeMCPServerName + "__" + tool
}

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

// addApprovalTool registers the permission prompt on a turn's MCP server.
func (t *claudeTurn) addApprovalTool(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        claudeApprovalTool,
		Description: "Asks the people in the Veyloom room whether the agent may use a tool.",
	}, t.approvalPrompt)
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
	command, args := mcpProxyServer(proxy, endpoint)
	cfg := map[string]any{
		"mcpServers": map[string]any{
			claudeMCPServerName: map[string]any{
				"type":    "stdio",
				"command": command,
				"args":    args,
			},
		},
	}
	data, _ := json.Marshal(cfg)
	return string(data)
}
