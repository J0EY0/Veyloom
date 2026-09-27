package api

import (
	"net/http"
	"strings"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
)

// The skill library over HTTP (docs/design.md 5.10): the same reads as a
// project's wiki, at /library, and what only skills have: the team that
// owns one, and the turns that used it.

// TransferSkillRequest is the body of POST /library/transfer.
type TransferSkillRequest struct {
	UserID string `json:"user_id"`
	// Name is the skill's; ProjectID the project whose team takes it over.
	Name      string `json:"name"`
	ProjectID string `json:"project_id"`
}

// ImportSkillRequest is the body of POST /library/import.
type ImportSkillRequest struct {
	UserID string `json:"user_id"`
	// Folder is the full path of a skill's folder on the hub's machine, the
	// one holding its SKILL.md.
	Folder string `json:"folder"`
	// ProjectID is the project whose team looks after the skill; empty
	// leaves it to no team.
	ProjectID string `json:"project_id"`
}

// InstallSkillRequest is the body of POST /library/install.
type InstallSkillRequest struct {
	Name    string `json:"name"`
	AgentID string `json:"agent_id"`
	// Installed false takes the skill off the agent.
	Installed bool `json:"installed"`
}

// RollbackSkillRequest is the body of POST /library/rollback: the skill
// on trial goes back to the version before the agent's change.
type RollbackSkillRequest struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
	// Reason is why, for the library's log; it may be empty.
	Reason string `json:"reason"`
}

// SkillInstallsResponse answers POST /library/install: the agents the
// skill is installed for now, by name.
type SkillInstallsResponse struct {
	Installed []store.AgentRef `json:"installed"`
}

// SkillUsesResponse is the body of GET /library/usage, newest first.
type SkillUsesResponse struct {
	Uses []store.SkillUse `json:"uses"`
}

func (h *handlers) libraryCatalog(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	catalog, err := wikis.LibraryCatalog(r.Context())
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiCatalogResponse{Wiki: catalog})
}

func (h *handlers) libraryPage(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	page, err := wikis.LibraryPage(r.Context(), path)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiPageResponse{Page: page})
}

func (h *handlers) searchLibrary(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	limit, ok := h.listLimit(w, r)
	if !ok {
		return
	}
	hits, err := wikis.SearchLibrary(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiSearchResponse{Hits: hits})
}

func (h *handlers) libraryHistory(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	limit, ok := h.listLimit(w, r)
	if !ok {
		return
	}
	commits, err := wikis.LibraryHistory(r.Context(), r.URL.Query().Get("path"), limit)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiHistoryResponse{Commits: commits})
}

func (h *handlers) revertLibrary(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	var req RevertWikiRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	userID, ok := deciderID(r, req.UserID)
	if !ok {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if strings.TrimSpace(req.SHA) == "" {
		writeError(w, http.StatusBadRequest, "sha is required")
		return
	}
	undo, err := wikis.RevertLibrary(r.Context(), req.SHA, userID, req.Reason)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, RevertWikiResponse{Commit: undo})
}

func (h *handlers) verifyLibraryPage(w http.ResponseWriter, r *http.Request) {
	h.personChangesPage(w, r, func(wikis Wikis, userID string, req WikiPageRequest) (hub.WikiPageView, error) {
		return wikis.VerifyLibraryPage(r.Context(), req.Path, userID)
	})
}

func (h *handlers) transferSkill(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	var req TransferSkillRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	userID, ok := deciderID(r, req.UserID)
	if !ok {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.Name == "" || req.ProjectID == "" {
		writeError(w, http.StatusBadRequest, "name and project_id are required")
		return
	}
	page, err := wikis.TransferSkill(r.Context(), req.Name, req.ProjectID, userID)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiPageResponse{Page: page})
}

// BuiltinSkillsResponse is the body of GET /api/v1/skills/builtin:
// Veyloom's own skills, which every agent has, apart from those installed
// from the library (docs/design.md 5.23.6).
type BuiltinSkillsResponse struct {
	Skills []hub.BuiltinSkill `json:"skills"`
}

func (h *handlers) builtinSkills(w http.ResponseWriter, _ *http.Request) {
	skills := []hub.BuiltinSkill{}
	if h.deps.Wikis != nil {
		skills = append(skills, h.deps.Wikis.BuiltinSkills()...)
	}
	writeJSON(w, http.StatusOK, BuiltinSkillsResponse{Skills: skills})
}

func (h *handlers) importSkill(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	var req ImportSkillRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	userID, ok := deciderID(r, req.UserID)
	if !ok {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if strings.TrimSpace(req.Folder) == "" {
		writeError(w, http.StatusBadRequest, "folder is required")
		return
	}
	page, err := wikis.ImportSkill(r.Context(), req.Folder, req.ProjectID, userID)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, WikiPageResponse{Page: page})
}

func (h *handlers) installSkill(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	var req InstallSkillRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	if req.Name == "" || req.AgentID == "" {
		writeError(w, http.StatusBadRequest, "name and agent_id are required")
		return
	}
	installed, err := wikis.InstallSkill(r.Context(), req.Name, req.AgentID, req.Installed)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, SkillInstallsResponse{Installed: installed})
}

func (h *handlers) rollbackSkill(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	var req RollbackSkillRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	userID, ok := deciderID(r, req.UserID)
	if !ok {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	page, err := wikis.RollbackSkill(r.Context(), req.Name, userID, req.Reason)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiPageResponse{Page: page})
}

func (h *handlers) skillUses(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	limit, ok := h.listLimit(w, r)
	if !ok {
		return
	}
	uses, err := wikis.SkillUses(r.Context(), name, limit)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, SkillUsesResponse{Uses: uses})
}

// listLimit reads ?limit=, at most and by default a page's worth.
func (h *handlers) listLimit(w http.ResponseWriter, r *http.Request) (int, bool) {
	limit, _, err := queryInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "limit: "+err.Error())
		return 0, false
	}
	if limit == 0 || limit > store.MaxPageLimit {
		limit = store.MaxPageLimit
	}
	return int(limit), true
}
