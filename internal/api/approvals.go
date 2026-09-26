package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// ApprovalStore reads recorded approvals. Deciding goes through Chat so
// the decision reaches the waiting turn.
type ApprovalStore interface {
	GetApproval(ctx context.Context, id string) (store.Approval, error)
	ListPendingRoomApprovals(ctx context.Context, roomID string) ([]store.Approval, error)
	ListPendingApprovals(ctx context.Context) ([]store.PendingApproval, error)
	ListTurnApprovals(ctx context.Context, turnID string) ([]store.Approval, error)
}

// PendingApprovalsResponse is the body of GET /api/v1/approvals?status=pending:
// what waits for a person, across every project.
type PendingApprovalsResponse struct {
	Approvals []store.PendingApproval `json:"approvals"`
}

// DecideApprovalRequest is the body of POST /api/v1/approvals/{id}/decide.
// The signed-in user decides; user_id in the body counts only when the
// API runs without sign-in (tests).
type DecideApprovalRequest struct {
	UserID string `json:"user_id"`
	Allow  bool   `json:"allow"`
	// Message is an optional note; on a denial the agent sees it.
	Message string `json:"message"`
	// Answer goes with an allowed request that asked for more than yes or
	// no: for a question {"answers": {"<question id>": ["..."]}}.
	Answer json.RawMessage `json:"answer,omitempty"`
	// Scope is how far an allow goes (docs/design.md 4.6): once, similar
	// (the like of the request for the rest of the turn, as its runtime
	// offered), always (that, and kept for the member from now on) or turn
	// (whatever else the turn asks for). Empty is once, or similar when
	// Similar is set.
	Scope store.AllowScope `json:"scope,omitempty"`
	// Similar is the older way of asking for scope similar.
	Similar bool `json:"similar,omitempty"`
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
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	if user, ok := userFrom(r.Context()); ok {
		req.UserID = user.ID
	}
	if strings.TrimSpace(req.UserID) == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if string(req.Answer) == "null" {
		req.Answer = nil
	}
	if len(req.Answer) > 0 && !isJSONObject(req.Answer) {
		writeError(w, http.StatusBadRequest, "answer must be a JSON object")
		return
	}
	if req.Scope != "" && !req.Scope.Valid() {
		writeError(w, http.StatusBadRequest, "scope must be once, similar, always or turn")
		return
	}
	a, err := h.deps.Chat.DecideApproval(r.Context(), r.PathValue("id"), req.UserID, runtime.Decision{
		Allow: req.Allow, Similar: req.Similar, Message: strings.TrimSpace(req.Message), Answer: req.Answer,
	}, req.Scope)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ApprovalResponse{Approval: a})
}

// listApprovals answers GET /api/v1/approvals. Only status=pending exists:
// the "for me" page's list, across every project.
func (h *handlers) listApprovals(w http.ResponseWriter, r *http.Request) {
	if status := r.URL.Query().Get("status"); status != string(store.ApprovalPending) {
		writeError(w, http.StatusBadRequest, "status must be pending")
		return
	}
	approvals, err := h.deps.Approvals.ListPendingApprovals(r.Context())
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, PendingApprovalsResponse{Approvals: approvals})
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

// isJSONObject reports whether raw is a JSON object.
func isJSONObject(raw json.RawMessage) bool {
	var v map[string]json.RawMessage
	return json.Unmarshal(raw, &v) == nil && v != nil
}
