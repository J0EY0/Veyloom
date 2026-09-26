package hub

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Members in worktrees of their own, on a real runtime (docs/design.md
// 5.21). The first member is the leader: it sets the project up when the
// second, an employee, is first asked for something, writing down how a
// new worktree is got ready. The employee commits in its worktree, and its
// work goes onto the main line as one commit; a conflict with the main
// line, handed over the way the Branches tab does, it resolves by merging
// the main line in. Both are in the full-auto preset, which trusts the
// agent with git as with everything else; its work reaches the main line
// only once a person merges it.

func TestPiSmoke_Worktrees(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	worktrees(t, runners, store.NewAgent{Name: "Pi", Runtime: "pi", PermissionPreset: store.PermissionFullAuto})
}

func TestClaudeRealSmoke_Worktrees(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	worktrees(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto})
}

func TestCodexSmoke_Worktrees(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	worktrees(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionFullAuto})
}

// worktrees runs the rounds with a leader and an employee made like agent.
func worktrees(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(base, "repo")
	makeCheckout(t, repo)
	lead := agent
	lead.Name = agent.Name + " lead"
	lead.RoleCard = "You lead the project's members."
	r := newSmokeRoom(t, runners, lead, repo)
	dev := agent
	dev.Name = agent.Name + " dev"
	dev.RoleCard = "You write the project's code. Do what you are asked, then say in a line what you did."
	dev.MachineID = r.h.Machines()[0].ID
	if dev.Runtime == "codex" {
		dev = forCodexSmoke(dev)
	}
	created, err := r.s.CreateAgent(r.ctx, dev)
	if err != nil {
		t.Fatal(err)
	}
	employee, err := r.s.CreateMember(r.ctx, store.NewMember{RoomID: r.room.ID, AgentID: created.ID, DisplayName: dev.Name, RepoPath: repo})
	if err != nil {
		t.Fatal(err)
	}
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}

	// Asked for the first time, the employee waits for the leader to set
	// the project up, then commits in its worktree.
	r.sayTo(employee, "在项目根目录新建 hello.txt，内容是一行 WORKTREE-OK。然后用 git 把它提交到你当前的分支上，提交说明写 add hello。", "")
	turns := r.waitAll(2)
	project, err := r.s.GetProject(r.ctx, r.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("the leader wrote down: copy %v, run %q", project.WorkspaceCopy, project.WorkspaceRun)
	if project.InitializedAt == nil || (len(project.WorkspaceCopy) == 0 && project.WorkspaceRun == "") {
		setup, _ := turnOf(turns, r.member, store.TurnSetup)
		t.Fatalf("the leader set nothing down: %+v\n%s", project, transcriptOf(t, setup))
	}
	member, err := r.s.GetMember(r.ctx, employee.ID)
	if err != nil {
		t.Fatal(err)
	}
	work, _ := turnOf(turns, employee, store.TurnChat)
	if member.WorkDir == "" || !strings.HasPrefix(member.Branch, "veyloom/") || specOf(t, work).WorkDir != member.WorkDir {
		t.Fatalf("the employee does not work in a worktree: %+v", member)
	}
	if slices.Contains(project.WorkspaceCopy, ".env") {
		if _, err := os.Stat(filepath.Join(member.WorkDir, ".env")); err != nil {
			t.Errorf("the worktree lacks the .env the steps copy: %v", err)
		}
	}
	if last := git(member.WorkDir, "log", "-1", "--format=%s"); !strings.Contains(last, "hello") {
		t.Fatalf("the employee's last commit is %q\n%s", last, transcriptOf(t, work))
	}
	if files := git(member.WorkDir, "show", "--name-only", "--format=", "HEAD"); !strings.Contains(files, "hello.txt") {
		t.Errorf("the commit has %q", files)
	}
	if _, err := os.Stat(filepath.Join(repo, "hello.txt")); err == nil {
		t.Error("the employee wrote into the checkout")
	}

	// Its work goes onto the main line as one commit.
	merged, err := r.h.Merge(r.ctx, employee.ID, "Add hello", nil)
	if err != nil || merged.Commit == "" {
		t.Fatalf("merge: %+v %v", merged, err)
	}
	if body, err := os.ReadFile(filepath.Join(repo, "hello.txt")); err != nil || !strings.Contains(string(body), "WORKTREE-OK") {
		t.Errorf("the main line lacks the work: %q %v", body, err)
	}

	// The employee and a person both change README.md: a conflict.
	r.sayTo(employee, "把 README.md 的内容整个换成一行：# hello from the worktree。然后用 git 提交，提交说明写 retitle。", "")
	r.waitAll(3)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# app\n\nFrom the main line.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(repo, "commit", "-qam", "Say where it is from")
	clash, err := r.h.Merge(r.ctx, employee.ID, "Retitle", nil)
	if err != nil || !slices.Equal(clash.Conflicts, []string{"README.md"}) {
		t.Fatalf("a clashing merge: %+v %v", clash, err)
	}

	// Handed over, as the Branches tab words it, the employee merges the
	// main line in and settles the conflict; then the work goes on.
	r.sayTo(employee, "你的分支和主线 main 在这些文件上冲突了：README.md。请把 main 合并进你的分支，解决冲突（两边的内容都保留）后提交，然后告诉我。", "")
	turns = r.waitAll(4)
	if exec.Command("git", "-C", member.WorkDir, "rev-parse", "-q", "--verify", "MERGE_HEAD").Run() == nil {
		t.Error("a merge is still under way in the worktree")
	}
	if _, err := exec.Command("git", "-C", member.WorkDir, "merge-base", "--is-ancestor", "main", "HEAD").CombinedOutput(); err != nil {
		t.Errorf("the employee's branch does not have the main line: %v\n%s", err, transcriptOf(t, turns[0]))
	}
	if body, _ := os.ReadFile(filepath.Join(member.WorkDir, "README.md")); strings.Contains(string(body), "<<<<<<<") {
		t.Errorf("README.md still has conflict markers:\n%s", body)
	}
	settled, err := r.h.Merge(r.ctx, employee.ID, "Retitle", nil)
	if err != nil || settled.Commit == "" {
		t.Fatalf("the merge after the conflict was settled: %+v %v", settled, err)
	}
	t.Logf("README.md on the main line:\n%s", git(repo, "show", "HEAD:README.md"))
}

