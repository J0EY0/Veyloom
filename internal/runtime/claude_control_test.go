package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
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
	return flagOf(args, flag)
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

// runScripted runs a turn on the scripted fake, prompt naming its
// exchanges, under preset. Each request to people is answered as decide
// says, or left alone when it returns nil. It returns the events and what
// the fake was answered, by exchange.
func runScripted(t *testing.T, prompt, preset string, decide func(Event) *Decision) ([]Event, map[string]json.RawMessage) {
	t.Helper()
	scriptedClaudeCLI(t)
	runner := NewClaudeRunner(ClaudeConfig{})
	t.Cleanup(func() { runner.Close() })
	turn, err := runner.StartTurn(context.Background(), TurnSpec{Prompt: prompt, Permission: preset})
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev, ok := <-turn.Events():
			if !ok {
				res, err := turn.Result()
				if err != nil {
					t.Fatalf("turn failed: %v", err)
				}
				return events, fakeClaudeAnswers(t, res.Output)
			}
			events = append(events, ev)
			if ev.Kind == EventApprovalRequest && decide != nil {
				if d := decide(ev); d != nil {
					if err := turn.Answer(ev.ApprovalID, *d); err != nil {
						t.Errorf("answer %s: %v", ev.Tool, err)
					}
				}
			}
		case <-timeout:
			t.Fatal("turn did not finish")
		}
	}
}

// eventsOf is the events of one kind.
func eventsOf(events []Event, kind EventKind) []Event {
	var out []Event
	for _, ev := range events {
		if ev.Kind == kind {
			out = append(out, ev)
		}
	}
	return out
}

func answerIs(t *testing.T, answers map[string]json.RawMessage, keyword, want string) {
	t.Helper()
	if got := string(answers[keyword]); got != want {
		t.Errorf("answer to %s = %s\n                want %s", keyword, got, want)
	}
}

// Every preset hands what the CLI would ask a person to the runner, over
// stdin and stdout.
func TestClaude_PromptToolInEveryPreset(t *testing.T) {
	for _, preset := range []string{PermissionReadOnly, PermissionEditWithApproval, PermissionFullAuto, ""} {
		argsPath, _ := fakeClaudeCLI(t, claudeFixture, 0, "")
		if _, _, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "x", Permission: preset}); err != nil {
			t.Fatal(err)
		}
		args := claudeArgs(t, argsPath)
		if flagValue(args, "--permission-prompt-tool") != "stdio" || flagValue(args, "--input-format") != "stream-json" {
			t.Errorf("%q: args = %q, want the prompt tool and the input on stdio", preset, args)
		}
		if flagValue(args, "--mcp-config") != "" {
			t.Errorf("%q: a turn with no room tools needs no MCP server: %q", preset, args)
		}
	}
}

func TestClaude_CommandsAllowedAndDenied(t *testing.T) {
	events, answers := runScripted(t, "[bash] [rm]", PermissionEditWithApproval, func(ev Event) *Decision {
		if strings.Contains(ev.Input, "make test") {
			return &Decision{Allow: true}
		}
		return &Decision{Message: "not that"}
	})

	asked := eventsOf(events, EventApprovalRequest)
	if len(asked) != 2 || asked[0].Tool != "Bash" || asked[0].ApprovalKind != "" || asked[0].Input != `{"command":"make test","description":"run the tests"}` {
		t.Fatalf("requests = %+v, want Bash with its input as the CLI sent it", asked)
	}
	answerIs(t, answers, "[bash]", `{"behavior":"allow","updatedInput":{"command":"make test","description":"run the tests"},"toolUseID":"tu-bash"}`)
	answerIs(t, answers, "[rm]", `{"behavior":"deny","message":"not that","toolUseID":"tu-rm"}`)
	// The CLI lists the denial on its result too; people have seen it.
	if notices := eventsOf(events, EventNotice); len(notices) != 0 {
		t.Errorf("notices = %+v, want none for a denial a person made", notices)
	}
}

