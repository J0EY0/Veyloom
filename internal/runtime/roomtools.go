package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The room tools are how an agent reads its project's chat on request
// (design.md 5.7). A brief pushes what is new; everything else, older or in
// another topic, the agent fetches itself. They only read, and only the
// turn's own room, so every permission preset has them and none asks.
//
// The same tools reach each runtime its own way: Claude Code and Codex as
// MCP tools on the turn's endpoint, Pi through an extension that posts to
// the endpoint's plain JSON route. All of them end in TurnHost.QueryRoom.
const (
	RoomToolListTopics  = "list_topics"
	RoomToolReadTopic   = "read_topic"
	RoomToolReadTurn    = "read_turn"
	RoomToolReadMessage = "read_message"
	RoomToolReadRoom    = "read_room"
	RoomToolSearch      = "search_messages"
)

// RoomToolNames lists the room tools in the order they are presented.
var RoomToolNames = []string{RoomToolListTopics, RoomToolReadTopic, RoomToolReadTurn, RoomToolReadMessage, RoomToolReadRoom, RoomToolSearch}

// AgentToolNames lists every tool a turn gets from Veyloom: the room tools,
// then the wiki tools (wikitools.go). The memory tools (memorytools.go)
// are a turn's only while the person uses a memory (docs/design.md 5.19),
// so they come as ExtraTools.
var AgentToolNames = append(append([]string{}, RoomToolNames...), WikiToolNames...)

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
	// Args are the call's arguments as the agent gave them, for the tools
	// whose arguments only the hub reads (the wiki tools).
	Args json.RawMessage `json:"args,omitempty"`
}

// TurnHost is what a running turn may ask of the machine it runs on. The
// machine passes one with each turn; tests and hosts with no hub pass none,
// and the turn then simply has no room tools.
type TurnHost interface {
	// QueryRoom answers a room tool call with text for the agent to read.
	QueryRoom(ctx context.Context, q RoomQuery) (string, error)
}

// roomToolSpec describes one tool Veyloom gives a turn: MCP servers and the
// Pi extension are both generated from these, so the two cannot drift
// apart.
type roomToolSpec struct {
	Name        string
	Description string
	// Params are the tool's arguments: RoomQuery fields, or with RawArgs
	// whatever the hub reads.
	Params []roomToolParam
	// ReadOnly tools change nothing. Claude Code's plan mode lets them
	// through and they are marked so to every MCP client.
	ReadOnly bool
	// RawArgs sends the arguments to the hub as they came, in RoomQuery.Args.
	RawArgs bool
}

type roomToolParam struct {
	Name        string // JSON name, as in RoomQuery
	Type        string // "integer", "string" or "boolean"; see Schema for anything else
	Description string
	Required    bool
	// Enum limits a string to these values.
	Enum []string
	// Schema is the JSON Schema of an argument that is not a plain integer
	// or string; it takes the place of Type and Enum.
	Schema map[string]any
}

var (
	paramBefore = roomToolParam{Name: "before", Type: "integer", Description: "Show only what is older than this position; use the value an earlier answer gave for its next page."}
	paramLimit  = roomToolParam{Name: "limit", Type: "integer", Description: "How many items to show at most."}
)

