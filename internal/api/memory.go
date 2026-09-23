package api

import (
	"net/http"
	"strings"

	"github.com/J0EY0/veyloom/internal/hub"
)

// The memories over HTTP (docs/design.md 5.16): the personal memory,
// which Settings keeps, and each project's, which its Wiki tab keeps. A
// project's memory is a page of its wiki, so its history is the wiki's;
// the personal memory has a history of its own.

// MemoryResponse is the body of the endpoints that return a memory.
type MemoryResponse struct {
	Memory hub.MemoryView `json:"memory"`
}

// MemoryRequest is the body of PUT /memory and PUT /projects/{id}/memory:
// the memory as the person left it, one entry a line. Entries left as
// they were keep their day and source; the rest are the person's, today.
type MemoryRequest struct {
	UserID  string    `json:"user_id"`
	Entries *[]string `json:"entries"`
	// Hash is the memory's as it was read; a memory changed since is a 409.
	Hash string `json:"hash"`
}

func (h *handlers) personalMemory(w http.ResponseWriter, r *http.Request) {
	h.readMemory(w, r, "")
}

func (h *handlers) projectMemory(w http.ResponseWriter, r *http.Request) {
	h.readMemory(w, r, r.PathValue("id"))
}

func (h *handlers) setPersonalMemory(w http.ResponseWriter, r *http.Request) {
	h.writeMemory(w, r, "")
}

func (h *handlers) setProjectMemory(w http.ResponseWriter, r *http.Request) {
	h.writeMemory(w, r, r.PathValue("id"))
}

// readMemory answers with a project's memory, or the personal one for an
// empty projectID.
func (h *handlers) readMemory(w http.ResponseWriter, r *http.Request, projectID string) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	memory, err := wikis.Memory(r.Context(), projectID)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MemoryResponse{Memory: memory})
}

func (h *handlers) writeMemory(w http.ResponseWriter, r *http.Request, projectID string) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	var req MemoryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	userID, ok := deciderID(r, req.UserID)
	if !ok {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.Entries == nil {
		writeError(w, http.StatusBadRequest, "entries is required")
		return
	}
	memory, err := wikis.SetMemory(r.Context(), projectID, *req.Entries, req.Hash, userID)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MemoryResponse{Memory: memory})
}

func (h *handlers) memoryHistory(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	limit, ok := h.listLimit(w, r)
	if !ok {
		return
	}
	commits, err := wikis.MemoryHistory(r.Context(), limit)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiHistoryResponse{Commits: commits})
}

func (h *handlers) revertMemory(w http.ResponseWriter, r *http.Request) {
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
	undo, err := wikis.RevertMemory(r.Context(), req.SHA, userID, req.Reason)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, RevertWikiResponse{Commit: undo})
}
