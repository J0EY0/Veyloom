package worktree_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/worktree"
)

func TestStatusAndDiff(t *testing.T) {
	r := newRepo(t)
	ws := r.create("demo/coder", "veyloom/coder")
	ctx := context.Background()
	if st, err := r.g.Status(ctx, r.dir, ws.Dir); err != nil || st.Branch != "veyloom/coder" || st.Base != "main" || st.Ahead+st.Behind+len(st.Files) != 0 {
		t.Fatalf("a new worktree: %+v %v", st, err)
	}

	// Committed, not committed, and new: all of it counts.
	r.write(ws.Dir, "b.txt", "bee\nhive\n")
	r.commit(ws.Dir, "hive")
	r.write(ws.Dir, "a.txt", "one\nTWO\nthree\n")
	r.write(ws.Dir, "new.txt", "brand new\n")
	// The main line moves on meanwhile.
	r.write(r.dir, "c.txt", "sea\n")
	r.commit(r.dir, "sea")

	st, err := r.g.Status(ctx, r.dir, ws.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Ahead != 1 || st.Behind != 1 || st.Uncommitted != 2 {
		t.Errorf("counts: %+v", st)
	}
	// Its own commit, by what its message says; not the main line's.
	if len(st.Commits) != 1 || st.Commits[0].Subject != "hive" || st.Commits[0].Hash == "" {
		t.Errorf("commits: %+v", st.Commits)
	}
	// b.txt committed; a.txt and new.txt not (the star).
	if want := []string{"A* new.txt", "M b.txt", "M* a.txt"}; !slices.Equal(paths(st.Files), want) {
		t.Errorf("files %v, want %v", paths(st.Files), want)
	}
	for _, f := range st.Files {
		if f.Path == "b.txt" && (f.Added != 1 || f.Deleted != 0) {
			t.Errorf("b.txt counts: %+v", f)
		}
	}
	// What the main line did since is none of the worktree's.
	for _, f := range st.Files {
		if f.Path == "c.txt" {
			t.Error("the main line's own change is listed")
		}
	}
	// The worktree's own index is as it was.
	if staged := r.git(ws.Dir, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("the worktree's index was touched: %q", staged)
	}

	patch, cut, err := r.g.Diff(ctx, r.dir, ws.Dir, 0)
	if err != nil || cut {
		t.Fatal(err, cut)
	}
	for _, want := range []string{"+TWO", "+hive", "+brand new", "new file mode"} {
		if !strings.Contains(patch, want) {
			t.Errorf("the patch lacks %q:\n%s", want, patch)
		}
	}
	short, cut, _ := r.g.Diff(ctx, r.dir, ws.Dir, 40)
	if !cut || len(short) > 40 {
		t.Errorf("cut to 40 bytes: %v %q", cut, short)
	}
}

// A worktree's own commits are named by their messages, oldest first; the
// main line merged in is not one of them.
func TestStatusNamesOwnCommits(t *testing.T) {
	r := newRepo(t)
	ws := r.create("demo/coder", "veyloom/coder")
	ctx := context.Background()
	r.write(ws.Dir, "bees.txt", "bee\n")
	r.git(ws.Dir, "add", "-A")
	r.git(ws.Dir, "commit", "-q", "-m", "Add bees", "-m", "They make honey.")
	r.write(r.dir, "c.txt", "sea\n")
	r.commit(r.dir, "sea")
	r.git(ws.Dir, "merge", "-q", "--no-edit", "main")
	r.write(ws.Dir, "bees.txt", "bee\nhive\n")
	r.commit(ws.Dir, "Add a hive")

	st, err := r.g.Status(ctx, r.dir, ws.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Commits) != 2 || st.Commits[0].Subject != "Add bees" || st.Commits[0].Body != "They make honey." || st.Commits[1].Subject != "Add a hive" || st.Commits[1].Body != "" {
		t.Errorf("commits: %+v", st.Commits)
	}
}

func TestSquash(t *testing.T) {
	r := newRepo(t)
	ws := r.create("demo/coder", "veyloom/coder")
	ctx := context.Background()
	if _, err := r.g.Squash(ctx, r.dir, ws.Dir, "nothing", nil); !errors.Is(err, worktree.ErrNoChanges) {
		t.Errorf("nothing to put on the main line: %v", err)
	}

	r.write(ws.Dir, "b.txt", "bee\nhive\n")
	r.commit(ws.Dir, "hive")
	r.write(ws.Dir, "a.txt", "one\nTWO\nthree\n")
	r.write(ws.Dir, "new.txt", "brand new\n")
	// The main line has moved on, elsewhere.
	r.write(r.dir, "c.txt", "sea\n")
	before := r.commit(r.dir, "sea")

	got, err := r.g.Squash(ctx, r.dir, ws.Dir, "Add the hive (Coder)", nil)
	if err != nil || got.Commit == "" || len(got.Conflicts) != 0 {
		t.Fatalf("squash: %+v %v", got, err)
	}
	// One commit on the main line, on top of it, saying what the person said.
	if head := r.git(r.dir, "rev-parse", "HEAD"); head != got.Commit {
		t.Errorf("the main line is at %s, want %s", head, got.Commit)
	}
	if parent := r.git(r.dir, "rev-parse", "HEAD^"); parent != before {
		t.Errorf("the new commit's parent is %s, want the main line's %s", parent, before)
	}
	if msg := r.git(r.dir, "log", "-1", "--format=%s%n%an"); msg != "Add the hive (Coder)\nAlice" {
		t.Errorf("the commit says %q", msg)
	}
	// The person's checkout has it all, clean.
	if r.read(r.dir, "new.txt") != "brand new\n" || r.read(r.dir, "a.txt") != "one\nTWO\nthree\n" || r.git(r.dir, "status", "--porcelain") != "" {
		t.Error("the checkout does not have the work")
	}
	// The member starts over from the new main line.
	if st, err := r.g.Status(ctx, r.dir, ws.Dir); err != nil || st.Ahead+st.Behind+len(st.Files) != 0 {
		t.Errorf("after the squash: %+v %v", st, err)
	}
	if r.read(ws.Dir, "c.txt") != "sea\n" {
		t.Error("the worktree lacks the main line's own change")
	}
}

func TestSquash_Refused(t *testing.T) {
	r := newRepo(t)
	ws := r.create("demo/coder", "veyloom/coder")
	ctx := context.Background()

	// Both changed the same line: nothing moves, the file is named.
	r.write(ws.Dir, "a.txt", "one\nmember\nthree\n")
	r.write(r.dir, "a.txt", "one\nperson\nthree\n")
	main := r.commit(r.dir, "person")
	got, err := r.g.Squash(ctx, r.dir, ws.Dir, "clash", nil)
	if err != nil || got.Commit != "" || !slices.Equal(got.Conflicts, []string{"a.txt"}) {
		t.Fatalf("a conflict: %+v %v", got, err)
	}
	if r.git(r.dir, "rev-parse", "HEAD") != main || r.read(ws.Dir, "a.txt") != "one\nmember\nthree\n" {
		t.Error("a conflict changed something")
	}

	// The person's own uncommitted change in the way: git refuses, and the
	// main line stays.
	r.write(ws.Dir, "a.txt", "one\nperson\nthree\n")
	r.write(ws.Dir, "b.txt", "bee\nmember\n")
	r.write(r.dir, "b.txt", "bee\nlocal\n")
	if _, err := r.g.Squash(ctx, r.dir, ws.Dir, "in the way", nil); err == nil || !strings.Contains(err.Error(), "b.txt") {
		t.Errorf("uncommitted changes in the way: %v", err)
	}
	if r.git(r.dir, "rev-parse", "HEAD") != main || r.read(r.dir, "b.txt") != "bee\nlocal\n" {
		t.Error("the refused squash changed the checkout")
	}

	// A checkout on no branch has no main line to put work on.
	r.git(r.dir, "checkout", "-q", "b.txt")
	r.git(r.dir, "checkout", "-q", "--detach")
	if _, err := r.g.Squash(ctx, r.dir, ws.Dir, "detached", nil); !errors.Is(err, worktree.ErrDetached) {
		t.Errorf("detached: %v", err)
	}
}

func TestSync(t *testing.T) {
	r := newRepo(t)
	ws := r.create("demo/coder", "veyloom/coder")
	ctx := context.Background()
	if got, err := r.g.Sync(ctx, r.dir, ws.Dir, true); err != nil || got.Updated || got.Skipped {
		t.Errorf("up to date already: %+v %v", got, err)
	}

	// Nothing of its own: brought forward, even when only that is asked.
	r.write(r.dir, "c.txt", "sea\n")
	main := r.commit(r.dir, "sea")
	if got, err := r.g.Sync(ctx, r.dir, ws.Dir, true); err != nil || !got.Updated {
		t.Fatalf("fast-forward: %+v %v", got, err)
	}
	if r.git(ws.Dir, "rev-parse", "HEAD") != main {
		t.Error("the worktree is not at the main line")
	}

	// Work of its own: left alone when only a fast-forward is asked,
	// merged otherwise, the uncommitted work kept.
	r.write(ws.Dir, "b.txt", "bee\nhive\n")
	r.write(r.dir, "d.txt", "dee\n")
	main = r.commit(r.dir, "dee")
	if got, err := r.g.Sync(ctx, r.dir, ws.Dir, true); err != nil || !got.Skipped {
		t.Errorf("work of its own: %+v %v", got, err)
	}
	if got, err := r.g.Sync(ctx, r.dir, ws.Dir, false); err != nil || !got.Updated {
		t.Fatalf("merge: %+v %v", got, err)
	}
	if r.read(ws.Dir, "d.txt") != "dee\n" || r.read(ws.Dir, "b.txt") != "bee\nhive\n" {
		t.Error("the merge lost something")
	}

	// A conflict: given up, the files named, the worktree as it was.
	r.write(ws.Dir, "a.txt", "one\nmember\nthree\n")
	mine := r.commit(ws.Dir, "member")
	r.write(r.dir, "a.txt", "one\nperson\nthree\n")
	r.commit(r.dir, "person")
	got, err := r.g.Sync(ctx, r.dir, ws.Dir, false)
	if err != nil || !slices.Equal(got.Conflicts, []string{"a.txt"}) || got.Updated {
		t.Fatalf("a conflict: %+v %v", got, err)
	}
	if r.git(ws.Dir, "rev-parse", "HEAD") != mine || r.read(ws.Dir, "b.txt") != "bee\nhive\n" {
		t.Error("the given-up merge changed the worktree")
	}
	if st, _ := r.g.Status(ctx, r.dir, ws.Dir); st.Merging {
		t.Error("a merge is still under way")
	}
}