// A request offers what Claude Code suggests allowing besides, and a
// person who takes it up has it allowed for the session, the turn, not
// saved to the project's settings; a request without suggestions offers
// nothing, and a person cannot take up what was not offered.
func TestClaude_TheLikeOfARequestIsAllowedForTheTurn(t *testing.T) {
	events, answers := runScripted(t, "[bash] [rm]", PermissionEditWithApproval, func(ev Event) *Decision {
		return &Decision{Allow: true, Similar: true}
	})
	asked := eventsOf(events, EventApprovalRequest)
	if len(asked) != 2 || asked[0].Similar == nil || !slices.Equal(asked[0].Similar.Rules, []string{"Bash(make test)"}) || asked[1].Similar != nil {
		t.Fatalf("offers: %+v", asked)
	}
	answerIs(t, answers, "[bash]", `{"behavior":"allow","updatedInput":{"command":"make test","description":"run the tests"},`+
		`"updatedPermissions":[{"behavior":"allow","destination":"session","rules":[{"ruleContent":"make test","toolName":"Bash"}],"type":"addRules"}],"toolUseID":"tu-bash"}`)
	answerIs(t, answers, "[rm]", `{"behavior":"allow","updatedInput":{"command":"rm -rf build"},"toolUseID":"tu-rm"}`)
}

func TestClaudeSimilar(t *testing.T) {
	for _, c := range []struct {
		name, suggestions string
		want              *Similar
	}{
		{"a rule", `[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"go test *"}],"behavior":"allow","destination":"localSettings"}]`, &Similar{Rules: []string{"Bash(go test *)"}}},
		{"a mode", `[{"type":"setMode","mode":"acceptEdits","destination":"session"}]`, &Similar{Mode: "acceptEdits"}},
		{"folders", `[{"type":"addRules","rules":[{"toolName":"Read","ruleContent":"//etc/**"}],"behavior":"allow","destination":"session"},{"type":"addDirectories","directories":["/srv"],"destination":"session"}]`,
			&Similar{Rules: []string{"Read(//etc/**)"}, Dirs: []string{"/srv"}}},
		{"a rule that denies", `[{"type":"addRules","rules":[{"toolName":"Bash"}],"behavior":"deny","destination":"session"}]`, nil},
		{"none", `[]`, nil},
	} {
		var raw []json.RawMessage
		if err := json.Unmarshal([]byte(c.suggestions), &raw); err != nil {
			t.Fatal(err)
		}
		got, updates := claudeSimilar(raw)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
		for _, u := range updates {
			if !strings.Contains(string(u), `"destination":"session"`) {
				t.Errorf("%s: an update not for the session: %s", c.name, u)
			}
		}
	}
}

// Two requests at once wait for people side by side.
func TestClaude_RequestsWaitSideBySide(t *testing.T) {
	_, answers := runScripted(t, "[both]", PermissionEditWithApproval, func(Event) *Decision { return &Decision{Allow: true} })
	for _, keyword := range []string{"[bash]", "[rm]"} {
		if !strings.Contains(string(answers[keyword]), `"behavior":"allow"`) {
			t.Errorf("answer to %s = %s", keyword, answers[keyword])
		}
	}
}

// The read-only member reads and plans; a request to do more is turned
// down on the spot, and people are shown what was asked.
func TestClaude_ReadOnlyTurnsDownWithoutAsking(t *testing.T) {
	events, answers := runScripted(t, "[bash]", PermissionReadOnly, func(ev Event) *Decision {
		t.Errorf("nobody should be asked: %+v", ev)
		return &Decision{Allow: true}
	})
	answerIs(t, answers, "[bash]", `{"behavior":"deny","message":"`+claudeReadOnlyRefusal+`","toolUseID":"tu-bash"}`)
	notices := eventsOf(events, EventNotice)
	if len(notices) != 1 || notices[0].Level != NoticeWarning ||
		notices[0].Text != "Claude Code asked to run `make test`; the member is read-only, so it was turned down without asking anyone" {
		t.Errorf("notices = %+v", notices)
	}
}

// Veyloom's own tools are let through in every preset, plan mode's
// included, without asking anyone: the wiki is not the project.
func TestClaude_OwnToolsPassEvenReadOnly(t *testing.T) {
	events, answers := runScripted(t, "[wiki]", PermissionReadOnly, func(ev Event) *Decision {
		t.Errorf("nobody should be asked: %+v", ev)
		return nil
	})
	answerIs(t, answers, "[wiki]", `{"behavior":"allow","updatedInput":{"type":"Fact","slug":"go-version","title":"Go","description":"d","body":"b"},"toolUseID":"tu-wiki"}`)
	if notices := eventsOf(events, EventNotice); len(notices) != 0 {
		t.Errorf("notices = %+v", notices)
	}
}

