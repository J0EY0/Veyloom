package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// fakeAgents is an in-memory AgentStore. Agents are "ag1", "ag2", ...;
// members "mb1", ...; it mirrors the store's uniqueness rules. working
// holds the members with a turn running.
type fakeAgents struct {
	rooms    *fakeProjects
	machines map[string]bool
	agents   map[string]store.Agent
	members  map[string]store.Member
	working  map[string]bool
	// sessions are the members' open sessions, by member; turns counts the
	// turns run in each, by session.
	sessions map[string]store.MemberSession
	turns    map[string]int
}

func newFakeAgents(rooms *fakeProjects, machineIDs ...string) *fakeAgents {
	f := &fakeAgents{rooms: rooms, machines: map[string]bool{}, agents: map[string]store.Agent{}, members: map[string]store.Member{}, working: map[string]bool{},
		sessions: map[string]store.MemberSession{}, turns: map[string]int{}}
	for _, id := range machineIDs {
		f.machines[id] = true
	}
	return f
}

func (f *fakeAgents) CreateAgent(_ context.Context, t store.NewAgent) (store.Agent, error) {
	for _, existing := range f.agents {
		if existing.Name == t.Name {
			return store.Agent{}, fmt.Errorf("name: %w", store.ErrConflict)
		}
	}
	if !f.machines[t.MachineID] {
		return store.Agent{}, fmt.Errorf("machine %s: %w", t.MachineID, store.ErrNotFound)
	}
	agent := store.Agent{ID: fmt.Sprintf("ag%d", len(f.agents)+1), Name: t.Name, Avatar: t.Avatar, MachineID: t.MachineID, MachineName: t.MachineID, Projects: []string{}, Runtime: t.Runtime, Model: t.Model, RoleCard: t.RoleCard, PermissionPreset: t.PermissionPreset, RuntimeOptions: t.RuntimeOptions, Skills: t.Skills}
	f.agents[agent.ID] = agent
	return agent, nil
}

func (f *fakeAgents) UpdateAgent(ctx context.Context, id string, t store.NewAgent) (store.Agent, error) {
	agent, err := f.GetAgent(ctx, id)
	if err != nil {
		return store.Agent{}, err
	}
	if projects := f.projectsOf(id); t.MachineID != agent.MachineID && len(projects) > 0 {
		return store.Agent{}, &store.AgentInUseError{ID: id, Projects: projects}
	}
	agent.Name, agent.Avatar, agent.MachineID, agent.Runtime, agent.Model, agent.RoleCard, agent.PermissionPreset, agent.Skills = t.Name, t.Avatar, t.MachineID, t.Runtime, t.Model, t.RoleCard, t.PermissionPreset, t.Skills
	f.agents[id] = agent
	return agent, nil
}

// projectsOf names the projects an agent is a current member of, sorted.
func (f *fakeAgents) projectsOf(id string) []string {
	var projects []string
	for _, member := range f.members {
		if member.AgentID == id && !member.Removed() {
			name := f.rooms.projects[f.rooms.rooms[member.RoomID].ProjectID].Name
			if !slices.Contains(projects, name) {
				projects = append(projects, name)
			}
		}
	}
	slices.Sort(projects)
	return projects
}

func (f *fakeAgents) GetAgent(_ context.Context, id string) (store.Agent, error) {
	if err := checkID(id); err != nil {
		return store.Agent{}, err
	}
	agent, ok := f.agents[id]
	if !ok {
		return store.Agent{}, fmt.Errorf("agent %s: %w", id, store.ErrNotFound)
	}
	return agent, nil
}

