package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// fakeWorktrees records what the branch routes ask of the hub.
type fakeWorktrees struct {
	released []string
	merged   string
	adopted  *bool
	setup    string
	written  string
	aborted  string
	// committed is the last commit in a checkout: project, message, files.
	committed string
}

func (f *fakeWorktrees) ReleaseWorktree(m store.Member) { f.released = append(f.released, m.ID) }

func (f *fakeWorktrees) Branches(_ context.Context, projectID string) (hub.Branches, error) {
	if projectID != "p1" {
		return hub.Branches{}, store.ErrNotFound
	}
	return hub.Branches{Main: hub.MainLine{Git: true, Branch: "main"}, Members: []hub.MemberBranch{{MemberID: "m1", Name: "Coder"}}}, nil
}

func (f *fakeWorktrees) DiffOf(context.Context, string) (string, bool, error) {
	return "+new line\n", true, nil
}

func (f *fakeWorktrees) Merge(_ context.Context, memberID, message string, leave []string) (worktree.MergeResult, error) {
	f.merged = memberID + ": " + message
	if len(leave) > 0 {
		f.merged += " less " + strings.Join(leave, ", ")
	}
	return worktree.MergeResult{Commit: "abc"}, nil
}

func (f *fakeWorktrees) SetAside(_ context.Context, memberID string) (string, error) {
	if memberID == "busy" {
		return "", store.Conflicting("memberBusy", store.Params{"name": "Coder"}, "Coder is at work")
	}
	return "refs/veyloom/set-aside/veyloom/coder/1", nil
}

func (f *fakeWorktrees) SyncMember(context.Context, string) (worktree.SyncResult, error) {
	return worktree.SyncResult{Conflicts: []string{"a.go"}}, nil
}

func (f *fakeWorktrees) AbortMerge(_ context.Context, memberID string) error {
	if memberID == "busy" {
		return store.Conflicting("mergeUnderway", store.Params{"name": "Coder"}, "Coder's worktree is in the middle of a merge")
	}
	f.aborted = memberID
	return nil
}

func (f *fakeWorktrees) StartSetup(_ context.Context, projectID string) error {
	f.setup = projectID
	return nil
}

func (f *fakeWorktrees) SettleWorkspaceSteps(_ context.Context, _ string, adopt bool) error {
	f.adopted = &adopt
	return nil
}

func (f *fakeWorktrees) WorkspaceStepsWritten(projectID string) { f.written = projectID }

func (f *fakeWorktrees) CheckoutDiff(context.Context, string) (string, bool, error) {
	return "+by the leader\n", false, nil
}

func (f *fakeWorktrees) CommitCheckout(_ context.Context, projectID, message string, paths []string) (string, error) {
	if len(paths) == 0 {
		return "", store.Invalid("commitFiles", nil, "a commit needs the files it takes")
	}
	f.committed = projectID + ": " + message + " " + strings.Join(paths, ",")
	return "def", nil
}

func TestBranchRoutes(t *testing.T) {
	projects := newFakeProjects()
	projects.projects["p1"] = store.Project{ID: "p1", Name: "app"}
	wt := &fakeWorktrees{}
	handler := NewHandler(Deps{Projects: projects, Worktrees: wt})

	var branches BranchesResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p1/branches", "", &branches); rec.Code != http.StatusOK || branches.Branches.Main.Branch != "main" {
		t.Errorf("branches: %d %+v", rec.Code, branches)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p9/branches", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown project: %d", rec.Code)
	}
	var diff DiffResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/members/m1/diff", "", &diff); rec.Code != http.StatusOK || diff.Patch != "+new line\n" || !diff.Cut {
		t.Errorf("diff: %d %+v", rec.Code, diff)
	}
	var merge MergeResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/members/m1/merge", `{"message":"Add it"}`, &merge); rec.Code != http.StatusOK || merge.Merge.Commit != "abc" || wt.merged != "m1: Add it" {
		t.Errorf("merge: %d %+v %q", rec.Code, merge, wt.merged)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/members/m1/merge", `{"message":"Add it","leave":["app","out.json"]}`, &merge); rec.Code != http.StatusOK || wt.merged != "m1: Add it less app, out.json" {
		t.Errorf("a merge leaving files out: %d %q", rec.Code, wt.merged)
	}
	var aside SetAsideResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/members/m1/set-aside", ``, &aside); rec.Code != http.StatusOK || aside.Ref != "refs/veyloom/set-aside/veyloom/coder/1" {
		t.Errorf("set aside: %d %+v", rec.Code, aside)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/members/busy/set-aside", ``, nil); rec.Code != http.StatusConflict {
		t.Errorf("set aside while at work: %d", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/members/m1/merge", `{`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a merge body that is no JSON: %d", rec.Code)
	}
	var sync SyncResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/members/m1/sync", "", &sync); rec.Code != http.StatusOK || len(sync.Sync.Conflicts) != 1 {
		t.Errorf("sync: %d %+v", rec.Code, sync)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/members/m1/merge/abort", "", nil); rec.Code != http.StatusNoContent || wt.aborted != "m1" {
		t.Errorf("give a merge up: %d %q", rec.Code, wt.aborted)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/members/busy/merge/abort", "", nil); rec.Code != http.StatusConflict {
		t.Errorf("a refused give-up: %d", rec.Code)
	}
	var checkout DiffResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p1/checkout/diff", "", &checkout); rec.Code != http.StatusOK || checkout.Patch != "+by the leader\n" {
		t.Errorf("the checkout's diff: %d %+v", rec.Code, checkout)
	}
	var committed CommitResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/projects/p1/checkout/commit", `{"message":"Say who leads","paths":["README.md"]}`, &committed); rec.Code != http.StatusOK || committed.Commit != "def" || wt.committed != "p1: Say who leads README.md" {
		t.Errorf("the checkout's commit: %d %+v %q", rec.Code, committed, wt.committed)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/projects/p1/checkout/commit", `{"message":"No files"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a commit of no files: %d", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/projects/p1/setup", "", nil); rec.Code != http.StatusAccepted || wt.setup != "p1" {
		t.Errorf("setup: %d %q", rec.Code, wt.setup)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/projects/p1/workspace/pending", `{"adopt":true}`, nil); rec.Code != http.StatusNoContent || wt.adopted == nil || !*wt.adopted {
		t.Errorf("adopt: %d %v", rec.Code, wt.adopted)
	}

	// A person writing the steps down sets the project up.
	if rec := do(t, handler, http.MethodPatch, "/api/v1/projects/p1", `{"workspace_steps":{"copy":[" .env "],"run":" npm ci "}}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("steps: %d %s", rec.Code, rec.Body)
	}
	if steps := projects.patched.WorkspaceSteps; steps == nil || len(steps.Copy) != 1 || steps.Copy[0] != ".env" || steps.Run != "npm ci" || wt.written != "p1" {
		t.Errorf("the steps as stored: %+v, told %q", projects.patched.WorkspaceSteps, wt.written)
	}
	if rec := do(t, handler, http.MethodPatch, "/api/v1/projects/p1", `{"workspace_steps":{"copy":["../out"]}}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a path out of the checkout: %d", rec.Code)
	}

	// Without the hub's worktrees, the routes are not there.
	bare := NewHandler(Deps{Projects: projects})
	if rec := do(t, bare, http.MethodGet, "/api/v1/projects/p1/branches", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("no worktrees: %d", rec.Code)
	}
}
