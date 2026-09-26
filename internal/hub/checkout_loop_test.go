package hub

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// gitIn runs git in dir for a test.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// What the leader changed in the checkout and did not commit is read and
// committed by a person from the branch tab; until then it stops merging
// work that changes the same files, which says so in so many words
// (docs/design.md 5.21).
func TestLoop_ThePersonCommitsTheCheckout(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{RepoPath: &repo}); err != nil {
		t.Fatal(err)
	}
	l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"README.md"}})
	l.say("@Coder write the docs", "", coder)
	l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")

	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# app\n\nLed by Lead.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := l.h.Branches(l.ctx, l.room.ProjectID)
	if err != nil || len(b.Main.Changed) != 1 || b.Main.Changed[0].Path != "README.md" {
		t.Fatalf("the checkout's changes: %+v %v", b.Main, err)
	}
	_, err = l.h.Merge(l.ctx, coder.ID, "Write the docs", nil)
	var problem *store.Problem
	if !errors.As(err, &problem) || problem.Code != "checkoutChanged" || problem.Params["files"] != "README.md" {
		t.Fatalf("a merge with changes in the way: %v", err)
	}

	patch, cut, err := l.h.CheckoutDiff(l.ctx, l.room.ProjectID)
	if err != nil || cut || !strings.Contains(patch, "+Led by Lead.") {
		t.Errorf("the checkout's patch: %v %v\n%s", err, cut, patch)
	}
	if _, err := l.h.CommitCheckout(l.ctx, l.room.ProjectID, " ", []string{"README.md"}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a commit with no message: %v", err)
	}
	commit, err := l.h.CommitCheckout(l.ctx, l.room.ProjectID, "Say who leads", []string{"README.md"})
	if err != nil || commit == "" {
		t.Fatalf("the commit: %q %v", commit, err)
	}
	if got := gitIn(t, repo, "log", "-1", "--format=%H %s"); got != commit+" Say who leads" {
		t.Errorf("the main line's last commit: %q", got)
	}
	found := false
	for _, msg := range l.topLevel() {
		if msg.SenderKind == store.SenderSystem && msg.Body == "Committed the changes in the project's checkout as "+commit[:7]+": Say who leads" {
			found = true
		}
	}
	if !found {
		t.Error("the chat does not say the checkout was committed")
	}
	if _, err := l.h.CommitCheckout(l.ctx, l.room.ProjectID, "Again", []string{"README.md"}); !errors.As(err, &problem) || problem.Code != "checkoutClean" {
		t.Errorf("nothing left to commit: %v", err)
	}
	// Committed, the change is on the main line: the merge now meets it
	// as the conflict it is.
	merged, err := l.h.Merge(l.ctx, coder.ID, "Write the docs", nil)
	if err != nil || !slices.Equal(merged.Conflicts, []string{"README.md"}) {
		t.Errorf("the merge after the commit: %+v %v", merged, err)
	}
}

// A member that merged another's branch has that work in full: the files
// both changed are no overlap, in the chat or the branch tab, which says
// whose work the branch has (docs/design.md 5.21).
func TestLoop_WorkMergedInFullDoesNotOverlap(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{RepoPath: &repo}); err != nil {
		t.Fatal(err)
	}
	l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"README.md", "feature.go"}})
	tester := l.memberIn("Tester", repo, store.PermissionFullAuto, map[string]any{"reply": "Done.", "write": []any{"feature_test.go"}})
	l.say("@Coder add the feature", "", coder)
	l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")
	c, _ := l.s.GetMember(l.ctx, coder.ID)
	gitIn(t, c.WorkDir, "add", "-A")
	gitIn(t, c.WorkDir, "commit", "-q", "-m", "Add the feature")
	l.say("@Tester test it", "", tester)
	l.waitTurns(3, store.TurnDone, "Tester's turn")

	// Tester builds on Coder's work, as its brief says to.
	tt, _ := l.s.GetMember(l.ctx, tester.ID)
	gitIn(t, tt.WorkDir, "merge", "-q", "--no-edit", "veyloom/coder")
	l.say("@Tester more tests", "", tester)
	l.waitTurns(4, store.TurnDone, "Tester's second turn")
	l.h.turns.overlapChecks.Wait()
	for _, msg := range l.topLevel() {
		if msg.SenderKind == store.SenderSystem && strings.HasSuffix(msg.Body, "whichever is merged second may conflict.") {
			t.Errorf("told of an overlap: %s", msg.Body)
		}
	}

	b, err := l.h.Branches(l.ctx, l.room.ProjectID)
	if err != nil || len(b.Members) != 2 {
		t.Fatalf("the branches: %+v %v", b, err)
	}
	if !slices.Equal(b.Members[1].Contains, []string{coder.ID}) || len(b.Members[0].Contains) != 0 || len(b.Overlaps) != 0 {
		t.Errorf("Tester has Coder's work: %v %v, overlaps %+v", b.Members[1].Contains, b.Members[0].Contains, b.Overlaps)
	}

	// Coder's work, changed again and not committed, is no longer all in
	// Tester's branch; yet only Coder changed README.md since Tester took
	// its work in, so the two do not overlap there. Once Tester changes it
	// too, its own way, they do.
	if err := os.WriteFile(filepath.Join(c.WorkDir, "README.md"), []byte("# app\n\nMore.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ = l.h.Branches(l.ctx, l.room.ProjectID)
	if len(b.Members[1].Contains) != 0 || len(b.Overlaps) != 0 {
		t.Errorf("after Coder's new change: %v, overlaps %+v", b.Members[1].Contains, b.Overlaps)
	}
	if err := os.WriteFile(filepath.Join(tt.WorkDir, "README.md"), []byte("# app\n\nTested.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ = l.h.Branches(l.ctx, l.room.ProjectID)
	if len(b.Overlaps) != 1 || b.Overlaps[0].Path != "README.md" || !slices.Equal(b.Overlaps[0].Members, []string{coder.ID, tester.ID}) {
		t.Errorf("after Tester's change of the same file: %+v", b.Overlaps)
	}
}

func TestHolds(t *testing.T) {
	a := &worktree.Status{Commits: []worktree.Commit{{Hash: "a1"}, {Hash: "a2"}}}
	for _, c := range []struct {
		name string
		b, a *worktree.Status
		want bool
	}{
		{"its latest commit merged", &worktree.Status{Commits: []worktree.Commit{{Hash: "a1"}, {Hash: "a2"}, {Hash: "b1"}}}, a, true},
		{"an older one only", &worktree.Status{Commits: []worktree.Commit{{Hash: "a1"}, {Hash: "b1"}}}, a, false},
		{"work left uncommitted", &worktree.Status{Commits: []worktree.Commit{{Hash: "a2"}}}, &worktree.Status{Commits: a.Commits, Uncommitted: 1}, false},
		{"no commit to hold", &worktree.Status{Commits: []worktree.Commit{{Hash: "b1"}}}, &worktree.Status{Uncommitted: 2}, false},
		{"not known", nil, a, false},
	} {
		if got := holds(c.b, c.a); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
