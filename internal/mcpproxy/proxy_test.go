package mcpproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoInput struct {
	Text string `json:"text"`
}

// remoteServer is an HTTP MCP server with one tool that echoes its input
// and fails on request.
func remoteServer(t *testing.T) string {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "remote", Version: "test"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "echo", Description: "says it back"}, func(_ context.Context, _ *mcp.CallToolRequest, in echoInput) (*mcp.CallToolResult, any, error) {
		if in.Text == "boom" {
			return nil, nil, context.Canceled
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "echo: " + in.Text}}}, nil, nil
	})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestProxy_MirrorsToolsAndForwardsCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	endpoint := remoteServer(t)

	serverEnd, clientEnd := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, endpoint, serverEnd) }()

	session, err := mcp.NewClient(&mcp.Implementation{Name: "cli", Version: "test"}, nil).Connect(ctx, clientEnd, nil)
	if err != nil {
		t.Fatal(err)
	}

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "echo" || tools.Tools[0].Description != "says it back" {
		t.Fatalf("tools = %+v", tools.Tools)
	}
	schema, _ := tools.Tools[0].InputSchema.(map[string]any)
	if props, _ := schema["properties"].(map[string]any); props["text"] == nil {
		t.Errorf("the remote input schema should be mirrored, got %v", tools.Tools[0].InputSchema)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if text := res.Content[0].(*mcp.TextContent).Text; text != "echo: hi" || res.IsError {
		t.Errorf("result = %q (isError %v)", text, res.IsError)
	}

	// Tool errors come back as tool errors, not protocol failures.
	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "echo", Arguments: map[string]any{"text": "boom"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "canceled") {
		t.Errorf("expected the remote tool error to be forwarded, got %+v", res)
	}

	// Closing the local side ends the proxy.
	session.Close()
	if err := <-done; err != nil {
		t.Errorf("Run returned %v after the client left", err)
	}
}

func TestProxy_UnreachableEndpoint(t *testing.T) {
	serverEnd, _ := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := Run(ctx, "http://127.0.0.1:1/mcp", serverEnd); err == nil {
		t.Error("expected an error for an endpoint nothing listens on")
	}
}
