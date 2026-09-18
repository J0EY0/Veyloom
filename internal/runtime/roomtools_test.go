package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// recordingHost answers every room query with a line naming it, and keeps
// what it was asked.
type recordingHost struct {
	mu    sync.Mutex
	asked []RoomQuery
	err   error
}

func (h *recordingHost) QueryRoom(_ context.Context, q RoomQuery) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.asked = append(h.asked, q)
	if h.err != nil {
		return "", h.err
	}
	return "answer to " + q.Tool, nil
}

func (h *recordingHost) last(t *testing.T) RoomQuery {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.asked) == 0 {
		t.Fatal("the host was never asked")
	}
	return h.asked[len(h.asked)-1]
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("content = %+v, want one text block", res.Content)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content = %T, want text", res.Content[0])
	}
	return text.Text
}

// connectTools opens an MCP session on a turn's endpoint, as the CLI does
// through the proxy.
func connectTools(t *testing.T, url string) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "fake-cli", Version: "test"}, nil).
		Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestRoomTools_OverMCP(t *testing.T) {
	e := newToolEndpoint()
	t.Cleanup(func() { e.close() })
	host := &recordingHost{}
	ep, err := e.register(host, nil)
	if err != nil {
		t.Fatal(err)
	}
	session := connectTools(t, ep.MCP)
	ctx := context.Background()

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
		// Read-only, said where the CLIs look: what lets them through
		// Claude Code's plan mode and past Codex's approvals.
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("%s has no description", tool.Name)
		}
	}
	// The server lists them by name; which ones matters, not the order.
	want := append([]string{}, RoomToolNames...)
	sort.Strings(names)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("tools = %v, want %v", names, want)
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: RoomToolReadTopic, Arguments: map[string]any{"topic": 12, "before": 340, "limit": 5}})
	if err != nil {
		t.Fatal(err)
	}
	if got := textOf(t, res); got != "answer to read_topic" {
		t.Errorf("answer = %q", got)
	}
	if q := host.last(t); q.Tool != RoomToolReadTopic || q.Topic != 12 || q.Before != 340 || q.Limit != 5 {
		t.Errorf("the host was asked %+v", q)
	}

	// No arguments at all is a call too.
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{Name: RoomToolListTopics}); err != nil {
		t.Fatal(err)
	}
	if q := host.last(t); q != (RoomQuery{Tool: RoomToolListTopics}) {
		t.Errorf("the host was asked %+v", q)
	}

	// What goes wrong is an answer the agent can read, not a broken call.
	host.err = errors.New("this chat has no topic #99")
	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: RoomToolReadTopic, Arguments: map[string]any{"topic": 99}})
	if err != nil {
		t.Fatal(err)
	}
	if got := textOf(t, res); got != "error: this chat has no topic #99" {
		t.Errorf("answer = %q", got)
	}
}

func TestRoomTools_ArgumentsCannotPickTheTool(t *testing.T) {
	host := &recordingHost{}
	runRoomTool(context.Background(), host, RoomToolReadRoom, json.RawMessage(`{"tool":"search_messages","limit":3}`))
	if q := host.last(t); q.Tool != RoomToolReadRoom || q.Limit != 3 {
		t.Errorf("the host was asked %+v, want read_room whatever the arguments say", q)
	}
	if got := runRoomTool(context.Background(), host, RoomToolReadRoom, json.RawMessage(`{"limit":"many"}`)); !strings.HasPrefix(got, "error: bad arguments") {
		t.Errorf("bad arguments = %q", got)
	}
	if got := runRoomTool(context.Background(), nil, RoomToolReadRoom, nil); !strings.Contains(got, "no connection") {
		t.Errorf("without a host = %q", got)
	}
}

