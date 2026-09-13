package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
)

// Chat is the hub's entry point for user messages, turn control and
// approval decisions. These go through the hub rather than the store so
// that routing to agents, and delivery to waiting turns, can never be
// bypassed.
type Chat interface {
	PostUserMessage(ctx context.Context, m store.NewMessage) (store.Message, error)
	CancelTurn(ctx context.Context, turnID string) error
	DecideApproval(ctx context.Context, approvalID, userID string, d engine.Decision) (store.Approval, error)
	Subscribe(roomID string) hub.Subscription
}

// TurnStore reads recorded turns.
type TurnStore interface {
	GetTurn(ctx context.Context, id string) (store.Turn, error)
	ListRoomTurns(ctx context.Context, roomID string, limit int) ([]store.Turn, error)
}

// TurnResponse is the body of single-turn endpoints.
type TurnResponse struct {
	Turn store.Turn `json:"turn"`
}

// TurnsResponse is the body of GET /api/v1/rooms/{id}/turns, newest first.
type TurnsResponse struct {
	Turns []store.Turn `json:"turns"`
}

func (h *handlers) getTurn(w http.ResponseWriter, r *http.Request) {
	turn, err := h.deps.Turns.GetTurn(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, TurnResponse{Turn: turn})
}

func (h *handlers) listRoomTurns(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	if _, err := h.deps.Projects.GetRoom(r.Context(), roomID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	limit, _, err := queryInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "limit: "+err.Error())
		return
	}
	turns, err := h.deps.Turns.ListRoomTurns(r.Context(), roomID, int(limit))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, TurnsResponse{Turns: turns})
}

// cancelTurn stops a running turn. The turn is looked up first so that an
// unknown id is a 404 while a known but finished turn is a 409.
func (h *handlers) cancelTurn(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.deps.Turns.GetTurn(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := h.deps.Chat.CancelTurn(r.Context(), id); err != nil {
		if errors.Is(err, hub.ErrUnknownTurn) {
			writeError(w, http.StatusConflict, "turn is not running")
			return
		}
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}
