package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// How a project's new worktrees are got ready, and whether it is set up
// for them (docs/design.md 5.21).
func TestProjects_WorkspaceSteps(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	project := f.room.ProjectID
	get := func() store.Project {
		t.Helper()
		p, err := f.s.GetProject(ctx, project)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := get(); p.InitializedAt != nil || len(p.WorkspaceCopy) != 0 || p.WorkspaceRun != "" || p.WorkspacePending != nil {
		t.Fatalf("a new project is not set up: %+v", p)
	}

	// The leader writes steps with a command down: they wait for a person.
	note, _ := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderSystem, Body: "steps"})
	if err := f.s.SetWorkspacePending(ctx, project, store.WorkspaceSteps{Copy: []string{".env"}, Run: "npm ci"}, note.ID); err != nil {
		t.Fatal(err)
	}
	p := get()
	if p.WorkspacePending == nil || p.WorkspacePending.Run != "npm ci" || p.WorkspacePendingMessageID != note.ID || p.InitializedAt != nil {
		t.Fatalf("waiting steps: %+v", p)
	}
	if err := f.s.AdoptWorkspacePending(ctx, project); err != nil {
		t.Fatal(err)
	}
	p = get()
	if !slices.Equal(p.WorkspaceCopy, []string{".env"}) || p.WorkspaceRun != "npm ci" || p.WorkspacePending != nil || p.InitializedAt == nil {
		t.Fatalf("adopted: %+v", p)
	}
	if err := f.s.AdoptWorkspacePending(ctx, project); !errors.Is(err, store.ErrConflict) {
		t.Errorf("adopting twice: %v", err)
	}

	// Turned down, they are dropped; the project stays set up.
	f.s.SetWorkspacePending(ctx, project, store.WorkspaceSteps{Run: "rm -rf /"}, "")
	if err := f.s.DropWorkspacePending(ctx, project); err != nil {
		t.Fatal(err)
	}
	if p := get(); p.WorkspacePending != nil || p.WorkspaceRun != "npm ci" || p.InitializedAt == nil {
		t.Errorf("dropped: %+v", p)
	}
	if err := f.s.DropWorkspacePending(ctx, project); !errors.Is(err, store.ErrConflict) {
		t.Errorf("dropping twice: %v", err)
	}

	// Written down at once, or by a person in the settings, they stand in
	// place of anything waiting.
	f.s.SetWorkspacePending(ctx, project, store.WorkspaceSteps{Run: "make"}, "")
	if err := f.s.SetWorkspaceSteps(ctx, project, store.WorkspaceSteps{Copy: []string{"docs/"}}); err != nil {
		t.Fatal(err)
	}
	if p := get(); !slices.Equal(p.WorkspaceCopy, []string{"docs/"}) || p.WorkspaceRun != "" || p.WorkspacePending != nil {
		t.Errorf("written down: %+v", p)
	}
	f.s.SetWorkspacePending(ctx, project, store.WorkspaceSteps{Run: "make"}, "")
	got, err := f.s.UpdateProject(ctx, project, store.ProjectPatch{WorkspaceSteps: &store.WorkspaceSteps{Copy: []string{".env.local"}, Run: "go mod download"}})
	if err != nil || got.WorkspaceRun != "go mod download" || !slices.Equal(got.WorkspaceCopy, []string{".env.local"}) || got.WorkspacePending != nil || got.InitializedAt == nil {
		t.Errorf("a person's steps: %+v %v", got, err)
	}

	// A project set up with no steps at all.
	other, room, _ := f.s.CreateProject(ctx, store.NewProject{Name: "bare"})
	if err := f.s.MarkProjectInitialized(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := f.s.GetProject(ctx, other.ID); p.InitializedAt == nil || len(p.WorkspaceCopy) != 0 {
		t.Errorf("set up bare: %+v", p)
	}

	// The setup topic is recorded once.
	root, _ := f.s.CreateMessage(ctx, store.NewMessage{RoomID: room.ID, SenderKind: store.SenderSystem, Body: "setup"})
	thread, _ := f.s.ThreadForMessage(ctx, root.ID)
	if ok, err := f.s.SetProjectSetupThread(ctx, other.ID, thread.ID); !ok || err != nil {
		t.Fatalf("the setup topic: %v %v", ok, err)
	}
	if ok, _ := f.s.SetProjectSetupThread(ctx, other.ID, thread.ID); ok {
		t.Error("the setup topic recorded twice")
	}
	if p, _ := f.s.GetProject(ctx, other.ID); p.SetupThreadID != thread.ID {
		t.Errorf("setup topic %q", p.SetupThreadID)
	}
}

