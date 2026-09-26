package worktree_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/worktree"
)

// New files a person leaves out of a merge stay in the worktree, as they
// were, staged or not, even where the main line has a file of its own;
// only new files can be left out, since the worktree starts over.
func TestSquashLeavesNewFilesOut(t *testing.T) {
	r := newRepo(t)
	ws := r.create("demo/coder", "veyloom/coder")
	ctx := context.Background()
	r.write(ws.Dir, "c.txt", "sea\n")
	r.commit(ws.Dir, "sea")
	r.write(ws.Dir, "feature.txt", "the work\n")
	r.write(ws.Dir, "app", "\x00\x01built\n")
	r.write(ws.Dir, "links.json", "{}\n")
	r.git(ws.Dir, "add", "links.json")
	// The main line got a notes.md of its own meanwhile.
	r.write(ws.Dir, "notes.md", "the member's notes\n")
	r.write(r.dir, "notes.md", "the person's notes\n")
	r.commit(r.dir, "notes")

	st, err := r.g.Status(ctx, r.dir, ws.Dir)
	if err != nil {
		t.Fatal(err)
	}
	var fresh []string
	for _, f := range st.Files {
		if f.New {
			fresh = append(fresh, f.Path)
		}
	}
	if slices.Sort(fresh); !slices.Equal(fresh, []string{"app", "feature.txt", "links.json", "notes.md"}) {
		t.Errorf("the new files: %v", fresh)
	}

	var notNew *worktree.NotNewError
	if _, err := r.g.Squash(ctx, r.dir, ws.Dir, "no", []string{"app", "c.txt"}); !errors.As(err, &notNew) || !slices.Equal(notNew.Files, []string{"c.txt"}) {
		t.Fatalf("leaving out a committed file: %v", err)
	}
	got, err := r.g.Squash(ctx, r.dir, ws.Dir, "Add the feature", []string{"app", "links.json", "notes.md"})
	if err != nil || got.Commit == "" || got.Unsettled != "" {
		t.Fatalf("the merge: %+v %v", got, err)
	}
	if files := r.git(r.dir, "show", "--name-only", "--format=", "HEAD"); files != "c.txt\nfeature.txt" {
		t.Errorf("the merge has:\n%s", files)
	}
	if r.read(r.dir, "notes.md") != "the person's notes\n" {
		t.Error("the main line's notes.md was changed")
	}
	// Left out, they stay as they were, and are the member's still.
	for name, body := range map[string]string{"app": "\x00\x01built\n", "links.json": "{}\n", "notes.md": "the member's notes\n"} {
		if r.read(ws.Dir, name) != body {
			t.Errorf("%s in the worktree: %q", name, r.read(ws.Dir, name))
		}
	}
	st, _ = r.g.Status(ctx, r.dir, ws.Dir)
	if want := []string{"A* app", "A* links.json", "M* notes.md"}; st.Ahead != 0 || !slices.Equal(paths(st.Files), want) {
		t.Errorf("the worktree after: ahead %d, files %v, want %v", st.Ahead, paths(st.Files), want)
	}
	if left, _ := filepath.Glob(filepath.Join(ws.Dir, ".veyloom-left-*")); len(left) > 0 {
		t.Errorf("left behind: %v", left)
	}
}

// Work set aside is kept under a ref of Veyloom's, committed or not, and
// the worktree starts over from the main line, keeping its own files.
func TestSetAside(t *testing.T) {
	r := newRepo(t)
	ws := r.create("demo/reviewer", "veyloom/reviewer")
	ctx := context.Background()
	if _, err := r.g.Prepare(ctx, r.dir, ws.WorkDir, nil, "echo ready > ready.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.g.SetAside(ctx, r.dir, ws.Dir, "nothing"); !errors.Is(err, worktree.ErrNoChanges) {
		t.Errorf("nothing to set aside: %v", err)
	}
	r.write(ws.Dir, "x.txt", "committed\n")
	r.git(ws.Dir, "add", "x.txt")
	r.git(ws.Dir, "commit", "-q", "-m", "x")
	r.write(ws.Dir, "a.txt", "one\nchanged\nthree\n")
	r.write(ws.Dir, "build/app", "built\n")

	ref, err := r.g.SetAside(ctx, r.dir, ws.Dir, "What Reviewer had")
	if err != nil || !strings.HasPrefix(ref, worktree.SetAsideRefs+"veyloom/reviewer/") {
		t.Fatalf("set aside: %q %v", ref, err)
	}
	if kept := r.git(r.dir, "show", "--name-only", "--format=%s", ref); kept != "What Reviewer had\n\na.txt\nbuild/app" {
		t.Errorf("kept:\n%s", kept)
	}
	if r.git(r.dir, "rev-parse", ref+"^") != r.git(ws.Dir, "rev-parse", "HEAD@{1}") {
		t.Error("the work kept does not follow the member's commit")
	}
	st, err := r.g.Status(ctx, r.dir, ws.Dir)
	if err != nil || st.Ahead+st.Behind+len(st.Files) != 0 {
		t.Errorf("after: %+v %v", st, err)
	}
	if _, err := os.Stat(filepath.Join(ws.Dir, "build")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the build is still there: %v", err)
	}
	if r.read(ws.Dir, "ready.txt") != "ready\n" {
		t.Error("the worktree's own file went")
	}
}
