// Command elicitmcp is an MCP server for the Codex smoke test: its tools
// ask the person through MCP elicitation, one for a form and one for a link
// to open, and return what came back, so the test can see a runtime's
// elicitation reach a person and their answer reach the server.
package main

import (
	"context"
	"encoding/json"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type none struct{}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "elicit", Version: "test"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "deploy_form",
		Description: "Asks the user, through a form, which region to deploy to, and returns what they said.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ none) (*mcp.CallToolResult, any, error) {
		return answered(req.Session.Elicit(ctx, &mcp.ElicitParams{
			Message: "Which region should this deploy go to?",
			RequestedSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"region": map[string]any{"type": "string", "title": "Region", "enum": []string{"eu", "us"}},
				},
				"required": []string{"region"},
			},
		}))
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        "sign_in",
		Description: "Asks the user to open a sign-in page, and returns whether they did.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ none) (*mcp.CallToolResult, any, error) {
		return answered(req.Session.Elicit(ctx, &mcp.ElicitParams{
			Mode:          "url",
			Message:       "Sign in to finish the deploy",
			URL:           "https://example.com/device",
			ElicitationID: "sign-in-1",
		}))
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

// answered returns an elicitation's outcome as the tool's text: the action,
// and the content for a form.
func answered(res *mcp.ElicitResult, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	text := res.Action
	if len(res.Content) > 0 {
		content, _ := json.Marshal(res.Content)
		text += " " + string(content)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}
