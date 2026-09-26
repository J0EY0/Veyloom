package worktree_test

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/worktree"
)

// startMerge has the member at ws merge the main line in, as an agent
// trusted with git does, stopping on the conflict in a.txt.
func startMerge(r *repo, ws worktree.Workspace, mine, theirs string) {
	r.t.Helper()
	r.write(ws.Dir, "a.txt", "one\n"+mine+"\nthree\n")
	r.commit(ws.Dir, mine)
	r.write(r.dir, "a.txt", "one\n"+theirs+"\nthree\n")
	r.commit(r.dir, theirs)
	merge := exec.Command("git", "merge", "-q", "main")
	merge.Dir = ws.Dir
	if out, err := merge.CombinedOutput(); err == nil || !strings.Contains(string(out), "CONFLICT") {
		r.t.Fatalf("the merge should stop on a conflict: %v\n%s", err, out)
	}
}

// A merge a member left under way: nothing goes onto the main line or
// comes in meanwhile; it is committed once settled, named while not, and
// can be given up (docs/design.md 5.21).
func TestConclude(t *testing.T) {
	r := newRepo(t)
	ws := r.create("demo/coder", "veyloom/coder")
	ctx := context.Background()
	if got, err := r.g.Conclude(ctx, ws.Dir); err != nil || got.Commit != "" || got.Unresolved != nil {
		t.Errorf("no merge under way: %+v %v", got, err)
	}

	startMerge(r, ws, "member", "person")
	st, err := r.g.Status(ctx, r.dir, ws.Dir)
	if err != nil || !st.Merging || !slices.Equal(st.Conflicts, []string{"a.txt"}) {
		t.Fatalf("the merge under way: %+v %v", st, err)
	}
	if _, err := r.g.Squash(ctx, r.dir, ws.Dir, "Merge it", nil); !errors.Is(err, worktree.ErrMergeUnderway) {
		t.Errorf("squash during a merge: %v", err)
	}
	if _, err := r.g.Sync(ctx, r.dir, ws.Dir, false); !errors.Is(err, worktree.ErrMergeUnderway) {
		t.Errorf("sync during a merge: %v", err)
	}
	// Before a turn, the worktree is only left be: the member's merge is
	// not given up for it.
	if got, err := r.g.Sync(ctx, r.dir, ws.Dir, true); err != nil || !got.Skipped {
		t.Errorf("fast-forward during a merge: %+v %v", got, err)
	}
	if got, err := r.g.Conclude(ctx, ws.Dir); err != nil || got.Commit != "" || !slices.Equal(got.Unresolved, []string{"a.txt"}) {
		t.Errorf("conflict markers left: %+v %v", got, err)
	}
	if st, _ := r.g.Status(ctx, r.dir, ws.Dir); !st.Merging {
		t.Fatal("the merge was given up")
	}

	// Settled in the file, not committed: committed for the member, the
	// main line one of its parents, and the work goes on.
	r.write(ws.Dir, "a.txt", "one\nmember and person\nthree\n")
	got, err := r.g.Conclude(ctx, ws.Dir)
	if err != nil || got.Commit == "" || got.Unresolved != nil {
		t.Fatalf("settled: %+v %v", got, err)
	}
	if parents := strings.Fields(r.git(ws.Dir, "rev-list", "--parents", "-n", "1", "HEAD")); len(parents) != 3 || parents[0] != got.Commit {
		t.Errorf("the merge commit's parents: %v", parents)
	}
	if st, _ := r.g.Status(ctx, r.dir, ws.Dir); st.Merging || st.Behind != 0 || st.Uncommitted != 0 {
		t.Errorf("after the merge: %+v", st)
	}
	merged, err := r.g.Squash(ctx, r.dir, ws.Dir, "Settle a.txt", nil)
	if err != nil || merged.Commit == "" || r.read(r.dir, "a.txt") != "one\nmember and person\nthree\n" {
		t.Fatalf("onto the main line: %+v %v", merged, err)
	}

	// Marked settled with the markers still in: named all the same.
	startMerge(r, ws, "again", "not again")
	r.git(ws.Dir, "add", "a.txt")
	if got, err := r.g.Conclude(ctx, ws.Dir); err != nil || !slices.Equal(got.Unresolved, []string{"a.txt"}) {
		t.Errorf("markers marked settled: %+v %v", got, err)
	}

	// Given up: as it was before the merge; again, nothing to do.
	before := r.git(ws.Dir, "rev-parse", "HEAD")
	if err := r.g.AbortMerge(ctx, ws.Dir); err != nil {
		t.Fatal(err)
	}
	if st, _ := r.g.Status(ctx, r.dir, ws.Dir); st.Merging || r.git(ws.Dir, "rev-parse", "HEAD") != before || r.read(ws.Dir, "a.txt") != "one\nagain\nthree\n" {
		t.Errorf("given up: %+v", st)
	}
	if err := r.g.AbortMerge(ctx, ws.Dir); err != nil {
		t.Errorf("nothing to give up: %v", err)
	}
}
