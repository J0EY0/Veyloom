package hub

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// settled waits until n turns of the room have run and none is running.
func (l *loop) settled(n int, what string) []store.Turn {
	l.t.Helper()
	var turns []store.Turn
	eventually(l.t, func() bool {
		turns = l.turns()
		if len(turns) != n {
			return false
		}
		for _, turn := range turns {
			if turn.Status == store.TurnRunning {
				return false
			}
		}
		return true
	}, what)
	return turns
}

// trialOf waits for the skill's latest trial to stand as status.
func (l *loop) trialOf(name string, status store.SkillTrialStatus) store.SkillTrial {
	l.t.Helper()
	var trial store.SkillTrial
	eventually(l.t, func() bool {
		var err error
		trial, err = l.s.LatestSkillTrial(l.ctx, name)
		return err == nil && trial.Status == status
	}, "the trial of "+name+" to be "+string(status))
	return trial
}

// onTrial reports whether the library's list of skills marks the skill as
// on trial.
func (l *loop) onTrial(name string) bool {
	l.t.Helper()
	catalog, err := l.h.LibraryCatalog(l.ctx)
	if err != nil {
		l.t.Fatal(err)
	}
	for _, p := range catalog.Pages {
		if p.Path == wiki.SkillPath(name) {
			return p.OnTrial
		}
	}
	l.t.Fatalf("no skill %s in the library", name)
	return false
}

func improve(line string) map[string]any {
	return map[string]any{"tool_calls": []any{call(runtime.WikiToolPatch, map[string]any{
		"scope": "library", "path": "/skills/go-table-tests/SKILL.md", "reason": "the task showed it",
		"edits": []any{map[string]any{"op": "append", "content": line}},
	})}}
}

func TestLoop_SkillsEvolveOnTrial(t *testing.T) {
	l, dir := wikiLoop(t)
	l.addSkill("go-table-tests", "Use when writing Go tests with several cases.", "Write the cases as a table.", l.project().ID)
	file := filepath.Join(dir, "library", "skills", "go-table-tests", "SKILL.md")
	r, _ := l.h.openLibrary(l.ctx)
	base, _ := r.bundle.Head(l.ctx)

	// Not installed for it, an agent does not change the skill.
	coder := l.member("Coder", improve("Name each case."))
	asked := l.say("@Coder write the tests", "", coder)
	l.waitTurns(1, store.TurnDone, "Coder's first turn")
	if answer := l.root(l.topic(asked)).Body; !strings.Contains(answer, "not installed for you") {
		t.Errorf("Coder should hear the skill is not theirs:\n%s", answer)
	}
	if data, _ := os.ReadFile(file); strings.Contains(string(data), "Name each case.") {
		t.Error("the skill is unchanged")
	}
	if l.onTrial("go-table-tests") {
		t.Error("an unchanged skill is not on trial")
	}

	// Installed, it changes the skill at once, on trial.
	l.install("go-table-tests", coder)
	again := l.say("@Coder write the tests", "", coder)
	turns := l.waitTurns(2, store.TurnDone, "Coder's second turn")
	if answer := l.root(l.topic(again)).Body; !strings.Contains(answer, "on trial until 3 turns") {
		t.Errorf("Coder should hear the change is on trial:\n%s", answer)
	}
	if data, _ := os.ReadFile(file); !strings.Contains(string(data), "Name each case.") {
		t.Errorf("the change is in the library:\n%s", data)
	}
	trial := l.trialOf("go-table-tests", store.TrialOpen)
	if trial.BaseSHA != base || trial.TurnID != turns[0].ID || trial.ChangedBy != "Coder" || trial.ProjectName != "p" {
		t.Errorf("the trial %+v, base %s", trial, base)
	}
	if !l.onTrial("go-table-tests") {
		t.Error("the list of skills marks the changed one as on trial")
	}
	page, _ := l.h.LibraryPage(l.ctx, wiki.SkillPath("go-table-tests"))
	if page.Trial == nil || page.Trial.Status != store.TrialOpen || page.Trial.Needed != 3 || page.Trial.Uses != 0 || page.Trial.TopicNumber != l.topic(again).Number {
		t.Errorf("the skill's page shows the trial %+v", page.Trial)
	}
	// An agent it is installed for is told so, and that it may improve it.
	if brief := promptOf(t, turns[0]); !strings.Contains(brief, "Installed for you from the skill library: go-table-tests.") {
		t.Errorf("the brief:\n%s", brief)
	}
	if standing := systemPromptOf(t, turns[0]); !strings.Contains(standing, "The skills installed for you, which the brief names, you improve as you use them") {
		t.Errorf("the standing instructions:\n%s", standing)
	}

	// Turns that use it since: a failure does not count, three that end
	// well keep it.
	reader := l.member("Reader", map[string]any{"use_skill": "go-table-tests"})
	l.install("go-table-tests", reader)
	failing := l.member("Failing", map[string]any{"use_skill": "go-table-tests", "fail": true})
	l.install("go-table-tests", failing)
	l.say("@Failing try", "", failing)
	l.settled(3, "the failed turn")
	for i := range 2 {
		l.say("@Reader use it", "", reader)
		l.settled(4+i, "a reader's turn")
	}
	if page, _ := l.h.LibraryPage(l.ctx, wiki.SkillPath("go-table-tests")); page.Trial.Uses != 2 || page.Trial.Failed != 1 || page.Trial.Status != store.TrialOpen {
		t.Errorf("two good uses and a failed one: %+v", page.Trial)
	}
	l.say("@Reader use it again", "", reader)
	l.settled(6, "the third good turn")
	kept := l.trialOf("go-table-tests", store.TrialKept)
	if kept.EndedBy != trialActor || kept.Reason != "kept after 3 turns used it" {
		t.Errorf("kept %+v", kept)
	}
	if l.onTrial("go-table-tests") {
		t.Error("a kept skill is no longer on trial")
	}
	page, _ = l.h.LibraryPage(l.ctx, wiki.SkillPath("go-table-tests"))
	if page.Tier != "machine-confirmed" || page.Trial.Status != store.TrialKept {
		t.Errorf("the kept skill %+v %+v", page.WikiPageInfo, page.Trial)
	}
	if log, _ := os.ReadFile(filepath.Join(dir, "library", "log.md")); !strings.Contains(string(log), "by process:skill-trial: kept after 3 turns used it") {
		t.Errorf("the log:\n%s", log)
	}
}

