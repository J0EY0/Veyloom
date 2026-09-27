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

// The wiki on the real Claude Code and Codex CLIs, a round each of the
// tools, the wiki in the brief, a skill of the library, the upkeep and a
// mounted bundle, on the same terms as their long smoke tests
// (TestClaudeRealSmoke, TestCodexSmoke): VEYLOOM_CLAUDE_REAL_SMOKE=1 or
// VEYLOOM_CODEX_SMOKE=1, a test database, the CLI installed and signed in.
// Pi's wiki rounds are part of TestPiSmoke_SessionLifecycle; the skill
// library across teams is TestPiSmoke_CrossTeam and its likes.

// wikiSmokeRunners builds veyloom for the MCP proxy the CLIs reach the
// tools through: the test binary is not veyloom.
func wikiSmokeRunners(t *testing.T) map[string]runtime.Runner {
	t.Helper()
	proxy := filepath.Join(t.TempDir(), "veyloom")
	if out, err := exec.Command("go", "build", "-o", proxy, "../../cmd/veyloom").CombinedOutput(); err != nil {
		t.Fatalf("build veyloom for the MCP proxy: %v\n%s", err, out)
	}
	// VEYLOOM_RECORD_DIR keeps what the CLIs print, for the replay tests
	// of internal/runtime (docs/design.md 5.23.9).
	return runtime.BuiltinRunnersWith(runtime.RunnerOptions{ToolDir: filepath.Join(t.TempDir(), "tools"), ProxyBinary: proxy, RecordDir: os.Getenv("VEYLOOM_RECORD_DIR")})
}

// TestClaudeRealSmoke_Wiki runs in the read-only preset, which is Claude
// Code's plan mode: the wiki is not the project, so its writes go through
// without anyone asked.
func TestClaudeRealSmoke_Wiki(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Claude wiki", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	turn, commits := r.wikiRound(1, "")
	if got := commits[0].Author; got != "claude-code/haiku" {
		t.Errorf("claude's writes should be signed claude-code/haiku, got %q", got)
	}
	approvalsOf(t, r, turn, 0)
	r.briefRound(2, topicOf(t, r, turn))
	r.skillRound(3, topicOf(t, r, turn))
	r.evolveRound(4, topicOf(t, r, turn))
	r.upkeepRound(5)
	r.mountRound(8, topicOf(t, r, turn))
}

// topicOf is the topic a turn asked in the room opened with its reply.
func topicOf(t *testing.T, r *smokeRoom, turn store.Turn) string {
	t.Helper()
	thread, err := r.s.ThreadForMessage(r.ctx, turn.ReplyMessageID)
	if err != nil {
		t.Fatal(err)
	}
	return thread.ID
}

func TestCodexSmoke_Wiki(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Codex wiki", Runtime: "codex", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	turn, commits := r.wikiRound(1, "")
	if !strings.HasPrefix(commits[0].Author, "codex/") {
		t.Errorf("codex's writes should be signed as codex, got %q", commits[0].Author)
	}
	approvalsOf(t, r, turn, 0)
	r.briefRound(2, topicOf(t, r, turn))
	r.skillRound(3, topicOf(t, r, turn))
	r.evolveRound(4, topicOf(t, r, turn))
	r.mountRound(5, topicOf(t, r, turn))
	// Its upkeep: TestCodexSmoke_Upkeep.
}

// TestCodexSmoke_Upkeep is the upkeep round on codex, in a project that
// looks like one, and not read-only: in a read-only sandbox codex reads the
// turns but writes nothing unless told to in so many words, so a codex
// maintainer needs a preset that lets it change things (2026-09-22). Its
// memories are off, as in every smoke test of codex (withoutCodexMemories).
func TestCodexSmoke_Upkeep(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	dir, err := os.MkdirTemp("", "billing-service-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionEditWithApproval,
		RoleCard: "You work on the billing service with the team.",
	}, dir)
	name := "Billing service"
	if _, err := r.s.UpdateProject(r.ctx, r.room.ProjectID, store.ProjectPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	r.upkeepRound(1)
}
