package worktree

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Files a merge leaves out (docs/design.md 5.21): new ones, never
// committed, that a person does not want on the main line, such as what a
// build or a run wrote. They stay in the worktree as they are, through the
// fresh start the merge gives it: moved aside while its branch starts
// over, and put back.

// NotNewError says a merge was asked to leave out files that are not new,
// that is committed or changes to files git has: those would be lost as
// the worktree starts over.
type NotNewError struct {
	Files []string
}

func (e *NotNewError) Error() string {
	return "only files never committed can be left out of a merge, not " + strings.Join(e.Files, ", ")
}

// checkLeave says whether every file in leave is new in the worktree at
// dir: made there and never committed.
func (g Git) checkLeave(ctx context.Context, dir string, leave []string) error {
	if len(leave) == 0 {
		return nil
	}
	changed, err := g.uncommitted(ctx, dir)
	if err != nil {
		return err
	}
	fresh := map[string]bool{}
	for _, c := range changed {
		if c.New {
			fresh[c.Path] = true
		}
	}
	var not []string
	for _, path := range leave {
		if !fresh[path] {
			not = append(not, path)
		}
	}
	if len(not) > 0 {
		return &NotNewError{Files: not}
	}
	return nil
}

// startOver moves the branch of the worktree at dir to commit, its files
// and all, but for the new files named in leave, which are moved aside
// meanwhile and put back: were they staged, or were the same path in
// commit, git would take them. Should putting them back fail, they are
// left where they were moved to, which the error names.
func (g Git) startOver(ctx context.Context, dir, commit string, leave []string) error {
	if len(leave) == 0 {
		_, err := g.run(ctx, dir, "reset", "-q", "--hard", commit)
		return err
	}
	root, err := g.root(ctx, dir)
	if err != nil {
		return err
	}
	// In the worktree itself, so that moving is renaming.
	aside, err := os.MkdirTemp(root, ".veyloom-left-")
	if err != nil {
		return err
	}
	moved, err := moveAll(root, aside, leave)
	if err == nil {
		_, err = g.run(ctx, dir, "reset", "-q", "--hard", commit)
	}
	if _, back := moveAll(aside, root, moved); back != nil {
		return fmt.Errorf("the files left out of the merge are in %s: %w", aside, back)
	}
	if rmErr := os.RemoveAll(aside); err == nil {
		err = rmErr
	}
	return err
}

// moveAll moves the files at paths under from to the same paths under to,
// making folders as needed; paths not there are passed over. It stops at
// the first that cannot be moved, and says which were.
func moveAll(from, to string, paths []string) (moved []string, err error) {
	for _, path := range paths {
		dst := filepath.Join(to, path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return moved, err
		}
		if err := os.Rename(filepath.Join(from, path), dst); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return moved, err
		}
		moved = append(moved, path)
	}
	return moved, nil
}
