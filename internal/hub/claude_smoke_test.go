package hub

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// TestClaudeSmoke drives the real Claude Code CLI through the hub, the
// model played by fakeAnthropic, so it needs no account and costs nothing;
// it still needs the CLI, and leaves its sessions in a throwaway config
// directory. It runs when VEYLOOM_CLAUDE_SMOKE=1, against claude on PATH
// or the binary VEYLOOM_CLAUDE_BIN names (and a test database):
//
//	VEYLOOM_CLAUDE_SMOKE=1 VEYLOOM_TEST_DATABASE_URL=... go test -run TestClaudeSmoke ./internal/hub/ -v
//
// What it proves is what the fake CLI in the runtime tests cannot: that
// the CLI takes the prompt and the answers as the runner writes them over
// stdin, and asks as the runner reads it; that a command waits for a person
// and runs or not as they say; that the next turn resumes the session; that
// AskUserQuestion gets the answers given on its card; that the room tools
// reach the hub through the proxy; that an MCP server's form and link reach
// a person and their answers the server; that full_auto asks nothing; that
// read_only turns down what would change things, and approving a plan
// there leaves it read-only; and that a request the person's own hook
// settles first is taken back from the room.
func TestClaudeSmoke(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_SMOKE=1 to run against the real claude CLI")
	}
	bin := os.Getenv("VEYLOOM_CLAUDE_BIN")
	if bin == "" {
		path, err := exec.LookPath("claude")
		if err != nil {
			t.Skipf("claude is not installed: %v", err)
		}
		bin = path
	}
	version, _ := exec.Command(bin, "--version").Output()
	t.Logf("claude %s", strings.TrimSpace(string(version)))

	api := newFakeAnthropic(t)
	config := t.TempDir()
	for key, value := range map[string]string{
		"ANTHROPIC_BASE_URL": api.URL, "ANTHROPIC_API_KEY": "sk-ant-veyloom-smoke-not-a-key", "ANTHROPIC_AUTH_TOKEN": "",
		"CLAUDE_CONFIG_DIR": config, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1", "DISABLE_AUTOUPDATER": "1", "DISABLE_TELEMETRY": "1",
	} {
		t.Setenv(key, value)
	}

	// The CLI runs the room tools' MCP server as `veyloom mcp-proxy`, and
	// the test binary is not veyloom: build one. And an MCP server that asks
	// a person for things.
	proxy := filepath.Join(t.TempDir(), "veyloom")
	if out, err := exec.Command("go", "build", "-o", proxy, "../../cmd/veyloom").CombinedOutput(); err != nil {
		t.Fatalf("build veyloom for the MCP proxy: %v\n%s", err, out)
	}
	elicit := filepath.Join(t.TempDir(), "elicitmcp")
	if out, err := exec.Command("go", "build", "-o", elicit, "./testdata/elicitmcp").CombinedOutput(); err != nil {
		t.Fatalf("build the elicitation server: %v\n%s", err, out)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{ToolDir: filepath.Join(t.TempDir(), "tools"), ProxyBinary: proxy})
	runners["claude"] = runtime.NewClaudeRunner(runtime.ClaudeConfig{Binary: bin, StreamPartials: true, ProxyBinary: proxy})

	dir := t.TempDir()
	r := newSmokeRoom(t, runners, store.NewAgent{
		Name: "Claude smoke", Runtime: "claude", PermissionPreset: store.PermissionEditWithApproval, Model: "claude-sonnet-4-5",
	}, dir)
	agent := r.agent
	configure := func(preset store.PermissionPreset, extra ...any) {
		if _, err := r.s.UpdateAgent(r.ctx, agent.ID, store.NewAgent{
			Name: agent.Name, MachineID: agent.MachineID, Runtime: agent.Runtime, Model: agent.Model,
			PermissionPreset: preset, RuntimeOptions: map[string]any{"extra_args": extra},
		}); err != nil {
			t.Fatal(err)
		}
	}
	const compute = `USE_TOOL Bash {"command":"python3 -c \"print(6*7)\"","description":"compute"}`

	// Turn 1: a command waits for a person, who allows it.
	r.say(compute, "")
	first, allowed := r.decideAll(1, true)
	thread, err := r.s.ThreadForMessage(r.ctx, first.ReplyMessageID)
	if err != nil {
		t.Fatal(err)
	}
	if allowed[0].Tool != "Bash" || !strings.Contains(string(allowed[0].Input), "print(6*7)") {
		t.Errorf("approval = %s %s, want the command", allowed[0].Tool, allowed[0].Input)
	}
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, `"content":"42`) {
		t.Errorf("the command should have run, the model got %q", reply)
	}
	session, err := r.s.GetOpenSession(r.ctx, r.member.ID)
	if err != nil || !session.Started() || session.Ref == "" {
		t.Fatalf("session after the first turn = %+v, %v", session, err)
	}
	api.longest()
	t.Logf("turn 1: allowed %s; reply %q", allowed[0].Input, r.lastReply(thread.ID))

	// Turn 2 resumes the session: the model sees turn 1 again. The person
	// denies the command this time, and the model hears why.
	r.say(compute, thread.ID)
	_, denied := r.decideAllWith(2, func(store.Approval) runtime.Decision { return runtime.Decision{Message: "not now"} })
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, `"is_error":true`) || !strings.Contains(reply, "not now") {
		t.Errorf("the model should have heard the denial, got %q", reply)
	}
	if n := api.longest(); n < 5 {
		t.Errorf("turn 2 sent at most %d messages, want turn 1's as well", n)
	}
	t.Logf("turn 2: denied %d request(s); reply %q", len(denied), r.lastReply(thread.ID))

	// Turn 3: AskUserQuestion's question is answered on its card.
	r.say(`USE_TOOL AskUserQuestion {"questions":[{"question":"Which colour?","header":"Colour","multiSelect":false,"options":[{"label":"red","description":"warm"},{"label":"blue","description":"cool"}]}]}`, thread.ID)
	_, asked := r.decideAllWith(3, func(a store.Approval) runtime.Decision {
		return runtime.Decision{Allow: true, Answer: json.RawMessage(`{"answers":{"1":["blue"]}}`)}
	})
	if asked[0].Kind != store.ApprovalQuestion {
		t.Errorf("request = %s %s, want a question", asked[0].Kind, asked[0].Input)
	}
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, `\"Which colour?\"=\"blue\"`) {
		t.Errorf("the model should have got the answer, got %q", reply)
	}
	t.Logf("turn 3: reply %q", r.lastReply(thread.ID))

	// Turn 4: a room tool, allowed up front, asks nobody.
	r.say(`USE_TOOL mcp__veyloom__list_topics {}`, thread.ID)
	fourth := r.waitDone(4)
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, "Topics, most recently active first") {
		t.Errorf("the room tool should have answered, got %q", reply)
	}
	approvalsOf(t, r, fourth, 0)
	t.Logf("turn 4: reply %.120q", r.lastReply(thread.ID))

	// Turns 5 and 6: an MCP server of the person's own asks for a form, then
	// a link. Calling its tools asks first, as for any MCP tool.
	configure(store.PermissionEditWithApproval, "--mcp-config", `{"mcpServers":{"elicit":{"type":"stdio","command":"`+elicit+`","args":[]}}}`)
	r.say(`USE_TOOL mcp__elicit__deploy_form {}`, thread.ID)
	_, formed := r.decideAllWith(5, func(a store.Approval) runtime.Decision {
		if a.Kind == store.ApprovalForm {
			return runtime.Decision{Allow: true, Answer: json.RawMessage(`{"content":{"region":"eu"}}`)}
		}
		return runtime.Decision{Allow: true}
	})
	if kinds := approvalKinds(formed); kinds != "tool_use,form" {
		t.Errorf("requests = %s, want the tool and then the form", kinds)
	}
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, `accept {\"region\":\"eu\"}`) {
		t.Errorf("the server should have got the form, got %q", reply)
	}
	t.Logf("turn 5: form %s; reply %q", formed[1].Input, r.lastReply(thread.ID))
	// Newer CLIs no longer offer MCP servers the link kind when driven like
	// this: the server then refuses to ask, and nobody is asked.
	r.say(`USE_TOOL mcp__elicit__sign_in {}`, thread.ID)
	_, linked := r.decideAll(6, true)
	if reply := r.lastReply(thread.ID); strings.Contains(reply, `does not support \"url\" elicitation`) {
		t.Logf("turn 6: this claude does not offer links (%q)", reply)
	} else {
		if kinds := approvalKinds(linked); kinds != "tool_use,link" {
			t.Errorf("requests = %s, want the tool and then the link", kinds)
		}
		if !strings.Contains(reply, `"content":"accept`) {
			t.Errorf("the server should have heard the link was done, got %q", reply)
		}
		t.Logf("turn 6: reply %q", reply)
	}

	// Turn 7: full_auto runs the command without asking.
	configure(store.PermissionFullAuto)
	r.say(compute, thread.ID)
	seventh := r.waitDone(7)
	approvalsOf(t, r, seventh, 0)
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, `"content":"42`) {
		t.Errorf("full_auto should have run the command, got %q", reply)
	}

	// Turn 8: read_only turns a write down without asking, and says so.
	configure(store.PermissionReadOnly)
	target := filepath.Join(dir, "nope.txt")
	r.say(`USE_TOOL Write {"file_path":"`+target+`","content":"x"}`, thread.ID)
	eighth := r.waitDone(8)
	approvalsOf(t, r, eighth, 0)
	if _, err := os.Stat(target); !os.IsNotExist(err) || len(eighth.FilesChanged) != 0 {
		t.Errorf("read_only should not have written %s: %v, files changed %q", target, err, eighth.FilesChanged)
	}
	if notices := noticesOf(t, eighth); len(notices) != 1 || !strings.Contains(notices[0], "the member is read-only") {
		t.Errorf("notices = %q, want the refusal shown", notices)
	}
	t.Logf("turn 8: reply %.160q", r.lastReply(thread.ID))

	// Turn 9: a plan approved in read_only goes back in words, and the CLI
	// stays in plan mode.
	r.say(`USE_TOOL ExitPlanMode {"plan":"# Tidy up\n1. Remove dead code"}`, thread.ID)
	ninth, planned := r.decideAll(9, true)
	if planned[0].Tool != "ExitPlanMode" || !strings.Contains(string(planned[0].Input), "Tidy up") {
		t.Errorf("approval = %s %s, want the plan", planned[0].Tool, planned[0].Input)
	}
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, "The plan is approved. This member stays read-only") {
		t.Errorf("the model should have heard the plan passed, got %q", reply)
	}
	for _, notice := range noticesOf(t, ninth) {
		if strings.Contains(notice, "permission mode") {
			t.Errorf("the CLI should have stayed in plan mode: %q", notice)
		}
	}

	// Turn 10: the person's own PermissionRequest hook allows the command
	// before anyone in the room does. The CLI takes its request back, and
	// the room's card is closed as withdrawn.
	hook := `{"hooks":{"PermissionRequest":[{"matcher":"Bash","hooks":[{"type":"command","command":"echo '{\"hookSpecificOutput\":{\"hookEventName\":\"PermissionRequest\",\"decision\":{\"behavior\":\"allow\"}}}'"}]}]}}`
	if err := os.WriteFile(filepath.Join(config, "settings.json"), []byte(hook), 0o600); err != nil {
		t.Fatal(err)
	}
	configure(store.PermissionEditWithApproval)
	r.say(compute, thread.ID)
	tenth := r.waitDone(10)
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, `"content":"42`) {
		t.Errorf("the hook should have let the command run, got %q", reply)
	}
	withdrawn := approvalsOf(t, r, tenth, 1)
	eventually(t, func() bool {
		a, err := r.s.GetApproval(r.ctx, withdrawn[0].ID)
		return err == nil && a.Status == store.ApprovalCancelled && a.Message == withdrawnReason
	}, "the approval to be closed as withdrawn")
}

// approvalsOf returns the approvals turn raised, failing unless there are n.
func approvalsOf(t *testing.T, r *smokeRoom, turn store.Turn, n int) []store.Approval {
	t.Helper()
	approvals, err := r.s.ListTurnApprovals(r.ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(approvals) != n {
		t.Errorf("turn raised %d approval(s), want %d: %+v", len(approvals), n, approvals)
	}
	return approvals
}

// approvalKinds lists the kinds of approvals, comma-separated.
func approvalKinds(approvals []store.Approval) string {
	kinds := make([]string, len(approvals))
	for i, a := range approvals {
		kinds[i] = string(a.Kind)
	}
	return strings.Join(kinds, ",")
}

// noticesOf is the texts of the notices in a turn's transcript.
func noticesOf(t *testing.T, turn store.Turn) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(transcriptOf(t, turn), "\n") {
		var rec struct {
			Event *runtime.Event `json:"event"`
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Event != nil && rec.Event.Kind == runtime.EventNotice {
			out = append(out, rec.Event.Text)
		}
	}
	return out
}
