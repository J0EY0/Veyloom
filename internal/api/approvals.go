package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/store"
)

// ApprovalStore reads recorded approvals. Deciding goes through Chat so
// the decision reaches the waiting turn.
type ApprovalStore interface {
	GetApproval(ctx context.Context, id string) (store.Approval, error)
	ListPendingRoomApprovals(ctx context.Context, roomID string) ([]store.Approval, error)
	ListTurnApprovals(ctx context.Context, turnID string) ([]store.Approval, error)
}

// DecideApprovalRequest is the body of POST /api/v1/approvals/{id}/decide.
// There is no authentication yet, so the client names the deciding user.
type DecideApprovalRequest struct {
	UserID string `json:"user_id"`
	Allow  bool   `json:"allow"`
	// Message is an optional note; on a denial the agent sees it.
	Message string `json:"message"`
}

// ApprovalResponse is the body of single-approval endpoints.
type ApprovalResponse struct {
	Approval store.Approval `json:"approval"`
}

// ApprovalsResponse is the body of approval listings, oldest first.
type ApprovalsResponse struct {
	Approvals []store.Approval `json:"approvals"`
}

func (h *handlers) getApproval(w http.ResponseWriter, r *http.Request) {
	a, err := h.deps.Approvals.GetApproval(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ApprovalResponse{Approval: a})
}

// decideApproval settles a pending approval. An unknown id is 404; one
// that was already decided, expired or cancelled is 409.
func (h *handlers) decideApproval(w http.ResponseWriter, r *http.Request) {
	var req DecideApprovalRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(req.UserID) == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	a, err := h.deps.Chat.DecideApproval(r.Context(), r.PathValue("id"), req.UserID, engine.Decision{Allow: req.Allow, Message: strings.TrimSpace(req.Message)})
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ApprovalResponse{Approval: a})
}

// listRoomApprovals returns what is waiting for a decision in a room.
func (h *handlers) listRoomApprovals(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	if _, err := h.deps.Projects.GetRoom(r.Context(), roomID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	approvals, err := h.deps.Approvals.ListPendingRoomApprovals(r.Context(), roomID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ApprovalsResponse{Approvals: approvals})
}

// listTurnApprovals returns every approval a turn raised, decided or not.
func (h *handlers) listTurnApprovals(w http.ResponseWriter, r *http.Request) {
	turnID := r.PathValue("id")
	if _, err := h.deps.Turns.GetTurn(r.Context(), turnID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	approvals, err := h.deps.Approvals.ListTurnApprovals(r.Context(), turnID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ApprovalsResponse{Approvals: approvals})
}
