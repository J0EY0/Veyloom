package api

import (
	"errors"
	"net/http"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
)

// PausesResponse is the body of GET /api/v1/pauses: what keeps turns from
// starting now, the oldest first (docs/design.md 5.23.3).
type PausesResponse struct {
	Pauses []store.Pause `json:"pauses"`
}

func (h *handlers) listPauses(w http.ResponseWriter, r *http.Request) {
	pauses := h.deps.Chat.Pauses(r.Context())
	if pauses == nil {
		pauses = []store.Pause{}
	}
	writeJSON(w, http.StatusOK, PausesResponse{Pauses: pauses})
}

// liftPause lifts a pause, and what it held up goes on. A pause no longer
// in effect, lifted already or run out, is a 404.
func (h *handlers) liftPause(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	if err := h.deps.Chat.LiftPause(r.Context(), r.PathValue("id"), user.ID); err != nil {
		if errors.Is(err, hub.ErrUnknownPause) {
			writeCoded(w, http.StatusNotFound, "pauseNotFound", nil, "no such pause is in effect")
			return
		}
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