// A plan is shown to people. Approving it lets Claude Code go ahead, except
// in the read-only preset, where the approval goes back in words and plan
// mode stays.
func TestClaude_Plans(t *testing.T) {
	const plan = `{"plan":"# Tidy up\n1. Remove dead code"}`
	cases := []struct {
		name     string
		preset   string
		decision Decision
		want     string
	}{
		{"approved", PermissionEditWithApproval, Decision{Allow: true}, `{"behavior":"allow","updatedInput":` + plan + `,"toolUseID":"tu-plan"}`},
		{"approved, read-only", PermissionReadOnly, Decision{Allow: true, Message: "ship it"}, `{"behavior":"deny","message":"` + claudePlanApprovedReadOnly + ` They added: ship it","toolUseID":"tu-plan"}`},
		{"sent back", PermissionReadOnly, Decision{Message: "too big"}, `{"behavior":"deny","message":"too big","toolUseID":"tu-plan"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			events, answers := runScripted(t, "[plan]", c.preset, func(Event) *Decision { return &c.decision })
			asked := eventsOf(events, EventApprovalRequest)
			if len(asked) != 1 || asked[0].Tool != "ExitPlanMode" || asked[0].Input != plan || asked[0].ApprovalKind != "" {
				t.Fatalf("requests = %+v, want the plan", asked)
			}
			answerIs(t, answers, "[plan]", c.want)
		})
	}
}

// AskUserQuestion's questions reach people; the answers go back in the
// input the tool then runs with, keyed by question text.
func TestClaude_QuestionsAnswered(t *testing.T) {
	events, answers := runScripted(t, "[ask]", PermissionFullAuto, func(Event) *Decision {
		return &Decision{Allow: true, Answer: json.RawMessage(`{"answers":{"1":["blue"]}}`)}
	})
	asked := eventsOf(events, EventApprovalRequest)
	if len(asked) != 1 || asked[0].ApprovalKind != ApprovalQuestion || asked[0].Tool != "AskUserQuestion" {
		t.Fatalf("requests = %+v", asked)
	}
	var set QuestionSet
	if err := json.Unmarshal([]byte(asked[0].Input), &set); err != nil {
		t.Fatal(err)
	}
	want := QuestionSet{Questions: []Question{{ID: "1", Header: "Colour", Question: "Which colour?", Options: []QuestionOption{{Label: "red", Description: "warm"}, {Label: "blue", Description: "cool"}}, Other: true}}}
	if !reflect.DeepEqual(set, want) {
		t.Errorf("questions = %+v\n     want %+v", set, want)
	}
	var reply struct {
		Behavior     string `json:"behavior"`
		UpdatedInput struct {
			Questions []any             `json:"questions"`
			Answers   map[string]string `json:"answers"`
		} `json:"updatedInput"`
	}
	if err := json.Unmarshal(answers["[ask]"], &reply); err != nil || reply.Behavior != "allow" || len(reply.UpdatedInput.Questions) != 1 ||
		!reflect.DeepEqual(reply.UpdatedInput.Answers, map[string]string{"Which colour?": "blue"}) {
		t.Errorf("answer = %s", answers["[ask]"])
	}

	_, answers = runScripted(t, "[ask]", PermissionFullAuto, func(Event) *Decision { return &Decision{} })
	answerIs(t, answers, "[ask]", `{"behavior":"deny","message":"the person chose not to answer","toolUseID":"tu-ask"}`)
}

// An MCP server's form and link reach people, in every preset, and what
// they did goes back as MCP says.
func TestClaude_ElicitationFormAndLink(t *testing.T) {
	events, answers := runScripted(t, "[form] [link]", PermissionReadOnly, func(ev Event) *Decision {
		if ev.ApprovalKind == ApprovalForm {
			return &Decision{Allow: true, Answer: json.RawMessage(`{"content":{"region":"eu"}}`)}
		}
		return &Decision{}
	})
	asked := eventsOf(events, EventApprovalRequest)
	if len(asked) != 2 || asked[0].ApprovalKind != ApprovalForm || asked[1].ApprovalKind != ApprovalLink || asked[0].Tool != "elicitation" {
		t.Fatalf("requests = %+v", asked)
	}
	if want := `{"server":"deploy","message":"Where to?","schema":{"type":"object","properties":{"region":{"type":"string","enum":["eu","us"]},"count":{"type":"integer"}},"required":["region"]}}`; asked[0].Input != want {
		t.Errorf("form = %s\nwant %s, the schema as the server wrote it", asked[0].Input, want)
	}
	if want := `{"server":"deploy","message":"Sign in","url":"https://example.com/device"}`; asked[1].Input != want {
		t.Errorf("link = %s", asked[1].Input)
	}
	answerIs(t, answers, "[form]", `{"action":"accept","content":{"region":"eu"}}`)
	// No content key at all: the CLI takes no null there.
	answerIs(t, answers, "[link]", `{"action":"decline"}`)
}

// A request the CLI takes back stops being asked, and gets no answer.
func TestClaude_WithdrawnRequest(t *testing.T) {
	events, answers := runScripted(t, "[withdraw]", PermissionEditWithApproval, func(Event) *Decision { return nil })
	asked := eventsOf(events, EventApprovalRequest)
	withdrawn := eventsOf(events, EventApprovalWithdrawn)
	if len(asked) != 1 || len(withdrawn) != 1 || withdrawn[0].ApprovalID != asked[0].ApprovalID {
		t.Fatalf("requests %+v, withdrawn %+v; want the one request taken back", asked, withdrawn)
	}
	if len(answers) != 0 {
		t.Errorf("the fake was answered %v, want nothing for a request it took back", answers)
	}
}

// A request the runner cannot answer gets an error, and people hear of it.
func TestClaude_UnsupportedRequestIsRefusedAndShown(t *testing.T) {
	events, answers := runScripted(t, "[hookcb]", PermissionEditWithApproval, nil)
	answerIs(t, answers, "[hookcb]", `{"error":"Veyloom does not handle hook_callback requests"}`)
	notices := eventsOf(events, EventNotice)
	if len(notices) != 1 || notices[0].Level != NoticeError || !strings.Contains(notices[0].Text, `"hook_callback"`) {
		t.Errorf("notices = %+v", notices)
	}
}

// The turns of one runner share the endpoint's listener, each under a
// token of its own that ends with the turn.
func TestClaude_RoomToolEndpointIsSharedAndClosable(t *testing.T) {
	runner := NewClaudeRunner(ClaudeConfig{ProxyBinary: "/opt/veyloom"})
	ctx := context.Background()
	var endpoints []string
	for range 2 {
		argsPath, _ := fakeClaudeCLI(t, claudeFixture, 0, "")
		turn, err := runner.StartTurn(ctx, TurnSpec{Prompt: "a", Permission: PermissionReadOnly, Host: &recordingHost{}})
		if err != nil {
			t.Fatal(err)
		}
		drain(t, turn)
		endpoints = append(endpoints, mcpEndpoint(t, flagValue(claudeArgs(t, argsPath), "--mcp-config"), "/opt/veyloom"))
	}
	portOf := func(u string) string { return strings.Split(strings.TrimPrefix(u, "http://"), "/")[0] }
	if portOf(endpoints[0]) != portOf(endpoints[1]) || endpoints[0] == endpoints[1] {
		t.Errorf("endpoints %v, want one listener and a token per turn", endpoints)
	}
	if _, err := mcp.NewClient(&mcp.Implementation{Name: "late", Version: "test"}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: endpoints[0], DisableStandaloneSSE: true, MaxRetries: -1}, nil); err == nil {
		t.Error("the turn's endpoint should be unregistered once the turn ends")
	}
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

	argsPath, _ := scriptedClaudeCLI(t)
	goPath := filepath.Join(t.TempDir(), "go")
	t.Setenv("VEYLOOM_FAKE_CLAUDE_GO", goPath)
	host := &recordingHost{}
	runner := NewClaudeRunner(ClaudeConfig{ProxyBinary: bin})
	t.Cleanup(func() { runner.Close() })
	ctx := context.Background()
	turn, err := runner.StartTurn(ctx, TurnSpec{Prompt: "[wait]", Permission: PermissionReadOnly, Host: host})
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
	if len(tools.Tools) != len(AgentToolNames) {
		t.Fatalf("tools through the proxy = %+v, want Veyloom's tools", tools.Tools)
	}
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "list_topics", Arguments: map[string]any{}})
	if err != nil || textOf(t, res) != "answer to list_topics" {
		t.Errorf("list_topics through the proxy = %+v, %v", res, err)
	}

	os.WriteFile(goPath, nil, 0o600)
	drain(t, turn)
	if _, err := turn.Result(); err != nil {
		t.Errorf("turn: %v", err)
	}
}

// After a turn ends, an answer for one of its requests finds nothing.
func TestClaude_AnswerAfterTheTurn(t *testing.T) {
	scriptedClaudeCLI(t)
	runner := NewClaudeRunner(ClaudeConfig{})
	t.Cleanup(func() { runner.Close() })
	turn, err := runner.StartTurn(context.Background(), TurnSpec{Prompt: "[withdraw]", Permission: PermissionEditWithApproval})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := awaitApproval(t, turn)
	drain(t, turn)
	if err := turn.Answer(req.ApprovalID, Decision{Allow: true}); !errors.Is(err, ErrUnknownApproval) {
		t.Errorf("late answer: %v, want ErrUnknownApproval", err)
	}
}

// What people allowed the member always goes to Claude Code as the
// permission rules it suggested, in settings of their own, whole: a rule
// with parentheses, commas and spaces of its own included. Claude Code
// matches them itself. Veyloom's own tools stay on --allowedTools, and
// without room tools the rules still go.
func TestClaude_MemberRulesGoAsSettings(t *testing.T) {
	rules := []string{"Bash(go test:*)", `Bash(git commit -m "fix(tags): a, b")`}
	runner := NewClaudeRunner(ClaudeConfig{ProxyBinary: "/opt/veyloom"})
	t.Cleanup(func() { runner.Close() })
	argsPath, _ := fakeClaudeCLI(t, claudeFixture, 0, "")
	turn, err := runner.StartTurn(context.Background(), TurnSpec{Prompt: "a", Permission: PermissionEditWithApproval, Host: &recordingHost{}, AllowedRules: rules})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	args := claudeArgs(t, argsPath)
	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(flagValue(args, "--settings")), &settings); err != nil || !slices.Equal(settings.Permissions.Allow, rules) {
		t.Errorf("--settings %q: %v", flagValue(args, "--settings"), err)
	}
	if allowed := flagValue(args, "--allowedTools"); strings.Contains(allowed, "Bash") || !strings.HasPrefix(allowed, claudeToolName(AgentToolNames[0])) {
		t.Errorf("--allowedTools %q", allowed)
	}

	argsPath, _ = fakeClaudeCLI(t, claudeFixture, 0, "")
	if _, _, err := runClaude(t, ClaudeConfig{}, TurnSpec{Prompt: "a", Permission: PermissionEditWithApproval, AllowedRules: rules[:1]}); err != nil {
		t.Fatal(err)
	}
	if args := claudeArgs(t, argsPath); flagValue(args, "--settings") != `{"permissions":{"allow":["Bash(go test:*)"]}}` || slices.Contains(args, "--allowedTools") {
		t.Errorf("without room tools: %q", args)
	}
}

// A maintainer's upkeep turn gets its two tools on its endpoint, allowed up
// front like Veyloom's other tools; they pass plan mode, since they read.
func TestClaude_UpkeepTurnsGetTheMaintainersTools(t *testing.T) {
	runner := NewClaudeRunner(ClaudeConfig{ProxyBinary: "/opt/veyloom"})
	t.Cleanup(func() { runner.Close() })
	argsPath, _ := fakeClaudeCLI(t, claudeFixture, 0, "")
	turn, err := runner.StartTurn(context.Background(), TurnSpec{Prompt: "a", Permission: PermissionReadOnly, Host: &recordingHost{}, ExtraTools: UpkeepToolNames})
	if err != nil {
		t.Fatal(err)
	}
	drain(t, turn)
	allowed := strings.Split(flagValue(claudeArgs(t, argsPath), "--allowedTools"), ",")
	for _, name := range append(append([]string{}, AgentToolNames...), UpkeepToolNames...) {
		if !slices.Contains(allowed, claudeToolName(name)) {
			t.Errorf("%s not allowed: %v", name, allowed)
		}
	}
	if !isVeyloomTool(claudeToolName(UpkeepToolListTurns)) {
		t.Error("list_turns is not taken for one of Veyloom's tools")
	}
}
