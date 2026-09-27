package hub

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// gitCheckout is a project checkout in a git repository: one commit, and
// an .env git ignores, which a worktree would lack.
func gitCheckout(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	makeCheckout(t, dir)
	return dir
}

// makeCheckout makes dir such a checkout.
func makeCheckout(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "Alice")
	run("config", "user.email", "alice@example.com")
	for name, body := range map[string]string{".gitignore": ".env\n", "README.md": "# app\n", ".env": "SECRET=1\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-q", "-m", "first")
}

// memberIn adds a fake-runtime member working in repo, with the preset.
func (l *loop) memberIn(name, repo string, preset store.PermissionPreset, options map[string]any) store.Member {
	l.t.Helper()
	agent, err := l.s.CreateAgent(l.ctx, store.NewAgent{Name: name + " agent", MachineID: l.machineID, Runtime: "fake", PermissionPreset: preset, RuntimeOptions: options})
	if err != nil {
		l.t.Fatal(err)
	}
	member, err := l.s.CreateMember(l.ctx, store.NewMember{RoomID: l.room.ID, AgentID: agent.ID, DisplayName: name, RepoPath: repo})
	if err != nil {
		l.t.Fatal(err)
	}
	return member
}

// specOf is the spec a turn started with, from its transcript.
func specOf(t *testing.T, turn store.Turn) runtime.TurnSpec {
	t.Helper()
	for _, line := range strings.Split(transcriptOf(t, turn), "\n") {
		var rec struct {
			Kind string          `json:"kind"`
			Spec json.RawMessage `json:"spec"`
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Kind == "start" {
			var spec runtime.TurnSpec
			if err := json.Unmarshal(rec.Spec, &spec); err != nil {
				t.Fatal(err)
			}
			return spec
		}
	}
	t.Fatalf("turn %s has no start record", turn.ID)
	return runtime.TurnSpec{}
}

// turnOf finds the turn of a member of the given kind among turns.
func turnOf(turns []store.Turn, member store.Member, kind store.TurnKind) (store.Turn, bool) {
	for _, turn := range turns {
		if turn.MemberID == member.ID && turn.Kind == kind {
			return turn, true
		}
	}
	return store.Turn{}, false
}

// setupCall is the leader's tool call that writes the steps down.
func setupCall(copies []string, run string) map[string]any {
	return map[string]any{"tool_calls": []any{map[string]any{"tool": runtime.SetupToolSteps, "args": map[string]any{"copy": copies, "run": run}}}, "reply": "Set up."}
}

// The first time a member is asked for something, the leader sets the
// project up, then the member's worktree is made and got ready, and the
// member works there (docs/design.md 5.21).
func TestLoop_AMemberWorksInItsWorktree(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	lead := l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall([]string{".env"}, "echo ready > prepared.txt"))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done."})

	l.say("@Coder add the feature", "", coder)
	turns := l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")
	setup, ok := turnOf(turns, lead, store.TurnSetup)
	if !ok {
		t.Fatalf("no setup turn of the leader: %+v", turns)
	}
	work, _ := turnOf(turns, coder, store.TurnChat)

	// The leader set up in the checkout, in the setup topic, with its tool.
	project, err := l.s.GetProject(l.ctx, l.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if project.InitializedAt == nil || !slices.Equal(project.WorkspaceCopy, []string{".env"}) || project.WorkspaceRun != "echo ready > prepared.txt" {
		t.Errorf("the project after its setup: %+v", project)
	}
	spec := specOf(t, setup)
	if spec.WorkDir != repo || !slices.Contains(spec.ExtraTools, runtime.SetupToolSteps) || setup.ThreadID != project.SetupThreadID {
		t.Errorf("the setup ran in %q with %v, in %s", spec.WorkDir, spec.ExtraTools, setup.ThreadID)
	}
	if !strings.Contains(spec.Prompt, "you are setting the project up") {
		t.Errorf("the setup brief:\n%s", spec.Prompt)
	}

	// Coder's worktree: made, got ready, and where it worked.
	member, _ := l.s.GetMember(l.ctx, coder.ID)
	want := filepath.Join(l.worktrees, "p", "coder")
	if member.WorktreeDir != want || member.WorkDir != want || member.Branch != "veyloom/coder" || member.PreparedAt == nil {
		t.Fatalf("Coder's worktree: %+v", member)
	}
	for name, body := range map[string]string{".env": "SECRET=1\n", "prepared.txt": "ready\n", "README.md": "# app\n"} {
		if got, err := os.ReadFile(filepath.Join(want, name)); err != nil || string(got) != body {
			t.Errorf("%s in the worktree: %q %v", name, got, err)
		}
	}
	coderSpec := specOf(t, work)
	if coderSpec.WorkDir != want || slices.Contains(coderSpec.ExtraTools, runtime.SetupToolSteps) {
		t.Errorf("Coder worked in %q with %v", coderSpec.WorkDir, coderSpec.ExtraTools)
	}
	if !strings.Contains(coderSpec.SystemPrompt, "You work in a git worktree of your own, "+want+", on the branch veyloom/coder") ||
		!strings.Contains(coderSpec.SystemPrompt, "Whatever you leave there is merged as your work") {
		t.Errorf("Coder's standing instructions:\n%s", coderSpec.SystemPrompt)
	}
	transcript := transcriptOf(t, work)
	for _, say := range []string{"Waiting for the project's leader", "Making Coder's worktree", "Getting the worktree ready"} {
		if !strings.Contains(transcript, say) {
			t.Errorf("the transcript lacks %q", say)
		}
	}

	// Asked again: no setup, the same worktree.
	l.say("@Coder and another", "", coder)
	turns = l.waitTurns(3, store.TurnDone, "Coder's second turn")
	if again := specOf(t, turns[0]); turns[0].MemberID != coder.ID || again.WorkDir != want {
		t.Errorf("the second turn: %+v in %q", turns[0], again.WorkDir)
	}

	// The leader's own turns are in the checkout, with the tool.
	l.say("@Lead look around", "", lead)
	turns = l.waitTurns(4, store.TurnDone, "the leader's turn")
	if own := specOf(t, turns[0]); own.WorkDir != repo || !slices.Contains(own.ExtraTools, runtime.SetupToolSteps) || !strings.Contains(own.SystemPrompt, "You are the project's leader") ||
		!strings.Contains(own.SystemPrompt, "commit the files you changed in the checkout yourself, and only those, before your turn ends") ||
		!strings.Contains(own.SystemPrompt, "goes on that branch with the work") {
		t.Errorf("the leader's chat turn in %q with %v", own.WorkDir, own.ExtraTools)
	}
}

// A checkout that is no git repository leaves every member in it, and
// nobody sets anything up.
func TestLoop_NoRepositoryNoWorktree(t *testing.T) {
	l := newLoop(t)
	plain, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	l.memberIn("Lead", plain, store.PermissionFullAuto, nil)
	coder := l.memberIn("Coder", plain, store.PermissionFullAuto, map[string]any{"reply": "Done."})
	l.say("@Coder go", "", coder)
	turns := l.waitTurns(1, store.TurnDone, "Coder's turn")
	if spec := specOf(t, turns[0]); spec.WorkDir != plain {
		t.Errorf("Coder worked in %q", spec.WorkDir)
	}
	if member, _ := l.s.GetMember(l.ctx, coder.ID); member.WorktreeDir != "" {
		t.Errorf("a worktree with no repository: %+v", member)
	}
}

// A leader that asks before it runs commands writes steps a person adopts
// first; the member waits for them, and goes on once they are adopted.
func TestLoop_SetupStepsWaitForAPerson(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	l.memberIn("Lead", repo, store.PermissionEditWithApproval, setupCall(nil, "echo ready > prepared.txt"))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done."})
	l.say("@Coder go", "", coder)

	var project store.Project
	eventually(t, func() bool {
		project, _ = l.s.GetProject(l.ctx, l.room.ProjectID)
		return project.WorkspacePending != nil
	}, "the steps to wait for a person")
	if project.WorkspacePending.Run != "echo ready > prepared.txt" || project.WorkspacePendingMessageID == "" || project.InitializedAt != nil {
		t.Fatalf("waiting steps: %+v", project)
	}
	card, err := l.s.GetMessage(l.ctx, project.WorkspacePendingMessageID)
	if err != nil || card.ThreadID != project.SetupThreadID || !strings.Contains(card.Body, "A person adopts the command") {
		t.Errorf("the card: %+v %v", card, err)
	}
	if running := l.turns(); len(running) != 2 || running[0].Status == store.TurnDone && running[1].Status == store.TurnDone {
		t.Fatalf("Coder should still wait: %+v", running)
	}
	// The card is a draft (design.md 5.23.5), which says it was adopted.
	drafts := l.drafts(project.SetupThreadID)
	if len(drafts) != 1 || drafts[0].Kind != store.DraftSetupSteps || drafts[0].MessageID != card.ID || drafts[0].Params.Steps == nil ||
		drafts[0].Params.Steps.Run != "echo ready > prepared.txt" || drafts[0].Status != store.DraftPending {
		t.Fatalf("the card's draft: %+v", drafts)
	}

	if err := l.h.SettleWorkspaceSteps(l.ctx, project.ID, l.user.ID, true); err != nil {
		t.Fatal(err)
	}
	l.waitTurns(2, store.TurnDone, "Coder's turn once the steps are adopted")
	if d, err := l.s.GetDraft(l.ctx, drafts[0].ID); err != nil || d.Status != store.DraftDone || d.DecidedBy != l.user.ID {
		t.Errorf("the card says it was adopted: %+v %v", d, err)
	}
	member, _ := l.s.GetMember(l.ctx, coder.ID)
	if got, err := os.ReadFile(filepath.Join(member.WorkDir, "prepared.txt")); err != nil || string(got) != "ready\n" {
		t.Errorf("the adopted command ran: %q %v", got, err)
	}
}

// Steps turned down end the member's waiting turn, saying so.
func TestLoop_SetupStepsTurnedDown(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	l.memberIn("Lead", repo, store.PermissionEditWithApproval, setupCall(nil, "make everything"))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, nil)
	l.say("@Coder go", "", coder)
	eventually(t, func() bool {
		p, _ := l.s.GetProject(l.ctx, l.room.ProjectID)
		return p.WorkspacePending != nil
	}, "the steps to wait for a person")
	// Turned down from the card itself.
	p, _ := l.s.GetProject(l.ctx, l.room.ProjectID)
	drafts := l.drafts(p.SetupThreadID)
	if len(drafts) != 1 {
		t.Fatalf("the card's draft: %+v", drafts)
	}
	if _, err := l.h.DeclineDraft(l.ctx, drafts[0].ID, l.user.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		turn, ok := turnOf(l.turns(), coder, store.TurnChat)
		return ok && turn.Status == store.TurnFailed && strings.Contains(turn.Error, "turned the leader's setup steps down")
	}, "Coder's turn to fail")
	if p, _ := l.s.GetProject(l.ctx, l.room.ProjectID); p.WorkspacePending != nil {
		t.Errorf("no steps wait any more: %+v", p.WorkspacePending)
	}
	if member, _ := l.s.GetMember(l.ctx, coder.ID); member.WorktreeDir != "" {
		t.Errorf("a worktree made after all: %+v", member)
	}
}

