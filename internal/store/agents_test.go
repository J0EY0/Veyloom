package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

// agentFixture is a project with its main room, a registered machine and one
// user-defined agent.
type agentFixture struct {
	s         *store.Store
	room      store.Room
	machineID string
	agent     store.Agent
}

func newAgentFixture(t *testing.T) agentFixture {
	t.Helper()
	s := storetest.New(t)
	ctx := context.Background()
	_, room, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	machineID, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.CreateAgent(ctx, store.NewAgent{
		Name:             "Reviewer",
		MachineID:        machineID,
		Runtime:          "claude",
		Model:            "claude-opus-5",
		RoleCard:         "Review carefully.",
		PermissionPreset: store.PermissionReadOnly,
		RuntimeOptions:   map[string]any{"max_turns": float64(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return agentFixture{s: s, room: room, machineID: machineID, agent: agent}
}

func TestAgent_Avatar(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()
	if f.agent.Avatar != "" {
		t.Errorf("no avatar asked for, got %q", f.agent.Avatar)
	}

	picture := "0123456789abcdef0123456789abcdef.webp"
	change := store.NewAgent{Name: f.agent.Name, Avatar: picture, MachineID: f.machineID, Runtime: f.agent.Runtime, PermissionPreset: f.agent.PermissionPreset}
	if updated, err := f.s.UpdateAgent(ctx, f.agent.ID, change); err != nil || updated.Avatar != picture {
		t.Fatalf("set avatar: %+v, %v", updated, err)
	}
	if list, err := f.s.ListAgents(ctx); err != nil || len(list) != 1 || list[0].Avatar != picture {
		t.Errorf("listed: %+v, %v", list, err)
	}
	if n, err := f.s.AgentsWithAvatar(ctx, picture); err != nil || n != 1 {
		t.Errorf("agents with the avatar: %d, %v", n, err)
	}

	change.Avatar = ""
	if updated, err := f.s.UpdateAgent(ctx, f.agent.ID, change); err != nil || updated.Avatar != "" {
		t.Fatalf("clear avatar: %+v, %v", updated, err)
	}
	if n, err := f.s.AgentsWithAvatar(ctx, picture); err != nil || n != 0 {
		t.Errorf("no agent shows it now: %d, %v", n, err)
	}
}

func TestAgent_CreateGetUpdateList(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()

	got, err := f.s.GetAgent(ctx, f.agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Reviewer" || got.Runtime != "claude" || got.PermissionPreset != store.PermissionReadOnly {
		t.Errorf("unexpected agent: %+v", got)
	}
	// Set up on its machine, which it is known by, and in no project yet.
	if got.MachineID != f.machineID || got.MachineName != "laptop" || got.Projects == nil || len(got.Projects) != 0 {
		t.Errorf("machine and projects: %+v", got)
	}
	if got.RuntimeOptions["max_turns"] != float64(3) {
		t.Errorf("runtime options not round-tripped: %v", got.RuntimeOptions)
	}

	updated, err := f.s.UpdateAgent(ctx, f.agent.ID, store.NewAgent{
		Name: "Reviewer v2", MachineID: f.machineID, Runtime: "codex", PermissionPreset: store.PermissionFullAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Reviewer v2" || updated.Runtime != "codex" || !updated.UpdatedAt.After(got.UpdatedAt) {
		t.Errorf("update not applied: %+v", updated)
	}
	if updated.RuntimeOptions == nil || len(updated.RuntimeOptions) != 0 {
		t.Errorf("nil options should be stored as {}, got %#v", updated.RuntimeOptions)
	}

	list, err := f.s.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != f.agent.ID {
		t.Errorf("unexpected list: %+v", list)
	}

	_, err = f.s.UpdateAgent(ctx, "00000000-0000-0000-0000-000000000000", store.NewAgent{Name: "x", MachineID: f.machineID, Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("update unknown: got %v, want ErrNotFound", err)
	}
}

// An agent is set up on one machine and its members run there, so it moves
// to another only while it is in no project.
func TestAgent_MovesOnlyOutsideProjects(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()
	other, err := f.s.RegisterMachine(ctx, "", "build-box", nil)
	if err != nil {
		t.Fatal(err)
	}
	move := store.NewAgent{Name: f.agent.Name, MachineID: other, Runtime: f.agent.Runtime, PermissionPreset: f.agent.PermissionPreset}

	member, err := f.s.CreateMember(ctx, store.NewMember{RoomID: f.room.ID, AgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.s.GetAgent(ctx, f.agent.ID); err != nil || !slices.Equal(got.Projects, []string{"p"}) {
		t.Errorf("projects of a member: %+v, %v", got.Projects, err)
	}

	// In a project it stays where it is; the rest of it can still change.
	_, err = f.s.UpdateAgent(ctx, f.agent.ID, move)
	var inUse *store.AgentInUseError
	if !errors.As(err, &inUse) || !errors.Is(err, store.ErrConflict) || !slices.Equal(inUse.Projects, []string{"p"}) {
		t.Fatalf("move while in a project: got %v, want AgentInUseError naming p", err)
	}
	stay := move
	stay.MachineID, stay.RoleCard = f.machineID, "Review twice."
	if kept, err := f.s.UpdateAgent(ctx, f.agent.ID, stay); err != nil || kept.MachineID != f.machineID || kept.RoleCard != "Review twice." {
		t.Errorf("edit in place: %+v, %v", kept, err)
	}

	// Taken out, it moves, and a member added after runs on the new machine.
	if _, err := f.s.RemoveMember(ctx, member.ID); err != nil {
		t.Fatal(err)
	}
	moved, err := f.s.UpdateAgent(ctx, f.agent.ID, move)
	if err != nil || moved.MachineID != other || moved.MachineName != "build-box" || len(moved.Projects) != 0 {
		t.Fatalf("move once out of the project: %+v, %v", moved, err)
	}
	again, err := f.s.CreateMember(ctx, store.NewMember{RoomID: f.room.ID, AgentID: f.agent.ID})
	if err != nil || again.MachineID != other {
		t.Errorf("member after the move: %+v, %v", again, err)
	}
	if former, err := f.s.GetMember(ctx, member.ID); err != nil || former.MachineID != f.machineID {
		t.Errorf("the member taken out keeps where it ran: %+v, %v", former, err)
	}
}

func TestAgent_Errors(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()

	_, err := f.s.CreateAgent(ctx, store.NewAgent{Name: "Reviewer", MachineID: f.machineID, Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
	if !errors.Is(err, store.ErrConflict) || store.Reason(err) != "another agent already has this name" {
		t.Errorf("duplicate name: got %v, want ErrConflict saying so", err)
	}
	_, err = f.s.CreateAgent(ctx, store.NewAgent{Name: "Bad", MachineID: f.machineID, Runtime: "pi", PermissionPreset: "yolo"})
	if !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("bad preset: got %v, want ErrInvalidInput", err)
	}
	_, err = f.s.CreateAgent(ctx, store.NewAgent{Name: " ", MachineID: f.machineID, Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
	if !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("blank name: got %v, want ErrInvalidInput", err)
	}
	_, err = f.s.CreateAgent(ctx, store.NewAgent{Name: "Nowhere", MachineID: "00000000-0000-0000-0000-000000000000", Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown machine: got %v, want ErrNotFound", err)
	}
}

// A fresh database ships with no agents at all (2026-09-16): the Agents
// page asks you to make the first one, so the list starts empty and comes
// back oldest first.
func TestDeleteAgent(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()

	spare, err := f.s.CreateAgent(ctx, store.NewAgent{Name: "Spare", MachineID: f.machineID, Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteAgent(ctx, spare.ID); err != nil {
		t.Fatalf("delete unused: %v", err)
	}
	if _, err := f.s.GetAgent(ctx, spare.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get after delete: got %v, want ErrNotFound", err)
	}
	if err := f.s.DeleteAgent(ctx, spare.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("delete twice: got %v, want ErrNotFound", err)
	}
	if err := f.s.DeleteAgent(ctx, "not-a-uuid"); !errors.Is(err, store.ErrInvalidID) || store.Reason(err) != `"not-a-uuid" is not an id` {
		t.Errorf("bad id: got %v, want ErrInvalidID saying so", err)
	}

	// While it is a member of a project the agent stays, and the error
	// says where to take it out.
	member, err := f.s.CreateMember(ctx, store.NewMember{RoomID: f.room.ID, AgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	err = f.s.DeleteAgent(ctx, f.agent.ID)
	var inUse *store.AgentInUseError
	if !errors.As(err, &inUse) || !errors.Is(err, store.ErrConflict) || !slices.Equal(inUse.Projects, []string{"p"}) {
		t.Fatalf("delete in use: got %v, want AgentInUseError naming project p", err)
	}
	if _, err := f.s.GetAgent(ctx, f.agent.ID); err != nil {
		t.Errorf("agent in use is gone: %v", err)
	}

	// Once taken out, the member no longer holds it back. Its row stays for
	// its history and only loses the link.
	if _, err := f.s.RemoveMember(ctx, member.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteAgent(ctx, f.agent.ID); err != nil {
		t.Fatalf("delete after the member was taken out: %v", err)
	}
	former, err := f.s.GetMember(ctx, member.ID)
	if err != nil || former.AgentID != "" || former.DisplayName != "Reviewer" || !former.Removed() {
		t.Errorf("former member: %+v, %v", former, err)
	}
}

func TestRemoveMember(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	// A running turn keeps the member in: it would go on replying for
	// someone who is gone.
	turn, err := f.s.CreateTurn(ctx, f.newTurn())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RemoveMember(ctx, f.member.ID); !errors.Is(err, store.ErrConflict) {
		t.Errorf("remove while working: got %v, want ErrConflict", err)
	}
	reply, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: store.TurnDone, ReplyMessageID: reply.ID}); err != nil {
		t.Fatal(err)
	}

	removed, err := f.s.RemoveMember(ctx, f.member.ID)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !removed.Removed() {
		t.Errorf("RemovedAt not set: %+v", removed)
	}

	// The row stays for what it said, but it is no longer a member.
	list, err := f.s.ListRoomMembers(ctx, f.room.ID)
	if err != nil || len(list) != 1 || !list[0].Removed() {
		t.Errorf("list after removal: %+v, %v", list, err)
	}
	if msg, err := f.s.GetMessage(ctx, reply.ID); err != nil || msg.MemberID != f.member.ID {
		t.Errorf("its reply: %+v, %v", msg, err)
	}
	if _, err := f.s.RemoveMember(ctx, f.member.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("remove twice: got %v, want ErrNotFound", err)
	}
	name := "Renamed"
	if _, err := f.s.UpdateMember(ctx, f.member.ID, store.MemberPatch{DisplayName: &name}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("edit after removal: got %v, want ErrNotFound", err)
	}
	if _, err := f.s.RemoveMember(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("remove unknown: got %v, want ErrNotFound", err)
	}

	// Its name is free again, so the same agent can come back.
	back, err := f.s.CreateMember(ctx, store.NewMember{RoomID: f.room.ID, AgentID: f.agent.ID})
	if err != nil {
		t.Fatalf("add back under the same name: %v", err)
	}
	if back.ID == f.member.ID || back.DisplayName != f.member.DisplayName || back.Removed() {
		t.Errorf("added back: %+v", back)
	}
}

func TestListAgents_EmptyThenOldestFirst(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	list, err := s.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("a fresh database should have no agents, got %d", len(list))
	}

	machineID, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Zeta", "Alpha"} {
		if _, err := s.CreateAgent(ctx, store.NewAgent{
			Name: name, MachineID: machineID, Runtime: "pi", PermissionPreset: store.PermissionReadOnly,
		}); err != nil {
			t.Fatal(err)
		}
	}
	list, err = s.ListAgents(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "Zeta" || list[1].Name != "Alpha" {
		t.Errorf("want the order they were added, got %v", names(list))
	}
}

func names(list []store.Agent) []string {
	out := make([]string, len(list))
	for i, agent := range list {
		out[i] = agent.Name
	}
	return out
}

func TestMember_CreateGetList(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()

	member, err := f.s.CreateMember(ctx, store.NewMember{
		RoomID: f.room.ID, AgentID: f.agent.ID, RepoPath: "/src/p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if member.DisplayName != "Reviewer" {
		t.Errorf("display name should default to the agent's, got %q", member.DisplayName)
	}
	if member.MachineID != f.machineID {
		t.Errorf("a member runs on its agent's machine, got %q", member.MachineID)
	}
	if member.BranchMode != store.BranchWorktree || !member.Enabled || member.PermissionPreset != "" || member.Model != "" {
		t.Errorf("unexpected defaults: %+v", member)
	}

	second, err := f.s.CreateMember(ctx, store.NewMember{
		RoomID: f.room.ID, AgentID: f.agent.ID,
		DisplayName: "Reviewer B", BranchMode: store.BranchShared, Model: "haiku", PermissionPreset: store.PermissionFullAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.BranchMode != store.BranchShared || second.Model != "haiku" || second.PermissionPreset != store.PermissionFullAuto {
		t.Errorf("overrides not stored: %+v", second)
	}

	got, err := f.s.GetMember(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RepoPath != "/src/p" || got.AgentID != f.agent.ID || got.MachineID != f.machineID {
		t.Errorf("unexpected member: %+v", got)
	}

	list, err := f.s.ListRoomMembers(ctx, f.room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != member.ID || list[1].ID != second.ID {
		t.Errorf("unexpected list: %+v", list)
	}
}

func TestMember_Errors(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()
	unknown := "00000000-0000-0000-0000-000000000000"
	base := store.NewMember{RoomID: f.room.ID, AgentID: f.agent.ID}

	if _, err := f.s.CreateMember(ctx, base); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateMember(ctx, base); !errors.Is(err, store.ErrConflict) || store.Reason(err) != "another member of this chat already has this name" {
		t.Errorf("duplicate display name in room: got %v, want ErrConflict saying so", err)
	}

	tests := map[string]store.NewMember{
		"unknown room":  {RoomID: unknown, AgentID: f.agent.ID},
		"unknown agent": {RoomID: f.room.ID, AgentID: unknown, DisplayName: "x"},
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := f.s.CreateMember(ctx, in); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("got %v, want ErrNotFound", err)
			}
		})
	}
	bad := base
	bad.DisplayName, bad.BranchMode = "z", "detached"
	if _, err := f.s.CreateMember(ctx, bad); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("bad branch mode: got %v, want ErrInvalidInput", err)
	}
}

func TestMessages_AgentSender(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()
	member, err := f.s.CreateMember(ctx, store.NewMember{RoomID: f.room.ID, AgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := f.s.CreateMessage(ctx, store.NewMessage{
		RoomID: f.room.ID, SenderKind: store.SenderAgent, MemberID: member.ID, Body: "done",
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.MemberID != member.ID || msg.UserID != "" || msg.SenderKind != store.SenderAgent {
		t.Errorf("unexpected message: %+v", msg)
	}

	_, err = f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderAgent, Body: "no sender"})
	if !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("agent kind without member: got %v, want ErrInvalidInput", err)
	}
	_, err = f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderAgent, MemberID: "00000000-0000-0000-0000-000000000000", Body: "ghost"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown member: got %v, want ErrNotFound", err)
	}
}

func TestMember_Update(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()
	member, err := f.s.CreateMember(ctx, store.NewMember{RoomID: f.room.ID, AgentID: f.agent.ID})
	if err != nil {
		t.Fatal(err)
	}

	name, off := "Renamed", false
	got, err := f.s.UpdateMember(ctx, member.ID, store.MemberPatch{DisplayName: &name, Enabled: &off})
	if err != nil {
		t.Fatal(err)
	}
	if got.DisplayName != "Renamed" || got.Enabled || got.RepoPath != member.RepoPath || got.PermissionPreset != member.PermissionPreset {
		t.Errorf("only the named fields change, got %+v", got)
	}
	preset := store.PermissionFullAuto
	if got, err = f.s.UpdateMember(ctx, member.ID, store.MemberPatch{PermissionPreset: &preset}); err != nil || got.PermissionPreset != preset || got.DisplayName != "Renamed" {
		t.Errorf("preset patch: %+v, %v", got, err)
	}
	if _, err := f.s.UpdateMember(ctx, "00000000-0000-0000-0000-000000000000", store.MemberPatch{}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown member: got %v, want ErrNotFound", err)
	}
}
