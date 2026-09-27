package hub

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// A member drafts what only a person does, on a real runtime
// (docs/design.md 5.23.5): asked to have a skill of the library installed
// for itself, it drafts that with draft_action, saying what it does then;
// a person runs the card, and the member is woken with what came of it and
// goes on.

func TestPiSmoke_Draft(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	draftRound(t, runners, store.NewAgent{Name: "Pi", Runtime: "pi", PermissionPreset: store.PermissionFullAuto})
}

func TestClaudeRealSmoke_Draft(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	draftRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto})
}

func TestCodexSmoke_Draft(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	draftRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionFullAuto})
}

// draftRound has the member draft a skill's install for itself, and a
// person run it.
func draftRound(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := newSmokeRoom(t, runners, agent, dir)
	skill := filepath.Join(t.TempDir(), "go-testing")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: go-testing\ndescription: Use when writing Go tests.\n---\n\nWrite the cases as a table.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.h.ImportSkill(r.ctx, skill, "", r.user.ID); err != nil {
		t.Fatal(err)
	}

	asked := r.say("Only a person can install a skill of the library for you. Call your draft_action tool with kind install_skill, skill go-testing, "+
		`and its "then" parameter set to: say the single word PAPAYA. After the call, reply with the single word DRAFTED.`, "")
	first := r.waitDone(1)
	for _, line := range strings.Split(transcriptOf(t, first), "\n") {
		if strings.Contains(line, `"kind":"tool_call"`) && strings.Contains(line, "draft_action") {
			t.Logf("the call: %s", line)
		}
	}
	thread, err := r.s.GetThread(r.ctx, first.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	drafts, err := r.s.ListThreadDrafts(r.ctx, thread.ID)
	if err != nil || len(drafts) != 1 {
		t.Fatalf("one draft: %+v %v\n%s", drafts, err, transcriptOf(t, first))
	}
	d := drafts[0]
	if d.Kind != store.DraftInstallSkill || d.Params.Skill != "go-testing" || d.TargetID != r.member.ID || d.Then == "" || d.MessageID == "" {
		t.Errorf("draft = %+v", d)
	}
	t.Logf("%s drafted, then %q (asked %s)", agent.Runtime, d.Then, asked.ID)

	ran, err := r.h.RunDraft(r.ctx, d.ID, r.user.ID, DraftEdit{})
	if err != nil || ran.Status != store.DraftDone {
		t.Fatalf("ran: %+v %v", ran, err)
	}
	if a, err := r.s.GetAgent(r.ctx, r.member.AgentID); err != nil || !slices.Contains(a.Skills, "go-testing") {
		t.Errorf("installed: %+v %v", a.Skills, err)
	}
	woken := r.waitDone(2)
	told, err := r.s.GetDraft(r.ctx, d.ID)
	if err != nil || woken.TriggerMessageID != told.ResultMessageID || woken.ChainMessageID != told.ResultMessageID {
		t.Fatalf("woken with what came of it: %+v %+v %v", woken, told, err)
	}
	var said []string
	for _, m := range r.messagesOf(thread.ID) {
		if m.TurnID == woken.ID && m.SenderKind == store.SenderAgent {
			said = append(said, m.Body)
		}
	}
	if !strings.Contains(strings.ToUpper(strings.Join(said, "\n")), "PAPAYA") {
		t.Errorf("the woken turn should do what it said then, said %q\n%s", said, transcriptOf(t, woken))
	}
	t.Logf("%s, woken: %q", agent.Runtime, said)
}
