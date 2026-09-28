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

// A person's message to the room that names no member, on a real runtime
// (docs/design.md 4.2): it goes to the project's leader, which is told why
// it has it and hands it on with send_message to the member whose work it
// is; that member takes it up in the same piece of work, and the leader
// sums up for the person. The member is played by the fake runtime, so the
// model's part is the leader's alone: seeing whose work it is, and handing
// it on rather than doing it. Both are in the full-auto preset, working in
// a plain directory, so no worktrees.

func TestPiSmoke_Dispatch(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	dispatch(t, runners, store.NewAgent{Name: "Pi", Runtime: "pi", PermissionPreset: store.PermissionFullAuto})
}

func TestClaudeRealSmoke_Dispatch(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	dispatch(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto})
}

func TestCodexSmoke_Dispatch(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	dispatch(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionFullAuto})
}

// dispatch runs the round with a leader made like agent and a coder on the
// fake runtime.
func dispatch(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A Go module, so that the leader checking what the coder wrote finds
	// code that builds.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module smoke\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lead := agent
	lead.Name = agent.Name + " lead"
	lead.RoleCard = "You lead the project: you plan the work and check it, and leave writing the code to the members."
	// The first member, and so the leader.
	r := newSmokeRoom(t, runners, lead, dir)
	made, err := r.s.CreateAgent(r.ctx, store.NewAgent{
		Name: "Coder", Runtime: "fake", MachineID: r.h.Machines()[0].ID, PermissionPreset: store.PermissionFullAuto,
		RoleCard: "You write the project's Go code.",
		RuntimeOptions: map[string]any{
			"write": []any{"calc.go"}, "content": "package calc\n\n// Add returns the sum of a and b.\nfunc Add(a, b int) int { return a + b }\n",
			"reply": "Added Add to calc.go.",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	coder, err := r.s.CreateMember(r.ctx, store.NewMember{RoomID: r.room.ID, AgentID: made.ID, DisplayName: "Coder", RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if to, err := r.h.Addressee(r.ctx, r.room.ID, "", r.user.ID); err != nil || to.Member.ID != r.member.ID || to.Reason != AddresseeLeader {
		t.Fatalf("a message to the room that names no member goes to %+v (%v), not the leader", to, err)
	}

	asked := r.sayToNoMember("在项目根目录加一个 calc.go，写一个 Add(a, b int) int 函数，把两个数加起来。", "")
	// The leader, the coder, and the leader summing up; a real model may
	// go on a little, checking what the coder did, say.
	turns := r.waitQuiet(3)
	t.Logf("%d turns", len(turns))
	handed := firstTurnOf(turns, r.member)
	if handed.TriggerMessageID != asked.ID {
		t.Fatalf("the leader did not take the message: %+v", turns)
	}
	sends := talkCalls(t, handed)
	work, ok := wokenBy(turns, coder, handed)
	if len(sends) == 0 || !ok || work.ChainMessageID != asked.ID {
		t.Fatalf("the leader did not hand it on to Coder: it called %v; Coder's turn %+v\n%s", toolsCalled(t, handed), work, transcriptOf(t, handed))
	}
	if slices.Contains(handed.FilesChanged, "calc.go") {
		t.Errorf("the leader wrote calc.go itself\n%s", transcriptOf(t, handed))
	}
	handedOn, err := r.s.GetMessage(r.ctx, work.TriggerMessageID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the leader handed it on: %q", handedOn.Body)

	// Woken once what it handed on is done, the leader sums up.
	var summed store.Turn
	var trigger store.Message
	for i := len(turns) - 1; i >= 0 && summed.ID == ""; i-- {
		turn := turns[i]
		if turn.MemberID != r.member.ID || turn.ID == handed.ID || turn.ChainMessageID != asked.ID {
			continue
		}
		if trigger, err = r.s.GetMessage(r.ctx, turn.TriggerMessageID); err == nil && strings.Contains(trigger.Body, "handed on is done") && turn.ThreadID == handed.ThreadID {
			summed = turn
		}
	}
	if summed.ID == "" {
		t.Fatalf("the leader did not sum up: %+v", turns)
	}
	said, err := r.s.GetMessage(r.ctx, summed.ReplyMessageID)
	if err != nil || !slices.Contains(said.Mentions, store.Mention{Kind: store.MentionUser, ID: r.user.ID}) {
		t.Errorf("the summing up is not addressed to the person: %+v %v", said, err)
	} else {
		t.Logf("the leader summed up: %q", said.Body)
	}
}

// wokenBy is member's turn among turns that by woke.
func wokenBy(turns []store.Turn, member store.Member, by store.Turn) (store.Turn, bool) {
	for _, turn := range turns {
		if turn.MemberID == member.ID && turn.WokenByTurnID == by.ID {
			return turn, true
		}
	}
	return store.Turn{}, false
}

// sayToNoMember posts body from the person, naming no member, in the
// thread or, with threadID empty, in the room itself.
func (r *smokeRoom) sayToNoMember(body, threadID string) store.Message {
	r.t.Helper()
	msg, err := r.h.PostUserMessage(r.ctx, store.NewMessage{RoomID: r.room.ID, ThreadID: threadID, UserID: r.user.ID, Body: body})
	if err != nil {
		r.t.Fatal(err)
	}
	return msg
}
