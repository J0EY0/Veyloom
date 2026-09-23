package api

import (
	"net/http"

	"github.com/J0EY0/veyloom/internal/hub"
)

// UpkeepResponse is the body of the wiki upkeep endpoints: how a project's
// wiki maintainer stands (docs/design.md 5.12).
type UpkeepResponse struct {
	Upkeep hub.UpkeepStatus `json:"upkeep"`
}

func (h *handlers) upkeepStatus(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	status, err := wikis.UpkeepStatus(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, UpkeepResponse{Upkeep: status})
}

// startUpkeep runs the project's maintainer now: 202, since the upkeep is
// a turn that runs on after the answer.
func (h *handlers) startUpkeep(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	status, err := wikis.StartUpkeep(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, UpkeepResponse{Upkeep: status})
}
