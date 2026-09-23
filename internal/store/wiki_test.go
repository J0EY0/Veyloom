package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

func TestCreateProject_GivesEachAWikiFolder(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	for _, tc := range []struct{ name, want string }{
		{"Veyloom", "veyloom"},
		{"Veyloom", "veyloom-2"},
		{"  My Great App (v2)!  ", "my-great-app-v2"},
		{"测试项目", "project"},
		{"测试 Project", "project-2"},
	} {
		p, _, err := s.CreateProject(ctx, store.NewProject{Name: tc.name})
		if err != nil {
			t.Fatal(err)
		}
		if p.WikiSlug != tc.want {
			t.Errorf("%q: wiki folder %q, want %q", tc.name, p.WikiSlug, tc.want)
		}
		got, err := s.GetProject(ctx, p.ID)
		if err != nil || got.WikiSlug != tc.want {
			t.Errorf("read back %q: %+v %v", tc.name, got, err)
		}
	}
	long, _, err := s.CreateProject(ctx, store.NewProject{Name: "a very long project name that goes on and on past forty characters"})
	if err != nil || long.WikiSlug != "a-very-long-project-name-that-goes-on-an" {
		t.Errorf("long name: %q %v", long.WikiSlug, err)
	}
}

func TestUpdateProject_KeepsTheWikiFolder(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	p, _, err := s.CreateProject(ctx, store.NewProject{Name: "Veyloom"})
	if err != nil {
		t.Fatal(err)
	}
	name := "Renamed"
	if updated, err := s.UpdateProject(ctx, p.ID, store.ProjectPatch{Name: &name}); err != nil || updated.Name != name || updated.WikiSlug != p.WikiSlug {
		t.Errorf("a rename leaves the folder: %+v %v", updated, err)
	}
}

func TestSkillUses(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	used, err := f.s.CreateTurn(ctx, f.newTurn())
	if err != nil {
		t.Fatal(err)
	}
	finished, err := f.s.FinishTurn(ctx, used.ID, store.TurnOutcome{Status: store.TurnDone, SkillsUsed: []string{"go-table-tests", "release-notes"}})
	if err != nil || len(finished.SkillsUsed) != 2 {
		t.Fatalf("finished %+v %v", finished, err)
	}
	other, _ := f.s.CreateTurn(ctx, f.newTurn())
	f.s.FinishTurn(ctx, other.ID, store.TurnOutcome{Status: store.TurnFailed, Error: "boom", SkillsUsed: []string{"go-table-tests"}})
	quiet, _ := f.s.CreateTurn(ctx, f.newTurn())
	f.s.FinishTurn(ctx, quiet.ID, store.TurnOutcome{Status: store.TurnDone})

	uses, err := f.s.ListSkillUses(ctx, "go-table-tests", 10)
	if err != nil || len(uses) != 2 {
		t.Fatalf("uses %+v %v", uses, err)
	}
	if u := uses[0]; u.TurnID != other.ID || u.Status != store.TurnFailed || u.ThreadID != f.thread.ID || u.TopicNumber != f.thread.Number ||
		u.MemberName != f.member.DisplayName || u.ProjectID != f.room.ProjectID || u.ProjectName == "" || u.EndedAt == nil {
		t.Errorf("the newest use %+v", u)
	}
	if uses, _ := f.s.ListSkillUses(ctx, "unused", 10); len(uses) != 0 {
		t.Errorf("a skill nobody used: %+v", uses)
	}
}

func TestProjectWikiTopicAndSlug(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	project, err := f.s.GetProject(ctx, f.room.ProjectID)
	if err != nil || project.WikiThreadID != "" {
		t.Fatalf("a new project has no wiki topic: %+v %v", project, err)
	}
	bySlug, err := f.s.GetProjectBySlug(ctx, project.WikiSlug)
	if err != nil || bySlug.ID != project.ID || bySlug.MainRoomID == "" {
		t.Errorf("by slug %+v %v", bySlug, err)
	}
	if _, err := f.s.GetProjectBySlug(ctx, "nobody"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("an unknown slug: %v", err)
	}
	set, err := f.s.SetProjectWikiThread(ctx, project.ID, f.thread.ID)
	if err != nil || !set {
		t.Fatalf("set %v %v", set, err)
	}
	// The first topic stays.
	other, _ := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderSystem, Body: "wiki"})
	otherThread, _ := f.s.ThreadForMessage(ctx, other.ID)
	if set, err := f.s.SetProjectWikiThread(ctx, project.ID, otherThread.ID); err != nil || set {
		t.Errorf("a second topic: %v %v", set, err)
	}
	if again, _ := f.s.GetProject(ctx, project.ID); again.WikiThreadID != f.thread.ID {
		t.Errorf("wiki topic %q, want %q", again.WikiThreadID, f.thread.ID)
	}
}

func TestProjects_WikiExternalBundles(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	if fresh, _ := f.s.GetProject(ctx, f.room.ProjectID); fresh.WikiExternalBundles == nil || len(fresh.WikiExternalBundles) != 0 {
		t.Fatalf("a new project mounts nothing: %#v", fresh.WikiExternalBundles)
	}
	mounts := []string{"/data/catalog", "/data/runbooks"}
	got, err := f.s.UpdateProject(ctx, f.room.ProjectID, store.ProjectPatch{WikiExternalBundles: &mounts})
	if err != nil || !slices.Equal(got.WikiExternalBundles, mounts) {
		t.Fatalf("mounted %#v %v", got.WikiExternalBundles, err)
	}
	name := "renamed"
	if got, _ := f.s.UpdateProject(ctx, f.room.ProjectID, store.ProjectPatch{Name: &name}); !slices.Equal(got.WikiExternalBundles, mounts) {
		t.Errorf("a rename keeps the mounts: %#v", got.WikiExternalBundles)
	}
	none := []string{}
	if got, _ := f.s.UpdateProject(ctx, f.room.ProjectID, store.ProjectPatch{WikiExternalBundles: &none}); len(got.WikiExternalBundles) != 0 {
		t.Errorf("unmounted %#v", got.WikiExternalBundles)
	}
}
