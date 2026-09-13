package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

// agentFixture is a project with its main room, a registered worker and one
// user-defined template.
type agentFixture struct {
	s        *store.Store
	room     store.Room
	workerID string
	template store.AgentTemplate
}

func newAgentFixture(t *testing.T) agentFixture {
	t.Helper()
	s := storetest.New(t)
	ctx := context.Background()
	_, room, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	workerID, err := s.RegisterWorker(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	template, err := s.CreateAgentTemplate(ctx, store.NewAgentTemplate{
		Name:             "Reviewer",
		Engine:           "claude",
		Model:            "claude-opus-5",
		RoleCard:         "Review carefully.",
		PermissionPreset: store.PermissionReadOnly,
		EngineOptions:    map[string]any{"max_turns": float64(3)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return agentFixture{s: s, room: room, workerID: workerID, template: template}
}

func TestAgentTemplate_CreateGetUpdateList(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()

	got, err := f.s.GetAgentTemplate(ctx, f.template.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Reviewer" || got.Engine != "claude" || got.PermissionPreset != store.PermissionReadOnly || got.Builtin {
		t.Errorf("unexpected template: %+v", got)
	}
	if got.EngineOptions["max_turns"] != float64(3) {
		t.Errorf("engine options not round-tripped: %v", got.EngineOptions)
	}

	updated, err := f.s.UpdateAgentTemplate(ctx, f.template.ID, store.NewAgentTemplate{
		Name: "Reviewer v2", Engine: "codex", PermissionPreset: store.PermissionFullAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Reviewer v2" || updated.Engine != "codex" || !updated.UpdatedAt.After(got.UpdatedAt) {
		t.Errorf("update not applied: %+v", updated)
	}
	if updated.EngineOptions == nil || len(updated.EngineOptions) != 0 {
		t.Errorf("nil options should be stored as {}, got %#v", updated.EngineOptions)
	}

	list, err := f.s.ListAgentTemplates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != f.template.ID {
		t.Errorf("unexpected list: %+v", list)
	}

	_, err = f.s.UpdateAgentTemplate(ctx, "00000000-0000-0000-0000-000000000000", store.NewAgentTemplate{Name: "x", Engine: "pi", PermissionPreset: store.PermissionReadOnly})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("update unknown: got %v, want ErrNotFound", err)
	}
}

func TestAgentTemplate_Errors(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()

	_, err := f.s.CreateAgentTemplate(ctx, store.NewAgentTemplate{Name: "Reviewer", Engine: "pi", PermissionPreset: store.PermissionReadOnly})
	if !errors.Is(err, store.ErrConflict) {
		t.Errorf("duplicate name: got %v, want ErrConflict", err)
	}
	_, err = f.s.CreateAgentTemplate(ctx, store.NewAgentTemplate{Name: "Bad", Engine: "pi", PermissionPreset: "yolo"})
	if !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("bad preset: got %v, want ErrInvalidInput", err)
	}
	_, err = f.s.CreateAgentTemplate(ctx, store.NewAgentTemplate{Name: " ", Engine: "pi", PermissionPreset: store.PermissionReadOnly})
	if !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("blank name: got %v, want ErrInvalidInput", err)
	}
}

func TestEnsureBuiltinAgentTemplates_SeedsOnceAndKeepsEdits(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	if err := s.EnsureBuiltinAgentTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListAgentTemplates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := len(store.BuiltinAgentTemplates())
	if len(list) != want {
		t.Fatalf("got %d templates, want %d builtins", len(list), want)
	}
	for _, tpl := range list {
		if !tpl.Builtin {
			t.Errorf("%s should be marked builtin", tpl.Name)
		}
	}

	// A user edit to a builtin must survive the next seeding.
	edited, err := s.UpdateAgentTemplate(ctx, list[0].ID, store.NewAgentTemplate{
		Name: list[0].Name, Engine: list[0].Engine, RoleCard: "tuned", PermissionPreset: store.PermissionFullAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureBuiltinAgentTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetAgentTemplate(ctx, edited.ID)
	if again.RoleCard != "tuned" {
		t.Errorf("seeding overwrote a user edit: %+v", again)
	}
	if list, _ := s.ListAgentTemplates(ctx); len(list) != want {
		t.Errorf("seeding twice created duplicates: %d templates", len(list))
	}
}

func TestAgentInstance_CreateGetList(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()

	inst, err := f.s.CreateAgentInstance(ctx, store.NewAgentInstance{
		RoomID: f.room.ID, TemplateID: f.template.ID, WorkerID: f.workerID, RepoPath: "/src/p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst.DisplayName != "Reviewer" {
		t.Errorf("display name should default to the template's, got %q", inst.DisplayName)
	}
	if inst.BranchMode != store.BranchWorktree || !inst.Enabled || inst.PermissionPreset != "" || inst.Model != "" {
		t.Errorf("unexpected defaults: %+v", inst)
	}

	second, err := f.s.CreateAgentInstance(ctx, store.NewAgentInstance{
		RoomID: f.room.ID, TemplateID: f.template.ID, WorkerID: f.workerID,
		DisplayName: "Reviewer B", BranchMode: store.BranchShared, Model: "haiku", PermissionPreset: store.PermissionFullAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.BranchMode != store.BranchShared || second.Model != "haiku" || second.PermissionPreset != store.PermissionFullAuto {
		t.Errorf("overrides not stored: %+v", second)
	}

	got, err := f.s.GetAgentInstance(ctx, inst.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RepoPath != "/src/p" || got.TemplateID != f.template.ID || got.WorkerID != f.workerID {
		t.Errorf("unexpected instance: %+v", got)
	}

	list, err := f.s.ListRoomAgentInstances(ctx, f.room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != inst.ID || list[1].ID != second.ID {
		t.Errorf("unexpected list: %+v", list)
	}
}

func TestAgentInstance_Errors(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()
	unknown := "00000000-0000-0000-0000-000000000000"
	base := store.NewAgentInstance{RoomID: f.room.ID, TemplateID: f.template.ID, WorkerID: f.workerID}

	if _, err := f.s.CreateAgentInstance(ctx, base); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateAgentInstance(ctx, base); !errors.Is(err, store.ErrConflict) {
		t.Errorf("duplicate display name in room: got %v, want ErrConflict", err)
	}

	tests := map[string]store.NewAgentInstance{
		"unknown room":     {RoomID: unknown, TemplateID: f.template.ID, WorkerID: f.workerID},
		"unknown template": {RoomID: f.room.ID, TemplateID: unknown, WorkerID: f.workerID, DisplayName: "x"},
		"unknown worker":   {RoomID: f.room.ID, TemplateID: f.template.ID, WorkerID: unknown, DisplayName: "y"},
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := f.s.CreateAgentInstance(ctx, in); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("got %v, want ErrNotFound", err)
			}
		})
	}
	bad := base
	bad.DisplayName, bad.BranchMode = "z", "detached"
	if _, err := f.s.CreateAgentInstance(ctx, bad); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("bad branch mode: got %v, want ErrInvalidInput", err)
	}
}

func TestMessages_AgentSender(t *testing.T) {
	f := newAgentFixture(t)
	ctx := context.Background()
	inst, err := f.s.CreateAgentInstance(ctx, store.NewAgentInstance{RoomID: f.room.ID, TemplateID: f.template.ID, WorkerID: f.workerID})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := f.s.CreateMessage(ctx, store.NewMessage{
		RoomID: f.room.ID, SenderKind: store.SenderAgent, AgentInstanceID: inst.ID, Body: "done",
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.AgentInstanceID != inst.ID || msg.UserID != "" || msg.SenderKind != store.SenderAgent {
		t.Errorf("unexpected message: %+v", msg)
	}

	_, err = f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderAgent, Body: "no sender"})
	if !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("agent kind without instance: got %v, want ErrInvalidInput", err)
	}
	_, err = f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderAgent, AgentInstanceID: "00000000-0000-0000-0000-000000000000", Body: "ghost"})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown instance: got %v, want ErrNotFound", err)
	}
}
