package api

import (
	"net/http"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// The members' git worktrees and the main line (docs/design.md 5.21): how
// they stand, and what a person does to them from the chat's branch tab.

// BranchesResponse is the body of GET /api/v1/projects/{id}/branches.
type BranchesResponse struct {
	Branches hub.Branches `json:"branches"`
}

// DiffResponse is the body of GET /api/v1/members/{id}/diff: the patch of
// what the member's worktree changed, cut at its start when long.
type DiffResponse struct {
	Patch string `json:"patch"`
	Cut   bool   `json:"cut,omitempty"`
}

// MergeRequest is the body of POST /api/v1/members/{id}/merge.
type MergeRequest struct {
	// Message is the commit's, a person's line saying what the work does.
	Message string `json:"message"`
	// Leave are new files, never committed, the merge leaves out: they
	// stay in the member's worktree.
	Leave []string `json:"leave,omitempty"`
}

// MergeResponse is how a merge went: the new commit, or the conflicting
// files and nothing changed.
type MergeResponse struct {
	Merge worktree.MergeResult `json:"merge"`
}

// SetAsideResponse says where the work set aside is kept: a ref of the
// repository's.
type SetAsideResponse struct {
	Ref string `json:"ref"`
}

// SyncResponse is how bringing the main line in went.
type SyncResponse struct {
	Sync worktree.SyncResult `json:"sync"`
}

// CommitRequest is the body of POST /api/v1/projects/{id}/checkout/commit:
// the files changed in the checkout a person commits, from the top of the
// repository, and the commit's message.
type CommitRequest struct {
	Message string   `json:"message"`
	Paths   []string `json:"paths"`
}

// CommitResponse is the new commit on the checkout's branch.
type CommitResponse struct {
	Commit string `json:"commit"`
}

// PendingStepsRequest is the body of POST /api/v1/projects/{id}/workspace/pending:
// adopt the steps the leader wrote down, or turn them down.
type PendingStepsRequest struct {
	Adopt bool `json:"adopt"`
}

// worktrees is the dependency the branch routes need, or a 404 when the
// API runs without one.
func (h *handlers) worktrees(w http.ResponseWriter) (Worktrees, bool) {
	if h.deps.Worktrees == nil {
		writeError(w, http.StatusNotFound, "worktrees are not kept here")
		return nil, false
	}
	return h.deps.Worktrees, true
}

func (h *handlers) branches(w http.ResponseWriter, r *http.Request) {
	wt, ok := h.worktrees(w)
	if !ok {
		return
	}
	branches, err := wt.Branches(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, BranchesResponse{Branches: branches})
}

func (h *handlers) memberDiff(w http.ResponseWriter, r *http.Request) {
	wt, ok := h.worktrees(w)
	if !ok {
		return
	}
	patch, cut, err := wt.DiffOf(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, DiffResponse{Patch: patch, Cut: cut})
}

// checkoutDiff is the patch of what the project's checkout changed and did
// not commit.
func (h *handlers) checkoutDiff(w http.ResponseWriter, r *http.Request) {
	wt, ok := h.worktrees(w)
	if !ok {
		return
	}
	patch, cut, err := wt.CheckoutDiff(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, DiffResponse{Patch: patch, Cut: cut})
}

// commitCheckout commits changes in the project's checkout: 200 with the
// new commit, 409 while a member works there or when git refuses.
func (h *handlers) commitCheckout(w http.ResponseWriter, r *http.Request) {
	wt, ok := h.worktrees(w)
	if !ok {
		return
	}
	var req CommitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	commit, err := wt.CommitCheckout(r.Context(), r.PathValue("id"), req.Message, req.Paths)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, CommitResponse{Commit: commit})
}

// mergeMember puts the member's work on the main line: 200 with the new
// commit or the conflicts, 409 when git or the member's state refuses it.
func (h *handlers) mergeMember(w http.ResponseWriter, r *http.Request) {
	wt, ok := h.worktrees(w)
	if !ok {
		return
	}
	var req MergeRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	merged, err := wt.Merge(r.Context(), r.PathValue("id"), req.Message, req.Leave)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MergeResponse{Merge: merged})
}

func (h *handlers) setAside(w http.ResponseWriter, r *http.Request) {
	wt, ok := h.worktrees(w)
	if !ok {
		return
	}
	ref, err := wt.SetAside(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, SetAsideResponse{Ref: ref})
}

func (h *handlers) syncMember(w http.ResponseWriter, r *http.Request) {
	wt, ok := h.worktrees(w)
	if !ok {
		return
	}
	synced, err := wt.SyncMember(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, SyncResponse{Sync: synced})
}

// abortMerge gives up a merge a member left under way in its worktree.
func (h *handlers) abortMerge(w http.ResponseWriter, r *http.Request) {
	wt, ok := h.worktrees(w)
	if !ok {
		return
	}
	if err := wt.AbortMerge(r.Context(), r.PathValue("id")); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// startSetup has the project's leader set it up again: 202, as the setup is
// a turn that runs on after the answer.
func (h *handlers) startSetup(w http.ResponseWriter, r *http.Request) {
	wt, ok := h.worktrees(w)
	if !ok {
		return
	}
	if err := wt.StartSetup(r.Context(), r.PathValue("id")); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *handlers) settlePendingSteps(w http.ResponseWriter, r *http.Request) {
	wt, ok := h.worktrees(w)
	if !ok {
		return
	}
	var req PendingStepsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	user, _ := userFrom(r.Context())
	if err := wt.SettleWorkspaceSteps(r.Context(), r.PathValue("id"), user.ID, req.Adopt); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
