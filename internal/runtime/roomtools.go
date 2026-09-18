package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The room tools are how an agent reads its project's chat on request
// (design.md 5.7). A brief pushes what is new; everything else, older or in
// another topic, the agent fetches itself. They only read, and only the
// turn's own room, so every permission preset has them and none asks.
//
// The same four tools reach each runtime its own way: Claude Code and Codex
// as MCP tools on the turn's endpoint, Pi through an extension that posts to
// the endpoint's plain JSON route. All of them end in TurnHost.QueryRoom.
const (
	RoomToolListTopics = "list_topics"
	RoomToolReadTopic  = "read_topic"
	RoomToolReadRoom   = "read_room"
	RoomToolSearch     = "search_messages"
)

// RoomToolNames lists the room tools in the order they are presented.
var RoomToolNames = []string{RoomToolListTopics, RoomToolReadTopic, RoomToolReadRoom, RoomToolSearch}

// RoomQuery is one call of a room tool. It travels over the protocol to the
// hub, which answers for the room of the turn that asked.
type RoomQuery struct {
	// Tool is one of the RoomTool constants.
	Tool string `json:"tool"`
	// Topic is the number of the topic to read (read_topic).
	Topic int `json:"topic,omitempty"`
	// Before pages backwards: only what is older than this seq, as given at
	// the end of an answer that had more to show. Zero starts at the newest.
	Before int64 `json:"before,omitempty"`
	// Limit caps the items of the answer; zero takes the hub's default.
	Limit int `json:"limit,omitempty"`
	// Text is what to search for (search_messages).
	Text string `json:"text,omitempty"`
}

// TurnHost is what a running turn may ask of the machine it runs on. The
// machine passes one with each turn; tests and hosts with no hub pass none,
// and the turn then simply has no room tools.
type TurnHost interface {
	// QueryRoom answers a room tool call with text for the agent to read.
	QueryRoom(ctx context.Context, q RoomQuery) (string, error)
}

// roomToolSpec describes one room tool to a runtime: MCP servers and the Pi
// extension are both generated from these, so the two cannot drift apart.
type roomToolSpec struct {
	Name        string
	Description string
	// Params names the RoomQuery fields the tool takes.
	Params []roomToolParam
}

type roomToolParam struct {
	Name        string // JSON name, as in RoomQuery
	Type        string // "integer" or "string"
	Description string
	Required    bool
}

var (
	paramBefore = roomToolParam{Name: "before", Type: "integer", Description: "Show only what is older than this position; use the value an earlier answer gave for its next page."}
	paramLimit  = roomToolParam{Name: "limit", Type: "integer", Description: "How many items to show at most."}
)

// roomToolSpecs are the room tools as an agent sees them.
var roomToolSpecs = []roomToolSpec{
	{
		Name:        RoomToolListTopics,
		Description: "List the topics of this project's chat, most recently active first: number, title, replies, last message. Topics are written #12 in the chat; read one with read_topic.",
		Params:      []roomToolParam{paramBefore, paramLimit},
	},
	{
		Name:        RoomToolReadTopic,
		Description: "Read a topic of this project's chat by its number: who said what, oldest first, and which files each agent turn changed. Shows the latest messages; page back with before.",
		Params:      []roomToolParam{{Name: "topic", Type: "integer", Description: "The topic's number, the 12 of #12.", Required: true}, paramBefore, paramLimit},
	},
	{
		Name:        RoomToolReadRoom,
		Description: "Read the top-level messages of this project's chat, oldest first: what people asked for and how agents answered, each with the number of the topic it opened. Shows the latest; page back with before.",
		Params:      []roomToolParam{paramBefore, paramLimit},
	},
	{
		Name:        RoomToolSearch,
		Description: "Search every message of this project's chat for a phrase, newest first, each hit with the number of its topic. The phrase is matched as written, ignoring case.",
		Params:      []roomToolParam{{Name: "text", Type: "string", Description: "The phrase to look for.", Required: true}, paramBefore, paramLimit},
	},
}

// inputSchema is the tool's parameters as a JSON Schema object.
func (s roomToolSpec) inputSchema() map[string]any {
	props := make(map[string]any, len(s.Params))
	required := []string{}
	for _, p := range s.Params {
		props[p.Name] = map[string]any{"type": p.Type, "description": p.Description}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

// errNoRoom is what a room tool answers when the turn has no host to ask.
var errNoRoom = errors.New("this turn has no connection to the project's chat")

// runRoomTool turns a tool call's arguments into a query and asks the host.
// What goes wrong becomes the tool's answer: the agent can read it and try
// something else, where an error would only abort its call.
func runRoomTool(ctx context.Context, host TurnHost, tool string, args json.RawMessage) string {
	if host == nil {
		return "error: " + errNoRoom.Error()
	}
	q := RoomQuery{Tool: tool}
	if len(args) > 0 && string(args) != "null" {
		if err := json.Unmarshal(args, &q); err != nil {
			return "error: bad arguments: " + err.Error()
		}
	}
	q.Tool = tool
	text, err := host.QueryRoom(ctx, q)
	if err != nil {
		return "error: " + err.Error()
	}
	return text
}

// addRoomTools registers the room tools on a turn's MCP server, marked
// read-only: Claude Code's plan mode lets such tools through, and Codex
// asks for no approval.
func addRoomTools(server *mcp.Server, host TurnHost) {
	for _, spec := range roomToolSpecs {
		name := spec.Name
		server.AddTool(&mcp.Tool{
			Name:        name,
			Description: spec.Description,
			InputSchema: spec.inputSchema(),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, Title: name},
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			text := runRoomTool(ctx, host, name, req.Params.Arguments)
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil
		})
	}
}

// roomToolCall is the body of the endpoint's plain JSON route, for runtimes
// without MCP.
type roomToolCall struct {
	Tool string          `json:"tool"`
	Args json.RawMessage `json:"args"`
}

// serveRoomTool answers one call on the plain JSON route.
func serveRoomTool(host TurnHost) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var call roomToolCall
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&call); err != nil {
			http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
			return
		}
		known := false
		for _, name := range RoomToolNames {
			known = known || name == call.Tool
		}
		if !known {
			http.Error(w, fmt.Sprintf("unknown tool %q", call.Tool), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": runRoomTool(r.Context(), host, call.Tool, call.Args)})
	})
}
