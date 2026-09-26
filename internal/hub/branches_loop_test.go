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
)

// The branch tab's view and actions, on real worktrees (docs/design.md
// 5.21): how each member's work stands, the files two of them changed, its
// patch, merging it onto the main line as one commit, bringing the main
// line into another.
func TestLoop_BranchesMergeAndSync(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{RepoPath: &repo}); err != nil {
		t.Fatal(err)
	}
	l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done."})
	tester := l.memberIn("Tester", repo, store.PermissionFullAuto, map[string]any{"reply": "Done."})
	l.say("@Coder add the feature", "", coder)
	l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")
	l.say("@Tester write the tests", "", tester)
	turns := l.waitTurns(3, store.TurnDone, "Tester's turn")
	// Tester is told where Coder's work is, and how to build on it.
	if prompt := specOf(t, turns[0]).Prompt; !strings.Contains(prompt, "The others' work is on their branches of the same repository: veyloom/coder (Coder). To build on what one of them committed, merge its branch into yours (git merge veyloom/coder, say)") {
		t.Errorf("Tester's brief:\n%s", prompt)
	}

	c, _ := l.s.GetMember(l.ctx, coder.ID)
	tt, _ := l.s.GetMember(l.ctx, tester.ID)
	write := func(dir, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(c.WorkDir, "feature.go", "package feature\n")
	write(c.WorkDir, "README.md", "# app\n\nWith a feature.\n")
	write(tt.WorkDir, "README.md", "# app\n\nWith tests.\n")

	b, err := l.h.Branches(l.ctx, l.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if !b.Main.Git || b.Main.Branch != "main" || b.Main.RepoPath != repo || len(b.Members) != 2 {
		t.Fatalf("the branches: %+v", b)
	}
	first := b.Members[0]
	if first.MemberID != coder.ID || first.Branch != "veyloom/coder" || !first.Prepared || first.Busy || first.Status == nil || first.Draft != "add the feature" {
		t.Errorf("Coder's branch: %+v", first)
	}
	if first.Status != nil && (first.Status.Uncommitted != 2 || len(first.Status.Files) != 2) {
		t.Errorf("Coder's changes: %+v", first.Status)
	}
	if len(b.Overlaps) != 1 || b.Overlaps[0].Path != "README.md" || !slices.Equal(b.Overlaps[0].Members, []string{coder.ID, tester.ID}) {
		t.Errorf("overlaps: %+v", b.Overlaps)
	}

	patch, _, err := l.h.DiffOf(l.ctx, coder.ID)
	if err != nil || !strings.Contains(patch, "+package feature") || !strings.Contains(patch, "+With a feature.") {
		t.Errorf("Coder's patch: %v\n%s", err, patch)
	}

	// Coder's work goes on the main line, and the chat says so.
	if _, err := l.h.Merge(l.ctx, coder.ID, "  ", nil); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a merge with no message: %v", err)
	}
	merged, err := l.h.Merge(l.ctx, coder.ID, "Add the feature", nil)
	if err != nil || merged.Commit == "" {
		t.Fatalf("merge: %+v %v", merged, err)
	}
	if body, err := os.ReadFile(filepath.Join(repo, "feature.go")); err != nil || string(body) != "package feature\n" {
		t.Errorf("the checkout lacks the work: %q %v", body, err)
	}
	log := exec.Command("git", "log", "-1", "--format=%s")
	log.Dir = repo
	if out, _ := log.Output(); strings.TrimSpace(string(out)) != "Add the feature" {
		t.Errorf("the main line's last commit: %q", out)
	}
	found := false
	for _, msg := range l.topLevel() {
		if msg.SenderKind == store.SenderSystem && strings.HasPrefix(msg.Body, "Merged Coder's work into the main line as "+merged.Commit[:7]) {
			found = true
		}
	}
	if !found {
		t.Error("the chat does not say Coder's work was merged")
	}

	// Tester's changes clash with what is on the main line now.
	b, _ = l.h.Branches(l.ctx, l.room.ProjectID)
	if st := b.Members[0].Status; st == nil || st.Ahead+len(st.Files) != 0 {
		t.Errorf("Coder after the merge: %+v", st)
	}
	if st := b.Members[1].Status; st == nil || st.Behind != 1 {
		t.Errorf("Tester after the merge: %+v", st)
	}
	clash, err := l.h.Merge(l.ctx, tester.ID, "Tests", nil)
	if err != nil || clash.Commit != "" || !slices.Equal(clash.Conflicts, []string{"README.md"}) {
		t.Errorf("a clashing merge: %+v %v", clash, err)
	}
	// Uncommitted, the change is in the way of bringing the main line in;
	// committed, the merge conflicts and is given up.
	if _, err := l.h.SyncMember(l.ctx, tester.ID); !errors.Is(err, store.ErrConflict) {
		t.Errorf("sync over uncommitted changes: %v", err)
	}
	commit := exec.Command("git", "commit", "-qam", "tests")
	commit.Dir = tt.WorkDir
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	synced, err := l.h.SyncMember(l.ctx, tester.ID)
	if err != nil || !slices.Equal(synced.Conflicts, []string{"README.md"}) {
		t.Errorf("a clashing sync: %+v %v", synced, err)
	}

	// A member with no worktree has nothing to merge.
	lead, _ := l.s.GetProject(l.ctx, l.room.ProjectID)
	if _, err := l.h.Merge(l.ctx, lead.LeaderID, "no", nil); !errors.Is(err, store.ErrConflict) {
		t.Errorf("the leader's merge: %v", err)
	}
}

// The merge is drafted from what the member's own commits say, when it
// made any: better than what it was asked.
func TestLoop_TheMergeDraftSaysWhatTheCommitsSay(t *testing.T) {
	l := newLoop(t)
	repo := gitCheckout(t)
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{RepoPath: &repo}); err != nil {
		t.Fatal(err)
	}
	l.memberIn("Lead", repo, store.PermissionFullAuto, setupCall(nil, ""))
	coder := l.memberIn("Coder", repo, store.PermissionFullAuto, map[string]any{"reply": "Done."})
	l.say("@Coder 请实现这个功能，改动两个文件：", "", coder)
	l.waitTurns(2, store.TurnDone, "the setup and Coder's turn")
	c, _ := l.s.GetMember(l.ctx, coder.ID)
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = c.WorkDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(c.WorkDir, "feature.go"), []byte("package feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "Add the feature", "-m", "It does the thing.")
	b, err := l.h.Branches(l.ctx, l.room.ProjectID)
	if err != nil || len(b.Members) != 1 || b.Members[0].Draft != "Add the feature\n\nIt does the thing." {
		t.Fatalf("one commit: %+v %v", b.Members, err)
	}
	if err := os.WriteFile(filepath.Join(c.WorkDir, "feature_test.go"), []byte("package feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "Test the feature")
	if b, err = l.h.Branches(l.ctx, l.room.ProjectID); err != nil || b.Members[0].Draft != "Add the feature\n\n- Test the feature" {
		t.Errorf("two commits: %q %v", b.Members[0].Draft, err)
	}
}