// roomToolSpecs are the room tools as an agent sees them.
var roomToolSpecs = []roomToolSpec{
	{
		Name:        RoomToolListTopics,
		ReadOnly:    true,
		Description: "List the topics of this project's chat, most recently active first: number, title, replies, last message. Topics are written #12 in the chat; read one with read_topic.",
		Params:      []roomToolParam{paramBefore, paramLimit},
	},
	{
		Name:        RoomToolReadTopic,
		ReadOnly:    true,
		Description: "Read a topic of this project's chat by its number: who said what, oldest first, and each agent turn's id and the files it changed. Shows the latest messages; page back with before.",
		Params:      []roomToolParam{{Name: "topic", Type: "integer", Description: "The topic's number, the 12 of #12.", Required: true}, paramBefore, paramLimit},
	},
	{
		Name: RoomToolReadTurn, ReadOnly: true, RawArgs: true,
		Description: "Read what happened in one agent turn of this project's chat, yours or another member's: what it was asked, what the agent said and which tools it called with what, " +
			"how it ended, and the next thing a person said in that topic afterwards, which is where a correction usually is. " +
			"For what a reply leaves out, such as the commands run and what came of them, or what you did yourself before your context was compacted. " +
			"read_topic gives each agent turn's id. A wiki maintainer also reads other projects' turns that used a skill its team owns.",
		Params: []roomToolParam{
			{Name: "turn", Type: "string", Required: true, Description: "The turn's id, as read_topic, list_turns or a brief gives it."},
		},
	},
	{
		Name: RoomToolReadMessage, ReadOnly: true, RawArgs: true,
		Description: "Read one message of this project's chat whole, by its id: who said it, when, where, all of its text and the files it carries. " +
			"A brief or another tool cuts a long message short and gives its id for this.",
		Params: []roomToolParam{
			{Name: "message", Type: "string", Required: true, Description: "The message's id, as the message cut short gives it."},
		},
	},
	{
		Name:        RoomToolReadRoom,
		ReadOnly:    true,
		Description: "Read the top-level messages of this project's chat, oldest first: what people asked for and how agents answered, each with the number of the topic it opened. Shows the latest; page back with before.",
		Params:      []roomToolParam{paramBefore, paramLimit},
	},
	{
		Name:        RoomToolSearch,
		ReadOnly:    true,
		Description: "Search every message of this project's chat for a phrase, newest first, each hit with the number of its topic. The phrase is matched as written, ignoring case.",
		Params:      []roomToolParam{{Name: "text", Type: "string", Description: "The phrase to look for.", Required: true}, paramBefore, paramLimit},
	},
}

// agentToolSpecs are every tool a turn gets, in the order presented.
var agentToolSpecs = append(append([]roomToolSpec{}, roomToolSpecs...), wikiToolSpecs...)

// agentToolSpec finds a tool by name, among every turn's and the optional
// ones.
func agentToolSpec(name string) (roomToolSpec, bool) {
	for _, specs := range [][]roomToolSpec{agentToolSpecs, optionalToolSpecs} {
		for _, s := range specs {
			if s.Name == name {
				return s, true
			}
		}
	}
	return roomToolSpec{}, false
}

// inputSchema is the tool's parameters as a JSON Schema object.
func (s roomToolSpec) inputSchema() map[string]any {
	props := make(map[string]any, len(s.Params))
	required := []string{}
	for _, p := range s.Params {
		prop := map[string]any{"type": p.Type}
		if p.Schema != nil {
			prop = make(map[string]any, len(p.Schema)+1)
			for k, v := range p.Schema {
				prop[k] = v
			}
		} else if len(p.Enum) > 0 {
			prop["enum"] = p.Enum
		}
		prop["description"] = p.Description
		props[p.Name] = prop
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
	spec, _ := agentToolSpec(tool)
	switch {
	case len(args) == 0 || string(args) == "null":
	case spec.RawArgs:
		if !json.Valid(args) {
			return "error: bad arguments: not JSON"
		}
		q.Args = args
	default:
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

// addRoomTools registers the tools a turn gets on its MCP server: every
// turn's, and the optional ones extra names. The ones that only read are
// marked so: Claude Code's plan mode lets such tools through. Codex asks
// for no approval for any of them (see the server's config in
// codex_runner.go).
func addRoomTools(server *mcp.Server, host TurnHost, extra []string) {
	for _, spec := range turnToolSpecs(extra) {
		name := spec.Name
		server.AddTool(&mcp.Tool{
			Name:        name,
			Description: spec.Description,
			InputSchema: spec.inputSchema(),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: spec.ReadOnly, Title: name},
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

// serveRoomTool answers one call on the plain JSON route, for a turn given
// the optional tools extra names.
func serveRoomTool(host TurnHost, extra []string) http.Handler {
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
		if !slices.Contains(turnToolNames(extra), call.Tool) {
			http.Error(w, fmt.Sprintf("unknown tool %q", call.Tool), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": runRoomTool(r.Context(), host, call.Tool, call.Args)})
	})
}
