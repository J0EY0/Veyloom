package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// AgentStore persists agents and their members.
type AgentStore interface {
	CreateAgent(ctx context.Context, t store.NewAgent) (store.Agent, error)
	UpdateAgent(ctx context.Context, id string, t store.NewAgent) (store.Agent, error)
	GetAgent(ctx context.Context, id string) (store.Agent, error)
	ListAgents(ctx context.Context) ([]store.Agent, error)
	DeleteAgent(ctx context.Context, id string) error
	AgentsWithAvatar(ctx context.Context, avatar string) (int64, error)
	CreateMember(ctx context.Context, in store.NewMember) (store.Member, error)
	GetMember(ctx context.Context, id string) (store.Member, error)
	ListRoomMembers(ctx context.Context, roomID string) ([]store.Member, error)
	UpdateMember(ctx context.Context, id string, patch store.MemberPatch) (store.Member, error)
	RemoveMember(ctx context.Context, id string) (store.Member, error)
	ListMachineMembers(ctx context.Context, machineID string) ([]store.MachineMember, error)
	GetOpenSession(ctx context.Context, memberID string) (store.MemberSession, error)
	SessionTurns(ctx context.Context, sessionID string) (int, error)
	ResetSession(ctx context.Context, memberID string) error
}

// MemberSessionResponse is the body of GET /api/v1/members/{id}/session:
// the member's conversation with its runtime, as far as a person cares.
// Session is absent when the member has none open: before its first turn,
// or after someone asked for a new one.
type MemberSessionResponse struct {
	Session *store.MemberSession `json:"session,omitempty"`
	// Turns is how many turns have run in it.
	Turns int `json:"turns"`
}

// UpdateMemberRequest is the body of PATCH /api/v1/members/{id}. Every field
// is optional; an absent one is left as it is.
type UpdateMemberRequest struct {
	DisplayName      *string                 `json:"display_name"`
	Model            *string                 `json:"model"`
	PermissionPreset *store.PermissionPreset `json:"permission_preset"`
	RepoPath         *string                 `json:"repo_path"`
	Enabled          *bool                   `json:"enabled"`
}

// AgentRequest is the body of POST and PUT on agents.
// MachineID is the machine the agent is set up on; while that machine is
// connected, Runtime must be one it found installed.
type AgentRequest struct {
	Name string `json:"name"`
	// Avatar is a name POST /api/v1/avatars answered with; empty shows the
	// runtime's mark.
	Avatar           string                 `json:"avatar"`
	MachineID        string                 `json:"machine_id"`
	Runtime          string                 `json:"runtime"`
	Model            string                 `json:"model"`
	RoleCard         string                 `json:"role_card"`
	PermissionPreset store.PermissionPreset `json:"permission_preset"`
	RuntimeOptions   map[string]any         `json:"runtime_options"`
	// Skills names the skills of the library installed for the agent; absent
	// keeps those it has.
	Skills *[]string `json:"skills"`
}

// AgentResponse is the body of single-agent endpoints.
type AgentResponse struct {
	Agent store.Agent `json:"agent"`
}

// AgentInUseResponse is the 409 from DELETE /api/v1/agents/{id},
// and from PUT when it would move the agent to another machine, while the
// agent is still a member of some project: Projects names where to take
// it out first.
type AgentInUseResponse struct {
	Error    string   `json:"error"`
	Projects []string `json:"projects"`
}

// AgentsResponse is the body of GET /api/v1/agents.
type AgentsResponse struct {
	Agents []store.Agent `json:"agents"`
}

// CreateMemberRequest is the body of POST /api/v1/rooms/{id}/members. Only
// agent_id is required; the member runs on its agent's machine.
type CreateMemberRequest struct {
	AgentID          string                 `json:"agent_id"`
	DisplayName      string                 `json:"display_name"`
	RepoPath         string                 `json:"repo_path"`
	BranchMode       store.BranchMode       `json:"branch_mode"`
	Model            string                 `json:"model"`
	PermissionPreset store.PermissionPreset `json:"permission_preset"`
}

// MemberResponse is the body of single-member endpoints.
type MemberResponse struct {
	Member store.Member `json:"member"`
}

// MembersResponse is the body of GET /api/v1/rooms/{id}/members.
type MembersResponse struct {
	Members []store.Member `json:"members"`
}

func (h *handlers) createAgent(w http.ResponseWriter, r *http.Request) {
	in, ok := h.decodeAgent(w, r, nil)
	if !ok {
		return
	}
	agent, err := h.deps.Agents.CreateAgent(r.Context(), in)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, AgentResponse{Agent: agent})
}

