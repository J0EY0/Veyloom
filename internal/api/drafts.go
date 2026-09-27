package api

import (
	"net/http"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
)

// DraftsResponse is the body of GET /api/v1/threads/{id}/drafts: what
// members drafted in the topic for a person to run, in the order drafted,
// as it stands (docs/design.md 5.23.5). The topic's cards are drawn by
// these.
type DraftsResponse struct {
	Drafts []store.Draft `json:"drafts"`
}

// DraftResponse is the body of running or turning a draft down: the draft
// as it stands after.
type DraftResponse struct {
	Draft store.Draft `json:"draft"`
}

// RunDraftRequest is the body of POST /api/v1/drafts/{id}/run: a merge as
// a person changed it, its message, empty for the drafted one, and the new
// files they leave out.
type RunDraftRequest struct {
	Message string   `json:"message"`
	Leave   []string `json:"leave"`
}

func (h *handlers) threadDrafts(w http.ResponseWriter, r *http.Request) {
	drafts, err := h.deps.Turns.ListThreadDrafts(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if drafts == nil {
		drafts = []store.Draft{}
	}
	writeJSON(w, http.StatusOK, DraftsResponse{Drafts: drafts})
}

// runDraft does what a draft has a person do. What cannot be done for now
// is refused as the branches refuse it, and the draft waits again; one run
// or turned down already is a 409 (draftSettled).
func (h *handlers) runDraft(w http.ResponseWriter, r *http.Request) {
	var req RunDraftRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeReason(w, http.StatusBadRequest, err)
			return
		}
	}
	user, _ := userFrom(r.Context())
	d, err := h.deps.Chat.RunDraft(r.Context(), r.PathValue("id"), user.ID, hub.DraftEdit{Message: req.Message, Leave: req.Leave})
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, DraftResponse{Draft: d})
}

// declineDraft turns a draft down.
func (h *handlers) declineDraft(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	d, err := h.deps.Chat.DeclineDraft(r.Context(), r.PathValue("id"), user.ID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, DraftResponse{Draft: d})
}
