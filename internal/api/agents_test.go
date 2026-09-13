package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// fakeAgents is an in-memory AgentStore. Templates are "at1", "at2", ...;
// instances "ai1", ...; it mirrors the store's uniqueness rules.
type fakeAgents struct {
	rooms     *fakeProjects
	workers   map[string]bool
	templates map[string]store.AgentTemplate
	instances map[string]store.AgentInstance
}

func newFakeAgents(rooms *fakeProjects, workerIDs ...string) *fakeAgents {
	f := &fakeAgents{rooms: rooms, workers: map[string]bool{}, templates: map[string]store.AgentTemplate{}, instances: map[string]store.AgentInstance{}}
	for _, id := range workerIDs {
		f.workers[id] = true
	}
	return f
}

func (f *fakeAgents) CreateAgentTemplate(_ context.Context, t store.NewAgentTemplate) (store.AgentTemplate, error) {
	for _, existing := range f.templates {
		if existing.Name == t.Name {
			return store.AgentTemplate{}, fmt.Errorf("name: %w", store.ErrConflict)
		}
	}
	tpl := store.AgentTemplate{ID: fmt.Sprintf("at%d", len(f.templates)+1), Name: t.Name, Engine: t.Engine, Model: t.Model, RoleCard: t.RoleCard, PermissionPreset: t.PermissionPreset, EngineOptions: t.EngineOptions}
	f.templates[tpl.ID] = tpl
	return tpl, nil
}

func (f *fakeAgents) UpdateAgentTemplate(ctx context.Context, id string, t store.NewAgentTemplate) (store.AgentTemplate, error) {
	tpl, err := f.GetAgentTemplate(ctx, id)
	if err != nil {
		return store.AgentTemplate{}, err
	}
	tpl.Name, tpl.Engine, tpl.Model, tpl.RoleCard, tpl.PermissionPreset = t.Name, t.Engine, t.Model, t.RoleCard, t.PermissionPreset
	f.templates[id] = tpl
	return tpl, nil
}

func (f *fakeAgents) GetAgentTemplate(_ context.Context, id string) (store.AgentTemplate, error) {
	if err := checkID(id); err != nil {
		return store.AgentTemplate{}, err
	}
	tpl, ok := f.templates[id]
	if !ok {
		return store.AgentTemplate{}, fmt.Errorf("agent template %s: %w", id, store.ErrNotFound)
	}
	return tpl, nil
}

func (f *fakeAgents) ListAgentTemplates(context.Context) ([]store.AgentTemplate, error) {
	out := []store.AgentTemplate{}
	for _, t := range f.templates {
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeAgents) CreateAgentInstance(ctx context.Context, in store.NewAgentInstance) (store.AgentInstance, error) {
	if _, err := f.rooms.GetRoom(ctx, in.RoomID); err != nil {
		return store.AgentInstance{}, err
	}
	tpl, err := f.GetAgentTemplate(ctx, in.TemplateID)
	if err != nil {
		return store.AgentInstance{}, err
	}
	if !f.workers[in.WorkerID] {
		return store.AgentInstance{}, fmt.Errorf("worker %s: %w", in.WorkerID, store.ErrNotFound)
	}
	if in.DisplayName == "" {
		in.DisplayName = tpl.Name
	}
	for _, existing := range f.instances {
		if existing.RoomID == in.RoomID && existing.DisplayName == in.DisplayName {
			return store.AgentInstance{}, fmt.Errorf("display name: %w", store.ErrConflict)
		}
	}
	if in.BranchMode == "" {
		in.BranchMode = store.BranchWorktree
	}
	inst := store.AgentInstance{ID: fmt.Sprintf("ai%d", len(f.instances)+1), RoomID: in.RoomID, TemplateID: in.TemplateID, WorkerID: in.WorkerID, DisplayName: in.DisplayName, RepoPath: in.RepoPath, BranchMode: in.BranchMode, Model: in.Model, PermissionPreset: in.PermissionPreset, Enabled: true}
	f.instances[inst.ID] = inst
	return inst, nil
}

func (f *fakeAgents) GetAgentInstance(_ context.Context, id string) (store.AgentInstance, error) {
	if err := checkID(id); err != nil {
		return store.AgentInstance{}, err
	}
	inst, ok := f.instances[id]
	if !ok {
		return store.AgentInstance{}, fmt.Errorf("agent instance %s: %w", id, store.ErrNotFound)
	}
	return inst, nil
}

func (f *fakeAgents) ListRoomAgentInstances(_ context.Context, roomID string) ([]store.AgentInstance, error) {
	out := []store.AgentInstance{}
	for _, inst := range f.instances {
		if inst.RoomID == roomID {
			out = append(out, inst)
		}
	}
	return out, nil
}

// agentsHandler wires the agent fakes with one project (and main room) and
// one known worker "w1".
func agentsHandler(t *testing.T) (http.Handler, store.Room, *fakeAgents) {
	t.Helper()
	projects := newFakeProjects()
	_, room, _ := projects.CreateProject(context.Background(), store.NewProject{Name: "p"})
	agents := newFakeAgents(projects, "w1")
	return NewHandler(Deps{Projects: projects, Agents: agents}), room, agents
}

const validTemplate = `{"name":"Reviewer","engine":"claude","permission_preset":"read_only","role_card":"Review.","engine_options":{"max_turns":3}}`

func TestAgentTemplates_CRUD(t *testing.T) {
	handler, _, _ := agentsHandler(t)

	var created AgentTemplateResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/agent-templates", validTemplate, &created); rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d; body: %s", rec.Code, rec.Body)
	}
	if created.Template.Engine != "claude" || created.Template.EngineOptions["max_turns"] != float64(3) {
		t.Errorf("unexpected template: %+v", created.Template)
	}

	var got AgentTemplateResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/agent-templates/"+created.Template.ID, "", &got); rec.Code != http.StatusOK || got.Template.Name != "Reviewer" {
		t.Errorf("get: status = %d, template = %+v", rec.Code, got.Template)
	}

	var updated AgentTemplateResponse
	if rec := do(t, handler, http.MethodPut, "/api/v1/agent-templates/"+created.Template.ID, `{"name":"Reviewer 2","engine":"codex","permission_preset":"full_auto"}`, &updated); rec.Code != http.StatusOK || updated.Template.Engine != "codex" {
		t.Errorf("update: status = %d, template = %+v", rec.Code, updated.Template)
	}

	var list AgentTemplatesResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/agent-templates", "", &list); rec.Code != http.StatusOK || len(list.Templates) != 1 {
		t.Errorf("list: status = %d, templates = %+v", rec.Code, list.Templates)
	}

	if rec := do(t, handler, http.MethodPost, "/api/v1/agent-templates", `{"name":"Reviewer 2","engine":"pi","permission_preset":"read_only"}`, nil); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name: status = %d, want 409", rec.Code)
	}
	if rec := do(t, handler, http.MethodPut, "/api/v1/agent-templates/at404", validTemplate, nil); rec.Code != http.StatusNotFound {
		t.Errorf("update unknown: status = %d, want 404", rec.Code)
	}
}