// Steps that fail end the member's turn and wake the leader, in the setup
// topic, with what the command said.
func TestLoop_SetupStepsThatFail(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	lead := l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, "echo no compiler here; exit 3"))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, nil)
	l.say("@Coder go", "", coder)
	eventually(t, func() bool {
		turn, ok := turnOf(l.turns(), coder, store.TurnChat)
		return ok && turn.Status == store.TurnFailed
	}, "Coder's turn to fail")
	turn, _ := turnOf(l.turns(), coder, store.TurnChat)
	if !strings.Contains(turn.Error, "could not be got ready") {
		t.Errorf("Coder's turn: %q", turn.Error)
	}
	// The leader is told, and asked, in the setup topic.
	eventually(t, func() bool {
		var chats int
		for _, turn := range l.turns() {
			if turn.MemberID == lead.ID && turn.Kind == store.TurnChat {
				chats++
			}
		}
		return chats == 1
	}, "the leader to be asked to set the steps right")
	project, _ := l.s.GetProject(l.ctx, l.room.ProjectID)
	notes := l.replies(project.SetupThreadID, store.SenderSystem)
	found := false
	for _, note := range notes {
		if strings.Contains(note.Body, "@Lead getting Coder's worktree ready failed") && strings.Contains(note.Body, "no compiler here") {
			found = true
		}
	}
	if !found {
		t.Errorf("the setup topic says:\n%+v", notes)
	}
}