func TestRoomTools_OverPlainJSON(t *testing.T) {
	e := newToolEndpoint()
	t.Cleanup(func() { e.close() })
	host := &recordingHost{}
	ep, err := e.register(host, nil)
	if err != nil {
		t.Fatal(err)
	}
	post := func(url, body string) (int, string) {
		t.Helper()
		res, err := http.Post(url, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out struct {
			Text string `json:"text"`
		}
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out.Text
	}

	code, text := post(ep.Room, `{"tool":"search_messages","args":{"text":"rate limit","limit":4}}`)
	if code != http.StatusOK || text != "answer to search_messages" {
		t.Errorf("status %d, text %q", code, text)
	}
	if q := host.last(t); q.Tool != RoomToolSearch || q.Text != "rate limit" || q.Limit != 4 {
		t.Errorf("the host was asked %+v", q)
	}
	if code, _ := post(ep.Room, `{"tool":"delete_everything"}`); code != http.StatusBadRequest {
		t.Errorf("an unknown tool: status %d, want 400", code)
	}
	if code, _ := post(strings.Replace(ep.Room, ep.token, strings.Repeat("0", 32), 1), `{"tool":"read_room"}`); code != http.StatusNotFound {
		t.Errorf("another turn's token: status %d, want 404", code)
	}
	// Once the turn is over its tools are gone.
	e.unregister(ep.token)
	if code, _ := post(ep.Room, `{"tool":"read_room"}`); code != http.StatusNotFound {
		t.Errorf("after the turn: status %d, want 404", code)
	}
}

func TestToolEndpoint_NoHostNoRoomTools(t *testing.T) {
	e := newToolEndpoint()
	t.Cleanup(func() { e.close() })
	ep, err := e.register(nil, func(s *mcp.Server) {
		mcp.AddTool(s, &mcp.Tool{Name: "only"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{}, nil, nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := connectTools(t, ep.MCP).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].Name != "only" {
		t.Errorf("tools = %+v, want only what the runner added", listed.Tools)
	}
}

func TestClaude_RoomToolsInEveryPreset(t *testing.T) {
	for _, preset := range []string{PermissionReadOnly, PermissionEditWithApproval, PermissionFullAuto} {
		t.Run(preset, func(t *testing.T) {
			argsPath, _ := fakeClaudeCLI(t, claudeFixture, 0, "")
			runner := NewClaudeRunner(ClaudeConfig{ProxyBinary: "/opt/veyloom"})
			t.Cleanup(func() { runner.Close() })
			turn, err := runner.StartTurn(context.Background(), TurnSpec{Prompt: "hi", Permission: preset, Host: &recordingHost{}})
			if err != nil {
				t.Fatal(err)
			}
			drain(t, turn)
			raw, _ := os.ReadFile(argsPath)
			args := strings.Split(strings.TrimSpace(string(raw)), "\n")

			mcpEndpoint(t, flagValue(args, "--mcp-config"), "/opt/veyloom")
			want := "mcp__veyloom__list_topics,mcp__veyloom__read_topic,mcp__veyloom__read_room,mcp__veyloom__search_messages"
			if got := flagValue(args, "--allowedTools"); got != want {
				t.Errorf("--allowedTools = %q, want %q", got, want)
			}
			// The permission prompt stays with the one preset that prompts.
			if got := flagValue(args, "--permission-prompt-tool") != ""; got != (preset == PermissionEditWithApproval) {
				t.Errorf("--permission-prompt-tool present = %v", got)
			}
		})
	}
}

func TestClaude_NoHostNoToolFlags(t *testing.T) {
	argsPath, _ := fakeClaudeCLI(t, claudeFixture, 0, "")
	if _, _, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "hi", Permission: PermissionReadOnly}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(argsPath)
	for _, flag := range []string{"--mcp-config", "--allowedTools"} {
		if strings.Contains(string(raw), flag) {
			t.Errorf("a turn with no host to ask has no use for %s: %s", flag, raw)
		}
	}
}

func TestCodex_RoomToolsServerInTheThreadConfig(t *testing.T) {
	for _, session := range []Session{{}, {Key: "key-1", Ref: "thr-old", Resume: true}} {
		h := newCodexHarness(t)
		h.runner.cfg.ProxyBinary = "/opt/veyloom"
		t.Cleanup(func() { h.runner.Close() })
		turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "hi", Permission: PermissionReadOnly, Session: session, Host: &recordingHost{}})
		if err != nil {
			t.Fatal(err)
		}
		drain(t, turn)
		if _, err := turn.Result(); err != nil {
			t.Fatal(err)
		}
		method := "thread/start"
		if session.Resume {
			method = "thread/resume"
		}
		params := paramsOf(t, h.sent(t)[method][0])
		config, _ := params["config"].(map[string]any)
		server, _ := config["mcp_servers.veyloom"].(map[string]any)
		args, _ := server["args"].([]any)
		if server["command"] != "/opt/veyloom" || len(args) != 3 || args[0] != "mcp-proxy" || args[1] != "--url" || server["default_tools_approval_mode"] != "approve" {
			t.Fatalf("%s config = %v", method, params["config"])
		}
		if url, _ := args[2].(string); !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.HasSuffix(url, "/mcp") {
			t.Errorf("%s: endpoint %q", method, args[2])
		}
	}

	// Without a host the thread's config is left alone.
	h := newCodexHarness(t)
	turn, err := h.runner.StartTurn(context.Background(), TurnSpec{Prompt: "hi", Permission: PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	if params := paramsOf(t, h.sent(t)["thread/start"][0]); params["config"] != nil {
		t.Errorf("config = %v, want none", params["config"])
	}
}

func TestPi_RoomToolsThroughTheExtension(t *testing.T) {
	toolDir := filepath.Join(t.TempDir(), "tools")
	ext := piExtensionFile(toolDir)
	cases := map[string]string{
		PermissionReadOnly:         "read,grep,find,ls," + strings.Join(RoomToolNames, ","),
		PermissionEditWithApproval: "read,grep,find,ls,edit,write," + strings.Join(RoomToolNames, ","),
		PermissionFullAuto:         "", // no whitelist: every tool, the extension's included
	}
	for preset, tools := range cases {
		t.Run(preset, func(t *testing.T) {
			argsPath := fakePiCLI(t, piFixture, 0, "")
			runner := NewPiRunner(PiConfig{ToolDir: toolDir})
			t.Cleanup(func() { runner.Close() })
			turn, err := runner.StartTurn(context.Background(), TurnSpec{Prompt: "the prompt", Permission: preset, Host: &recordingHost{}})
			if err != nil {
				t.Fatal(err)
			}
			drain(t, turn)
			raw, _ := os.ReadFile(argsPath)
			args := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if got := flagValue(args, "-e"); got != ext {
				t.Errorf("-e = %q, want %q", got, ext)
			}
			if got := flagValue(args, "--tools"); got != tools {
				t.Errorf("--tools = %q, want %q", got, tools)
			}
			if args[len(args)-1] != "the prompt" {
				t.Errorf("the prompt must stay last: %q", args)
			}
		})
	}

	source, err := os.ReadFile(ext)
	if err != nil {
		t.Fatalf("the extension should have been written: %v", err)
	}
	for _, want := range append([]string{piRoomURLEnv, "registerTool", `"required": true`}, RoomToolNames...) {
		if !strings.Contains(string(source), want) {
			t.Errorf("the extension lacks %q", want)
		}
	}
	// Written once: the same file serves every turn.
	if files, _ := os.ReadDir(toolDir); len(files) != 1 {
		t.Errorf("tool directory holds %d files, want the one extension", len(files))
	}

	// Without a host, none of it.
	argsPath := fakePiCLI(t, piFixture, 0, "")
	runPi(t, PiConfig{ToolDir: toolDir}, TurnSpec{Prompt: "x", Permission: PermissionReadOnly})
	if raw, _ := os.ReadFile(argsPath); strings.Contains(string(raw), "-e\n") || strings.Contains(string(raw), RoomToolListTopics) {
		t.Errorf("a turn with no host has no use for the extension: %s", raw)
	}
}

func TestPi_ExtensionGetsTheTurnsEndpoint(t *testing.T) {
	// A pi that prints what it was given, then nothing a parser wants.
	dir := t.TempDir()
	envPath := filepath.Join(dir, "env")
	fakeBinary(t, "pi", "printf '%s' \"$"+piRoomURLEnv+"\" > "+envPath+"\n/bin/cat <<'EOF'\n"+piFixture+"EOF\n")
	runner := NewPiRunner(PiConfig{ToolDir: filepath.Join(dir, "tools")})
	t.Cleanup(func() { runner.Close() })
	turn, err := runner.StartTurn(context.Background(), TurnSpec{Prompt: "x", Host: &recordingHost{}})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	url, _ := os.ReadFile(envPath)
	if !strings.HasPrefix(string(url), "http://127.0.0.1:") || !strings.HasSuffix(string(url), "/room") {
		t.Errorf("%s = %q", piRoomURLEnv, url)
	}
}
