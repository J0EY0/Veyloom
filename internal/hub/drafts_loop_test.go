package hub

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// draftCall is a fake turn's draft_action call.
func draftCall(args map[string]any) map[string]any {
	return map[string]any{"tool": runtime.MessageToolDraft, "args": args}
}

// drafts are what members drafted in a topic, in the order drafted.
func (l *loop) drafts(threadID string) []store.Draft {
	l.t.Helper()
	ds, err := l.s.ListThreadDrafts(l.ctx, threadID)
	if err != nil {
		l.t.Fatal(err)
	}
	return ds
}

// withWork is a loop whose project has a git checkout, with Lead, its
// leader, and Coder, who has worked in a worktree of its own and written
// feature.go there.
func withWork(t *testing.T, cfg Config) (*loop, string, store.Member, store.Member) {
	t.Helper()
	l := newLoopWith(t, cfg)
	repo := gitCheckout(t)
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{RepoPath: &repo}); err != nil {
		t.Fatal(err)
	}
	lead := l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"feature.go"}})
	l.say("@Coder add the feature", "", coder)
	l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")
	return l, repo, lead, coder
}

// A leader drafts putting a member's work on the main line; a person runs
// it with one press, and the leader, which said what it does then, is
// woken with the commit, for that person (docs/design.md 5.23.5).
func TestLoop_ADraftedMergeRunsWithOnePress(t *testing.T) {
	l, repo, lead, coder := withWork(t, Config{})
	l.setOptions(lead, map[string]any{"tool_calls": []any{draftCall(map[string]any{
		"kind": "merge", "member": "Coder", "message": "Add the feature\n\nIt does what was asked.", "then": "hand the tests to Tester",
	})}, "reply": "Drafted the merge."})
	msg := l.say("@Lead put Coder's work on the main line once it is ready", "", lead)
	first := l.waitTurns(3, store.TurnDone, "Lead's turn drafting it")[0]
	thread := l.topic(msg)
	ds := l.drafts(thread.ID)
	if len(ds) != 1 {
		t.Fatalf("drafts = %+v", ds)
	}
	d := ds[0]
	if d.Kind != store.DraftMerge || d.Status != store.DraftPending || d.MemberID != lead.ID || d.TargetID != coder.ID || d.TurnID != first.ID ||
		d.Params.Message != "Add the feature\n\nIt does what was asked." || d.Then != "hand the tests to Tester" || d.MessageID == "" {
		t.Errorf("draft = %+v", d)
	}
	if notes := l.notes(thread.ID); countContaining(notes, "Lead drafted putting Coder's work on the main line, as: Add the feature") != 1 {
		t.Errorf("the card: %q", notes)
	}
	if tx := transcriptOf(t, first); !strings.Contains(tx, "Drafted: a card in this topic lets a person put Coder's work on the main line with one press. You are woken with what comes of it once they run it.") {
		t.Errorf("what Lead was told:\n%s", tx)
	}
	if system := specOf(t, first).SystemPrompt; !strings.Contains(system, runtime.MessageToolDraft+" drafts one on a card for the person to run with one press") {
		t.Errorf("the standing instructions tell of drafts:\n%s", system)
	}

	l.setOptions(lead, map[string]any{"reply": "Merged; the tests are next."})
	ran, err := l.h.RunDraft(l.ctx, d.ID, l.user.ID, DraftEdit{})
	if err != nil || ran.Status != store.DraftDone || ran.Result.Commit == "" || ran.DecidedBy != l.user.ID {
		t.Fatalf("ran: %+v %v", ran, err)
	}
	if got := gitIn(t, repo, "log", "--format=%s", "-1"); got != "Add the feature" {
		t.Errorf("the main line's last commit: %q", got)
	}
	woken := l.waitTurns(4, store.TurnDone, "Lead woken with what came of it")[0]
	told, err := l.s.GetDraft(l.ctx, d.ID)
	if err != nil || told.ResultMessageID == "" || woken.TriggerMessageID != told.ResultMessageID || woken.MemberID != lead.ID {
		t.Fatalf("the woken turn %+v, the draft %+v %v", woken, told, err)
	}
	if woken.ChainMessageID != told.ResultMessageID || woken.WokenByTurnID != "" || woken.ThreadID != thread.ID {
		t.Errorf("a piece of work of its own, which the person's press started: %+v", woken)
	}
	want := "alice put Coder's work on the main line as Lead drafted, as commit " + ran.Result.Commit[:7] + ". Then, Lead said: hand the tests to Tester"
	if notes := l.notes(thread.ID); countContaining(notes, want) != 1 {
		t.Errorf("the topic does not say %q: %q", want, notes)
	}
	if !slices.ContainsFunc(l.replies(thread.ID, store.SenderAgent), func(m store.Message) bool {
		return m.TurnID == woken.ID && slices.Contains(m.Mentions, store.Mention{Kind: store.MentionUser, ID: l.user.ID})
	}) {
		t.Errorf("what Lead came to reaches the person who ran it: %+v", l.replies(thread.ID, store.SenderAgent))
	}
	var problem *store.Problem
	if _, err := l.h.RunDraft(l.ctx, d.ID, l.user.ID, DraftEdit{}); !errors.As(err, &problem) || problem.Code != "draftSettled" {
		t.Errorf("run twice: %v", err)
	}
}