// Two members woken at once share one setup, even when one of them read
// the project before the setup was over and waits only after it settled.
func TestLoop_MembersWokenAtOnceShareTheSetup(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	lead := l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done."})
	tester := l.memberIn("Tester", repo, store.PermissionFullAuto, map[string]any{"reply": "Tested."})
	stale, err := l.s.GetProject(l.ctx, l.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}

	l.say("@Coder @Tester go", "", coder, tester)
	turns := l.waitTurns(3, store.TurnDone, "the setup and both turns")
	setups := 0
	for _, turn := range turns {
		if turn.Kind == store.TurnSetup && turn.MemberID == lead.ID {
			setups++
		}
	}
	if setups != 1 {
		t.Fatalf("%d setups: %+v", setups, turns)
	}

	// The project as read before its setup: no setup is needed any more.
	if wait := l.h.turns.setupFor(l.ctx, stale, setupForWorktree); wait != nil {
		<-wait.done
		t.Fatalf("a setup started again: %v", wait.err)
	}
	if got := l.turns(); len(got) != 3 {
		t.Errorf("%d turns: %+v", len(got), got)
	}
	// A person asking sets it up again all the same.
	if err := l.h.StartSetup(l.ctx, stale.ID); err != nil {
		t.Fatal(err)
	}
	l.waitTurns(4, store.TurnDone, "the setup a person asked for")
}