func TestCleanWorkspaceSteps(t *testing.T) {
	got, err := store.CleanWorkspaceSteps(store.WorkspaceSteps{Copy: []string{" .env ", "", "docs/"}, Run: "  npm ci \n"})
	if err != nil || !slices.Equal(got.Copy, []string{".env", "docs/"}) || got.Run != "npm ci" {
		t.Errorf("cleaned %+v %v", got, err)
	}
	for _, bad := range []string{"/etc/passwd", "../elsewhere", "a/../../b", ".."} {
		if _, err := store.CleanWorkspaceSteps(store.WorkspaceSteps{Copy: []string{bad}}); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

// A member's worktree, once made and once ready.
func TestMembers_Workspace(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	m, err := f.s.SetMemberWorkspace(ctx, f.member.ID, "/wt/p/reviewer", "/wt/p/reviewer/web", "veyloom/reviewer")
	if err != nil || m.WorktreeDir != "/wt/p/reviewer" || m.WorkDir != "/wt/p/reviewer/web" || m.Branch != "veyloom/reviewer" || m.PreparedAt != nil {
		t.Fatalf("made: %+v %v", m, err)
	}
	if m, err = f.s.MarkMemberPrepared(ctx, f.member.ID); err != nil || m.PreparedAt == nil {
		t.Fatalf("ready: %+v %v", m, err)
	}
	if got, _ := f.s.GetMember(ctx, f.member.ID); got.PreparedAt == nil || got.WorkDir != "/wt/p/reviewer/web" || len(got.OverlapsNoted) != 0 {
		t.Errorf("read back: %+v", got)
	}

	// The overlaps the chat was told about: set in place of those there
	// were, added to without repeats.
	overlaps := func() []string {
		t.Helper()
		got, err := f.s.GetMember(ctx, f.member.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got.OverlapsNoted
	}
	if err := f.s.SetMemberOverlaps(ctx, f.member.ID, []string{"b.go", "a.go"}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.AddMemberOverlaps(ctx, f.member.ID, []string{"c.go", "a.go"}); err != nil {
		t.Fatal(err)
	}
	if got := overlaps(); !slices.Equal(got, []string{"a.go", "b.go", "c.go"}) {
		t.Errorf("added to: %v", got)
	}
	if err := f.s.SetMemberOverlaps(ctx, f.member.ID, []string{"c.go"}); err != nil {
		t.Fatal(err)
	}
	if got := overlaps(); !slices.Equal(got, []string{"c.go"}) {
		t.Errorf("set: %v", got)
	}

	if err := f.s.ClearMemberWorkspace(ctx, f.member.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.s.GetMember(ctx, f.member.ID); got.WorktreeDir != "" || got.PreparedAt != nil || len(got.OverlapsNoted) != 0 {
		t.Errorf("forgotten: %+v", got)
	}

	// A setup turn is a turn of its own kind.
	nt := f.newTurn()
	nt.Kind = store.TurnSetup
	if turn, err := f.s.CreateTurn(ctx, nt); err != nil || turn.Kind != store.TurnSetup {
		t.Errorf("a setup turn: %+v %v", turn, err)
	}
}
