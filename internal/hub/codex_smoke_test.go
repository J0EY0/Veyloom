package hub

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// TestCodexSmoke drives the real codex CLI through the hub. It spends a few
// model calls on the account codex is signed in to and leaves one thread in
// codex's own session store, so it runs only when VEYLOOM_CODEX_SMOKE=1
// (and a test database is configured, and codex is installed and signed
// in):
//
//	VEYLOOM_CODEX_SMOKE=1 VEYLOOM_TEST_DATABASE_URL=... go test -run TestCodexSmoke ./internal/hub/ -v -timeout 20m
//
// What it proves is what the fake app-server in the runtime tests cannot:
// that codex takes the requests as the runner writes them and reports back
// as the runner reads it, that the thread one turn starts is resumed by the
// next and remembers it, that codex starts the room tools' MCP server from
// the thread's config and reaches the turn's endpoint through it from the
// read-only sandbox, that a command waiting on a person runs when they
// allow it and does not when they deny it, that the room tools are not
// held up for approval under a policy that asks, that what Codex's own
// reviewer decides is recorded for people to see, that an MCP server's form
// and link reach a person and their answer the server, that a question
// Codex puts to a person is answered on its card, and that codex asking for
// more than its sandbox allows reaches a person as an approval.
func TestCodexSmoke(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}

	// Codex runs the room tools' MCP server as `veyloom mcp-proxy`, and the
	// test binary is not veyloom: build one.
	proxy := filepath.Join(t.TempDir(), "veyloom")
	if out, err := exec.Command("go", "build", "-o", proxy, "../../cmd/veyloom").CombinedOutput(); err != nil {
		t.Fatalf("build veyloom for the MCP proxy: %v\n%s", err, out)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{ToolDir: filepath.Join(t.TempDir(), "tools"), ProxyBinary: proxy})

	dir := t.TempDir()
	r := newSmokeRoom(t, runners, store.NewAgent{
		Name: "Codex smoke", Runtime: "codex", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, dir)

	// Turn 1 starts a thread, which codex names; the hub keeps the name.
	r.say("Remember the word banana. Reply with exactly: ok", "")
	first := r.waitDone(1)
	thread, err := r.s.ThreadForMessage(r.ctx, first.ReplyMessageID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := r.s.GetOpenSession(r.ctx, r.member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID != session.ID || !session.Started() || session.Ref == "" {
		t.Fatalf("session after the first turn = %+v (turn ran in %s)", session, first.SessionID)
	}
	if first.Usage.OutputTokens == 0 {
		t.Errorf("turn 1 should report the tokens it spent, got %+v", first.Usage)
	}
	t.Logf("turn 1: codex said %q in thread %s, spending %+v", r.lastReply(thread.ID), session.Ref, first.Usage)

	// Turn 2 resumes the thread. The word is only in the first brief, so the
	// answer can only come from the thread.
	r.say("Which word did I ask you to remember? One word.", thread.ID)
	second := r.waitDone(2)
	if second.SessionID != session.ID {
		t.Fatalf("the second turn ran in session %s, want %s", second.SessionID, session.ID)
	}
	if prompt := promptOf(t, second); strings.Contains(strings.ToLower(prompt), "banana") {
		t.Errorf("the second brief should not repeat what the thread has read:\n%s", prompt)
	}
	if reply := r.lastReply(thread.ID); !strings.Contains(strings.ToLower(reply), "banana") {
		t.Errorf("codex should remember the word, said %q", reply)
	}
	t.Logf("turn 2 resumed the thread: codex said %q", r.lastReply(thread.ID))

	// Turn 3: the room tools from inside the read-only sandbox. Codex starts
	// the MCP server the thread's config names, the proxy reaches the
	// turn's endpoint, and the machine asks the hub.
	r.say("Call your list_topics tool, then tell me how many topics this chat has. Reply with just the number.", thread.ID)
	third := r.waitDone(3)
	if got := toolResults(t, third, "veyloom/list_topics"); len(got) == 0 || !strings.Contains(got[0], "Topics, most recently active first") {
		t.Fatalf("codex should have called list_topics and got the directory, got %q:\n%s", got, transcriptOf(t, third))
	}
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, "1") {
		t.Errorf("the chat has one topic, codex said %q", reply)
	}
	t.Logf("turn 3 with the room tools: codex said %q", r.lastReply(thread.ID))

	// Turns 4 and 5 run commands that wait on a person. The preset lets
	// codex write the workspace; untrusted makes it ask before any command
	// outside its known-safe list, which touch is not on.
	agent := r.agent
	if _, err := r.s.UpdateAgent(r.ctx, agent.ID, store.NewAgent{
		Name: agent.Name, MachineID: agent.MachineID, Runtime: agent.Runtime, RoleCard: agent.RoleCard,
		PermissionPreset: store.PermissionEditWithApproval,
		RuntimeOptions:   map[string]any{"approval_policy": "untrusted"},
	}); err != nil {
		t.Fatal(err)
	}

	r.say("Run this exact shell command: touch allowed.txt\nThen reply with exactly: done", thread.ID)
	_, allowed := r.decideAll(4, true)
	if allowed[0].Tool != "commandExecution" || !strings.Contains(string(allowed[0].Input), "allowed.txt") {
		t.Errorf("the first approval asked about %s %s, want the touch command", allowed[0].Tool, allowed[0].Input)
	}
	if _, err := os.Stat(filepath.Join(dir, "allowed.txt")); err != nil {
		t.Errorf("the allowed command should have run: %v", err)
	}
	t.Logf("turn 4: allowed %d request(s), codex said %q", len(allowed), r.lastReply(thread.ID))

	r.say("Run this exact shell command: touch denied.txt\nIf it is declined, do not try anything else; reply with exactly: declined", thread.ID)
	fifth, denied := r.decideAll(5, false)
	if _, err := os.Stat(filepath.Join(dir, "denied.txt")); !os.IsNotExist(err) {
		t.Errorf("the denied command should not have run: %v", err)
	}
	if got := toolResults(t, fifth, "commandExecution"); len(got) == 0 || got[0] != "declined" {
		t.Errorf("the command's result should say it was declined, got %q", got)
	}
	t.Logf("turn 5: denied %d request(s), codex said %q", len(denied), r.lastReply(thread.ID))

	// Turn 6: the room tools under a policy that asks. Their server is set
	// up never to ask (default_tools_approval_mode); were it to, codex would
	// ask by MCP elicitation, which reaches a person as a form, and the turn
	// would wait for them.
	r.say("Call your list_topics tool again, then reply with just the number of topics.", thread.ID)
	sixth := r.waitDone(6)
	if got := toolResults(t, sixth, "veyloom/list_topics"); len(got) == 0 || !strings.Contains(got[0], "Topics, most recently active first") {
		t.Errorf("the room tools should answer under untrusted without asking, got %q:\n%s", got, transcriptOf(t, sixth))
	}
	t.Logf("turn 6 with the room tools under untrusted: codex said %q", r.lastReply(thread.ID))

	// Turn 7: Codex's own reviewer decides instead of a person
	// (approvals_reviewer), and people see what it decided and why, recorded
	// as settled. A thread keeps the reviewer it started with, so the turn
	// starts a new one.
	if err := r.s.ResetSession(r.ctx, r.member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.UpdateAgent(r.ctx, agent.ID, store.NewAgent{
		Name: agent.Name, MachineID: agent.MachineID, Runtime: agent.Runtime, RoleCard: agent.RoleCard,
		PermissionPreset: store.PermissionEditWithApproval,
		RuntimeOptions:   map[string]any{"extra_args": []any{"-c", `approvals_reviewer="auto_review"`}},
	}); err != nil {
		t.Fatal(err)
	}
	r.say("Run exactly this shell command: curl -sI https://example.com | head -1\nThe sandbox blocks the network, so run it with escalated permissions right away. Then reply with just the first line it printed.", thread.ID)
	seventh := r.waitDone(7)
	settled, err := r.s.ListTurnApprovals(r.ctx, seventh.ID)
	if err != nil {
		t.Fatal(err)
	}
	var reviewed []store.Approval
	for _, a := range settled {
		if a.Reviewer == "codex_auto_review" {
			reviewed = append(reviewed, a)
		}
	}
	if len(reviewed) == 0 || reviewed[0].Status == store.ApprovalPending || reviewed[0].MessageID == "" {
		t.Fatalf("Codex's reviewer should have decided and the thread said so; approvals = %+v\n%s", settled, transcriptOf(t, seventh))
	}
	t.Logf("turn 7: Codex's reviewer %s %s %s (%s): %s; codex said %q", reviewed[0].Status, reviewed[0].Tool, reviewed[0].Input, reviewed[0].Answer, reviewed[0].Message, r.lastReply(thread.ID))

	// The rest asks the person through tools Codex has yet to turn on by
	// default; the agent's extra_args turn them on for its app-server. Codex
	// may be set up to have what it asks reviewed by a subagent of its own
	// (approvals_reviewer) rather than by a person, so they also send it to
	// the person, and since a thread keeps the reviewer it started with,
	// each starts a new one. A codex without the tool skips the turn.
	n := 7
	// configure gives the agent's app-server these arguments, and starts the
	// member on a new thread, which keeps the reviewer it starts with.
	configure := func(extra ...any) {
		if _, err := r.s.UpdateAgent(r.ctx, agent.ID, store.NewAgent{
			Name: agent.Name, MachineID: agent.MachineID, Runtime: agent.Runtime, RoleCard: agent.RoleCard,
			PermissionPreset: store.PermissionEditWithApproval,
			RuntimeOptions:   map[string]any{"extra_args": extra},
		}); err != nil {
			t.Fatal(err)
		}
		if err := r.s.ResetSession(r.ctx, r.member.ID); err != nil {
			t.Fatal(err)
		}
	}

	// An MCP server asks the person through Codex (MCP elicitation): a form
	// to fill in, then a link to open. Its tools are let through without
	// asking, so what reaches the person is the server's own request.
	elicit := filepath.Join(t.TempDir(), "elicitmcp")
	if out, err := exec.Command("go", "build", "-o", elicit, "./testdata/elicitmcp").CombinedOutput(); err != nil {
		t.Fatalf("build the elicitation server: %v\n%s", err, out)
	}
	configure("-c", `approvals_reviewer="user"`, "-c", "mcp_servers.elicit.command="+strconv.Quote(elicit), "-c", `mcp_servers.elicit.default_tools_approval_mode="approve"`)
	n++
	r.say("Call the deploy_form tool of the elicit MCP server, then reply with exactly what it returned.", thread.ID)
	_, forms := r.decideAllWith(n, func(a store.Approval) runtime.Decision {
		if a.Kind != store.ApprovalForm {
			return runtime.Decision{Allow: false, Message: "expected a form"}
		}
		return runtime.Decision{Allow: true, Answer: json.RawMessage(`{"content":{"region":"eu"}}`)}
	})
	if forms[0].Kind != store.ApprovalForm || !strings.Contains(string(forms[0].Input), "Which region") {
		t.Errorf("the request = %s %s, want the server's form", forms[0].Kind, forms[0].Input)
	}
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, `accept {"region":"eu"}`) {
		t.Errorf("the server should have got the form, codex said %q", reply)
	}
	t.Logf("turn %d: filled in %s, codex said %q", n, forms[0].Input, r.lastReply(thread.ID))

	n++
	r.say("Call the sign_in tool of the elicit MCP server, then reply with exactly what it returned.", thread.ID)
	_, links := r.decideAllWith(n, func(a store.Approval) runtime.Decision {
		return runtime.Decision{Allow: a.Kind == store.ApprovalLink, Message: "expected a link"}
	})
	if links[0].Kind != store.ApprovalLink || !strings.Contains(string(links[0].Input), "https://example.com/device") {
		t.Errorf("the request = %s %s, want the server's link", links[0].Kind, links[0].Input)
	}
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, "accept") {
		t.Errorf("the server should have heard the link was opened, codex said %q", reply)
	}
	t.Logf("turn %d: opened %s, codex said %q", n, links[0].Input, r.lastReply(thread.ID))

	features, _ := exec.Command("codex", "features", "list").Output()
	askWith := func(feature string) bool {
		if !strings.Contains(string(features), feature) {
			t.Logf("skipped: this codex has no %s feature", feature)
			return false
		}
		configure("--enable", feature, "-c", `approvals_reviewer="user"`)
		n++
		return true
	}

	// Codex puts a question to the person: it reaches a question card, and
	// the answer given there is what Codex gets.
	if askWith("default_mode_request_user_input") {
		r.say("Use your request_user_input tool to ask me which colour I prefer, offering red and blue as the options. Then reply with just the colour I chose.", thread.ID)
		_, asked := r.decideAllWith(n, func(a store.Approval) runtime.Decision {
			var set runtime.QuestionSet
			if a.Kind != store.ApprovalQuestion || json.Unmarshal(a.Input, &set) != nil || len(set.Questions) == 0 {
				return runtime.Decision{Allow: false, Message: "expected a question"}
			}
			return runtime.Decision{Allow: true, Answer: json.RawMessage(fmt.Sprintf(`{"answers":{%q:["blue"]}}`, set.Questions[0].ID))}
		})
		if asked[0].Kind != store.ApprovalQuestion || asked[0].Tool != "requestUserInput" {
			t.Errorf("the request = %s %s %s, want a question", asked[0].Kind, asked[0].Tool, asked[0].Input)
		}
		if reply := r.lastReply(thread.ID); !strings.Contains(strings.ToLower(reply), "blue") {
			t.Errorf("codex should have got the answer, said %q", reply)
		}
		t.Logf("turn %d: codex asked %s, was told blue and said %q", n, asked[0].Input, r.lastReply(thread.ID))
	}

	// Codex asks for more than its sandbox allows, here the network.
	if askWith("request_permissions_tool") {
		r.say("Use your request_permissions tool to ask for network access. Then reply with exactly: granted, or: refused, whichever you got.", thread.ID)
		_, asked := r.decideAll(n, true)
		if asked[0].Tool != "permissions" || !strings.Contains(string(asked[0].Input), "network") {
			t.Errorf("the approval asked about %s %s, want the network permission", asked[0].Tool, asked[0].Input)
		}
		if reply := r.lastReply(thread.ID); !strings.Contains(strings.ToLower(reply), "granted") {
			t.Errorf("codex should have got the network, said %q", reply)
		}
		t.Logf("turn %d: allowed %s %s, codex said %q", n, asked[0].Tool, asked[0].Input, r.lastReply(thread.ID))
	}
}

// toolResults returns what the turn's calls to tool reported, in order.
func toolResults(t *testing.T, turn store.Turn, tool string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(transcriptOf(t, turn), "\n") {
		var rec struct {
			Event *runtime.Event `json:"event"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil || rec.Event == nil {
			continue
		}
		if ev := rec.Event; ev.Kind == runtime.EventToolResult && ev.Tool == tool {
			out = append(out, ev.Text)
		}
	}
	return out
}