func (h *handlers) updateAgent(w http.ResponseWriter, r *http.Request) {
	// The avatar it had goes once nothing shows it any more.
	before, err := h.deps.Agents.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	in, ok := h.decodeAgent(w, r, &before)
	if !ok {
		return
	}
	agent, err := h.deps.Agents.UpdateAgent(r.Context(), r.PathValue("id"), in)
	var inUse *store.AgentInUseError
	if errors.As(err, &inUse) {
		writeJSON(w, http.StatusConflict, AgentInUseResponse{Error: store.Reason(err), Projects: inUse.Projects})
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if before.Avatar != agent.Avatar {
		h.dropAvatar(r.Context(), before.Avatar)
	}
	writeJSON(w, http.StatusOK, AgentResponse{Agent: agent})
}

// decodeAgent reads and validates an agent request, writing the 400
// itself when something is wrong. For an agent there already (before),
// what it keeps is not checked again: a runtime taken off its machine
// since, or an avatar file gone, does not stand in the way of other edits.
func (h *handlers) decodeAgent(w http.ResponseWriter, r *http.Request, before *store.Agent) (store.NewAgent, bool) {
	var req AgentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return store.NewAgent{}, false
	}
	name, err := requireName("name", req.Name)
	if err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return store.NewAgent{}, false
	}
	if !slices.Contains(runtime.Names(), req.Runtime) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("runtime must be one of %s", strings.Join(runtime.Names(), ", ")))
		return store.NewAgent{}, false
	}
	machineID := strings.TrimSpace(req.MachineID)
	if machineID == "" {
		writeError(w, http.StatusBadRequest, "machine_id is required")
		return store.NewAgent{}, false
	}
	// A connected machine says what it has; one that is not connected
	// cannot be asked, so any known runtime is let through for it.
	kept := before != nil && before.Runtime == req.Runtime && before.MachineID == machineID
	if info, ok := h.connectedMachine(machineID); ok && !kept && !installed(info, req.Runtime) {
		writeCoded(w, http.StatusBadRequest, "runtimeMissing", store.Params{"runtime": req.Runtime, "machine": info.Name},
			fmt.Sprintf("runtime %s is not installed on %s", req.Runtime, info.Name))
		return store.NewAgent{}, false
	}
	if err := validatePreset(req.PermissionPreset, false); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return store.NewAgent{}, false
	}
	avatar := strings.TrimSpace(req.Avatar)
	if avatar != "" && (before == nil || before.Avatar != avatar) && !h.avatarExists(avatar) {
		writeCoded(w, http.StatusBadRequest, "avatarMissing", nil, fmt.Sprintf("avatar %q was not uploaded", avatar))
		return store.NewAgent{}, false
	}
	skills, ok := h.agentSkills(w, r, req.Skills, before)
	if !ok {
		return store.NewAgent{}, false
	}
	return store.NewAgent{
		Name:             name,
		Avatar:           avatar,
		MachineID:        machineID,
		Runtime:          req.Runtime,
		Model:            req.Model,
		RoleCard:         req.RoleCard,
		PermissionPreset: req.PermissionPreset,
		RuntimeOptions:   req.RuntimeOptions,
		Skills:           skills,
	}, true
}

// agentSkills reads the skills an agent request installs, each once. Only
// one newly installed must be a current skill of the library: one the
// agent has already stays until someone takes it off, retired or not.
func (h *handlers) agentSkills(w http.ResponseWriter, r *http.Request, asked *[]string, before *store.Agent) ([]string, bool) {
	var had []string
	if before != nil {
		had = before.Skills
	}
	if asked == nil {
		return had, true
	}
	var skills, added []string
	for _, name := range *asked {
		name = strings.TrimSpace(name)
		if name == "" || slices.Contains(skills, name) {
			continue
		}
		skills = append(skills, name)
		if !slices.Contains(had, name) {
			added = append(added, name)
		}
	}
	if h.deps.Wikis == nil || len(added) == 0 {
		return skills, true
	}
	if err := h.deps.Wikis.CheckSkills(r.Context(), added); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeReason(w, http.StatusBadRequest, err)
		} else {
			h.writeWikiError(w, r, err)
		}
		return nil, false
	}
	return skills, true
}

func (h *handlers) getAgent(w http.ResponseWriter, r *http.Request) {
	agent, err := h.deps.Agents.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, AgentResponse{Agent: agent})
}

func (h *handlers) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := h.deps.Agents.ListAgents(r.Context())
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, AgentsResponse{Agents: agents})
}

