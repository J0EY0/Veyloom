package worktree_test

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/worktree"
)

// Work squashed onto the main line is on it for every member that merged
// it: counted, diffed and merged as the main line's, not again as the
// member's own. The person's git sees one commit.
func TestSquashedWorkIsFollowed(t *testing.T) {
	r := newRepo(t)
	coder := r.create("demo/coder", "veyloom/coder")
	tester := r.create("demo/tester", "veyloom/tester")
	reviewer := r.create("demo/reviewer", "veyloom/reviewer")
	ctx := context.Background()

	r.write(coder.Dir, "a.txt", "one\ntwo\nthree\nfour\n")
	r.commit(coder.Dir, "Add four")
	// Tester builds on it, changing the same line; Reviewer only reads it.
	r.git(tester.Dir, "merge", "-q", "veyloom/coder")
	r.write(tester.Dir, "a.txt", "one\ntwo\nthree\nFOUR\n")
	r.write(tester.Dir, "t.txt", "tested\n")
	r.commit(tester.Dir, "Test four")
	r.git(reviewer.Dir, "merge", "-q", "veyloom/coder")

	squashed, err := r.g.Squash(ctx, r.dir, coder.Dir, "Add the fourth line", nil)
	if err != nil || squashed.Commit == "" {
		t.Fatalf("squash Coder: %+v %v", squashed, err)
	}
	if parents := strings.Fields(r.git(r.dir, "rev-list", "--parents", "-n", "1", "HEAD")); len(parents) != 2 {
		t.Errorf("the person's git sees parents %v, want the main line's alone", parents[1:])
	}

	st, err := r.g.Status(ctx, r.dir, tester.Dir)
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, c := range st.Commits {
		subjects = append(subjects, c.Subject)
	}
	if st.Ahead != 1 || st.Behind != 1 || !slices.Equal(subjects, []string{"Test four"}) || !slices.Equal(paths(st.Files), []string{"A t.txt", "M a.txt"}) {
		t.Errorf("Tester after Coder's merge: ahead %d, behind %d, commits %v, files %v", st.Ahead, st.Behind, subjects, paths(st.Files))
	}
	// The agents' git reads the merge as Coder's commit merged.
	if got := r.git(r.dir, "log", "--format=%s", "main"); got != "Add the fourth line\nfirst" {
		t.Errorf("the person's log:\n%s", got)
	}
	agents := exec.Command("git", "log", "--topo-order", "--format=%s", "main")
	agents.Dir, agents.Env = r.dir, append(os.Environ(), worktree.AgentEnv...)
	if got, _ := agents.Output(); strings.TrimSpace(string(got)) != "Add the fourth line\nAdd four\nfirst" {
		t.Errorf("the agents' log:\n%s", got)
	}
	if patch, _, _ := r.g.Diff(ctx, r.dir, tester.Dir, 0); strings.Contains(patch, "+four") {
		t.Errorf("Tester's patch has Coder's line again:\n%s", patch)
	}

	// Reviewer had nothing of its own: it follows the main line.
	if st, err := r.g.Status(ctx, r.dir, reviewer.Dir); err != nil || st.Ahead != 0 || len(st.Files) != 0 {
		t.Errorf("Reviewer after Coder's merge: %+v %v", st, err)
	}
	if got, err := r.g.Sync(ctx, r.dir, reviewer.Dir, true); err != nil || !got.Updated {
		t.Errorf("Reviewer brought forward: %+v %v", got, err)
	}

	// Tester takes the main line in, and is merged, without conflict.
	if got, err := r.g.Sync(ctx, r.dir, tester.Dir, false); err != nil || !got.Updated || len(got.Conflicts) != 0 {
		t.Fatalf("Tester takes the main line in: %+v %v", got, err)
	}
	merged, err := r.g.Squash(ctx, r.dir, tester.Dir, "Test the fourth line", nil)
	if err != nil || len(merged.Conflicts) != 0 {
		t.Fatalf("squash Tester: %+v %v", merged, err)
	}
	if r.read(r.dir, "a.txt") != "one\ntwo\nthree\nFOUR\n" || r.read(r.dir, "t.txt") != "tested\n" {
		t.Error("the main line lacks Tester's work")
	}
	if got := r.git(r.dir, "log", "--format=%s", "main"); got != "Test the fourth line\nAdd the fourth line\nfirst" {
		t.Errorf("the person's log:\n%s", got)
	}
}

