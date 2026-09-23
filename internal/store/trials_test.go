package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestSkillTrials(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	// used finishes a turn that used the skill, as it ended.
	used := func(status store.TurnStatus) store.Turn {
		t.Helper()
		turn, err := f.s.CreateTurn(ctx, f.newTurn())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: status, Error: map[bool]string{true: "boom"}[status == store.TurnFailed], SkillsUsed: []string{"go-table-tests"}}); err != nil {
			t.Fatal(err)
		}
		return turn
	}
	before := used(store.TurnDone)
	if _, err := f.s.OpenSkillTrial(ctx, "go-table-tests"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("no trial yet: %v", err)
	}

	trial, err := f.s.StartSkillTrial(ctx, store.NewSkillTrial{Skill: "go-table-tests", BaseSHA: "aaa111", TurnID: before.ID, ChangedBy: "Coder", ProjectName: "Veyloom"})
	if err != nil || trial.Status != store.TrialOpen || trial.Changes != 1 || trial.TurnID != before.ID || trial.EndedAt != nil {
		t.Fatalf("started %+v %v", trial, err)
	}
	// The turn that made the change, and those before it, used the version
	// before; the ones after count.
	used(store.TurnDone)
	used(store.TurnFailed)
	used(store.TurnCancelled)
	if done, failed, err := f.s.SkillTrialUses(ctx, "go-table-tests", trial.ChangedAt); err != nil || done != 1 || failed != 1 {
		t.Errorf("uses since the change: %d done, %d failed, %v", done, failed, err)
	}

	// Changed again: the count starts again, the way back stays.
	time.Sleep(10 * time.Millisecond)
	again, err := f.s.StartSkillTrial(ctx, store.NewSkillTrial{Skill: "go-table-tests", BaseSHA: "bbb222", ChangedBy: "Writer", ProjectName: "Docs"})
	if err != nil || again.ID != trial.ID || again.BaseSHA != "aaa111" || again.Changes != 2 || again.ChangedBy != "Writer" || again.TurnID != "" || !again.ChangedAt.After(trial.ChangedAt) {
		t.Fatalf("changed again %+v %v", again, err)
	}
	if done, failed, _ := f.s.SkillTrialUses(ctx, "go-table-tests", again.ChangedAt); done != 0 || failed != 0 {
		t.Errorf("counted again from the change: %d, %d", done, failed)
	}
	if open, err := f.s.ListOpenSkillTrials(ctx, []string{"go-table-tests", "other"}); err != nil || len(open) != 1 || open[0].ID != trial.ID {
		t.Errorf("open trials %+v %v", open, err)
	}

	kept, err := f.s.EndSkillTrial(ctx, trial.ID, store.TrialKept, "process:skill-trial", "3 turns used it")
	if err != nil || kept.Status != store.TrialKept || kept.EndedBy != "process:skill-trial" || kept.EndedAt == nil || kept.Reason != "3 turns used it" {
		t.Fatalf("kept %+v %v", kept, err)
	}
	var p *store.Problem
	if _, err := f.s.EndSkillTrial(ctx, trial.ID, store.TrialRolledBack, "human:alice", ""); !errors.Is(err, store.ErrConflict) || !errors.As(err, &p) || p.Code != "trialEnded" {
		t.Errorf("ended twice: %v", err)
	}
	if _, err := f.s.EndSkillTrial(ctx, trial.ID, store.TrialOpen, "human:alice", ""); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("ended as open: %v", err)
	}
	if latest, err := f.s.LatestSkillTrial(ctx, "go-table-tests"); err != nil || latest.ID != trial.ID || latest.Status != store.TrialKept {
		t.Errorf("the latest is the one that ended: %+v %v", latest, err)
	}

	// A new trial is open beside the ended one, and comes first.
	next, err := f.s.StartSkillTrial(ctx, store.NewSkillTrial{Skill: "go-table-tests", BaseSHA: "ccc333", ChangedBy: "Coder"})
	if err != nil || next.ID == trial.ID || next.Changes != 1 || next.BaseSHA != "ccc333" {
		t.Fatalf("a new trial %+v %v", next, err)
	}
	if latest, _ := f.s.LatestSkillTrial(ctx, "go-table-tests"); latest.ID != next.ID {
		t.Errorf("the open one is the latest: %+v", latest)
	}
	if _, err := f.s.StartSkillTrial(ctx, store.NewSkillTrial{Skill: "Not A Name", BaseSHA: "x"}); err == nil {
		t.Error("a skill is named as the library names it")
	}
	if _, err := f.s.LatestSkillTrial(ctx, "never"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("never on trial: %v", err)
	}
}
