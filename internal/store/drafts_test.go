package store_test

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

// What members draft for a person to do (docs/design.md 5.23.5): one
// pending draft about a subject at a time, run once, turned down once.
func TestDrafts(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	machineID, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	project, room, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	member := func(name string) store.Member {
		agent, err := s.CreateAgent(ctx, store.NewAgent{Name: name, MachineID: machineID, Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		m, err := s.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: agent.ID, DisplayName: name})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	lead, coder := member("Lead"), member("Coder")
	asked, err := s.CreateMessage(ctx, store.NewMessage{RoomID: room.ID, SenderKind: store.SenderAgent, MemberID: lead.ID, Body: "on it"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.ThreadForMessage(ctx, asked.ID)
	if err != nil {
		t.Fatal(err)
	}
	draft := func(kind store.DraftKind, subject string, params store.DraftParams) (store.Draft, []store.Draft) {
		d, gone, err := s.CreateDraft(ctx, store.NewDraft{
			ProjectID: project.ID, RoomID: room.ID, ThreadID: thread.ID, MemberID: lead.ID, Kind: kind, TargetID: coder.ID,
			Subject: subject, Params: params, Then: "hand the tests to Tester",
		})
		if err != nil {
			t.Fatal(err)
		}
		return d, gone
	}

	merge, gone := draft(store.DraftMerge, "work:"+coder.ID, store.DraftParams{Message: "Add tags"})
	if merge.Status != store.DraftPending || merge.Params.Message != "Add tags" || merge.Then == "" || merge.TargetID != coder.ID || len(gone) != 0 {
		t.Fatalf("drafted: %+v %+v", merge, gone)
	}
	// The same member's work drafted anew: the first gives way.
	aside, gone := draft(store.DraftSetAside, "work:"+coder.ID, store.DraftParams{Reason: "wrong approach"})
	if len(gone) != 1 || gone[0].ID != merge.ID || gone[0].Status != store.DraftSuperseded {
		t.Errorf("superseded: %+v", gone)
	}
	// Another subject stands apart.
	skill, gone := draft(store.DraftInstallSkill, "skill:"+coder.ID+":go-testing", store.DraftParams{Skill: "go-testing"})
	if len(gone) != 0 {
		t.Errorf("a skill supersedes no work: %+v", gone)
	}

	card, err := s.CreateMessage(ctx, store.NewMessage{RoomID: room.ID, ThreadID: thread.ID, SenderKind: store.SenderSystem, Body: "drafted"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDraftMessage(ctx, aside.ID, card.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetDraftByMessage(ctx, card.ID); err != nil || got.ID != aside.ID {
		t.Errorf("by its card: %+v %v", got, err)
	}

	// A run claims it once; one that cannot be done for now lets it wait.
	if _, err := s.ClaimDraft(ctx, aside.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClaimDraft(ctx, aside.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("claimed twice: %v", err)
	}
	if d, err := s.ReleaseDraft(ctx, aside.ID); err != nil || d.Status != store.DraftPending {
		t.Errorf("released: %+v %v", d, err)
	}
	if _, err := s.ClaimDraft(ctx, aside.ID); err != nil {
		t.Fatal(err)
	}
	account := store.NewID()
	done, err := s.SettleDraft(ctx, aside.ID, store.DraftDone, store.DraftResult{Ref: "refs/veyloom/set-aside/x"}, account)
	if err != nil || done.Status != store.DraftDone || done.Result.Ref != "refs/veyloom/set-aside/x" || done.DecidedBy != account || done.SettledAt == nil {
		t.Errorf("settled: %+v %v", done, err)
	}
	if _, err := s.SettleDraft(ctx, aside.ID, store.DraftDone, store.DraftResult{}, account); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("settled twice: %v", err)
	}
	result, err := s.CreateMessage(ctx, store.NewMessage{RoomID: room.ID, ThreadID: thread.ID, SenderKind: store.SenderSystem, Body: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDraftResultMessage(ctx, aside.ID, result.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetDraftByMessage(ctx, result.ID); err != nil || got.ID != aside.ID || got.ResultMessageID != result.ID {
		t.Errorf("by its outcome: %+v %v", got, err)
	}

	// Turned down once; a run the hub left under way waits again.
	if d, err := s.DeclineDraft(ctx, skill.ID, account); err != nil || d.Status != store.DraftDeclined {
		t.Errorf("declined: %+v %v", d, err)
	}
	if _, err := s.DeclineDraft(ctx, skill.ID, account); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("declined twice: %v", err)
	}
	again, _ := draft(store.DraftMerge, "work:"+coder.ID, store.DraftParams{Message: "Add tags, again"})
	if _, err := s.ClaimDraft(ctx, again.ID); err != nil {
		t.Fatal(err)
	}
	if released, err := s.ReleaseRunningDrafts(ctx); err != nil || len(released) != 1 || released[0].ID != again.ID {
		t.Errorf("released at start: %+v %v", released, err)
	}

	// The one not settled about a subject, and giving way when it is
	// settled otherwise.
	if open, err := s.OpenDraft(ctx, project.ID, "work:"+coder.ID); err != nil || open.ID != again.ID {
		t.Errorf("open: %+v %v", open, err)
	}
	if gone, err := s.SupersedeDrafts(ctx, project.ID, "work:"+coder.ID); err != nil || len(gone) != 1 || gone[0].ID != again.ID {
		t.Errorf("superseded: %+v %v", gone, err)
	}
	if _, err := s.OpenDraft(ctx, project.ID, "work:"+coder.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("none open: %v", err)
	}

	all, err := s.ListThreadDrafts(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	statuses := make([]store.DraftStatus, len(all))
	for i, d := range all {
		statuses[i] = d.Status
	}
	if !slices.Equal(statuses, []store.DraftStatus{store.DraftSuperseded, store.DraftDone, store.DraftDeclined, store.DraftSuperseded}) {
		t.Errorf("the topic's, in the order drafted: %v", statuses)
	}
	if n, err := s.CountTurnDrafts(ctx, store.NewID()); err != nil || n != 0 {
		t.Errorf("a turn with none: %d %v", n, err)
	}

	// Drafted by many at once, one stays pending and the rest gave way.
	subject := "skill:" + coder.ID + ":go-testing"
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, _, err := s.CreateDraft(ctx, store.NewDraft{
				ProjectID: project.ID, RoomID: room.ID, ThreadID: thread.ID, MemberID: lead.ID, Kind: store.DraftInstallSkill, TargetID: coder.ID,
				Subject: subject, Params: store.DraftParams{Skill: "go-testing"},
			}); err != nil {
				t.Errorf("drafted at once: %v", err)
			}
		})
	}
	wg.Wait()
	all, err = s.ListThreadDrafts(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	pending, superseded := 0, 0
	for _, d := range all {
		if d.Subject != subject {
			continue
		}
		switch d.Status {
		case store.DraftPending:
			pending++
		case store.DraftSuperseded:
			superseded++
		}
	}
	if pending != 1 || superseded != 7 {
		t.Errorf("drafted at once: %d pending, %d gave way", pending, superseded)
	}
}