// A turn waiting for the setup can be cancelled.
func TestLoop_CancelWhileWaitingForTheSetup(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	slow := setupCall(nil, "")
	slow["delay_ms"] = 60_000
	l.memberIn("Lead", repo, store.PermissionFullAuto, slow)
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, nil)
	l.say("@Coder go", "", coder)
	var turn store.Turn
	eventually(t, func() bool {
		var ok bool
		turn, ok = turnOf(l.turns(), coder, store.TurnChat)
		return ok
	}, "Coder's turn to start")
	if err := l.h.CancelTurn(l.ctx, turn.ID, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		turn, _ = turnOf(l.turns(), coder, store.TurnChat)
		return turn.Status == store.TurnCancelled
	}, "Coder's turn to be cancelled")
}

// A member taken out of its project leaves its worktree; what it had not
// committed is kept on its branch.
func TestLoop_ARemovedMembersWorktreeGoes(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done."})
	l.say("@Coder go", "", coder)
	l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")
	member, _ := l.s.GetMember(l.ctx, coder.ID)
	if err := os.WriteFile(filepath.Join(member.WorkDir, "notes.txt"), []byte("half done\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := l.s.RemoveMember(l.ctx, coder.ID)
	if err != nil {
		t.Fatal(err)
	}
	l.h.ReleaseWorktree(removed)
	eventually(t, func() bool {
		got, _ := l.s.GetMember(l.ctx, coder.ID)
		return got.WorktreeDir == ""
	}, "the worktree to be forgotten")
	if _, err := os.Stat(member.WorktreeDir); !os.IsNotExist(err) {
		t.Errorf("the worktree is still there: %v", err)
	}
	show := exec.Command("git", "show", "veyloom/coder:notes.txt")
	show.Dir = repo
	if out, err := show.CombinedOutput(); err != nil || string(out) != "half done\n" {
		t.Errorf("the branch keeps the work: %q %v", out, err)
	}
}
