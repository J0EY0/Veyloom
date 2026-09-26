package hub

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// TestClaudeSmoke_AlwaysAllowed checks on the real CLI what the fake CLI in
// the runtime tests cannot (docs/design.md 4.6): that what a person allows
// always reaches Claude Code, as settings, and that it matches the rules
// itself. A command allowed always runs unasked in the next turn, and so
// does one whose rule has parentheses, a comma and spaces of its own,
// which the CLI's own splitting of --allowedTools would break. Like
// TestClaudeSmoke it runs when VEYLOOM_CLAUDE_SMOKE=1:
//
//	VEYLOOM_CLAUDE_SMOKE=1 VEYLOOM_TEST_DATABASE_URL=... go test -run TestClaudeSmoke_AlwaysAllowed ./internal/hub/ -v
//
// Claude Code asks about some commands whatever the rules say, such as
// python3 -c with code to run; those are no test of the rules.
func TestClaudeSmoke_AlwaysAllowed(t *testing.T) {
	cli := claudeSmokeCLI(t)
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{ToolDir: filepath.Join(t.TempDir(), "tools"), ProxyBinary: cli.proxy})
	runners["claude"] = runtime.NewClaudeRunner(runtime.ClaudeConfig{Binary: cli.bin, StreamPartials: true, ProxyBinary: cli.proxy})
	r := newSmokeRoom(t, runners, store.NewAgent{
		Name: "Claude smoke", Runtime: "claude", PermissionPreset: store.PermissionEditWithApproval, Model: "claude-sonnet-4-5",
	}, t.TempDir())
	const test = `USE_TOOL Bash {"command":"go test ./... -v","description":"run the tests"}`

	// Turn 1: the command is allowed always, with the rule the CLI offered.
	r.say(test, "")
	var offered []store.Approval
	first := r.waitTurn(1, func() {
		pending, err := r.s.ListPendingRoomApprovals(r.ctx, r.room.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range pending {
			d, err := r.h.DecideApproval(r.ctx, a.ID, r.user.ID, runtime.Decision{Allow: true}, store.ScopeAlways)
			if err != nil {
				t.Fatalf("allow always %s (offer %s): %v", a.Input, a.Similar, err)
			}
			offered = append(offered, d)
		}
	})
	if len(offered) != 1 {
		t.Fatalf("turn 1 asked %d times, want once", len(offered))
	}
	thread, err := r.s.ThreadForMessage(r.ctx, first.ReplyMessageID)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := r.s.ListMemberRules(r.ctx, r.member.ID, "claude")
	// Older CLIs write the rule Bash(go test:*), newer ones Bash(go test *).
	if err != nil || len(rules) != 1 || (rules[0].Rule != "Bash(go test:*)" && rules[0].Rule != "Bash(go test *)") {
		t.Fatalf("rules kept: %+v, %v (offered %s)", rules, err, offered[0].Similar)
	}

	// Turn 2: the same command runs without anyone being asked.
	r.say(test, thread.ID)
	second := r.waitDone(2)
	approvalsOf(t, r, second, 0)
	if reply := r.lastReply(thread.ID); !strings.Contains(reply, "./...") {
		t.Errorf("go test should have run (and found no module to test), the model got %q", reply)
	}

	// Turn 3: a rule with parentheses, a comma and spaces of its own stays
	// one rule.
	commit := `Bash(git commit -m "fix(tags): trim, then sort")`
	if _, err := r.s.AddMemberRules(r.ctx, store.NewMemberRules{MemberID: r.member.ID, Runtime: "claude", Rules: []string{commit}}); err != nil {
		t.Fatal(err)
	}
	r.say(`USE_TOOL Bash {"command":"git commit -m \"fix(tags): trim, then sort\"","description":"commit"}`, thread.ID)
	third := r.waitDone(3)
	approvalsOf(t, r, third, 0)
	t.Logf("turn 3: reply %.160q", r.lastReply(thread.ID))
}
