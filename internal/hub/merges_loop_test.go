package hub

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// Work one member merged from another goes on the main line once
// (docs/design.md 5.21). Merged with the member that had it all, it is
// said so in the chat, and that other member follows the main line at
// once; a member that built on it, changing the same lines, is counted
// and merged for its own work alone, with no conflict and no overlap.
func TestLoop_WorkMergedAlongGoesOnOnce(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{RepoPath: &repo}); err != nil {
		t.Fatal(err)
	}
	l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"feature.go"}})
	tester := l.memberIn("Tester", repo, store.PermissionFullAuto, map[string]any{"reply": "Done."})
	reviewer := l.memberIn("Reviewer", repo, store.PermissionFullAuto, map[string]any{"reply": "Done."})
	l.say("@Coder add the feature", "", coder)
	l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")
	c, _ := l.s.GetMember(l.ctx, coder.ID)
	gitIn(t, c.WorkDir, "add", "-A")
	gitIn(t, c.WorkDir, "commit", "-q", "-m", "Add the feature")
	l.say("@Tester test it", "", tester)
	l.waitTurns(3, store.TurnDone, "Tester's turn")
	l.say("@Reviewer review it", "", reviewer)
	l.waitTurns(4, store.TurnDone, "Reviewer's turn")

	// Tester builds on Coder's work, changing its lines; Reviewer only
	// reads it.
	tt, _ := l.s.GetMember(l.ctx, tester.ID)
	rv, _ := l.s.GetMember(l.ctx, reviewer.ID)
	gitIn(t, tt.WorkDir, "merge", "-q", "veyloom/coder")
	if err := os.WriteFile(filepath.Join(tt.WorkDir, "feature.go"), []byte("package feature\n\n// Tested.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, tt.WorkDir, "commit", "-q", "-am", "Test the feature")
	gitIn(t, rv.WorkDir, "merge", "-q", "veyloom/coder")

	merged, err := l.h.Merge(l.ctx, coder.ID, "Add the feature\n\nIt does what was asked.", nil)
	if err != nil || merged.Commit == "" {
		t.Fatalf("merge Coder: %+v %v", merged, err)
	}
	want := "Merged Coder's work, which had Reviewer's in it, into the main line as " + merged.Commit[:7] + ": Add the feature"
	if !slices.ContainsFunc(l.topLevel(), func(m store.Message) bool { return m.Body == want }) {
		t.Errorf("the chat does not say %q", want)
	}

	b, err := l.h.Branches(l.ctx, l.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range b.Members {
		st := m.Status
		switch m.MemberID {
		case reviewer.ID:
			if st == nil || st.Ahead+st.Behind+len(st.Files) != 0 {
				t.Errorf("Reviewer does not follow the main line: %+v", st)
			}
		case tester.ID:
			if st == nil || st.Ahead != 1 || len(st.Files) != 1 || st.Files[0].Path != "feature.go" {
				t.Errorf("Tester's own work: %+v", st)
			}
		}
	}
	if len(b.Overlaps) != 0 {
		t.Errorf("overlaps: %+v", b.Overlaps)
	}

	merged, err = l.h.Merge(l.ctx, tester.ID, "Test the feature", nil)
	if err != nil || merged.Commit == "" || len(merged.Conflicts) != 0 {
		t.Fatalf("merge Tester after: %+v %v", merged, err)
	}
	if body, _ := os.ReadFile(filepath.Join(repo, "feature.go")); !strings.Contains(string(body), "// Tested.") {
		t.Errorf("the main line has %q", body)
	}
	if got := gitIn(t, repo, "log", "--format=%s", "-3"); got != "Test the feature\nAdd the feature\nfirst" {
		t.Errorf("the main line's log:\n%s", got)
	}
}

// A person leaves a build out of a merge, which stays in the member's
// worktree, and then sets the member's work aside: kept in git, said in
// the chat, and the worktree starts over (docs/design.md 5.21).
func TestLoop_ThePersonLeavesFilesOutAndSetsWorkAside(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{RepoPath: &repo}); err != nil {
		t.Fatal(err)
	}
	l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"feature.go", "app"}})
	l.say("@Coder add the feature", "", coder)
	l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")
	c, _ := l.s.GetMember(l.ctx, coder.ID)

	gitIn(t, c.WorkDir, "add", "feature.go")
	gitIn(t, c.WorkDir, "commit", "-q", "-m", "Add the feature")
	var problem *store.Problem
	if _, err := l.h.Merge(l.ctx, coder.ID, "Add the feature", []string{"feature.go"}); !errors.As(err, &problem) || problem.Code != "leaveNotNew" || problem.Params["files"] != "feature.go" {
		t.Fatalf("leaving out a committed file: %v", err)
	}
	merged, err := l.h.Merge(l.ctx, coder.ID, "Add the feature", []string{"app"})
	if err != nil || merged.Commit == "" {
		t.Fatalf("the merge: %+v %v", merged, err)
	}
	if files := gitIn(t, repo, "show", "--name-only", "--format=", "HEAD"); files != "feature.go" {
		t.Errorf("the merge has %q", files)
	}
	if _, err := os.Stat(filepath.Join(c.WorkDir, "app")); err != nil {
		t.Errorf("the build left out is not in Coder's worktree: %v", err)
	}

	ref, err := l.h.SetAside(l.ctx, coder.ID)
	if err != nil || !strings.HasPrefix(ref, "refs/veyloom/set-aside/veyloom/coder/") {
		t.Fatalf("set aside: %q %v", ref, err)
	}
	want := "Reset Coder's branch veyloom/coder to the main line; its work is archived as " + ref + "."
	if !slices.ContainsFunc(l.topLevel(), func(m store.Message) bool { return m.Body == want }) {
		t.Errorf("the chat does not say %q", want)
	}
	if _, err := os.Stat(filepath.Join(c.WorkDir, "app")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the build is still in Coder's worktree: %v", err)
	}
	if kept := gitIn(t, repo, "show", "--name-only", "--format=", ref); kept != "app" {
		t.Errorf("kept: %q", kept)
	}
	if _, err := l.h.SetAside(l.ctx, coder.ID); !errors.As(err, &problem) || problem.Code != "noChanges" {
		t.Errorf("nothing left to set aside: %v", err)
	}
}