// deleteAgent answers 204, or 409 naming the projects the agent
// is still a member of.
func (h *handlers) deleteAgent(w http.ResponseWriter, r *http.Request) {
	before, _ := h.deps.Agents.GetAgent(r.Context(), r.PathValue("id"))
	err := h.deps.Agents.DeleteAgent(r.Context(), r.PathValue("id"))
	var inUse *store.AgentInUseError
	if errors.As(err, &inUse) {
		writeJSON(w, http.StatusConflict, AgentInUseResponse{Error: store.Reason(err), Projects: inUse.Projects})
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.dropAvatar(r.Context(), before.Avatar)
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) createMember(w http.ResponseWriter, r *http.Request) {
	var req CreateMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.AgentID) == "" {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	if req.BranchMode != "" && req.BranchMode != store.BranchWorktree && req.BranchMode != store.BranchShared {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("branch_mode must be %q or %q", store.BranchWorktree, store.BranchShared))
		return
	}
	if err := validatePreset(req.PermissionPreset, true); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}

	member, err := h.deps.Agents.CreateMember(r.Context(), store.NewMember{
		RoomID:           r.PathValue("id"),
		AgentID:          req.AgentID,
		DisplayName:      strings.TrimSpace(req.DisplayName),
		RepoPath:         cleanRepoPath(req.RepoPath),
		BranchMode:       req.BranchMode,
		Model:            req.Model,
		PermissionPreset: req.PermissionPreset,
	})
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, MemberResponse{Member: member})
}

func (h *handlers) listRoomMembers(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	if _, err := h.deps.Projects.GetRoom(r.Context(), roomID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	members, err := h.deps.Agents.ListRoomMembers(r.Context(), roomID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MembersResponse{Members: members})
}

func (h *handlers) updateMember(w http.ResponseWriter, r *http.Request) {
	var req UpdateMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	patch := store.MemberPatch{Model: req.Model, Enabled: req.Enabled}
	if req.RepoPath != nil {
		path := cleanRepoPath(*req.RepoPath)
		patch.RepoPath = &path
	}
	if req.DisplayName != nil {
		name, err := requireName("display_name", *req.DisplayName)
		if err != nil {
			writeReason(w, http.StatusBadRequest, err)
			return
		}
		patch.DisplayName = &name
	}
	if req.PermissionPreset != nil {
		if err := validatePreset(*req.PermissionPreset, true); err != nil {
			writeReason(w, http.StatusBadRequest, err)
			return
		}
		patch.PermissionPreset = req.PermissionPreset
	}
	member, err := h.deps.Agents.UpdateMember(r.Context(), r.PathValue("id"), patch)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MemberResponse{Member: member})
}

// removeMember takes a member out of its project: 204, 404 when it is
// unknown or already out, 409 while one of its turns is running.
func (h *handlers) removeMember(w http.ResponseWriter, r *http.Request) {
	if _, err := h.deps.Agents.RemoveMember(r.Context(), r.PathValue("id")); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getMemberSession tells what session the member's next turn continues.
func (h *handlers) getMemberSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.deps.Agents.GetMember(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	session, err := h.deps.Agents.GetOpenSession(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, MemberSessionResponse{})
		return
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	turns, err := h.deps.Agents.SessionTurns(r.Context(), session.ID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MemberSessionResponse{Session: &session, Turns: turns})
}

// resetMemberSession ends the member's session at a person's request, so
// its next turn starts a new one: 204, 404 for an unknown member, 409 while
// one of its turns is running. Sessions renew themselves when they have to;
// this is the way out for when one has gone wrong all the same.
func (h *handlers) resetMemberSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.deps.Agents.GetMember(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := h.deps.Agents.ResetSession(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) getMember(w http.ResponseWriter, r *http.Request) {
	member, err := h.deps.Agents.GetMember(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MemberResponse{Member: member})
}

// validatePreset checks a permission preset; allowEmpty is for member
// overrides, where empty means "inherit from the agent".
func validatePreset(p store.PermissionPreset, allowEmpty bool) error {
	if p == "" && allowEmpty {
		return nil
	}
	if slices.Contains(store.PermissionPresets, p) {
		return nil
	}
	names := make([]string, len(store.PermissionPresets))
	for i, preset := range store.PermissionPresets {
		names[i] = string(preset)
	}
	return fmt.Errorf("permission_preset must be one of %s", strings.Join(names, ", "))
}

// connectedMachine finds a machine among those connected to the hub.
func (h *handlers) connectedMachine(id string) (hub.MachineInfo, bool) {
	if h.deps.Machines == nil {
		return hub.MachineInfo{}, false
	}
	for _, info := range h.deps.Machines.Machines() {
		if info.ID == id {
			return info, true
		}
	}
	return hub.MachineInfo{}, false
}

// installed reports whether a machine found a runtime installed.
func installed(info hub.MachineInfo, name string) bool {
	return slices.ContainsFunc(info.Runtimes, func(e runtime.Info) bool {
		return e.Name == name && e.Status != runtime.StatusNotInstalled
	})
}
