package api

import (
	"net/http"

	"github.com/J0EY0/veyloom/internal/hub"
)

// AddresseeResponse is the body of GET /api/v1/rooms/{id}/addressee: where
// a message of the person's that names nobody would go, in the room or in
// the topic thread_id names, and why (docs/design.md 4.2). MemberID is
// empty when it would go to no member.
type AddresseeResponse struct {
	MemberID string              `json:"member_id,omitempty"`
	Reason   hub.AddresseeReason `json:"reason"`
}

// addressee answers the composer's question: who takes a message that
// names nobody, here.
func (h *handlers) addressee(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	if _, err := h.deps.Projects.GetRoom(r.Context(), roomID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	threadID := r.URL.Query().Get("thread_id")
	if threadID != "" {
		thread, err := h.deps.Messages.GetThread(r.Context(), threadID)
		if err != nil {
			h.writeStoreError(w, r, err)
			return
		}
		if thread.RoomID != roomID {
			writeError(w, http.StatusNotFound, "no such topic in this room")
			return
		}
	}
	user, _ := userFrom(r.Context())
	to, err := h.deps.Chat.Addressee(r.Context(), roomID, threadID, user.ID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, AddresseeResponse{MemberID: to.Member.ID, Reason: to.Reason})
}