func TestAgentTemplates_BadRequests(t *testing.T) {
	handler, _, _ := agentsHandler(t)

	cases := map[string]string{
		"blank name":     `{"name":" ","engine":"claude","permission_preset":"read_only"}`,
		"unknown engine": `{"name":"x","engine":"gpt","permission_preset":"read_only"}`,
		"bad preset":     `{"name":"x","engine":"claude","permission_preset":"yolo"}`,
		"empty preset":   `{"name":"x","engine":"claude"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := do(t, handler, http.MethodPost, "/api/v1/agent-templates", body, nil); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestAgents_CreateListGet(t *testing.T) {
	handler, room, agents := agentsHandler(t)
	tpl, _ := agents.CreateAgentTemplate(context.Background(), store.NewAgentTemplate{Name: "Reviewer", Engine: "claude", PermissionPreset: store.PermissionReadOnly})
	path := "/api/v1/rooms/" + room.ID + "/agents"

	var created AgentResponse
	if rec := do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"template_id":%q,"worker_id":"w1","repo_path":"/src/p"}`, tpl.ID), &created); rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d; body: %s", rec.Code, rec.Body)
	}
	if created.Agent.DisplayName != "Reviewer" || created.Agent.BranchMode != store.BranchWorktree || !created.Agent.Enabled {
		t.Errorf("unexpected agent: %+v", created.Agent)
	}

	var second AgentResponse
	if rec := do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"template_id":%q,"worker_id":"w1","display_name":" Reviewer B ","branch_mode":"shared","permission_preset":"full_auto"}`, tpl.ID), &second); rec.Code != http.StatusCreated || second.Agent.DisplayName != "Reviewer B" || second.Agent.PermissionPreset != store.PermissionFullAuto {
		t.Errorf("create with overrides: status = %d, agent = %+v", rec.Code, second.Agent)
	}

	var list AgentsResponse
	if rec := do(t, handler, http.MethodGet, path, "", &list); rec.Code != http.StatusOK || len(list.Agents) != 2 {
		t.Errorf("list: status = %d, agents = %+v", rec.Code, list.Agents)
	}
	var got AgentResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/agents/"+created.Agent.ID, "", &got); rec.Code != http.StatusOK || got.Agent.RepoPath != "/src/p" {
		t.Errorf("get: status = %d, agent = %+v", rec.Code, got.Agent)
	}
}

func TestAgents_Errors(t *testing.T) {
	handler, room, agents := agentsHandler(t)
	tpl, _ := agents.CreateAgentTemplate(context.Background(), store.NewAgentTemplate{Name: "Reviewer", Engine: "claude", PermissionPreset: store.PermissionReadOnly})
	path := "/api/v1/rooms/" + room.ID + "/agents"
	do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"template_id":%q,"worker_id":"w1"}`, tpl.ID), nil)

	cases := map[string]struct {
		path, body string
		want       int
	}{
		"duplicate display name": {path, fmt.Sprintf(`{"template_id":%q,"worker_id":"w1"}`, tpl.ID), http.StatusConflict},
		"missing worker":         {path, fmt.Sprintf(`{"template_id":%q}`, tpl.ID), http.StatusBadRequest},
		"bad branch mode":        {path, fmt.Sprintf(`{"template_id":%q,"worker_id":"w1","display_name":"x","branch_mode":"detached"}`, tpl.ID), http.StatusBadRequest},
		"bad preset override":    {path, fmt.Sprintf(`{"template_id":%q,"worker_id":"w1","display_name":"x","permission_preset":"yolo"}`, tpl.ID), http.StatusBadRequest},
		"unknown template":       {path, `{"template_id":"at404","worker_id":"w1"}`, http.StatusNotFound},
		"unknown worker":         {path, fmt.Sprintf(`{"template_id":%q,"worker_id":"w404","display_name":"y"}`, tpl.ID), http.StatusNotFound},
		"unknown room":           {"/api/v1/rooms/r404/agents", fmt.Sprintf(`{"template_id":%q,"worker_id":"w1"}`, tpl.ID), http.StatusNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := do(t, handler, http.MethodPost, tc.path, tc.body, nil); rec.Code != tc.want {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/r404/agents", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("list unknown room: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/agents/ai404", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("get unknown agent: status = %d, want 404", rec.Code)
	}
}
