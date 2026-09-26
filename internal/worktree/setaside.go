package worktree

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Setting a member's work aside (docs/design.md 5.21): a person gives up
// what a worktree has, such as a reviewer's scratch or an attempt that
// went wrong, and its branch starts over from the main line. Nothing is
// lost for good: the work, committed or not, is first kept as a commit
// under a ref of Veyloom's own.

// SetAsideRefs is where work set aside is kept, by branch and time.
const SetAsideRefs = "refs/veyloom/set-aside/"

// SetAside keeps what the worktree at dir has, committed or not, as a
// commit with message under SetAsideRefs, then starts the worktree over
// from the checkout's commit: its branch moved there, a merge under way
// given up, and the files git does not track taken away, but for the
// worktree's own. It is the ref the work is kept under. ErrNoChanges when
// the worktree has nothing of its own.
func (g Git) SetAside(ctx context.Context, checkout, dir, message string) (string, error) {
	_, commit, err := g.mainLine(ctx, checkout)
	if err != nil {
		return "", err
	}
	head, err := g.head(ctx, dir)
	if err != nil {
		return "", err
	}
	tree, err := g.snapshot(ctx, dir)
	if err != nil {
		return "", err
	}
	headTree, err := g.line(ctx, dir, "rev-parse", head+"^{tree}")
	if err != nil {
		return "", err
	}
	ahead, err := g.count(ctx, dir, commit+"..HEAD")
	if err != nil {
		return "", err
	}
	if ahead == 0 && tree == headTree {
		return "", ErrNoChanges
	}
	kept, err := lineOf(g.runEnv(ctx, dir, g.identity(ctx, dir), "commit-tree", tree, "-p", head, "-m", message))
	if err != nil {
		return "", err
	}
	branch, err := g.branch(ctx, dir)
	if err != nil {
		return "", err
	}
	if branch == "" {
		branch = "detached"
	}
	ref := SetAsideRefs + branch + "/" + time.Now().UTC().Format("20060102T150405.000Z")
	if _, err := g.run(ctx, dir, "update-ref", ref, kept); err != nil {
		return "", err
	}
	if _, err := g.run(ctx, dir, "reset", "-q", "--hard", commit); err != nil {
		return ref, err
	}
	return ref, g.removeUntracked(ctx, dir)
}

// removeUntracked takes away the files in the worktree at dir that git
// neither tracks nor ignores, but for the worktree's own, and the folders
// that leaves empty.
func (g Git) removeUntracked(ctx context.Context, dir string) error {
	root, err := g.root(ctx, dir)
	if err != nil {
		return err
	}
	out, err := g.run(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	own, err := g.prepared(ctx, dir)
	if err != nil {
		return err
	}
	for _, path := range strings.Split(out, "\x00") {
		if path == "" || own[path] {
			continue
		}
		if err := os.Remove(filepath.Join(root, path)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		// Folders left empty go too; one with anything in it stays.
		for parent := filepath.Dir(path); parent != "."; parent = filepath.Dir(parent) {
			if os.Remove(filepath.Join(root, parent)) != nil {
				break
			}
		}
	}
	return nil
}
