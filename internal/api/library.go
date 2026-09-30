package api

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
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

// UpdateSkillRequest is the body of POST /library/update.
type UpdateSkillRequest struct {
	UserID string `json:"user_id"`
	// Folder is the full path of a skill's folder on the hub's machine, the
	// one the library's skill of its name came from, say.
	Folder string `json:"folder"`
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

// RetireSkillRequest is the body of POST /library/retire: retired takes
// the skill out of use, false puts it back.
type RetireSkillRequest struct {
	UserID  string `json:"user_id"`
	Name    string `json:"name"`
	Retired bool   `json:"retired"`
}

// DeleteSkillRequest is the body of POST /library/delete.
type DeleteSkillRequest struct {
	UserID string `json:"user_id"`
	Name   string `json:"name"`
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

// updateSkill replaces the library's copy of a skill with what its folder
// on this machine holds now (docs/design.md 5.15).
func (h *handlers) updateSkill(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	var req UpdateSkillRequest
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
	page, err := wikis.UpdateSkill(r.Context(), req.Folder, userID)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiPageResponse{Page: page})
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

// retireSkill takes a skill out of use or puts it back (docs/design.md
// 5.15).
func (h *handlers) retireSkill(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	var req RetireSkillRequest
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
	page, err := wikis.RetireSkill(r.Context(), req.Name, req.Retired, userID)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiPageResponse{Page: page})
}

// deleteSkill removes a skill from the library and takes it off the agents
// it is installed for (docs/design.md 5.15).
func (h *handlers) deleteSkill(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	var req DeleteSkillRequest
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
	if err := wikis.DeleteSkill(r.Context(), req.Name, userID); err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

// exportSkill hands a skill of the library over as a zip file of its
// folder, in Agent Skills form, to be saved.
func (h *handlers) exportSkill(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	data, err := wikis.ExportSkill(r.Context(), name)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name + ".zip"}))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// LocalSkillsResponse is the body of GET /library/local.
type LocalSkillsResponse struct {
	Skills []hub.LocalSkill `json:"skills"`
}

// UploadSkillsResponse is the body of POST /library/upload: how each
// skill the upload held went.
type UploadSkillsResponse struct {
	Skills []hub.UploadedSkill `json:"skills"`
}

// localSkills lists the skills on this machine a person may bring into
// the library.
func (h *handlers) localSkills(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	skills, err := wikis.LocalSkills(r.Context())
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, LocalSkillsResponse{Skills: skills})
}

// uploadSkills takes a multipart form from the browser: one zip file in
// the file field, or a folder's files in file fields, each with its path in
// a path field of its own, in the same order (a multipart file name keeps
// no folders); project_id names the team, user_id the person.
func (h *handlers) uploadSkills(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, hub.MaxUploadBytes+formMemory)
	if err := r.ParseMultipartForm(formMemory); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeCoded(w, http.StatusRequestEntityTooLarge, "uploadTooLarge", store.Params{"mb": strconv.Itoa(hub.MaxUploadBytes >> 20), "files": strconv.Itoa(hub.MaxUploadFiles)}, "upload too large")
			return
		}
		writeError(w, http.StatusBadRequest, "expected a multipart form with file fields")
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck // temp files only
	userID, ok := deciderID(r, r.FormValue("user_id"))
	if !ok {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	files := r.MultipartForm.File["file"]
	paths := r.MultipartForm.Value["path"]
	up := hub.SkillUpload{Team: r.FormValue("project_id"), UserID: userID}
	switch {
	case len(files) == 1 && len(paths) == 0:
		f, err := files[0].Open()
		if err != nil {
			writeError(w, http.StatusBadRequest, "the file does not read")
			return
		}
		defer f.Close()
		up.Name, up.Zip, up.ZipSize = files[0].Filename, f, files[0].Size
	case len(files) > 0 && len(paths) == len(files):
		for i, header := range files {
			up.Files = append(up.Files, hub.UploadFile{Path: paths[i], Open: func() (io.ReadCloser, error) { return header.Open() }})
		}
		up.Name = strings.SplitN(paths[0], "/", 2)[0]
	default:
		writeError(w, http.StatusBadRequest, "expected one zip file, or files each with its path")
		return
	}
	skills, err := wikis.UploadSkills(r.Context(), up)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, UploadSkillsResponse{Skills: skills})
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