func TestLoop_SkillsRolledBack(t *testing.T) {
	// The account's edits in an editor are committed under its name.
	dir := t.TempDir()
	l := newLoopWith(t, Config{WikiDir: dir}, WithPerson(func() string { return "alice" }))
	home := l.project()
	l.addSkill("go-table-tests", "Use when writing Go tests with several cases.", "Write the cases as a table.", home.ID)
	file := filepath.Join(dir, "library", "skills", "go-table-tests", "SKILL.md")
	coder := l.member("Coder", improve("Copy each case."))
	l.install("go-table-tests", coder)
	change := func(n int) {
		t.Helper()
		l.say("@Coder write the tests", "", coder)
		l.settled(n, "Coder's change")
		l.trialOf("go-table-tests", store.TrialOpen)
	}

	// A person rolls it back.
	change(1)
	if _, err := l.h.RollbackSkill(l.ctx, "go-table-tests", l.user.ID, "copying drifts"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(file); strings.Contains(string(data), "Copy each case.") {
		t.Errorf("rolled back:\n%s", data)
	}
	back := l.trialOf("go-table-tests", store.TrialRolledBack)
	if back.EndedBy != "human:alice" || back.Reason != "copying drifts" {
		t.Errorf("rolled back %+v", back)
	}
	if _, err := l.h.RollbackSkill(l.ctx, "go-table-tests", l.user.ID, ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("nothing on trial to roll back: %v", err)
	}
	// Whoever reads the skill next is told what was rolled back, and why.
	reader := l.member("Reader", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolRead, map[string]any{"scope": "library", "path": "/skills/go-table-tests/SKILL.md"}),
	}})
	asked := l.say("@Reader read the skill", "", reader)
	l.settled(2, "Reader's turn")
	if answer := l.root(l.topic(asked)).Body; !strings.Contains(answer, "rolled back before") || !strings.Contains(answer, "by human:alice: copying drifts") ||
		!strings.Contains(answer, "+Copy each case.") {
		t.Errorf("the rollback told:\n%s", answer)
	}
	turns := 2

	// A person's confirmation keeps a change.
	change(turns + 1)
	if _, err := l.h.VerifyLibraryPage(l.ctx, wiki.SkillPath("go-table-tests"), l.user.ID); err != nil {
		t.Fatal(err)
	}
	if kept := l.trialOf("go-table-tests", store.TrialKept); kept.EndedBy != "human:alice" {
		t.Errorf("confirmed %+v", kept)
	}

	// So does a person's own edit, made in their editor.
	change(turns + 2)
	data, _ := os.ReadFile(file)
	if err := os.WriteFile(file, []byte(string(data)+"\nBy hand.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := l.h.LibraryPage(l.ctx, wiki.SkillPath("go-table-tests")); err != nil {
		t.Fatal(err)
	}
	if kept := l.trialOf("go-table-tests", store.TrialKept); kept.EndedBy != "human:alice" || kept.Reason != "changed by hand" {
		t.Errorf("edited by hand %+v", kept)
	}

	// The maintainer of the team that owns it sees the trial and what was
	// rolled back before, and rolls it back; a skill of another team is not
	// its to roll back.
	change(turns + 3)
	keeper := l.member("Keeper", nil)
	l.keep(keeper, store.UpkeepManual)
	l.setOptions(keeper, map[string]any{"tool_calls": []any{
		call(runtime.UpkeepToolRollback, map[string]any{"skill": "go-table-tests", "reason": "the tests drifted again"}),
		call(runtime.UpkeepToolRollback, map[string]any{"skill": "someone-elses", "reason": "x"}),
	}})
	if _, err := l.h.StartUpkeep(l.ctx, home.ID); err != nil {
		t.Fatal(err)
	}
	upkeep := l.upkeeps(1)[0]
	if brief := promptOf(t, upkeep); !strings.Contains(brief, "Changes on trial to those skills (1):\n- go-table-tests: changed by Coder of project \"p\"") ||
		!strings.Contains(brief, "roll it back with rollback_skill") ||
		!strings.Contains(brief, "rolled back lately, not to be made again") || !strings.Contains(brief, "- go-table-tests: rolled back on ") ||
		!strings.Contains(brief, "by human:alice: copying drifts") {
		t.Errorf("the upkeep's brief:\n%s", brief)
	}
	back = l.trialOf("go-table-tests", store.TrialRolledBack)
	if back.EndedBy != "fake/default" || back.Reason != "the tests drifted again" {
		t.Errorf("rolled back by the maintainer %+v", back)
	}
	if answer := l.upkeepReply(l.upkeeps(1)[0]); !strings.Contains(answer, "someone-elses is not a skill this project's team owns") {
		t.Errorf("the maintainer hears which is not its own:\n%s", answer)
	}
	// Back to the version before the trial: the person's, with the change
	// they kept by confirming it.
	if data, _ := os.ReadFile(file); strings.Count(string(data), "Copy each case.") != 2 || !strings.Contains(string(data), "By hand.") {
		t.Errorf("back to the person's version:\n%s", data)
	}
}