// sayTo posts body from the person, mentioning member, in the thread or,
// with threadID empty, in the room itself.
func (r *smokeRoom) sayTo(member store.Member, body, threadID string) store.Message {
	r.t.Helper()
	msg, err := r.h.PostUserMessage(r.ctx, store.NewMessage{
		RoomID: r.room.ID, ThreadID: threadID, UserID: r.user.ID, Body: "@" + member.DisplayName + " " + body,
		Mentions: []store.Mention{{Kind: store.MentionAgent, ID: member.ID}},
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return msg
}

// waitAll waits until the room has n turns and none runs any more, newest
// first; it fails unless every one is done. A member's first turn waits
// for the leader's setup, which starts after it and ends before it.
func (r *smokeRoom) waitAll(n int) []store.Turn {
	r.t.Helper()
	start := time.Now()
	for {
		turns, err := r.s.ListRoomTurns(r.ctx, r.room.ID, 50)
		if err != nil {
			r.t.Fatal(err)
		}
		if len(turns) == n && !slices.ContainsFunc(turns, func(t store.Turn) bool { return t.Status == store.TurnRunning }) {
			for _, turn := range turns {
				if turn.Status != store.TurnDone {
					r.t.Fatalf("a %s turn ended %s: %s", turn.Kind, turn.Status, turn.Error)
				}
			}
			return turns
		}
		if time.Since(start) > 2*smokeTurnTimeout {
			r.t.Fatalf("the turns did not finish in time%s", r.stuck(r.room.ID))
		}
		time.Sleep(200 * time.Millisecond)
	}
}
