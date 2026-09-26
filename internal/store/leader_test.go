package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// A project's leader is the member a person made it, or the first of the
// current, enabled members to have joined (docs/design.md 5.21).
func TestProjects_Leader(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()
	second, err := f.s.CreateAgent(ctx, store.NewAgent{Name: "Coder", MachineID: f.machineID, Runtime: "codex", PermissionPreset: store.PermissionFullAuto})
	if err != nil {
		t.Fatal(err)
	}
	empty, _, _ := f.s.CreateProject(ctx, store.NewProject{Name: "empty"})
	if empty.LeaderID != "" || empty.LeaderMemberID != "" {
		t.Errorf("a project without members has no leader: %+v", empty)
	}
	// Picked second, the reviewer still joins first.
	project, room, err := f.s.CreateProject(ctx, store.NewProject{Name: "team", AgentIDs: []string{f.agent.ID, second.ID}})
	if err != nil {
		t.Fatal(err)
	}
	members, _ := f.s.ListRoomMembers(ctx, room.ID)
	first, coder := members[0], members[1]
	if project.LeaderID != first.ID || project.LeaderMemberID != "" {
		t.Fatalf("the first to join leads: %+v, members %+v", project, members)
	}
	leaderOf := func() store.Project {
		t.Helper()
		got, err := f.s.GetProject(ctx, project.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	// Switched off, the first leads no more, until switched on again.
	off, on := false, true
	f.s.UpdateMember(ctx, first.ID, store.MemberPatch{Enabled: &off})
	if got := leaderOf(); got.LeaderID != coder.ID {
		t.Errorf("a member switched off still leads by default: %+v", got)
	}
	f.s.UpdateMember(ctx, first.ID, store.MemberPatch{Enabled: &on})
	if got := leaderOf(); got.LeaderID != first.ID {
		t.Errorf("switched on again: %+v", got)
	}

	// Made the leader, a member leads even while switched off: a person
	// chooses another.
	got, err := f.s.UpdateProject(ctx, project.ID, store.ProjectPatch{Leader: &coder.ID})
	if err != nil || got.LeaderMemberID != coder.ID || got.LeaderID != coder.ID {
		t.Fatalf("make the coder the leader: %+v %v", got, err)
	}
	f.s.UpdateMember(ctx, coder.ID, store.MemberPatch{Enabled: &off})
	if got := leaderOf(); got.LeaderID != coder.ID {
		t.Errorf("the leader a person chose: %+v", got)
	}
	f.s.UpdateMember(ctx, coder.ID, store.MemberPatch{Enabled: &on})

	// Every way of reading a project says who leads it.
	if byRoom, _ := f.s.RoomProject(ctx, room.ID); byRoom.LeaderID != coder.ID || byRoom.MainRoomID != room.ID {
		t.Errorf("the project of the room: %+v", byRoom)
	}
	if bySlug, _ := f.s.GetProjectBySlug(ctx, project.WikiSlug); bySlug.LeaderID != coder.ID {
		t.Errorf("the project by its wiki: %+v", bySlug)
	}
	listed, _ := f.s.ListProjects(ctx)
	for _, p := range listed {
		if p.ID == project.ID && p.LeaderID != coder.ID {
			t.Errorf("the listed project: %+v", p)
		}
	}

	// Only a current member of the project leads it.
	_, otherRoom, _ := f.s.CreateProject(ctx, store.NewProject{Name: "elsewhere"})
	stranger, _ := f.s.CreateMember(ctx, store.NewMember{RoomID: otherRoom.ID, AgentID: f.agent.ID})
	if _, err := f.s.UpdateProject(ctx, project.ID, store.ProjectPatch{Leader: &stranger.ID}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("another project's member: %v", err)
	}
	none := ""
	if got, err := f.s.UpdateProject(ctx, project.ID, store.ProjectPatch{Leader: &none}); err != nil || got.LeaderMemberID != "" || got.LeaderID != first.ID {
		t.Errorf("back to the first to join: %+v %v", got, err)
	}

	// Taken out of the project, the leader a person chose leads no more.
	f.s.UpdateProject(ctx, project.ID, store.ProjectPatch{Leader: &coder.ID})
	if _, err := f.s.RemoveMember(ctx, coder.ID); err != nil {
		t.Fatal(err)
	}
	if got := leaderOf(); got.LeaderMemberID != "" || got.LeaderID != first.ID {
		t.Errorf("a removed member still leads: %+v", got)
	}
	if _, err := f.s.UpdateProject(ctx, project.ID, store.ProjectPatch{Leader: &coder.ID}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a removed member: %v", err)
	}
}
