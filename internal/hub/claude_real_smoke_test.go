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

// TestClaudeRealSmoke drives the real Claude Code CLI with the real model,
// through the hub. It spends a few short model calls on the account claude
// is signed in to (on haiku) and leaves its sessions in claude's own store,
// so it runs only when VEYLOOM_CLAUDE_REAL_SMOKE=1 (and a test database is
// configured, and claude is installed and signed in):
//
//	VEYLOOM_CLAUDE_REAL_SMOKE=1 VEYLOOM_TEST_DATABASE_URL=... go test -run TestClaudeRealSmoke ./internal/hub/ -v -timeout 20m
//
// TestClaudeSmoke already proves the protocol with a scripted model; this
// proves a real model works it: that a session is resumed and remembers,
// that the model finds and calls the room tools, that a command waits for a
// person and runs or not as they say, that AskUserQuestion is answered on
// its card, and that under read_only a plan reaches people and nothing is
// written.
func TestClaudeRealSmoke(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}

	proxy := filepath.Join(t.TempDir(), "veyloom")
	if out, err := exec.Command("go", "build", "-o", proxy, "../../cmd/veyloom").CombinedOutput(); err != nil {
		t.Fatalf("build veyloom for the MCP proxy: %v\n%s", err, out)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{ToolDir: filepath.Join(t.TempDir(), "tools"), ProxyBinary: proxy})

	dir := t.TempDir()
	r := newSmokeRoom(t, runners, store.NewAgent{
		Name: "Claude real", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionEditWithApproval,
		RoleCard: "Answer in as few words as you can.",
	}, dir)
	agent := r.agent

	// Turn 1 starts the session under the name the hub gave it.
	r.say("Remember the word banana. Reply with exactly: ok", "")
	first := r.waitDone(1)
	thread := mustThread(t, r, first)
	session, err := r.s.GetOpenSession(r.ctx, r.member.ID)
	if err != nil || !session.Started() || session.Ref == "" {
		t.Fatalf("session after the first turn = %+v, %v", session, err)
	}
	if first.Usage.OutputTokens == 0 {
		t.Errorf("turn 1 should report the tokens it spent, got %+v", first.Usage)
	}
	t.Logf("turn 1: claude said %q in session %s, spending %+v", r.lastReply(thread.ID), session.Ref, first.Usage)

	// Turn 2 resumes it: the word is only in the first brief.
	r.say("Which word did I ask you to remember? One word.", thread.ID)
	r.waitDone(2)
	if reply := r.lastReply(thread.ID); !strings.Contains(strings.ToLower(reply), "banana") {
		t.Errorf("the resumed session should remember banana, claude said %q", reply)
	}

	// Turn 3: the room tools, through the proxy, asking nobody.
	r.say("Call your list_topics tool, then reply with just the number of topics it lists.", thread.ID)
	third := r.waitDone(3)
	if got := toolResults(t, third, "mcp__veyloom__list_topics"); len(got) == 0 || !strings.Contains(got[0], "Topics, most recently active first") {
		t.Errorf("list_topics should have answered, got %q", got)
	}
	approvalsOf(t, r, third, 0)
	t.Logf("turn 3: claude said %q", r.lastReply(thread.ID))

	// Turn 4: a command waits for a person, who allows it.
	r.say(`Run this exact command with your Bash tool: python3 -c "open('allowed.txt','w').close()"`+"\nThen reply with exactly: done", thread.ID)
	_, allowed := r.decideAll(4, true)
	if allowed[0].Tool != "Bash" {
		t.Errorf("the approval asked about %s %s, want the command", allowed[0].Tool, allowed[0].Input)
	}
	if _, err := os.Stat(filepath.Join(dir, "allowed.txt")); err != nil {
		t.Errorf("the allowed command should have run: %v", err)
	}

	// Turn 5: the person denies one, and it does not run.
	r.say(`Run this exact command with your Bash tool: python3 -c "open('denied.txt','w').close()"`+"\nIf it is declined, do not try anything else; reply with exactly: declined", thread.ID)
	r.decideAll(5, false)
	if _, err := os.Stat(filepath.Join(dir, "denied.txt")); !os.IsNotExist(err) {
		t.Errorf("the denied command should not have run: %v", err)
	}
	t.Logf("turn 5: claude said %q", r.lastReply(thread.ID))

	// Turn 6: a question, answered on its card.
	r.say("Use your AskUserQuestion tool to ask me which colour I prefer, with the two options red and blue. Then reply with just the colour I chose.", thread.ID)
	_, asked := r.decideAllWith(6, func(a store.Approval) runtime.Decision {
		if a.Kind != store.ApprovalQuestion {
			return runtime.Decision{Message: "expected a question"}
		}
		return runtime.Decision{Allow: true, Answer: json.RawMessage(`{"answers":{"1":["blue"]}}`)}
	})
	if asked[0].Kind != store.ApprovalQuestion {
		t.Errorf("the request = %s %s, want a question", asked[0].Kind, asked[0].Input)
	}
	if reply := r.lastReply(thread.ID); !strings.Contains(strings.ToLower(reply), "blue") {
		t.Errorf("claude should have got the answer, said %q", reply)
	}
	t.Logf("turn 6: claude asked %s and said %q", asked[0].Input, r.lastReply(thread.ID))

	// Turns 7 and 8 under read_only: a plan reaches people, and approving it
	// changes nothing; a write is turned down without anyone asked.
	if _, err := r.s.UpdateAgent(r.ctx, agent.ID, store.NewAgent{
		Name: agent.Name, MachineID: agent.MachineID, Runtime: agent.Runtime, Model: agent.Model, RoleCard: agent.RoleCard,
		PermissionPreset: store.PermissionReadOnly,
	}); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(dir, "notes.md")
	r.say("Plan how you would add a file notes.md that says hello. Put the plan up for approval with your ExitPlanMode tool and do nothing else.", thread.ID)
	seventh, planned := r.decideAll(7, true)
	if planned[0].Tool != "ExitPlanMode" {
		t.Errorf("the approval asked about %s %s, want the plan", planned[0].Tool, planned[0].Input)
	}
	// Plan mode has the CLI write the plan to a file of its own, under
	// ~/.claude/plans; the project is left alone.
	if _, err := os.Stat(notes); !os.IsNotExist(err) || changedIn(seventh, dir) {
		t.Errorf("an approved plan must not be carried out under read_only: %v, files %q", err, seventh.FilesChanged)
	}
	t.Logf("turn 7: plan %.200s; claude said %q", planned[0].Input, r.lastReply(thread.ID))

	r.say("Now create notes.md containing hello with your Write tool.", thread.ID)
	eighth := r.waitDone(8)
	if _, err := os.Stat(notes); !os.IsNotExist(err) || changedIn(eighth, dir) {
		t.Errorf("read_only must not write: %v, files %q", err, eighth.FilesChanged)
	}
	approvalsOf(t, r, eighth, 0)
	t.Logf("turn 8: notices %q; claude said %q", noticesOf(t, eighth), r.lastReply(thread.ID))
}

// changedIn reports whether turn wrote any file under dir.
func changedIn(turn store.Turn, dir string) bool {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		real = dir
	}
	for _, path := range turn.FilesChanged {
		if resolved, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
			path = filepath.Join(resolved, filepath.Base(path))
		}
		for _, root := range []string{dir, real} {
			if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
				return true
			}
		}
	}
	return false
}
