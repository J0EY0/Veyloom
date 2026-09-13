package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

func TestCreateProject_CreatesMainRoomAtomically(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	project, room, err := s.CreateProject(ctx, store.NewProject{Name: "veyloom", RepoURL: "git@example.com:v.git"})
	if err != nil {
		t.Fatal(err)
	}
	if project.ID == "" || project.Name != "veyloom" || project.DefaultBranch != "main" {
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

	created, _, err := s.CreateProject(ctx, store.NewProject{Name: "p", DefaultBranch: "develop"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := s.GetProject(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "p" || got.DefaultBranch != "develop" {
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
	// There is no DeleteProject yet; this guards the schema's ON DELETE
	// CASCADE so that adding one later cannot strand rooms.
	s := storetest.New(t)
	ctx := context.Background()

	project, room, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if err := storetest.Exec(t, s, "DELETE FROM projects WHERE id = $1", project.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := s.GetRoom(ctx, room.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("room should be gone with its project, got %v", err)
	}
}
