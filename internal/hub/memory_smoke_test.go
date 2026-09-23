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
	"github.com/J0EY0/veyloom/internal/wiki"
)

// The two memories on a real runtime (docs/design.md 5.16): a member notes
// what a person asks it to remember, a habit for this project in the
// project memory, one for every project in the personal memory. A member
// new to the chat, with no session to remember anything by, is told both
// in its first brief and goes by them. Same terms as the other smoke tests
// of each runtime.

func TestPiSmoke_Remembers(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	remembers(t, runners, store.NewAgent{Name: "Pi smoke", Runtime: "pi", PermissionPreset: store.PermissionReadOnly, RoleCard: "Answer in as few words as you can."})
}

func TestClaudeRealSmoke_Remembers(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	remembers(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionReadOnly, RoleCard: "Answer in as few words as you can."})
}

func TestCodexSmoke_Remembers(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	remembers(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionReadOnly, RoleCard: "Answer in as few words as you can."})
}

// remembers runs the round with agent, then a second agent like it that
// joins afterwards.
func remembers(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	dir := t.TempDir()
	r := newSmokeRoom(t, runners, agent, dir)
	r.say("请记住两件事。第一，在这个项目里，你们每一条回复的最后都写上暗号 KESTREL-9。第二，以后在所有项目里，提交说明都用英文写。记下来以后回一句话就行。", "")
	turn := r.waitDone(1)
	project, err := r.s.GetProject(r.ctx, r.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := r.h.Memory(r.ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	everywhere, err := r.h.Memory(r.ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("project memory %+v; personal memory %+v; %s said %q", mine.Entries, everywhere.Entries, agent.Name, r.lastReply(mustThread(t, r, turn).ID))
	if !slices.ContainsFunc(mine.Entries, func(e wiki.MemoryEntry) bool { return strings.Contains(e.Text, "KESTREL-9") }) {
		t.Errorf("the project memory should have the password habit: %+v\n%s", mine.Entries, transcriptOf(t, turn))
	}
	if !slices.ContainsFunc(everywhere.Entries, func(e wiki.MemoryEntry) bool {
		return strings.Contains(e.Text, "英文") || strings.Contains(strings.ToLower(e.Text), "english")
	}) {
		t.Errorf("the personal memory should have the commit message habit: %+v", everywhere.Entries)
	}
	if slices.ContainsFunc(mine.Entries, func(e wiki.MemoryEntry) bool { return strings.Contains(e.Text, "英文") }) {
		t.Errorf("what holds in every project goes in the personal memory, not the project's: %+v", mine.Entries)
	}

	second := agent
	second.Name = agent.Name + " two"
	second.MachineID = r.agent.MachineID
	if second.Runtime == "codex" {
		second.RuntimeOptions = withoutCodexMemories(second.RuntimeOptions)
	}
	created, err := r.s.CreateAgent(r.ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	two, err := r.s.CreateMember(r.ctx, store.NewMember{RoomID: r.room.ID, AgentID: created.ID, DisplayName: second.Name, RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.h.PostUserMessage(r.ctx, store.NewMessage{
		RoomID: r.room.ID, UserID: r.user.ID, Body: "@" + second.Name + " 用一句话介绍你自己。",
		Mentions: []store.Mention{{Kind: store.MentionAgent, ID: two.ID}},
	}); err != nil {
		t.Fatal(err)
	}
	next := r.waitDone(2)
	prompt := promptOf(t, next)
	for _, want := range []string{"Personal memory, what the person you work for wants in every project", "Project memory, how to work in this project:", "KESTREL-9"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the new member's first brief lacks %q:\n%s", want, prompt)
		}
	}
	reply := r.lastReply(mustThread(t, r, next).ID)
	t.Logf("%s said %q", second.Name, reply)
	if !strings.Contains(reply, "KESTREL-9") {
		t.Errorf("the new member should go by the project memory: %q", reply)
	}
}
