package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/store"
)

// AgentStore persists agent templates and instances.
type AgentStore interface {
	CreateAgentTemplate(ctx context.Context, t store.NewAgentTemplate) (store.AgentTemplate, error)
	UpdateAgentTemplate(ctx context.Context, id string, t store.NewAgentTemplate) (store.AgentTemplate, error)
	GetAgentTemplate(ctx context.Context, id string) (store.AgentTemplate, error)
	ListAgentTemplates(ctx context.Context) ([]store.AgentTemplate, error)
	CreateAgentInstance(ctx context.Context, in store.NewAgentInstance) (store.AgentInstance, error)
	GetAgentInstance(ctx context.Context, id string) (store.AgentInstance, error)
	ListRoomAgentInstances(ctx context.Context, roomID string) ([]store.AgentInstance, error)
}

// AgentTemplateRequest is the body of POST and PUT on agent templates.
type AgentTemplateRequest struct {
	Name             string                 `json:"name"`
	Engine           string                 `json:"engine"`
	Model            string                 `json:"model"`
	RoleCard         string                 `json:"role_card"`
	PermissionPreset store.PermissionPreset `json:"permission_preset"`
	EngineOptions    map[string]any         `json:"engine_options"`
}

// AgentTemplateResponse is the body of single-template endpoints.
type AgentTemplateResponse struct {
	Template store.AgentTemplate `json:"template"`
}

// AgentTemplatesResponse is the body of GET /api/v1/agent-templates.
type AgentTemplatesResponse struct {
	Templates []store.AgentTemplate `json:"templates"`
}

// CreateAgentRequest is the body of POST /api/v1/rooms/{id}/agents. Only
// template_id and worker_id are required.
type CreateAgentRequest struct {
	TemplateID       string                 `json:"template_id"`
	WorkerID         string                 `json:"worker_id"`
	DisplayName      string                 `json:"display_name"`
	RepoPath         string                 `json:"repo_path"`
	BranchMode       store.BranchMode       `json:"branch_mode"`
	Model            string                 `json:"model"`
	PermissionPreset store.PermissionPreset `json:"permission_preset"`
}

// AgentResponse is the body of single-agent endpoints.
type AgentResponse struct {
	Agent store.AgentInstance `json:"agent"`
}

// AgentsResponse is the body of GET /api/v1/rooms/{id}/agents.
type AgentsResponse struct {
	Agents []store.AgentInstance `json:"agents"`
}

func (h *handlers) createAgentTemplate(w http.ResponseWriter, r *http.Request) {
	in, ok := h.decodeTemplate(w, r)
	if !ok {
		return
	}
	template, err := h.deps.Agents.CreateAgentTemplate(r.Context(), in)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, AgentTemplateResponse{Template: template})
}

func (h *handlers) updateAgentTemplate(w http.ResponseWriter, r *http.Request) {
	in, ok := h.decodeTemplate(w, r)
	if !ok {
		return
	}
	template, err := h.deps.Agents.UpdateAgentTemplate(r.Context(), r.PathValue("id"), in)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, AgentTemplateResponse{Template: template})
}

// decodeTemplate reads and validates a template request, writing the 400
// itself when something is wrong.
func (h *handlers) decodeTemplate(w http.ResponseWriter, r *http.Request) (store.NewAgentTemplate, bool) {
	var req AgentTemplateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return store.NewAgentTemplate{}, false
	}
	name, err := requireName("name", req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return store.NewAgentTemplate{}, false
	}
	if !slices.Contains(engine.Names(), req.Engine) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("engine must be one of %s", strings.Join(engine.Names(), ", ")))
		return store.NewAgentTemplate{}, false
	}
	if err := validatePreset(req.PermissionPreset, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return store.NewAgentTemplate{}, false
	}
	return store.NewAgentTemplate{
		Name:             name,
		Engine:           req.Engine,
		Model:            req.Model,
		RoleCard:         req.RoleCard,
		PermissionPreset: req.PermissionPreset,
		EngineOptions:    req.EngineOptions,
	}, true
}

func (h *handlers) getAgentTemplate(w http.ResponseWriter, r *http.Request) {
	template, err := h.deps.Agents.GetAgentTemplate(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, AgentTemplateResponse{Template: template})
}

func (h *handlers) listAgentTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := h.deps.Agents.ListAgentTemplates(r.Context())
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, AgentTemplatesResponse{Templates: templates})
}

func (h *handlers) createAgent(w http.ResponseWriter, r *http.Request) {
	var req CreateAgentRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.TemplateID) == "" || strings.TrimSpace(req.WorkerID) == "" {
		writeError(w, http.StatusBadRequest, "template_id and worker_id are required")
		return
	}
	if req.BranchMode != "" && req.BranchMode != store.BranchWorktree && req.BranchMode != store.BranchShared {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("branch_mode must be %q or %q", store.BranchWorktree, store.BranchShared))
		return
	}
	if err := validatePreset(req.PermissionPreset, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	agent, err := h.deps.Agents.CreateAgentInstance(r.Context(), store.NewAgentInstance{
		RoomID:           r.PathValue("id"),
		TemplateID:       req.TemplateID,
		WorkerID:         req.WorkerID,
		DisplayName:      strings.TrimSpace(req.DisplayName),
		RepoPath:         req.RepoPath,
		BranchMode:       req.BranchMode,
		Model:            req.Model,
		PermissionPreset: req.PermissionPreset,
	})
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, AgentResponse{Agent: agent})
}

func (h *handlers) listRoomAgents(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	if _, err := h.deps.Projects.GetRoom(r.Context(), roomID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	agents, err := h.deps.Agents.ListRoomAgentInstances(r.Context(), roomID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, AgentsResponse{Agents: agents})
}

func (h *handlers) getAgent(w http.ResponseWriter, r *http.Request) {
	agent, err := h.deps.Agents.GetAgentInstance(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, AgentResponse{Agent: agent})
}

// validatePreset checks a permission preset; allowEmpty is for instance
// overrides, where empty means "inherit from the template".
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
