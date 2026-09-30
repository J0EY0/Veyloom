package hub

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// A tool built for the machine, in a skill's folder, runs for the real
// CLIs as it came (docs/design.md 5.11), on the same terms as the other
// smoke tests: VEYLOOM_CLAUDE_REAL_SMOKE=1, VEYLOOM_CODEX_SMOKE=1 or
// VEYLOOM_PI_SMOKE=1, a test database, the CLI installed and signed in.

// TestClaudeRealSmoke_SkillTool: Claude Code runs a skill's tool as the
// skill came, on a member that may run commands unasked.
func TestClaudeRealSmoke_SkillTool(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Claude tools", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	r.toolRound(1, "")
}

// TestCodexSmoke_SkillTool is TestClaudeRealSmoke_SkillTool on Codex, in
// its read-only sandbox, which runs commands that only read.
func TestCodexSmoke_SkillTool(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Codex tools", Runtime: "codex", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	r.toolRound(1, "")
}

// TestPiSmoke_SkillTool is TestClaudeRealSmoke_SkillTool on Pi, whose
// full-auto member alone has bash: the other presets only read.
func TestPiSmoke_SkillTool(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{
		Name: "Pi tools", Runtime: "pi", PermissionPreset: store.PermissionFullAuto,
		RoleCard: "Answer in as few words as you can.",
	}, t.TempDir())
	r.toolRound(1, "")
}

// toolRound imports a skill whose word only its tool prints, a program
// built for this machine that nothing but its mode says runs, installs it
// for the member and, as turn n, asks for the word.
func (r *smokeRoom) toolRound(n int, threadID string) store.Turn {
	r.t.Helper()
	dir := filepath.Join(r.t.TempDir(), "tool-word")
	src := filepath.Join(r.t.TempDir(), "main.go")
	program := "package main\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\nfunc main() { fmt.Printf(\"%s-%d\\n\", strings.ToUpper(\"ce\"+\"dar\"), 3+4) }\n"
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		r.t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", filepath.Join(dir, "bin", "word"), src)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		r.t.Fatalf("build the tool: %v\n%s", err, out)
	}
	skill := "---\nname: tool-word\ndescription: \"Use this skill whenever someone asks for the Veyloom tool word: its tool prints it.\"\n---\n\n" +
		"The Veyloom tool word is what bin/word, in this skill's folder, prints. Run it as it is and reply with just what it printed.\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644); err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.h.ImportSkill(r.ctx, dir, "", r.user.ID); err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.h.InstallSkill(r.ctx, "tool-word", r.agent.ID, true); err != nil {
		r.t.Fatal(err)
	}
	r.say("What is the Veyloom tool word? Use the skill that has it, then reply with just the word.", threadID)
	turn := r.waitDone(n)
	if threadID == "" {
		threadID = topicOf(r.t, r, turn)
	}
	reply := r.lastReply(threadID)
	tx, _ := os.ReadFile(turn.TranscriptPath)
	var calls []string
	for _, line := range strings.Split(string(tx), "\n") {
		if !strings.Contains(line, `"tool_call"`) {
			continue
		}
		// Any call of the turn, whole: a chmod may come on its own, or
		// past where a long path is cut short.
		if strings.Contains(line, "chmod") {
			r.t.Errorf("turn %d made the tool runnable itself; it should have come so:\n%s", n, line)
		}
		var e struct{ Event struct{ Tool, Input string } }
		if strings.Contains(line, "bin/word") && json.Unmarshal([]byte(line), &e) == nil {
			calls = append(calls, e.Event.Tool+" "+e.Event.Input)
		}
	}
	if !strings.Contains(reply, "CEDAR-7") {
		r.t.Errorf("turn %d should have run the skill's tool, said %q, through:\n%s", n, reply, strings.Join(calls, "\n"))
	}
	if !slices.Contains(turn.SkillsUsed, "tool-word") {
		r.t.Errorf("turn %d should be recorded as using the skill: %v", n, turn.SkillsUsed)
	}
	r.t.Logf("turn %d answered %q; the calls naming the tool: %s", n, reply, strings.Join(calls, " | "))
	return turn
}
