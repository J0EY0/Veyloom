package worktree_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/worktree"
)

// Changes in the checkout not committed, in files the merge writes, stop
// it in so many words, naming them; changes elsewhere there do not.
func TestSquashNamesTheChangesInTheWay(t *testing.T) {
	r := newRepo(t)
	ws := r.create("demo/coder", "veyloom/coder")
	ctx := context.Background()
	main := r.git(r.dir, "rev-parse", "HEAD")

	r.write(ws.Dir, "b.txt", "bee\nmember\n")
	r.write(ws.Dir, "new.txt", "the member's\n")
	r.write(r.dir, "b.txt", "bee\nlocal\n")
	r.write(r.dir, "new.txt", "the person's\n")
	r.write(r.dir, "a.txt", "one\nTWO\nthree\n")
	_, err := r.g.Squash(ctx, r.dir, ws.Dir, "in the way", nil)
	var changed *worktree.CheckoutChangedError
	if !errors.As(err, &changed) || !slices.Equal(changed.Files, []string{"b.txt", "new.txt"}) {
		t.Fatalf("changes in the way: %v", err)
	}
	if r.git(r.dir, "rev-parse", "HEAD") != main || r.read(r.dir, "b.txt") != "bee\nlocal\n" {
		t.Error("the refused merge changed the checkout")
	}

	// Out of the way, the change to a.txt does not stop it, and stays.
	r.git(r.dir, "checkout", "-q", "b.txt")
	r.git(r.dir, "clean", "-q", "-f", "new.txt")
	got, err := r.g.Squash(ctx, r.dir, ws.Dir, "Add the member's", nil)
	if err != nil || got.Commit == "" {
		t.Fatalf("the merge: %+v %v", got, err)
	}
	if r.read(r.dir, "a.txt") != "one\nTWO\nthree\n" || r.read(r.dir, "b.txt") != "bee\nmember\n" {
		t.Error("the checkout lost its change, or lacks the member's")
	}
}

// What the checkout changed and did not commit is read as a patch, new
// files too, and committed file by file: the files picked, nothing else.
func TestCheckoutDiffAndCommit(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	if patch, cut, err := r.g.CheckoutDiff(ctx, r.dir, 0); err != nil || patch != "" || cut {
		t.Fatalf("a clean checkout: %q %v %v", patch, cut, err)
	}

	r.write(r.dir, "a.txt", "one\nTWO\nthree\n")
	r.write(r.dir, "notes.md", "# Notes\n")
	r.git(r.dir, "rm", "-q", "b.txt")
	// Staged by someone else, and not picked.
	r.write(r.dir, "c.txt", "sea\n")
	r.git(r.dir, "add", "c.txt")

	patch, cut, err := r.g.CheckoutDiff(ctx, r.dir, 0)
	if err != nil || cut {
		t.Fatal(err, cut)
	}
	for _, want := range []string{"+TWO", "+# Notes", "new file mode", "deleted file mode", "+sea"} {
		if !strings.Contains(patch, want) {
			t.Errorf("the patch lacks %q:\n%s", want, patch)
		}
	}
	// Reading it leaves the checkout's index as it was.
	if staged := r.git(r.dir, "diff", "--cached", "--name-only"); staged != "b.txt\nc.txt" {
		t.Errorf("the index was touched: %q", staged)
	}

	commit, err := r.g.CommitCheckout(ctx, r.dir, "Write the notes down", []string{"a.txt", "notes.md", "b.txt"})
	if err != nil || commit != r.git(r.dir, "rev-parse", "HEAD") {
		t.Fatalf("the commit: %q %v", commit, err)
	}
	if got := r.git(r.dir, "show", "--name-status", "--format=%s", "HEAD"); got != "Write the notes down\n\nM\ta.txt\nD\tb.txt\nA\tnotes.md" {
		t.Errorf("the commit has:\n%s", got)
	}
	// What was not picked stays as it was, staged.
	if left := r.git(r.dir, "status", "--porcelain"); left != "A  c.txt" {
		t.Errorf("left in the checkout: %q", left)
	}

	if _, err := r.g.CommitCheckout(ctx, r.dir, "Again", []string{"a.txt"}); !errors.Is(err, worktree.ErrNoChanges) {
		t.Errorf("nothing left to commit in the files picked: %v", err)
	}
	r.git(r.dir, "checkout", "-q", "--detach")
	if _, err := r.g.CommitCheckout(ctx, r.dir, "Detached", []string{"c.txt"}); !errors.Is(err, worktree.ErrDetached) {
		t.Errorf("a checkout on no branch: %v", err)
	}
}
