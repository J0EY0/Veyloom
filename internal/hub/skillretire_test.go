package hub

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// A person takes a skill out of use and puts it back, or removes it: out
// of use it goes to no agent and cannot be installed anew, back in use it
// goes to them again as it was; removed, it leaves the library and every
// agent it was installed for, and the library's history can bring its
// files back.
func TestHub_RetireAndDeleteSkill(t *testing.T) {
	l, dir := wikiLoop(t)
	l.addSkill("go-table-tests", "Use when writing Go tests with several cases.", "Write the cases as a table.", l.project().ID)
	coder, reader := l.member("Coder", nil), l.member("Reader", nil)
	l.install("go-table-tests", coder)
	given := func() int {
		t.Helper()
		agent, _ := l.s.GetAgent(l.ctx, coder.AgentID)
		return len(l.h.turns.librarySkills(l.ctx, agent))
	}
	logOf := func() string {
		data, _ := os.ReadFile(filepath.Join(dir, "library", "log.md"))
		return string(data)
	}

	page, err := l.h.RetireSkill(l.ctx, "go-table-tests", true, l.user.ID)
	if err != nil || page.Status != string(okf.Deprecated) {
		t.Fatalf("retired: %+v %v", page.WikiPageInfo, err)
	}
	if n := given(); n != 0 {
		t.Errorf("a retired skill goes to no agent: %d", n)
	}
	var p *store.Problem
	if _, err := l.h.InstallSkill(l.ctx, "go-table-tests", reader.AgentID, true); !errors.As(err, &p) || p.Code != "skillUnknown" {
		t.Errorf("not installed anew: %v", err)
	}
	if agents, _ := l.s.ListSkillAgents(l.ctx, "go-table-tests"); len(agents) != 1 {
		t.Errorf("still installed where it was: %v", agents)
	}
	if !strings.Contains(logOf(), "**Deprecation**") {
		t.Errorf("the log:\n%s", logOf())
	}
	page, err = l.h.RetireSkill(l.ctx, "go-table-tests", false, l.user.ID)
	if err != nil || page.Status != string(okf.Stable) {
		t.Fatalf("back in use: %+v %v", page.WikiPageInfo, err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "library", "skills", "go-table-tests", "SKILL.md")); strings.Contains(string(data), "status:") {
		t.Errorf("as it was, with no status:\n%s", data)
	}
	if n := given(); n != 1 {
		t.Errorf("back to its agents: %d", n)
	}

	// On trial, it is not removed.
	trial, err := l.s.StartSkillTrial(l.ctx, store.NewSkillTrial{Skill: "go-table-tests", BaseSHA: "abc", ChangedBy: "Coder", ProjectName: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.h.DeleteSkill(l.ctx, "go-table-tests", l.user.ID); !errors.As(err, &p) || p.Code != "skillOnTrial" {
		t.Errorf("on trial: %v", err)
	}
	if _, err := l.s.EndSkillTrial(l.ctx, trial.ID, store.TrialKept, "human:alice", ""); err != nil {
		t.Fatal(err)
	}

	if err := l.h.DeleteSkill(l.ctx, "go-table-tests", l.user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "library", "skills", "go-table-tests")); !os.IsNotExist(err) {
		t.Errorf("its folder is gone: %v", err)
	}
	if _, err := l.h.LibraryPage(l.ctx, wiki.SkillPath("go-table-tests")); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("its page is gone: %v", err)
	}
	if agents, _ := l.s.ListSkillAgents(l.ctx, "go-table-tests"); len(agents) != 0 {
		t.Errorf("taken off its agents: %v", agents)
	}
	if agent, _ := l.s.GetAgent(l.ctx, coder.AgentID); slices.Contains(agent.Skills, "go-table-tests") {
		t.Errorf("Coder's skills: %v", agent.Skills)
	}
	if !strings.Contains(logOf(), "**Removal**: /skills/go-table-tests/SKILL.md was removed by human:alice") {
		t.Errorf("the log:\n%s", logOf())
	}
	if index, _ := os.ReadFile(filepath.Join(dir, "library", "skills", "index.md")); strings.Contains(string(index), "go-table-tests") {
		t.Errorf("the index no longer lists it:\n%s", index)
	}
	if out, _ := exec.Command("git", "-C", filepath.Join(dir, "library"), "status", "--porcelain").Output(); len(out) > 0 {
		t.Errorf("all of it committed:\n%s", out)
	}
	history, _ := l.h.LibraryHistory(l.ctx, "", 1)
	if len(history) == 0 || history[0].Subject != "Removed the skill go-table-tests" || history[0].Author != "human:alice" {
		t.Fatalf("history %+v", history)
	}
	// The history brings its files back.
	if _, err := l.h.RevertLibrary(l.ctx, history[0].SHA, l.user.ID, "wanted after all"); err != nil {
		t.Fatal(err)
	}
	if page, err := l.h.LibraryPage(l.ctx, wiki.SkillPath("go-table-tests")); err != nil || !strings.Contains(page.Body, "Write the cases as a table.") {
		t.Errorf("back from the history: %+v %v", page.WikiPageInfo, err)
	}
	if err := l.h.DeleteSkill(l.ctx, "nope", l.user.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("no such skill: %v", err)
	}
}
