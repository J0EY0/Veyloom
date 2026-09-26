package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
)

// RuleStore reads and takes back what people allowed members always
// (docs/design.md 4.6). Rules are added by allowing a request "always".
type RuleStore interface {
	ListMemberRules(ctx context.Context, memberID, runtime string) ([]store.MemberRule, error)
	DeleteMemberRule(ctx context.Context, memberID, ruleID string) error
}

// MemberRulesResponse is the body of GET /api/v1/members/{id}/rules: what
// the member may always do without a person being asked, oldest first.
type MemberRulesResponse struct {
	Rules []store.MemberRule `json:"rules"`
}

// listMemberRules answers GET /api/v1/members/{id}/rules: every runtime's
// rules, the member's own runtime among them; 404 for an unknown member.
func (h *handlers) listMemberRules(w http.ResponseWriter, r *http.Request) {
	if h.deps.Rules == nil {
		writeError(w, http.StatusNotFound, "rules are not kept here")
		return
	}
	id := r.PathValue("id")
	if _, err := h.deps.Agents.GetMember(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	rules, err := h.deps.Rules.ListMemberRules(r.Context(), id, "")
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if rules == nil {
		rules = []store.MemberRule{}
	}
	writeJSON(w, http.StatusOK, MemberRulesResponse{Rules: rules})
}

// deleteMemberRule answers DELETE /api/v1/members/{id}/rules/{rule}: the
// member is asked again from its next turn on. 204, or 404 for a rule the
// member does not have.
func (h *handlers) deleteMemberRule(w http.ResponseWriter, r *http.Request) {
	if h.deps.Rules == nil {
		writeError(w, http.StatusNotFound, "rules are not kept here")
		return
	}
	if err := h.deps.Rules.DeleteMemberRule(r.Context(), r.PathValue("id"), r.PathValue("rule")); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// untrustTurn answers DELETE /api/v1/turns/{id}/trust: the running turn's
// requests go to people again. It returns the turn; 404 for an unknown
// turn, 409 for one not running.
func (h *handlers) untrustTurn(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.deps.Turns.GetTurn(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	turn, err := h.deps.Chat.UntrustTurn(r.Context(), id)
	if err != nil {
		if errors.Is(err, hub.ErrUnknownTurn) {
			writeCoded(w, http.StatusConflict, "turnNotRunning", nil, "turn is not running")
			return
		}
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, TurnResponse{Turn: turn})
}
