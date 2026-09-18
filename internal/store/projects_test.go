package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

func TestCreateProject_CreatesMainRoomAtomically(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	project, room, err := s.CreateProject(ctx, store.NewProject{Name: "veyloom", RepoPath: "/src/veyloom"})
	if err != nil {
		t.Fatal(err)
	}
	if project.ID == "" || project.Name != "veyloom" || project.RepoPath != "/src/veyloom" {
		t.Errorf("unexpected project: %+v", project)
	}
	if room.ProjectID != project.ID || room.Kind != store.RoomMain || room.Name != store.MainRoomName {
		t.Errorf("unexpected main room: %+v", room)
	}

	rooms, err := s.ListRooms(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].ID != room.ID {
		t.Errorf("project should have exactly its main room, got %+v", rooms)
	}
	if members, err := s.ListRoomMembers(ctx, room.ID); err != nil || len(members) != 0 {
		t.Errorf("no agents asked for, no members: %+v, %v", members, err)
	}
}

func TestCreateProject_WithMembers(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	laptop, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	box, err := s.RegisterMachine(ctx, "", "build-box", nil)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := s.CreateAgent(ctx, store.NewAgent{Name: "Reviewer", MachineID: laptop, Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	tester, err := s.CreateAgent(ctx, store.NewAgent{Name: "Tester", MachineID: box, Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}

	// Named twice, the reviewer joins once.
	project, room, err := s.CreateProject(ctx, store.NewProject{Name: "team", RepoPath: "/src/team", AgentIDs: []string{reviewer.ID, tester.ID, reviewer.ID}})
	if err != nil {
		t.Fatal(err)
	}
	members, err := s.ListRoomMembers(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("want the two agents as members, got %+v", members)
	}
	// Each goes by its agent's name, runs on its agent's machine and works
	// in the project's checkout.
	for i, want := range []struct{ agent, name, machine string }{{reviewer.ID, "Reviewer", laptop}, {tester.ID, "Tester", box}} {
		got := members[i]
		if got.AgentID != want.agent || got.DisplayName != want.name || got.MachineID != want.machine || got.RepoPath != "/src/team" || got.BranchMode != store.BranchWorktree {
			t.Errorf("member %d: %+v", i, got)
		}
	}
	if got, err := s.GetAgent(ctx, tester.ID); err != nil || len(got.Projects) != 1 || got.Projects[0] != project.Name {
		t.Errorf("the tester should be in the project: %+v, %v", got.Projects, err)
	}
}

func TestCreateProject_UnknownAgentLeavesNothing(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	laptop, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := s.CreateAgent(ctx, store.NewAgent{Name: "Reviewer", MachineID: laptop, Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}

	unknown := "00000000-0000-0000-0000-000000000000"
	if _, _, err := s.CreateProject(ctx, store.NewProject{Name: "team", AgentIDs: []string{reviewer.ID, unknown}}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown agent: got %v, want ErrNotFound", err)
	}
	if projects, err := s.ListProjects(ctx); err != nil || len(projects) != 0 {
		t.Errorf("the project should not be left behind: %+v, %v", projects, err)
	}
	if got, err := s.GetAgent(ctx, reviewer.ID); err != nil || len(got.Projects) != 0 {
		t.Errorf("the reviewer should be in no project: %+v, %v", got.Projects, err)
	}
	if _, _, err := s.CreateProject(ctx, store.NewProject{Name: "team", AgentIDs: []string{"nope"}}); !errors.Is(err, store.ErrInvalidID) {
		t.Errorf("malformed agent id: got %v, want ErrInvalidID", err)
	}
}

func TestCreateProject_RejectsBlankName(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	if _, _, err := s.CreateProject(ctx, store.NewProject{Name: "   "}); err == nil {
		t.Fatal("blank name should be rejected by the schema")
	}

	// The failed transaction must leave nothing behind.
	projects, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Errorf("expected no projects after a failed create, got %+v", projects)
	}
}

func TestGetProject(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	created, _, err := s.CreateProject(ctx, store.NewProject{Name: "p", RepoPath: "/src/p"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetProject(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "p" || got.RepoPath != "/src/p" {
		t.Errorf("unexpected project: %+v", got)
	}

	if _, err := s.GetProject(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown id: got %v, want ErrNotFound", err)
	}
	if _, err := s.GetProject(ctx, "nope"); !errors.Is(err, store.ErrInvalidID) {
		t.Errorf("malformed id: got %v, want ErrInvalidID", err)
	}
}

func TestListProjects_CreationOrder(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	for _, name := range []string{"first", "second", "third"} {
		if _, _, err := s.CreateProject(ctx, store.NewProject{Name: name}); err != nil {
			t.Fatal(err)
		}
	}

	projects, err := s.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 3 || projects[0].Name != "first" || projects[2].Name != "third" {
		t.Errorf("unexpected order: %+v", projects)
	}
}

func TestCreateRoom_TopicRoomsAfterMain(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	project, main, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	topic, err := s.CreateRoom(ctx, project.ID, "auth-refactor")
	if err != nil {
		t.Fatal(err)
	}
	if topic.Kind != store.RoomTopic || topic.ProjectID != project.ID {
		t.Errorf("unexpected topic room: %+v", topic)
	}

	rooms, err := s.ListRooms(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 2 || rooms[0].ID != main.ID || rooms[1].ID != topic.ID {
		t.Errorf("main room must be listed first, got %+v", rooms)
	}

	got, err := s.GetRoom(ctx, topic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "auth-refactor" {
		t.Errorf("unexpected room: %+v", got)
	}
}

func TestCreateRoom_UnknownProjectIsNotFound(t *testing.T) {
	s := storetest.New(t)

	_, err := s.CreateRoom(context.Background(), "00000000-0000-0000-0000-000000000000", "orphan")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestDeleteProjectCascadesToRooms(t *testing.T) {
	// The schema's ON DELETE CASCADE is what takes the rooms along.
	s := storetest.New(t)
	ctx := context.Background()

	project, room, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if remains, err := s.DeleteProject(ctx, project.ID); err != nil || len(remains.AttachmentPaths) != 0 || len(remains.TurnIDs) != 0 {
		t.Fatalf("delete an empty project: %+v, %v", remains, err)
	}

	if _, err := s.GetRoom(ctx, room.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("room should be gone with its project, got %v", err)
	}
}

func TestUpdateProject_RenamesWithoutMovingMembers(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	laptop, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.CreateAgent(ctx, store.NewAgent{Name: "Tester", MachineID: laptop, Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	project, room, err := s.CreateProject(ctx, store.NewProject{Name: "team", RepoPath: "/src/team", AgentIDs: []string{agent.ID}})
	if err != nil {
		t.Fatal(err)
	}
	custom := "/src/team/web"
	members, err := s.ListRoomMembers(ctx, room.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("members: %+v, %v", members, err)
	}
	if _, err := s.UpdateMember(ctx, members[0].ID, store.MemberPatch{RepoPath: &custom}); err != nil {
		t.Fatal(err)
	}

	name := "platform"
	updated, err := s.UpdateProject(ctx, project.ID, store.ProjectPatch{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "platform" || updated.RepoPath != "/src/team" || updated.MainRoomID != room.ID || updated.ID != project.ID {
		t.Errorf("updated: %+v", updated)
	}
	if got, err := s.GetMember(ctx, members[0].ID); err != nil || got.RepoPath != custom {
		t.Errorf("a rename leaves the members where they work: %+v, %v", got, err)
	}
	if got, err := s.GetProject(ctx, project.ID); err != nil || got.Name != "platform" {
		t.Errorf("stored: %+v, %v", got, err)
	}
}

func TestUpdateProject_MovesMembersWithTheCheckout(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	laptop, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	agent := func(name string) string {
		t.Helper()
		a, err := s.CreateAgent(ctx, store.NewAgent{Name: name, MachineID: laptop, Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		return a.ID
	}
	member := func(roomID, name, path string) string {
		t.Helper()
		m, err := s.CreateMember(ctx, store.NewMember{RoomID: roomID, AgentID: agent(name), DisplayName: name, RepoPath: path})
		if err != nil {
			t.Fatal(err)
		}
		return m.ID
	}
	project, room, err := s.CreateProject(ctx, store.NewProject{Name: "team", RepoPath: "/src/team"})
	if err != nil {
		t.Fatal(err)
	}
	inCheckout := member(room.ID, "InCheckout", "/src/team")
	below := member(room.ID, "Below", "/src/team/web/app")
	elsewhere := member(room.ID, "Elsewhere", "/tmp/scratch")
	lookalike := member(room.ID, "Lookalike", "/src/team-old")
	unset := member(room.ID, "Unset", "")
	gone := member(room.ID, "Gone", "/src/team")
	if _, err := s.RemoveMember(ctx, gone); err != nil {
		t.Fatal(err)
	}
	_, otherRoom, err := s.CreateProject(ctx, store.NewProject{Name: "other", RepoPath: "/src/team"})
	if err != nil {
		t.Fatal(err)
	}
	other := member(otherRoom.ID, "Other", "/src/team")

	path := "/work/team"
	updated, err := s.UpdateProject(ctx, project.ID, store.ProjectPatch{RepoPath: &path})
	if err != nil {
		t.Fatal(err)
	}
	if updated.RepoPath != "/work/team" || updated.Name != "team" || updated.MainRoomID != room.ID {
		t.Errorf("updated: %+v", updated)
	}
	for id, want := range map[string]string{
		inCheckout: "/work/team",
		below:      "/work/team/web/app",
		// Members work under the checkout: a path outside it, or none,
		// becomes the checkout. A sibling that only shares the prefix is not
		// below it.
		elsewhere: "/work/team",
		lookalike: "/work/team",
		unset:     "/work/team",
		// Taken out of the project, and another project's member, stay put.
		gone:  "/src/team",
		other: "/src/team",
	} {
		got, err := s.GetMember(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.RepoPath != want {
			t.Errorf("%s works in %q, want %q", got.DisplayName, got.RepoPath, want)
		}
	}

	// No checkout leaves every current member to wherever its machine runs.
	none := ""
	if _, err := s.UpdateProject(ctx, project.ID, store.ProjectPatch{RepoPath: &none}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{inCheckout, below, elsewhere} {
		if got, err := s.GetMember(ctx, id); err != nil || got.RepoPath != "" {
			t.Errorf("%s: %+v, %v", id, got, err)
		}
	}
}

func TestUpdateProject_UnknownOrBlank(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	project, _, err := s.CreateProject(ctx, store.NewProject{Name: "team", RepoPath: "/src/team"})
	if err != nil {
		t.Fatal(err)
	}

	name := "anything"
	if _, err := s.UpdateProject(ctx, "00000000-0000-0000-0000-000000000000", store.ProjectPatch{Name: &name}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown project: %v", err)
	}
	blank := "  "
	path := "/elsewhere"
	if _, err := s.UpdateProject(ctx, project.ID, store.ProjectPatch{Name: &blank, RepoPath: &path}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("blank name: %v", err)
	}
	if got, err := s.GetProject(ctx, project.ID); err != nil || got.Name != "team" || got.RepoPath != "/src/team" {
		t.Errorf("a refused update changes nothing: %+v, %v", got, err)
	}
}

func TestListRoomMembers_KeepsJoinOrderAfterEdits(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	laptop, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, name := range []string{"First", "Second", "Third"} {
		a, err := s.CreateAgent(ctx, store.NewAgent{Name: name, MachineID: laptop, Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, a.ID)
	}
	project, room, err := s.CreateProject(ctx, store.NewProject{Name: "team", RepoPath: "/src/team", AgentIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	members, err := s.ListRoomMembers(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Joining in one transaction still stamps each member apart, so the
	// order they joined in never rests on how rows happen to lie on disk.
	for i := 1; i < len(members); i++ {
		if !members[i].CreatedAt.After(members[i-1].CreatedAt) {
			t.Errorf("%s joined at %v, not after %s at %v", members[i].DisplayName, members[i].CreatedAt, members[i-1].DisplayName, members[i-1].CreatedAt)
		}
	}

	// Edits rewrite rows; the list still reads in the order they joined.
	web := "/src/team/web"
	if _, err := s.UpdateMember(ctx, members[0].ID, store.MemberPatch{RepoPath: &web}); err != nil {
		t.Fatal(err)
	}
	moved := "/work/team"
	if _, err := s.UpdateProject(ctx, project.ID, store.ProjectPatch{RepoPath: &moved}); err != nil {
		t.Fatal(err)
	}
	after, err := s.ListRoomMembers(ctx, room.ID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range after {
		names = append(names, m.DisplayName)
	}
	if strings.Join(names, ",") != "First,Second,Third" {
		t.Errorf("members read %v, want the order they joined", names)
	}
}

func TestDeleteProject_TakesTheWholeChat(t *testing.T) {
	f := newApprovalFixture(t)
	ctx := context.Background()
	projectID := f.room.ProjectID

	// A chat with everything in it: a request settled on a note, an upload
	// carried by a message, a reply, and the topic root the turn filled in.
	note, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderSystem, Body: "wants to run make test"})
	if err != nil {
		t.Fatal(err)
	}
	in := f.newApproval("r1")
	in.MessageID = note.ID
	approval, err := f.s.CreateApproval(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	upload, err := f.s.CreateAttachment(ctx, store.NewAttachment{ID: "11111111-1111-4111-8111-111111111111", RoomID: f.room.ID, Filename: "shot.png", MediaType: "image/png", Size: 3, Path: "ab/shot.png"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "look", AttachmentIDs: []string{upload.ID}}); err != nil {
		t.Fatal(err)
	}
	reply, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.UpdateMessageBody(ctx, f.root.ID, "@agent go", f.turn.ID, nil); err != nil {
		t.Fatal(err)
	}
	// Another project stays whole.
	other, otherRoom, err := f.s.CreateProject(ctx, store.NewProject{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}

	// Not while its turn runs.
	if _, err := f.s.DeleteProject(ctx, projectID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("running turn: %v", err)
	}
	if _, err := f.s.GetProject(ctx, projectID); err != nil {
		t.Fatalf("a refused delete keeps the project: %v", err)
	}

	if _, err := f.s.FinishTurn(ctx, f.turn.ID, store.TurnOutcome{Status: store.TurnDone, ReplyMessageID: reply.ID}); err != nil {
		t.Fatal(err)
	}
	remains, err := f.s.DeleteProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(remains.AttachmentPaths, ",") != "ab/shot.png" || strings.Join(remains.TurnIDs, ",") != f.turn.ID {
		t.Errorf("remains: %+v", remains)
	}
	for what, err := range map[string]error{
		"project":  errOf(f.s.GetProject(ctx, projectID)),
		"room":     errOf(f.s.GetRoom(ctx, f.room.ID)),
		"member":   errOf(f.s.GetMember(ctx, f.member.ID)),
		"message":  errOf(f.s.GetMessage(ctx, reply.ID)),
		"thread":   errOf(f.s.GetThread(ctx, f.thread.ID)),
		"turn":     errOf(f.s.GetTurn(ctx, f.turn.ID)),
		"approval": errOf(f.s.GetApproval(ctx, approval.ID)),
		"upload":   errOf(f.s.GetAttachment(ctx, upload.ID)),
	} {
		if !errors.Is(err, store.ErrNotFound) {
			t.Errorf("the %s should be gone, got %v", what, err)
		}
	}
	// The agent stays, in no project now.
	if agent, err := f.s.GetAgent(ctx, f.agent.ID); err != nil || len(agent.Projects) != 0 {
		t.Errorf("agent: %+v, %v", agent, err)
	}
	if _, err := f.s.GetProject(ctx, other.ID); err != nil {
		t.Errorf("the other project: %v", err)
	}
	if _, err := f.s.GetRoom(ctx, otherRoom.ID); err != nil {
		t.Errorf("the other room: %v", err)
	}

	if _, err := f.s.DeleteProject(ctx, projectID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleting again: %v", err)
	}
}

// errOf keeps the error of a two-valued call.
func errOf[T any](_ T, err error) error {
	return err
}
