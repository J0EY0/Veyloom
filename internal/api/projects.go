package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
)

// CreateProjectRequest is the body of POST /api/v1/projects. AgentIDs join
// the project's chat as its first members, working in RepoPath. The web
// client asks for at least one; scripts and tests may start empty.
type CreateProjectRequest struct {
	Name     string `json:"name"`
	RepoPath string `json:"repo_path"`
	// Description says what the project is; every agent's brief opens
	// with it. Optional.
	Description string   `json:"description"`
	AgentIDs    []string `json:"agent_ids"`
	// WikiMaintainerAgentID, one of AgentIDs, has that agent's member keep
	// the wiki from the start (docs/design.md 5.16), running as
	// WikiMaintainerTrigger, daily when empty. Optional: without one the
	// chat offers a maintainer later.
	WikiMaintainerAgentID string              `json:"wiki_maintainer_agent_id"`
	WikiMaintainerTrigger store.UpkeepTrigger `json:"wiki_maintainer_trigger"`
}

// maxProjectDescription bounds the description: it goes into every brief,
// so it is a paragraph, not a document.
const maxProjectDescription = 4000

// UpdateProjectRequest is the body of PATCH /api/v1/projects/{id}; an
// absent field keeps its value. Moving RepoPath moves the project's current
// members with it, since they work under the checkout.
type UpdateProjectRequest struct {
	Name        *string `json:"name"`
	RepoPath    *string `json:"repo_path"`
	Description *string `json:"description"`
	// WikiMaintainerMemberID is the member who keeps the wiki (docs/design.md
	// 5.12), one of the project's; "" is none.
	WikiMaintainerMemberID *string `json:"wiki_maintainer_member_id"`
	// WikiMaintainerTrigger is when it runs: idle, daily, every_3_days,
	// weekly or manual.
	WikiMaintainerTrigger *store.UpkeepTrigger `json:"wiki_maintainer_trigger"`
	// WikiOfferDeclined, true, records that a person said no to the wiki
	// maintainer the chat offered: it is not offered again.
	WikiOfferDeclined *bool `json:"wiki_offer_declined"`
	// WikiExternalBundles are the folders of the OKF bundles the wiki
	// mounts, read-only, all of them: an empty list mounts none.
	WikiExternalBundles *[]string `json:"wiki_external_bundles"`
}

// ProjectResponse is the body of project endpoints: the project and its
// rooms, main room first.
type ProjectResponse struct {
	Project store.Project `json:"project"`
	Rooms   []store.Room  `json:"rooms"`
}

// ProjectsResponse is the body of GET /api/v1/projects.
type ProjectsResponse struct {
	Projects []store.Project `json:"projects"`
}

// CreateRoomRequest is the body of POST /api/v1/projects/{id}/rooms.
type CreateRoomRequest struct {
	Name string `json:"name"`
}

// RoomResponse is the body of single-room endpoints.
type RoomResponse struct {
	Room store.Room `json:"room"`
}

// RoomsResponse is the body of GET /api/v1/projects/{id}/rooms.
type RoomsResponse struct {
	Rooms []store.Room `json:"rooms"`
}

func (h *handlers) createProject(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	name, err := requireName("name", req.Name)
	if err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}

	agentIDs := make([]string, 0, len(req.AgentIDs))
	for _, id := range req.AgentIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			writeError(w, http.StatusBadRequest, "agent_ids must not hold an empty id")
			return
		}
		agentIDs = append(agentIDs, id)
	}

	description, err := cleanDescription(req.Description)
	if err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	project, mainRoom, err := h.deps.Projects.CreateProject(r.Context(), store.NewProject{
		Name:                  name,
		RepoPath:              cleanRepoPath(req.RepoPath),
		Description:           description,
		AgentIDs:              agentIDs,
		WikiMaintainerAgentID: strings.TrimSpace(req.WikiMaintainerAgentID),
		WikiMaintainerTrigger: req.WikiMaintainerTrigger,
	})
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, ProjectResponse{Project: project, Rooms: []store.Room{mainRoom}})
}

func (h *handlers) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.deps.Projects.ListProjects(r.Context())
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ProjectsResponse{Projects: projects})
}

func (h *handlers) getProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	project, err := h.deps.Projects.GetProject(r.Context(), id)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	rooms, err := h.deps.Projects.ListRooms(r.Context(), id)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ProjectResponse{Project: project, Rooms: rooms})
}

