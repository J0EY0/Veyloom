package api

import (
	"net/http"

	"github.com/J0EY0/veyloom/internal/store"
)

// CreateProjectRequest is the body of POST /api/v1/projects.
type CreateProjectRequest struct {
	Name          string `json:"name"`
	RepoURL       string `json:"repo_url"`
	DefaultBranch string `json:"default_branch"`
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
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := requireName("name", req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	project, mainRoom, err := h.deps.Projects.CreateProject(r.Context(), store.NewProject{
		Name:          name,
		RepoURL:       req.RepoURL,
		DefaultBranch: req.DefaultBranch,
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

func (h *handlers) createRoom(w http.ResponseWriter, r *http.Request) {
	var req CreateRoomRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, err := requireName("name", req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