// One commit merged with its own message in the same second is squashed
// into that very commit: the main line then has it, and needs no graft,
// which would make it its own ancestor.
func TestSquashOfTheSameCommit(t *testing.T) {
	t.Setenv("GIT_AUTHOR_DATE", "2026-09-25T10:00:00Z")
	t.Setenv("GIT_COMMITTER_DATE", "2026-09-25T10:00:00Z")
	r := newRepo(t)
	coder := r.create("demo/coder", "veyloom/coder")
	reviewer := r.create("demo/reviewer", "veyloom/reviewer")
	ctx := context.Background()
	r.write(coder.Dir, "a.txt", "one\ntwo\nthree\nfour\n")
	own := r.commit(coder.Dir, "Add four")
	r.git(reviewer.Dir, "merge", "-q", "veyloom/coder")

	got, err := r.g.Squash(ctx, r.dir, coder.Dir, "Add four", nil)
	if err != nil || got.Commit != own {
		t.Fatalf("the squash: %+v %v, want the commit %s itself", got, err, own)
	}
	if refs := r.git(r.dir, "for-each-ref", worktree.ReplaceRefBase); refs != "" {
		t.Errorf("a graft of the commit onto itself: %s", refs)
	}
	if st, err := r.g.Status(ctx, r.dir, reviewer.Dir); err != nil || st.Ahead+st.Behind+len(st.Files) != 0 {
		t.Errorf("Reviewer, which has the commit: %+v %v", st, err)
	}
}

// A graft is written only for work that went on: a merge that the
// checkout's changes stop leaves none behind.
func TestSquashRefusedLeavesNoGraft(t *testing.T) {
	r := newRepo(t)
	ws := r.create("demo/coder", "veyloom/coder")
	r.write(ws.Dir, "b.txt", "bee\nmember\n")
	r.write(r.dir, "b.txt", "bee\nlocal\n")
	if _, err := r.g.Squash(context.Background(), r.dir, ws.Dir, "in the way", nil); err == nil {
		t.Fatal("the merge went on over the checkout's changes")
	}
	if refs := r.git(r.dir, "for-each-ref", worktree.ReplaceRefBase); refs != "" {
		t.Errorf("grafts left: %s", refs)
	}
}

// Work two members both have, one having merged it from the other, is no
// overlap, even after both went on; a file both changed their own ways is,
// committed or not, and one both changed alike is not.
func TestOverlap(t *testing.T) {
	r := newRepo(t)
	coder := r.create("demo/coder", "veyloom/coder")
	tester := r.create("demo/tester", "veyloom/tester")
	ctx := context.Background()
	r.write(coder.Dir, "a.txt", "one\ntwo\nthree\nfour\n")
	r.commit(coder.Dir, "Add four")
	r.git(tester.Dir, "merge", "-q", "veyloom/coder")
	// Both go on: Coder fixes its work, Tester tests it.
	r.write(coder.Dir, "b.txt", "bee\nfixed\n")
	r.commit(coder.Dir, "Fix")
	r.write(tester.Dir, "t.txt", "tested\n")
	r.commit(tester.Dir, "Test")

	overlap := func() []string {
		t.Helper()
		a, err := r.g.Status(ctx, r.dir, coder.Dir)
		if err != nil {
			t.Fatal(err)
		}
		b, err := r.g.Status(ctx, r.dir, tester.Dir)
		if err != nil {
			t.Fatal(err)
		}
		got, err := r.g.Overlap(ctx, r.dir, a.At, b.At)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := overlap(); len(got) != 0 {
		t.Errorf("work one merged from the other: %v", got)
	}
	r.write(coder.Dir, "c.txt", "Coder's\n")
	r.write(tester.Dir, "c.txt", "Tester's\n")
	r.write(coder.Dir, "d.txt", "alike\n")
	r.write(tester.Dir, "d.txt", "alike\n")
	if got := overlap(); !slices.Equal(got, []string{"c.txt"}) {
		t.Errorf("files changed on both sides: %v", got)
	}
}
