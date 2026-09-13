// Package mcpproxy bridges a stdio MCP client to a streamable HTTP MCP
// server.
//
// Agent CLIs spawn MCP servers as subprocesses and talk to them over
// stdin/stdout, while the tools Veyloom offers an agent live inside the
// worker process. The proxy is the subprocess: it connects to the worker's
// HTTP endpoint, mirrors the tools it finds there and forwards every call.
// Going through stdio also sidesteps the per-request timeouts some CLIs
// put on HTTP servers, which matters for a tool that waits for a person.
package mcpproxy

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Run serves MCP on local, normally stdio, by forwarding to the server at
// endpoint. It returns when the local client disconnects or ctx ends.
func Run(ctx context.Context, endpoint string, local mcp.Transport) error {
	client := mcp.NewClient(&mcp.Implementation{Name: "veyloom-mcp-proxy", Version: "dev"}, nil)
	// The worker never pushes notifications, so the standalone SSE stream
	// would only be an idle connection.
	remote, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", endpoint, err)
	}
	defer remote.Close()

	listed, err := remote.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools of %s: %w", endpoint, err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "veyloom", Version: "dev"}, nil)
	for _, tool := range listed.Tools {
		name := tool.Name
		server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return remote.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: req.Params.Arguments})
		})
	}
	return server.Run(ctx, local)
}
