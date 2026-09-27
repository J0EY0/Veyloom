package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

// An account's pause and a member's: one of each at a time, updated in
// place, lifted once.
func TestPauses(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	machineID, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, room, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.CreateAgent(ctx, store.NewAgent{Name: "Coder", MachineID: machineID, Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: agent.ID, DisplayName: "Coder"})
	if err != nil {
		t.Fatal(err)
	}

	resets := time.Now().Add(time.Hour).Truncate(time.Second)
	first, err := s.PauseAccount(ctx, machineID, "claude", store.PauseRateLimit, "429", &resets)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.PauseAccount(ctx, machineID, "claude", store.PauseQuota, "You've hit your limit", &resets)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != first.ID || again.Reason != store.PauseQuota || !again.CreatedAt.Equal(first.CreatedAt) || !again.Account() || again.MemberID != "" {
		t.Errorf("updated in place, keeping when it began: %+v then %+v", first, again)
	}
	if _, err := s.PauseAccount(ctx, machineID, "codex", store.PauseAuth, "unauthorized", nil); err != nil {
		t.Fatal(err)
	}
	paused, err := s.PauseMember(ctx, member.ID, store.PauseFailing, "exit status 1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Account() || paused.MachineID != "" || paused.EndsAt != nil {
		t.Errorf("a member's pause: %+v", paused)
	}

	all, err := s.ListPauses(ctx)
	if err != nil || len(all) != 3 || all[0].Runtime != "claude" || all[0].EndsAt == nil || !all[0].EndsAt.Equal(resets) || all[2].MemberID != member.ID {
		t.Fatalf("pauses = %+v, %v", all, err)
	}
	for _, lift := range []func() (bool, error){
		func() (bool, error) { return s.LiftAccountPause(ctx, machineID, "claude") },
		func() (bool, error) { return s.LiftMemberPause(ctx, member.ID) },
	} {
		if lifted, err := lift(); err != nil || !lifted {
			t.Errorf("lift: %v, %v", lifted, err)
		}
		if lifted, err := lift(); err != nil || lifted {
			t.Errorf("lift again: %v, %v", lifted, err)
		}
	}
	all, _ = s.ListPauses(ctx)
	if len(all) != 1 || all[0].Runtime != "codex" {
		t.Errorf("left: %+v", all)
	}
	if _, err := s.PauseMember(ctx, "not-a-uuid", store.PauseFailing, "", nil); err == nil {
		t.Error("a bad id is an error")
	}
}
