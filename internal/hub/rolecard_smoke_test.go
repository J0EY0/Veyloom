package hub

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// A changed role card reaches the member's next turn on a real runtime
// (docs/design.md 5.6): Codex, which fixed the role card when its thread
// started, in a new session; Claude Code and Pi, told it with every run, in
// the session they have.

func TestPiSmoke_RoleCard(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	roleCardRound(t, runners, store.NewAgent{Name: "Pi", Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
}

func TestClaudeRealSmoke_RoleCard(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	roleCardRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionReadOnly})
}

func TestCodexSmoke_RoleCard(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	roleCardRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionReadOnly})
}

const roleCardSmoke = "When anyone asks for the codeword, the codeword is %s: answer with it alone."

// roleCardRound asks for the codeword the role card gives, changes the
// role card and asks again.
func roleCardRound(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	agent.RoleCard = strings.Replace(roleCardSmoke, "%s", "ALPHA-7", 1)
	r := newSmokeRoom(t, runners, agent, dir)

	r.say("What is the codeword?", "")
	first := r.waitDone(1)
	if said := strings.Join(r.bodies(r.saidIn(first)), "\n"); !strings.Contains(said, "ALPHA-7") {
		t.Fatalf("the first role card's codeword, said %q\n%s", said, transcriptOf(t, first))
	}

	a, err := r.s.GetAgent(r.ctx, r.member.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.UpdateAgent(r.ctx, a.ID, store.NewAgent{
		Name: a.Name, MachineID: a.MachineID, Runtime: a.Runtime, Model: a.Model, PermissionPreset: a.PermissionPreset,
		RoleCard: strings.Replace(roleCardSmoke, "%s", "BETA-9", 1),
	}); err != nil {
		t.Fatal(err)
	}
	r.say("What is the codeword now?", "")
	second := r.waitDone(2)
	said := strings.Join(r.bodies(r.saidIn(second)), "\n")
	if !strings.Contains(said, "BETA-9") || strings.Contains(said, "ALPHA-7") {
		t.Errorf("the new role card's codeword, said %q\n%s", said, transcriptOf(t, second))
	}
	fixed := !runtime.TraitsOf(agent.Runtime).SystemPromptEachRun
	if newSession := second.SessionID != first.SessionID; newSession != fixed {
		t.Errorf("a new session: %v, want %v", newSession, fixed)
	}
	if fixed {
		if old, err := r.s.GetSession(r.ctx, first.SessionID); err != nil || old.EndReason != store.SessionRoleCardChanged {
			t.Errorf("the old session: %+v %v", old, err)
		}
	}
	t.Logf("%s: %q, then %q (session %s, then %s)", agent.Runtime, r.bodies(r.saidIn(first)), said, first.SessionID, second.SessionID)
}