// What cannot be drafted is refused at once; drafting the same work again
// replaces the card; a draft turned down wakes no one; one run while its
// member is at work waits again, and runs once it is not.
func TestLoop_DraftsAreCheckedReplacedAndTurnedDown(t *testing.T) {
	l, repo, lead, coder := withWork(t, Config{})
	l.setOptions(lead, map[string]any{"tool_calls": []any{
		draftCall(map[string]any{"kind": "merge", "message": "Mine"}),
		draftCall(map[string]any{"kind": "merge", "member": "Nobody", "message": "x"}),
		draftCall(map[string]any{"kind": "merge", "member": "Coder"}),
		draftCall(map[string]any{"kind": "set_aside", "member": "Coder"}),
		draftCall(map[string]any{"kind": "install_skill", "member": "Coder", "skill": "go-testing"}),
		draftCall(map[string]any{"kind": "deploy", "member": "Coder"}),
		draftCall(map[string]any{"kind": "merge", "member": "Coder", "message": "Add the feature"}),
		draftCall(map[string]any{"kind": "set_aside", "member": "@coder", "reason": "The approach is wrong."}),
	}, "reply": "Drafted."})
	msg := l.say("@Lead see to Coder's work", "", lead)
	first := l.waitTurns(3, store.TurnDone, "Lead's turn")[0]
	tx := transcriptOf(t, first)
	for _, want := range []string{
		"Lead has no branch of its own", "the project has no member Nobody", "a merge needs its message", "giving work up needs the reason why",
		"kind is merge, set_aside or install_skill", "You are not woken for it: its card shows what comes of it.",
	} {
		if !strings.Contains(tx, want) {
			t.Errorf("Lead was not told %q:\n%s", want, tx)
		}
	}
	thread := l.topic(msg)
	ds := l.drafts(thread.ID)
	if len(ds) != 2 || ds[0].Status != store.DraftSuperseded || ds[1].Kind != store.DraftSetAside || ds[1].Status != store.DraftPending ||
		ds[1].Params.Reason != "The approach is wrong." {
		t.Fatalf("the second replaces the first: %+v", ds)
	}

	declined, err := l.h.DeclineDraft(l.ctx, ds[1].ID, l.user.ID)
	if err != nil || declined.Status != store.DraftDeclined || declined.DecidedBy != l.user.ID {
		t.Fatalf("declined: %+v %v", declined, err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(l.turns()); n != 3 {
		t.Errorf("a draft turned down wakes no one: %d turns", n)
	}

	// Drafted again, and run while Coder works.
	l.setOptions(lead, map[string]any{"tool_calls": []any{draftCall(map[string]any{"kind": "merge", "member": "Coder", "message": "Add the feature"})}, "reply": "Drafted."})
	l.say("@Lead draft it again", thread.ID, lead)
	l.waitTurns(4, store.TurnDone, "Lead drafting again")
	d := l.drafts(thread.ID)[2]
	l.setOptions(coder, map[string]any{"reply": "Still at it.", "delay_ms": 1500})
	l.say("@Coder polish it", "", coder)
	eventually(t, func() bool { return l.h.turns.busy(coder.ID) }, "Coder at work")
	var problem *store.Problem
	if _, err := l.h.RunDraft(l.ctx, d.ID, l.user.ID, DraftEdit{}); !errors.As(err, &problem) || problem.Code != "memberBusy" {
		t.Fatalf("run while Coder works: %v", err)
	}
	if again, _ := l.s.GetDraft(l.ctx, d.ID); again.Status != store.DraftPending {
		t.Errorf("it waits again: %+v", again)
	}
	l.waitTurns(5, store.TurnDone, "Coder's turn")
	eventually(t, func() bool { return !l.h.turns.busy(coder.ID) }, "Coder done")
	// A person's own message, in place of the drafted one.
	ran, err := l.h.RunDraft(l.ctx, d.ID, l.user.ID, DraftEdit{Message: "Add the feature, polished"})
	if err != nil || ran.Status != store.DraftDone {
		t.Fatalf("ran: %+v %v", ran, err)
	}
	if got := gitIn(t, repo, "log", "--format=%s", "-1"); got != "Add the feature, polished" {
		t.Errorf("the person's message: %q", got)
	}
	time.Sleep(300 * time.Millisecond)
	if n := len(l.turns()); n != 5 {
		t.Errorf("no then, no wake: %d turns", n)
	}
}

// A merge that meets conflicts is what came of it: nothing changed, the
// files are kept, and the member that drafted it is told.
func TestLoop_ADraftedMergeThatConflicts(t *testing.T) {
	l, repo, lead, coder := withWork(t, Config{})
	c, _ := l.s.GetMember(l.ctx, coder.ID)
	if err := os.WriteFile(filepath.Join(c.WorkDir, "README.md"), []byte("# app, Coder's way\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, c.WorkDir, "commit", "-q", "-am", "Coder's README")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# app, the main line's way\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "commit", "-q", "-am", "The main line's README")

	l.setOptions(lead, map[string]any{"tool_calls": []any{draftCall(map[string]any{
		"kind": "merge", "member": "Coder", "message": "Add the feature", "then": "have Coder settle the conflicts",
	})}, "reply": "Drafted."})
	msg := l.say("@Lead merge Coder's work", "", lead)
	l.waitTurns(3, store.TurnDone, "Lead drafting")
	thread := l.topic(msg)
	d := l.drafts(thread.ID)[0]
	l.setOptions(lead, map[string]any{"reply": "Asking Coder to settle them."})
	ran, err := l.h.RunDraft(l.ctx, d.ID, l.user.ID, DraftEdit{})
	if err != nil || ran.Status != store.DraftConflicted || !slices.Contains(ran.Result.Conflicts, "README.md") || ran.Result.Commit != "" {
		t.Fatalf("ran: %+v %v", ran, err)
	}
	if got := gitIn(t, repo, "log", "--format=%s", "-1"); got != "The main line's README" {
		t.Errorf("nothing changed on the main line: %q", got)
	}
	l.waitTurns(4, store.TurnDone, "Lead woken with the conflicts")
	if notes := l.notes(thread.ID); countContaining(notes, "alice tried putting Coder's work on the main line, as Lead drafted, but it conflicts with the main line in README.md; nothing changed. Then, Lead said: have Coder settle the conflicts") != 1 {
		t.Errorf("the topic is told: %q", notes)
	}
}

// A skill of the library drafted for a member is installed for its agent
// with one press.
func TestLoop_ADraftedSkillIsInstalled(t *testing.T) {
	l := newLoopWith(t, Config{WikiDir: t.TempDir()})
	l.addSkill("go-testing", "Use when writing Go tests.", "Write the cases as a table.", "")
	lead := l.member("Lead", map[string]any{"tool_calls": []any{
		draftCall(map[string]any{"kind": "install_skill", "member": "Coder", "skill": "nope"}),
		draftCall(map[string]any{"kind": "install_skill", "member": "Coder", "skill": "go-testing"}),
	}, "reply": "Drafted."})
	coder := l.member("Coder", map[string]any{"reply": "Done."})
	msg := l.say("@Lead get Coder what it needs for the tests", "", lead)
	first := l.waitTurns(1, store.TurnDone, "Lead drafting")[0]
	if tx := transcriptOf(t, first); !strings.Contains(tx, "the skill library has no skill nope") {
		t.Errorf("an unknown skill is refused:\n%s", tx)
	}
	d := l.drafts(l.topic(msg).ID)[0]
	if d.Kind != store.DraftInstallSkill || d.Params.Skill != "go-testing" || d.TargetID != coder.ID {
		t.Fatalf("draft = %+v", d)
	}
	if ran, err := l.h.RunDraft(l.ctx, d.ID, l.user.ID, DraftEdit{}); err != nil || ran.Status != store.DraftDone {
		t.Fatalf("ran: %+v %v", ran, err)
	}
	agent, err := l.s.GetAgent(l.ctx, coder.AgentID)
	if err != nil || !slices.Contains(agent.Skills, "go-testing") {
		t.Errorf("installed: %+v %v", agent.Skills, err)
	}

	// Drafting it once installed is refused.
	l.setOptions(lead, map[string]any{"tool_calls": []any{draftCall(map[string]any{"kind": "install_skill", "member": "Coder", "skill": "go-testing"})}, "reply": "Drafted."})
	l.say("@Lead and again", "", lead)
	second := l.waitTurns(2, store.TurnDone, "Lead drafting again")[0]
	if tx := transcriptOf(t, second); !strings.Contains(tx, "Coder has the skill go-testing installed already") {
		t.Errorf("an installed skill is refused:\n%s", tx)
	}
}

// The leader's steps with a command wait for a person on a card like any
// draft: adopted from it, the members waiting go on; a person writing
// steps down themselves has the card give way.
func TestLoop_SetupStepsAreACard(t *testing.T) {
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
	d := l.drafts(project.SetupThreadID)[0]
	if notes := l.notes(project.SetupThreadID); countContaining(notes, "Lead wrote down how a new worktree is got ready: run echo ready > prepared.txt. A person adopts the command before it runs.") != 1 {
		t.Errorf("the card's note: %q", notes)
	}
	if ran, err := l.h.RunDraft(l.ctx, d.ID, l.user.ID, DraftEdit{}); err != nil || ran.Status != store.DraftDone {
		t.Fatalf("adopted from the card: %+v %v", ran, err)
	}
	l.waitTurns(2, store.TurnDone, "Coder's turn once the steps are adopted")
	if p, _ := l.s.GetProject(l.ctx, l.room.ProjectID); p.WorkspacePending != nil || p.WorkspaceRun != "echo ready > prepared.txt" || p.InitializedAt == nil {
		t.Errorf("the project's steps: %+v", p)
	}

	// Asked to set up again, and a person writes the steps down meanwhile.
	if err := l.h.StartSetup(l.ctx, l.room.ProjectID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(l.drafts(project.SetupThreadID)) == 2 }, "a second card")
	steps := store.WorkspaceSteps{Copy: []string{".env"}}
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{WorkspaceSteps: &steps}); err != nil {
		t.Fatal(err)
	}
	l.h.WorkspaceStepsWritten(l.room.ProjectID)
	if second := l.drafts(project.SetupThreadID)[1]; second.Status != store.DraftSuperseded {
		t.Errorf("the card gives way to the person's steps: %+v", second)
	}
}