// updateProject renames a project or moves its checkout, and answers with
// the project and its rooms like GET.
func (h *handlers) updateProject(w http.ResponseWriter, r *http.Request) {
	var req UpdateProjectRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	var patch store.ProjectPatch
	if req.Name != nil {
		name, err := requireName("name", *req.Name)
		if err != nil {
			writeReason(w, http.StatusBadRequest, err)
			return
		}
		patch.Name = &name
	}
	if req.RepoPath != nil {
		path := cleanRepoPath(*req.RepoPath)
		patch.RepoPath = &path
	}
	if req.Description != nil {
		description, err := cleanDescription(*req.Description)
		if err != nil {
			writeReason(w, http.StatusBadRequest, err)
			return
		}
		patch.Description = &description
	}
	patch.WikiMaintainer, patch.WikiMaintainerTrigger = req.WikiMaintainerMemberID, req.WikiMaintainerTrigger
	patch.DeclineWikiOffer = req.WikiOfferDeclined != nil && *req.WikiOfferDeclined
	if req.WikiExternalBundles != nil {
		mounts, err := hub.CleanWikiMounts(*req.WikiExternalBundles)
		if err != nil {
			writeReason(w, http.StatusBadRequest, err)
			return
		}
		patch.WikiExternalBundles = &mounts
	}

	id := r.PathValue("id")
	project, err := h.deps.Projects.UpdateProject(r.Context(), id, patch)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	rooms, err := h.deps.Projects.ListRooms(r.Context(), id)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ProjectResponse{Project: project, Rooms: rooms})
}

// deleteProject deletes a project with its whole chat: 204, 404 when it is
// unknown, 409 while one of its turns is running. The uploads and the turn
// transcripts the chat kept on disk go with it.
func (h *handlers) deleteProject(w http.ResponseWriter, r *http.Request) {
	remains, err := h.deps.Projects.DeleteProject(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.dropProjectFiles(remains)
	if h.deps.Wikis != nil {
		// What the team learned is put aside, not deleted.
		if err := h.deps.Wikis.ArchiveProjectWiki(remains.ProjectID, remains.WikiSlug); err != nil {
			h.deps.Logger.Error("archive a deleted project's wiki", "project", remains.ProjectID, "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// dropProjectFiles removes what a deleted project kept on disk: each upload
// (and its room's folder once empty) and each turn's transcript. The project
// is gone either way, so a failure is only logged.
func (h *handlers) dropProjectFiles(remains store.ProjectRemains) {
	remove := func(path string) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			h.deps.Logger.Error("remove a deleted project's file", "path", path, "err", err)
		}
	}
	if dir := h.deps.AttachmentDir; dir != "" {
		folders := map[string]bool{}
		for _, rel := range remains.AttachmentPaths {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			// Only ever below the attachment directory, whatever a path says.
			if inside, err := filepath.Rel(dir, path); err != nil || inside == "." || strings.HasPrefix(inside, "..") {
				continue
			}
			remove(path)
			if folder := filepath.Dir(path); folder != filepath.Clean(dir) {
				folders[folder] = true
			}
		}
		for folder := range folders {
			// Fails, harmlessly, while anything else is still in it.
			_ = os.Remove(folder)
		}
	}
	if dir := h.deps.TranscriptDir; dir != "" {
		for _, id := range remains.TurnIDs {
			remove(filepath.Join(dir, id+".jsonl"))
		}
	}
}

// cleanDescription trims a project's description and turns down one too
// long to open every brief with. It counts characters, as the web client's
// field does.
func cleanDescription(text string) (string, error) {
	text = strings.TrimSpace(text)
	if n := utf8.RuneCountInString(text); n > maxProjectDescription {
		return "", store.Invalid("descriptionTooLong", store.Params{"max": strconv.Itoa(maxProjectDescription)}, "description is %d characters, at most %d", n, maxProjectDescription)
	}
	return text, nil
}

// cleanRepoPath trims a checkout path and any trailing slash, so that a
// member's path can be told to lie below its project's by prefix.
func cleanRepoPath(path string) string {
	path = strings.TrimSpace(path)
	for len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}
	return path
}

func (h *handlers) createRoom(w http.ResponseWriter, r *http.Request) {
	var req CreateRoomRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	name, err := requireName("name", req.Name)
	if err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}

	room, err := h.deps.Projects.CreateRoom(r.Context(), r.PathValue("id"), name)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, RoomResponse{Room: room})
}

func (h *handlers) listRooms(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Confirm the project exists so an unknown id is a 404 rather than an
	// empty list.
	if _, err := h.deps.Projects.GetProject(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	rooms, err := h.deps.Projects.ListRooms(r.Context(), id)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, RoomsResponse{Rooms: rooms})
}

func (h *handlers) getRoom(w http.ResponseWriter, r *http.Request) {
	room, err := h.deps.Projects.GetRoom(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, RoomResponse{Room: room})
}
