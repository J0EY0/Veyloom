package hub

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	veyloom "github.com/J0EY0/veyloom"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Veyloom's own skills reach a real runtime (docs/design.md 5.23.6): asked
// what team-practices says of handing work back, the member loads the
// skill, its runtime's own way, and answers from it.

func TestPiSmoke_BuiltinSkill(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	builtinSkillRound(t, runners, store.NewAgent{Name: "Pi", Runtime: "pi", PermissionPreset: store.PermissionFullAuto})
}

func TestClaudeRealSmoke_BuiltinSkill(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	builtinSkillRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto})
}

func TestCodexSmoke_BuiltinSkill(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	builtinSkillRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionFullAuto})
}

// builtinSkillRound asks the member what team-practices says.
func builtinSkillRound(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	sub, err := fs.Sub(veyloom.Skills, "skills")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := newSmokeRoom(t, runners, agent, dir, WithSkills(sub))
	r.say("Load your team-practices skill and answer from it: what are the five questions to answer when handing work back? "+
		"Reply with the five, in order, one per line, as the skill words them.", "")
	done := r.waitDone(1)
	tx := transcriptOf(t, done)
	if !strings.Contains(tx, "team-practices") {
		t.Errorf("the turn should have loaded team-practices:\n%s", tx)
	}
	thread, err := r.s.GetThread(r.ctx, done.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	reply := strings.ToLower(r.lastReply(thread.ID))
	for _, want := range []string{"changed", "where", "evidence", "uncertain", "next"} {
		if !strings.Contains(reply, want) {
			t.Errorf("the reply should name %q, said %q", want, reply)
		}
	}
	t.Logf("%s answered %q", agent.Runtime, reply)
}
