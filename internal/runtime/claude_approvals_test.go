package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// claudeArgs waits for the fake CLI to record its arguments and returns
// them one per line.
func claudeArgs(t *testing.T, argsPath string) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(argsPath); err == nil && len(data) > 0 {
			return strings.Split(strings.TrimSpace(string(data)), "\n")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the fake CLI never recorded its arguments")
	return nil
}

// flagValue returns the argument after flag, or "" when absent.
func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// mcpEndpoint extracts the proxy's target URL from a --mcp-config value
// and checks the rest of the document.
func mcpEndpoint(t *testing.T, cfg, proxy string) string {
	t.Helper()
	var doc struct {
		Servers map[string]struct {
			Type    string   `json:"type"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(cfg), &doc); err != nil {
		t.Fatalf("mcp-config is not JSON: %v\n%s", err, cfg)
	}
	srv, ok := doc.Servers["veyloom"]
	if !ok || srv.Type != "stdio" || srv.Command != proxy || len(srv.Args) != 3 || srv.Args[0] != "mcp-proxy" || srv.Args[1] != "--url" {
		t.Fatalf("unexpected mcp-config: %s", cfg)
	}
	if ok, _ := regexp.MatchString(`^http://127\.0\.0\.1:\d+/turns/[0-9a-f]{32}/mcp$`, srv.Args[2]); !ok {
		t.Fatalf("unexpected endpoint %q", srv.Args[2])
	}
	return srv.Args[2]
}

func TestClaude_ApprovalFlagsOnlyForEditWithApproval(t *testing.T) {
	for _, tc := range []struct {
		permission string
		want       bool
	}{
		{PermissionReadOnly, false},
		{PermissionEditWithApproval, true},
		{PermissionFullAuto, false},
		{"", false},
	} {
		t.Run(tc.permission, func(t *testing.T) {
			argsPath, _ := fakeClaudeCLI(t, claudeFixture, 0, "")
			runner := NewClaudeRunner(ClaudeConfig{ProxyBinary: "/opt/veyloom"})
			t.Cleanup(func() { runner.Close() })
			turn, err := runner.StartTurn(context.Background(), TurnSpec{Prompt: "hi", Permission: tc.permission})
			if err != nil {
				t.Fatal(err)
			}
			drain(t, turn)
			args := claudeArgs(t, argsPath)

			tool := flagValue(args, "--permission-prompt-tool")
			cfg := flagValue(args, "--mcp-config")
			if !tc.want {
				if tool != "" || cfg != "" {
					t.Errorf("no approval flags expected, got %v", args)
				}
				return
			}
			if tool != "mcp__veyloom__approval_prompt" {
				t.Errorf("--permission-prompt-tool = %q", tool)
			}
			mcpEndpoint(t, cfg, "/opt/veyloom")
		})
	}
}

// blockingClaudeCLI is a fake CLI that prints its init record, then waits
// for a file to appear before printing the rest and exiting. The wait is
// where a real CLI would be calling the approval tool.
func blockingClaudeCLI(t *testing.T) (argsPath, goPath string) {
	t.Helper()
	dir := t.TempDir()
	argsPath = filepath.Join(dir, "args")
	goPath = filepath.Join(dir, "go")
	lines := strings.SplitN(claudeFixture, "\n", 2)
	initPath, restPath := filepath.Join(dir, "init"), filepath.Join(dir, "rest")
	if err := os.WriteFile(initPath, []byte(lines[0]+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(restPath, []byte(lines[1]), 0o600); err != nil {
		t.Fatal(err)
	}
	// The arguments are written to a temporary file and moved into place,
	// so a test polling for the file never reads a partial write.
	script := "printf '%s\\n' \"$@\" > " + argsPath + ".tmp && /bin/mv " + argsPath + ".tmp " + argsPath + "\n" +
		"/bin/cat " + initPath + "\n" +
		"i=0; while [ ! -e " + goPath + " ] && [ $i -lt 400 ]; do /bin/sleep 0.05; i=$((i+1)); done\n" +
		"/bin/cat " + restPath + "\n"
	fakeBinary(t, "claude", script)
	return argsPath, goPath
}

// promptArgs is what Claude Code sends to the approval tool.
func promptArgs(tool, command string) map[string]any {
	return map[string]any{"tool_name": tool, "input": map[string]any{"command": command}, "tool_use_id": "tu9"}
}

func callPrompt(ctx context.Context, session *mcp.ClientSession, args map[string]any) (string, error) {
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: claudeApprovalTool, Arguments: args})
	if err != nil {
		return "", err
	}
	if len(res.Content) != 1 {
		return "", fmt.Errorf("expected one content block, got %d", len(res.Content))
	}
	return res.Content[0].(*mcp.TextContent).Text, nil
}

func TestClaude_ApprovalPromptOverMCP(t *testing.T) {
	argsPath, goPath := blockingClaudeCLI(t)
	runner := NewClaudeRunner(ClaudeConfig{ProxyBinary: "/opt/veyloom"})
	t.Cleanup(func() { runner.Close() })
	ctx := context.Background()

	turn, err := runner.StartTurn(ctx, TurnSpec{Prompt: "hi", Permission: PermissionEditWithApproval})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := mcpEndpoint(t, flagValue(claudeArgs(t, argsPath), "--mcp-config"), "/opt/veyloom")

	// Play the CLI's side: connect to the turn's endpoint like the proxy
	// would and call the approval tool.
	client := mcp.NewClient(&mcp.Implementation{Name: "fake-claude", Version: "test"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != claudeApprovalTool || tools.Tools[0].InputSchema == nil {
		t.Fatalf("tools = %+v", tools.Tools)
	}

	type outcome struct {
		text string
		err  error
	}
	results := make(chan outcome, 1)
	go func() {
		text, err := callPrompt(ctx, session, promptArgs("Bash", "ls"))
		results <- outcome{text, err}
	}()

	req, before := awaitApproval(t, turn)
	if req.Tool != "Bash" || req.Input != `{"command":"ls"}` {
		t.Fatalf("unexpected approval request: %+v", req)
	}
	if len(before) != 2 || before[0].Kind != EventSession || before[1].Kind != EventStatus {
		t.Errorf("only the session and the init status should precede the request, got %+v", before)
	}
	if err := turn.Answer(req.ApprovalID, Decision{Allow: true}); err != nil {
		t.Fatal(err)
	}
	got := <-results
	if got.err != nil || got.text != `{"behavior":"allow","updatedInput":{"command":"ls"}}` {
		t.Errorf("allow reply = %q, %v", got.text, got.err)
	}

	go func() {
		text, err := callPrompt(ctx, session, promptArgs("Bash", "rm -rf /"))
		results <- outcome{text, err}
	}()
	req, _ = awaitApproval(t, turn)
	if err := turn.Answer(req.ApprovalID, Decision{Allow: false, Message: "absolutely not"}); err != nil {
		t.Fatal(err)
	}
	got = <-results
	if got.err != nil || got.text != `{"behavior":"deny","message":"absolutely not"}` {
		t.Errorf("deny reply = %q, %v", got.text, got.err)
	}

	// A request still open when the CLI finishes is denied, not left hanging.
	go func() {
		text, err := callPrompt(ctx, session, promptArgs("Bash", "make"))
		results <- outcome{text, err}
	}()
	awaitApproval(t, turn)
	if err := os.WriteFile(goPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	if _, err := turn.Result(); err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	got = <-results
	if got.err == nil && !strings.Contains(got.text, `"behavior":"deny"`) {
		t.Errorf("a request outliving the turn should be denied, got %q", got.text)
	}

	// The endpoint is gone with the turn.
	if _, err := mcp.NewClient(&mcp.Implementation{Name: "late", Version: "test"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoint, DisableStandaloneSSE: true, MaxRetries: -1}, nil); err == nil {
		t.Error("the turn's endpoint should be unregistered once the turn ends")
	}
}

func TestClaude_ApprovalEndpointIsSharedAndClosable(t *testing.T) {
	argsA, goA := blockingClaudeCLI(t)
	runner := NewClaudeRunner(ClaudeConfig{ProxyBinary: "/opt/veyloom"})
	ctx := context.Background()
	turnA, err := runner.StartTurn(ctx, TurnSpec{Prompt: "a", Permission: PermissionEditWithApproval})
	if err != nil {
		t.Fatal(err)
	}
	endpointA := mcpEndpoint(t, flagValue(claudeArgs(t, argsA), "--mcp-config"), "/opt/veyloom")

	argsB, goB := blockingClaudeCLI(t)
	turnB, err := runner.StartTurn(ctx, TurnSpec{Prompt: "b", Permission: PermissionEditWithApproval})
	if err != nil {
		t.Fatal(err)
	}
	endpointB := mcpEndpoint(t, flagValue(claudeArgs(t, argsB), "--mcp-config"), "/opt/veyloom")

	// One listener, two tokens.
	portOf := func(u string) string { return strings.Split(strings.TrimPrefix(u, "http://"), "/")[0] }
	if portOf(endpointA) != portOf(endpointB) {
		t.Errorf("turns should share the listener: %s vs %s", endpointA, endpointB)
	}
	if endpointA == endpointB {
		t.Error("each turn needs its own token")
	}

	os.WriteFile(goA, nil, 0o600)
	os.WriteFile(goB, nil, 0o600)
	drain(t, turnA)
	drain(t, turnB)
	if err := runner.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := runner.Close(); err != nil {
		t.Logf("second Close: %v (acceptable)", err)
	}
}

// TestClaude_ProxySubprocessEndToEnd runs the real `veyloom mcp-proxy` as
// Claude Code would: built from this tree, spawned over stdio, pointed at
// a turn's endpoint. It is the one test that exercises the whole bridge.
func TestClaude_ProxySubprocessEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the veyloom binary")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	bin := filepath.Join(t.TempDir(), "veyloom")
	build := exec.Command(goBin, "build", "-o", bin, "../../cmd/veyloom")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build veyloom: %v\n%s", err, out)
	}

	argsPath, goPath := blockingClaudeCLI(t)
	runner := NewClaudeRunner(ClaudeConfig{ProxyBinary: bin})
	t.Cleanup(func() { runner.Close() })
	ctx := context.Background()
	turn, err := runner.StartTurn(ctx, TurnSpec{Prompt: "hi", Permission: PermissionEditWithApproval})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := mcpEndpoint(t, flagValue(claudeArgs(t, argsPath), "--mcp-config"), bin)

	// Spawn the proxy exactly as the mcp-config tells the CLI to.
	proxy := exec.Command(bin, "mcp-proxy", "--url", endpoint)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "fake-claude", Version: "test"}, nil).Connect(ctx, &mcp.CommandTransport{Command: proxy}, nil)
	if err != nil {
		t.Fatalf("connect through the proxy: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != claudeApprovalTool {
		t.Fatalf("tools through the proxy = %+v", tools.Tools)
	}
	schema, _ := tools.Tools[0].InputSchema.(map[string]any)
	if props, _ := schema["properties"].(map[string]any); props["tool_name"] == nil || props["input"] == nil {
		t.Errorf("the CLI needs the tool's input schema; got %v", tools.Tools[0].InputSchema)
	}

	results := make(chan string, 1)
	go func() {
		text, err := callPrompt(ctx, session, promptArgs("Bash", "go test ./..."))
		if err != nil {
			text = "error: " + err.Error()
		}
		results <- text
	}()
	req, _ := awaitApproval(t, turn)
	if req.Tool != "Bash" || req.Input != `{"command":"go test ./..."}` {
		t.Fatalf("unexpected approval request: %+v", req)
	}
	if err := turn.Answer(req.ApprovalID, Decision{Allow: true}); err != nil {
		t.Fatal(err)
	}
	if got := <-results; got != `{"behavior":"allow","updatedInput":{"command":"go test ./..."}}` {
		t.Errorf("reply through the proxy = %q", got)
	}

	os.WriteFile(goPath, nil, 0o600)
	drain(t, turn)
}