// A page of a skill's folder is part of the skill: changing it takes
// having the skill, and puts the skill on trial.
func TestLoop_ASkillsReferenceIsTheSkill(t *testing.T) {
	l, _ := wikiLoop(t)
	folder := skillFolder(t, "release-notes", map[string]string{
		"SKILL.md":            "---\ndescription: Use when writing release notes.\n---\n\nSee [the style](references/style.md).\n",
		"references/style.md": "# Style\n\nShort lines.\n",
	})
	if _, err := l.h.ImportSkill(l.ctx, folder, "", l.user.ID); err != nil {
		t.Fatal(err)
	}
	patch := map[string]any{"tool_calls": []any{call(runtime.WikiToolPatch, map[string]any{
		"scope": "library", "path": "/skills/release-notes/references/style.md", "reason": "longer",
		"edits": []any{map[string]any{"op": "append", "content": "Group by kind."}},
	})}}
	writer := l.member("Writer", patch)
	asked := l.say("@Writer notes", "", writer)
	l.settled(1, "Writer's first turn")
	if answer := l.root(l.topic(asked)).Body; !strings.Contains(answer, "not installed for you") {
		t.Errorf("not installed:\n%s", answer)
	}
	l.install("release-notes", writer)
	l.say("@Writer notes again", "", writer)
	l.settled(2, "Writer's second turn")
	if trial := l.trialOf("release-notes", store.TrialOpen); trial.ChangedBy != "Writer" {
		t.Errorf("the skill is on trial for its reference: %+v", trial)
	}
	// Taken out, the reference is the file it was with the change, not the
	// library's page of it.
	r, _ := l.h.openLibrary(l.ctx)
	skill, err := r.bundle.ProjectSkill("release-notes")
	if got := string(skill.Files["references/style.md"]); err != nil || strings.HasPrefix(got, "---") || !strings.Contains(got, "Short lines.") || !strings.Contains(got, "Group by kind.") {
		t.Errorf("the changed reference: %q %v", got, err)
	}
	// On trial for it, the skill is not updated from its folder.
	var p *store.Problem
	if _, err := l.h.UpdateSkill(l.ctx, folder, l.user.ID); !errors.As(err, &p) || p.Code != "skillOnTrial" {
		t.Errorf("updated while on trial: %v", err)
	}
}