func (f *fakeAgents) ListAgents(context.Context) ([]store.Agent, error) {
	out := []store.Agent{}
	for _, t := range f.agents {
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeAgents) AgentsWithAvatar(_ context.Context, avatar string) (int64, error) {
	var n int64
	for _, agent := range f.agents {
		if agent.Avatar == avatar {
			n++
		}
	}
	return n, nil
}

func (f *fakeAgents) DeleteAgent(_ context.Context, id string) error {
	if err := checkID(id); err != nil {
		return err
	}
	if _, ok := f.agents[id]; !ok {
		return fmt.Errorf("agent %s: %w", id, store.ErrNotFound)
	}
	if projects := f.projectsOf(id); len(projects) > 0 {
		return &store.AgentInUseError{ID: id, Projects: projects}
	}
	for key, member := range f.members {
		if member.AgentID == id {
			member.AgentID = ""
			f.members[key] = member
		}
	}
	delete(f.agents, id)
	return nil
}

func (f *fakeAgents) CreateMember(ctx context.Context, in store.NewMember) (store.Member, error) {
	if _, err := f.rooms.GetRoom(ctx, in.RoomID); err != nil {
		return store.Member{}, err
	}
	agent, err := f.GetAgent(ctx, in.AgentID)
	if err != nil {
		return store.Member{}, err
	}
	if in.DisplayName == "" {
		in.DisplayName = agent.Name
	}
	for _, existing := range f.members {
		if existing.RoomID == in.RoomID && existing.DisplayName == in.DisplayName && !existing.Removed() {
			return store.Member{}, fmt.Errorf("display name: %w", store.ErrConflict)
		}
	}
	if in.BranchMode == "" {
		in.BranchMode = store.BranchWorktree
	}
	member := store.Member{ID: fmt.Sprintf("mb%d", len(f.members)+1), RoomID: in.RoomID, AgentID: in.AgentID, MachineID: agent.MachineID, DisplayName: in.DisplayName, RepoPath: in.RepoPath, BranchMode: in.BranchMode, Model: in.Model, PermissionPreset: in.PermissionPreset, Enabled: true}
	f.members[member.ID] = member
	return member, nil
}

func (f *fakeAgents) RemoveMember(_ context.Context, id string) (store.Member, error) {
	if err := checkID(id); err != nil {
		return store.Member{}, err
	}
	member, ok := f.members[id]
	if !ok || member.Removed() {
		return store.Member{}, fmt.Errorf("member %s: %w", id, store.ErrNotFound)
	}
	if f.working[id] {
		return store.Member{}, fmt.Errorf("member %s: %w: a turn is still running", id, store.ErrConflict)
	}
	now := time.Now()
	member.RemovedAt = &now
	f.members[id] = member
	return member, nil
}

func (f *fakeAgents) ListMachineMembers(_ context.Context, machineID string) ([]store.MachineMember, error) {
	if err := checkID(machineID); err != nil {
		return nil, err
	}
	out := []store.MachineMember{}
	for _, member := range f.members {
		if member.MachineID == machineID && !member.Removed() {
			room := f.rooms.rooms[member.RoomID]
			out = append(out, store.MachineMember{Member: member, ProjectID: room.ProjectID, ProjectName: f.rooms.projects[room.ProjectID].Name})
		}
	}
	slices.SortFunc(out, func(a, b store.MachineMember) int {
		return strings.Compare(a.ProjectName+a.Member.ID, b.ProjectName+b.Member.ID)
	})
	return out, nil
}

func (f *fakeAgents) UpdateMember(_ context.Context, id string, patch store.MemberPatch) (store.Member, error) {
	member, ok := f.members[id]
	if !ok || member.Removed() {
		return store.Member{}, fmt.Errorf("member %s: %w", id, store.ErrNotFound)
	}
	if patch.DisplayName != nil {
		member.DisplayName = *patch.DisplayName
	}
	if patch.Model != nil {
		member.Model = *patch.Model
	}
	if patch.PermissionPreset != nil {
		member.PermissionPreset = *patch.PermissionPreset
	}
	if patch.RepoPath != nil {
		member.RepoPath = *patch.RepoPath
	}
	if patch.Enabled != nil {
		member.Enabled = *patch.Enabled
	}
	f.members[id] = member
	return member, nil
}

func (f *fakeAgents) GetMember(_ context.Context, id string) (store.Member, error) {
	if err := checkID(id); err != nil {
		return store.Member{}, err
	}
	member, ok := f.members[id]
	if !ok {
		return store.Member{}, fmt.Errorf("member %s: %w", id, store.ErrNotFound)
	}
	return member, nil
}

func (f *fakeAgents) ListRoomMembers(_ context.Context, roomID string) ([]store.Member, error) {
	out := []store.Member{}
	for _, member := range f.members {
		if member.RoomID == roomID {
			out = append(out, member)
		}
	}
	return out, nil
}

// agentsHandler wires the agent fakes with one project (and main room) and
// one known machine "w1".
func agentsHandler(t *testing.T) (http.Handler, store.Room, *fakeAgents) {
	t.Helper()
	projects := newFakeProjects()
	_, room, _ := projects.CreateProject(context.Background(), store.NewProject{Name: "p"})
	agents := newFakeAgents(projects, "w1")
	return NewHandler(Deps{Projects: projects, Agents: agents}), room, agents
}

const validAgent = `{"name":"Reviewer","machine_id":"w1","runtime":"claude","permission_preset":"read_only","role_card":"Review.","runtime_options":{"max_turns":3}}`

func TestAgents_CRUD(t *testing.T) {
	handler, _, _ := agentsHandler(t)

	var created AgentResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/agents", validAgent, &created); rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d; body: %s", rec.Code, rec.Body)
	}
	if created.Agent.Runtime != "claude" || created.Agent.MachineID != "w1" || created.Agent.RuntimeOptions["max_turns"] != float64(3) {
		t.Errorf("unexpected agent: %+v", created.Agent)
	}

	var got AgentResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/agents/"+created.Agent.ID, "", &got); rec.Code != http.StatusOK || got.Agent.Name != "Reviewer" {
		t.Errorf("get: status = %d, agent = %+v", rec.Code, got.Agent)
	}

	var updated AgentResponse
	if rec := do(t, handler, http.MethodPut, "/api/v1/agents/"+created.Agent.ID, `{"name":"Reviewer 2","machine_id":"w1","runtime":"codex","permission_preset":"full_auto"}`, &updated); rec.Code != http.StatusOK || updated.Agent.Runtime != "codex" {
		t.Errorf("update: status = %d, agent = %+v", rec.Code, updated.Agent)
	}
	// The preset that hands decisions to the runtime's own reviewer.
	if rec := do(t, handler, http.MethodPut, "/api/v1/agents/"+created.Agent.ID, `{"name":"Reviewer 2","machine_id":"w1","runtime":"codex","permission_preset":"auto_review"}`, &updated); rec.Code != http.StatusOK || updated.Agent.PermissionPreset != store.PermissionAutoReview {
		t.Errorf("auto_review: status = %d, agent = %+v", rec.Code, updated.Agent)
	}

	var list AgentsResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/agents", "", &list); rec.Code != http.StatusOK || len(list.Agents) != 1 {
		t.Errorf("list: status = %d, agents = %+v", rec.Code, list.Agents)
	}

	if rec := do(t, handler, http.MethodPost, "/api/v1/agents", `{"name":"Reviewer 2","machine_id":"w1","runtime":"pi","permission_preset":"read_only"}`, nil); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name: status = %d, want 409", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/agents", `{"name":"Nowhere","machine_id":"w404","runtime":"pi","permission_preset":"read_only"}`, nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown machine: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodPut, "/api/v1/agents/ag404", validAgent, nil); rec.Code != http.StatusNotFound {
		t.Errorf("update unknown: status = %d, want 404", rec.Code)
	}
}

// An agent lives on one machine: while that machine is connected its runtime
// must be one the machine found installed, and the agent moves to another
// machine only while it is in no project.
func TestAgents_Machine(t *testing.T) {
	projects := newFakeProjects()
	_, room, _ := projects.CreateProject(context.Background(), store.NewProject{Name: "p"})
	agents := newFakeAgents(projects, "w1", "w2")
	laptop := hub.MachineInfo{ID: "w1", Name: "laptop", Runtimes: []runtime.Info{
		{Name: "claude", Status: runtime.StatusReady},
		{Name: "codex", Status: runtime.StatusNotInstalled},
	}}
	handler := NewHandler(Deps{Projects: projects, Agents: agents, Machines: stubMachines{laptop}})
	post := func(body string) (*httptest.ResponseRecorder, AgentResponse) {
		t.Helper()
		var out AgentResponse
		return do(t, handler, http.MethodPost, "/api/v1/agents", body, &out), out
	}

	if rec, _ := post(`{"name":"Coder","machine_id":"w1","runtime":"codex","permission_preset":"read_only"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not installed on laptop") {
		t.Errorf("runtime the machine lacks: status = %d, body %s", rec.Code, rec.Body)
	}
	rec, reviewer := post(`{"name":"Reviewer","machine_id":"w1","runtime":"claude","permission_preset":"read_only"}`)
	if rec.Code != http.StatusCreated || reviewer.Agent.MachineID != "w1" {
		t.Fatalf("runtime the machine has: status = %d, body %s", rec.Code, rec.Body)
	}
	// A machine that is not connected cannot be asked what it has.
	if rec, _ := post(`{"name":"Tester","machine_id":"w2","runtime":"pi","permission_preset":"read_only"}`); rec.Code != http.StatusCreated {
		t.Errorf("machine not connected: status = %d, body %s", rec.Code, rec.Body)
	}

	// In a project it stays on its machine, and the answer says where it is.
	if _, err := agents.CreateMember(context.Background(), store.NewMember{RoomID: room.ID, AgentID: reviewer.Agent.ID}); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/agents/" + reviewer.Agent.ID
	rec = do(t, handler, http.MethodPut, path, `{"name":"Reviewer","machine_id":"w2","runtime":"claude","permission_preset":"read_only"}`, nil)
	var inUse AgentInUseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &inUse); rec.Code != http.StatusConflict || err != nil || !slices.Equal(inUse.Projects, []string{"p"}) {
		t.Errorf("move while in a project: status = %d, body %s; want 409 naming p", rec.Code, rec.Body)
	}
	if rec := do(t, handler, http.MethodPut, path, `{"name":"Reviewer","machine_id":"w1","runtime":"claude","role_card":"Twice.","permission_preset":"read_only"}`, nil); rec.Code != http.StatusOK {
		t.Errorf("edit in place: status = %d, body %s", rec.Code, rec.Body)
	}
}

func TestAgents_Delete(t *testing.T) {
	handler, room, agents := agentsHandler(t)
	ctx := context.Background()
	unused, _ := agents.CreateAgent(ctx, store.NewAgent{Name: "Unused", MachineID: "w1", Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	used, _ := agents.CreateAgent(ctx, store.NewAgent{Name: "Used", MachineID: "w1", Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
	if _, err := agents.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: used.ID}); err != nil {
		t.Fatal(err)
	}

	rec := do(t, handler, http.MethodDelete, "/api/v1/agents/"+unused.ID, "", nil)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("delete: status = %d, body %q; want 204 and nothing", rec.Code, rec.Body)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/agents/"+unused.ID, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodDelete, "/api/v1/agents/"+unused.ID, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("delete twice: status = %d, want 404", rec.Code)
	}
	// While it is a member of a project it stays, and the answer says where.
	rec = do(t, handler, http.MethodDelete, "/api/v1/agents/"+used.ID, "", nil)
	var inUse AgentInUseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &inUse); rec.Code != http.StatusConflict || err != nil || !slices.Equal(inUse.Projects, []string{"p"}) {
		t.Errorf("delete in use: status = %d, body %s; want 409 naming project p", rec.Code, rec.Body)
	}
	if _, err := agents.GetAgent(ctx, used.ID); err != nil {
		t.Errorf("agent in use is gone: %v", err)
	}
}

func TestMembers_Remove(t *testing.T) {
	handler, room, agents := agentsHandler(t)
	ctx := context.Background()
	agent, _ := agents.CreateAgent(ctx, store.NewAgent{Name: "Reviewer", MachineID: "w1", Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	idle, _ := agents.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: agent.ID})
	busy, _ := agents.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: agent.ID, DisplayName: "Busy"})
	agents.working[busy.ID] = true

	if rec := do(t, handler, http.MethodDelete, "/api/v1/members/"+busy.ID, "", nil); rec.Code != http.StatusConflict {
		t.Errorf("remove while working: status = %d, want 409", rec.Code)
	}
	rec := do(t, handler, http.MethodDelete, "/api/v1/members/"+idle.ID, "", nil)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Errorf("remove: status = %d, body %q; want 204 and nothing", rec.Code, rec.Body)
	}

	// The room still lists it, marked, so its history keeps a name.
	var list MembersResponse
	do(t, handler, http.MethodGet, "/api/v1/rooms/"+room.ID+"/members", "", &list)
	for _, a := range list.Members {
		if (a.ID == idle.ID) != a.Removed() {
			t.Errorf("listed %s with removed_at %v", a.DisplayName, a.RemovedAt)
		}
	}
	if rec := do(t, handler, http.MethodDelete, "/api/v1/members/"+idle.ID, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("remove twice: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodPatch, "/api/v1/members/"+idle.ID, `{"enabled":true}`, nil); rec.Code != http.StatusNotFound {
		t.Errorf("patch after removal: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodDelete, "/api/v1/members/mb404", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("remove unknown: status = %d, want 404", rec.Code)
	}
}

func TestAgents_BadRequests(t *testing.T) {
	handler, _, _ := agentsHandler(t)

	cases := map[string]string{
		"blank name":      `{"name":" ","machine_id":"w1","runtime":"claude","permission_preset":"read_only"}`,
		"unknown runtime": `{"name":"x","machine_id":"w1","runtime":"gpt","permission_preset":"read_only"}`,
		"bad preset":      `{"name":"x","machine_id":"w1","runtime":"claude","permission_preset":"yolo"}`,
		"empty preset":    `{"name":"x","machine_id":"w1","runtime":"claude"}`,
		"no machine":      `{"name":"x","runtime":"claude","permission_preset":"read_only"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := do(t, handler, http.MethodPost, "/api/v1/agents", body, nil); rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestMembers_CreateListGet(t *testing.T) {
	handler, room, agents := agentsHandler(t)
	agent, _ := agents.CreateAgent(context.Background(), store.NewAgent{Name: "Reviewer", MachineID: "w1", Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	path := "/api/v1/rooms/" + room.ID + "/members"

	var created MemberResponse
	if rec := do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"agent_id":%q,"repo_path":" /src/p/ "}`, agent.ID), &created); rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d; body: %s", rec.Code, rec.Body)
	}
	// It runs on the machine its agent is set up on.
	if created.Member.DisplayName != "Reviewer" || created.Member.MachineID != "w1" || created.Member.BranchMode != store.BranchWorktree || !created.Member.Enabled {
		t.Errorf("unexpected member: %+v", created.Member)
	}

	var second MemberResponse
	if rec := do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"agent_id":%q,"display_name":" Reviewer B ","branch_mode":"shared","permission_preset":"full_auto"}`, agent.ID), &second); rec.Code != http.StatusCreated || second.Member.DisplayName != "Reviewer B" || second.Member.PermissionPreset != store.PermissionFullAuto {
		t.Errorf("create with overrides: status = %d, member = %+v", rec.Code, second.Member)
	}

	var list MembersResponse
	if rec := do(t, handler, http.MethodGet, path, "", &list); rec.Code != http.StatusOK || len(list.Members) != 2 {
		t.Errorf("list: status = %d, members = %+v", rec.Code, list.Members)
	}
	var got MemberResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/members/"+created.Member.ID, "", &got); rec.Code != http.StatusOK || got.Member.RepoPath != "/src/p" {
		t.Errorf("get: status = %d, member = %+v", rec.Code, got.Member)
	}
}

func TestMembers_Errors(t *testing.T) {
	handler, room, agents := agentsHandler(t)
	agent, _ := agents.CreateAgent(context.Background(), store.NewAgent{Name: "Reviewer", MachineID: "w1", Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	path := "/api/v1/rooms/" + room.ID + "/members"
	do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"agent_id":%q}`, agent.ID), nil)

	cases := map[string]struct {
		path, body string
		want       int
	}{
		"duplicate display name": {path, fmt.Sprintf(`{"agent_id":%q}`, agent.ID), http.StatusConflict},
		"missing agent":          {path, `{"display_name":"x"}`, http.StatusBadRequest},
		"bad branch mode":        {path, fmt.Sprintf(`{"agent_id":%q,"display_name":"x","branch_mode":"detached"}`, agent.ID), http.StatusBadRequest},
		"bad preset override":    {path, fmt.Sprintf(`{"agent_id":%q,"display_name":"x","permission_preset":"yolo"}`, agent.ID), http.StatusBadRequest},
		"unknown agent":          {path, `{"agent_id":"ag404"}`, http.StatusNotFound},
		"unknown room":           {"/api/v1/rooms/r404/members", fmt.Sprintf(`{"agent_id":%q}`, agent.ID), http.StatusNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := do(t, handler, http.MethodPost, tc.path, tc.body, nil); rec.Code != tc.want {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/r404/members", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("list unknown room: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/members/mb404", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("get unknown agent: status = %d, want 404", rec.Code)
	}
}

func TestMembers_Patch(t *testing.T) {
	handler, room, agents := agentsHandler(t)
	agent, _ := agents.CreateAgent(context.Background(), store.NewAgent{Name: "Reviewer", MachineID: "w1", Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	var created MemberResponse
	do(t, handler, http.MethodPost, "/api/v1/rooms/"+room.ID+"/members", fmt.Sprintf(`{"agent_id":%q}`, agent.ID), &created)
	path := "/api/v1/members/" + created.Member.ID

	var got MemberResponse
	if rec := do(t, handler, http.MethodPatch, path, `{"display_name":" Lead Reviewer ","enabled":false}`, &got); rec.Code != http.StatusOK {
		t.Fatalf("patch: status = %d; body: %s", rec.Code, rec.Body)
	}
	// The preset override stays empty: the member still follows its agent.
	if got.Member.DisplayName != "Lead Reviewer" || got.Member.Enabled || got.Member.PermissionPreset != created.Member.PermissionPreset {
		t.Errorf("patch should change only what it names, got %+v", got.Member)
	}
	if rec := do(t, handler, http.MethodPatch, path, `{"permission_preset":"full_auto","model":"opus","repo_path":"/src/q/"}`, &got); rec.Code != http.StatusOK || got.Member.PermissionPreset != store.PermissionFullAuto || got.Member.Model != "opus" || got.Member.RepoPath != "/src/q" || got.Member.Enabled {
		t.Errorf("second patch: status = %d, member = %+v", rec.Code, got.Member)
	}

	cases := map[string]struct {
		path, body string
		want       int
	}{
		"blank name":     {path, `{"display_name":"  "}`, http.StatusBadRequest},
		"bad preset":     {path, `{"permission_preset":"yolo"}`, http.StatusBadRequest},
		"bad json":       {path, `{`, http.StatusBadRequest},
		"unknown member": {"/api/v1/members/mb404", `{"enabled":true}`, http.StatusNotFound},
	}
	for name, c := range cases {
		if rec := do(t, handler, http.MethodPatch, c.path, c.body, nil); rec.Code != c.want {
			t.Errorf("%s: status = %d, want %d; body: %s", name, rec.Code, c.want, rec.Body)
		}
	}
}

func (f *fakeAgents) GetOpenSession(_ context.Context, memberID string) (store.MemberSession, error) {
	session, ok := f.sessions[memberID]
	if !ok {
		return store.MemberSession{}, fmt.Errorf("open session of member %s: %w", memberID, store.ErrNotFound)
	}
	return session, nil
}

func (f *fakeAgents) SessionTurns(_ context.Context, sessionID string) (int, error) {
	return f.turns[sessionID], nil
}

func (f *fakeAgents) ResetSession(_ context.Context, memberID string) error {
	if f.working[memberID] {
		return fmt.Errorf("member %s: %w: a turn is still running", memberID, store.ErrConflict)
	}
	delete(f.sessions, memberID)
	return nil
}

func TestMembers_Session(t *testing.T) {
	handler, room, agents := agentsHandler(t)
	ctx := context.Background()
	agent, _ := agents.CreateAgent(ctx, store.NewAgent{Name: "Reviewer", MachineID: "w1", Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	member, _ := agents.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: agent.ID})
	path := "/api/v1/members/" + member.ID + "/session"

	// Before its first turn a member has no session, and that is no error.
	var none MemberSessionResponse
	if rec := do(t, handler, http.MethodGet, path, "", &none); rec.Code != http.StatusOK || none.Session != nil || none.Turns != 0 {
		t.Errorf("no session yet: status %d, body %+v", rec.Code, none)
	}

	agents.sessions[member.ID] = store.MemberSession{ID: "s1", MemberID: member.ID, Runtime: "claude", Compactions: 2}
	agents.turns["s1"] = 53
	var got MemberSessionResponse
	do(t, handler, http.MethodGet, path, "", &got)
	if got.Session == nil || got.Session.ID != "s1" || got.Session.Compactions != 2 || got.Turns != 53 {
		t.Errorf("session = %+v", got)
	}

	// A new one on request, but not from under a running turn.
	agents.working[member.ID] = true
	if rec := do(t, handler, http.MethodDelete, path, "", nil); rec.Code != http.StatusConflict {
		t.Errorf("reset while working: status %d, want 409", rec.Code)
	}
	agents.working[member.ID] = false
	if rec := do(t, handler, http.MethodDelete, path, "", nil); rec.Code != http.StatusNoContent {
		t.Errorf("reset: status %d, want 204", rec.Code)
	}
	do(t, handler, http.MethodGet, path, "", &none)
	if none.Session != nil {
		t.Errorf("after the reset the member has no open session: %+v", none)
	}
	// Asking again is harmless: there is nothing left to end.
	if rec := do(t, handler, http.MethodDelete, path, "", nil); rec.Code != http.StatusNoContent {
		t.Errorf("reset twice: status %d, want 204", rec.Code)
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		if rec := do(t, handler, method, "/api/v1/members/mb404/session", "", nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s for an unknown member: status %d, want 404", method, rec.Code)
		}
	}
}

// What an edit keeps is not checked again: a runtime taken off the machine
// since, or an avatar file gone, stands in the way only of choosing it.
func TestAgents_EditsCheckWhatTheyChange(t *testing.T) {
	agents := newFakeAgents(newFakeProjects(), "w1")
	laptop := hub.MachineInfo{ID: "w1", Name: "laptop", Runtimes: []runtime.Info{{Name: "claude", Status: runtime.StatusReady}}}
	var created AgentResponse
	if rec := do(t, NewHandler(Deps{Agents: agents, Machines: stubMachines{laptop}}), http.MethodPost, "/api/v1/agents",
		`{"name":"Reviewer","machine_id":"w1","runtime":"claude","permission_preset":"read_only"}`, &created); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	// Claude Code is uninstalled from the laptop afterwards.
	laptop.Runtimes = []runtime.Info{{Name: "claude", Status: runtime.StatusNotInstalled}}
	handler := NewHandler(Deps{Agents: agents, Machines: stubMachines{laptop}})
	path := "/api/v1/agents/" + created.Agent.ID
	if rec := do(t, handler, http.MethodPut, path, `{"name":"Reviewer 2","machine_id":"w1","runtime":"claude","permission_preset":"read_only"}`, nil); rec.Code != http.StatusOK {
		t.Errorf("a rename keeping the runtime: %d %s", rec.Code, rec.Body)
	}
	rec := do(t, handler, http.MethodPut, path, `{"name":"Reviewer 2","machine_id":"w1","runtime":"codex","permission_preset":"read_only"}`, nil)
	var refused ErrorResponse
	if json.Unmarshal(rec.Body.Bytes(), &refused); rec.Code != http.StatusBadRequest || refused.Code != "runtimeMissing" || refused.Params["runtime"] != "codex" || refused.Params["machine"] != "laptop" {
		t.Errorf("choosing a runtime the machine lacks: %d %s", rec.Code, rec.Body)
	}
	rec = do(t, handler, http.MethodPut, path, `{"name":"Reviewer 2","machine_id":"w1","runtime":"claude","permission_preset":"read_only","avatar":"0123456789abcdef0123456789abcdef.png"}`, nil)
	if json.Unmarshal(rec.Body.Bytes(), &refused); rec.Code != http.StatusBadRequest || refused.Code != "avatarMissing" {
		t.Errorf("choosing an avatar never uploaded: %d %s", rec.Code, rec.Body)
	}
}

func TestAgentSkills(t *testing.T) {
	projects := newFakeProjects()
	agents := newFakeAgents(projects, "m1")
	wikis := &fakeWikis{}
	handler := NewHandler(Deps{Projects: projects, Agents: agents, Wikis: wikis})
	var created AgentResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/agents", `{"name":"Coder","machine_id":"m1","runtime":"codex","permission_preset":"read_only","skills":["go"," go ",""]}`, &created); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if !slices.Equal(created.Agent.Skills, []string{"go"}) {
		t.Errorf("installed once each: %v", created.Agent.Skills)
	}
	id := created.Agent.ID
	// Absent, the agent keeps its skills; a retired one it has stays.
	var updated AgentResponse
	do(t, handler, http.MethodPut, "/api/v1/agents/"+id, `{"name":"Coder","machine_id":"m1","runtime":"codex","permission_preset":"read_only"}`, &updated)
	if !slices.Equal(updated.Agent.Skills, []string{"go"}) {
		t.Errorf("kept when absent: %v", updated.Agent.Skills)
	}
	wikis.retired = []string{"go"}
	do(t, handler, http.MethodPut, "/api/v1/agents/"+id, `{"name":"Coder","machine_id":"m1","runtime":"codex","permission_preset":"read_only","skills":["go","docs"]}`, &updated)
	if !slices.Equal(updated.Agent.Skills, []string{"go", "docs"}) {
		t.Errorf("a retired skill it had stays: %v", updated.Agent.Skills)
	}
	// One newly installed must be in the library.
	rec := do(t, handler, http.MethodPut, "/api/v1/agents/"+id, `{"name":"Coder","machine_id":"m1","runtime":"codex","permission_preset":"read_only","skills":["nope"]}`, nil)
	var refused ErrorResponse
	if json.Unmarshal(rec.Body.Bytes(), &refused); rec.Code != http.StatusBadRequest || refused.Code != "skillUnknown" || refused.Params["name"] != "nope" {
		t.Errorf("an unknown skill: %d %s", rec.Code, rec.Body)
	}
	do(t, handler, http.MethodPut, "/api/v1/agents/"+id, `{"name":"Coder","machine_id":"m1","runtime":"codex","permission_preset":"read_only","skills":[]}`, &updated)
	if len(updated.Agent.Skills) != 0 {
		t.Errorf("all taken off: %v", updated.Agent.Skills)
	}
}
