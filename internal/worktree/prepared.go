package worktree

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// A worktree's own files (docs/design.md 5.21): what getting it ready made
// that git neither tracks nor ignores, such as settings copied in or what
// a setup command wrote. They are set down, once, in a file of the
// worktree's own git folder, and left out wherever the member's work is
// taken: its status, its patch, the commit that merges it.

// preparedFile names the list in the worktree's git folder.
const preparedFile = "veyloom-prepared"

// markPrepared sets down what the worktree at dir has that git neither
// tracks nor ignores, right after getting it ready.
func (g Git) markPrepared(ctx context.Context, dir string) error {
	root, err := g.root(ctx, dir)
	if err != nil {
		return err
	}
	out, err := g.run(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	path, err := g.line(ctx, root, "rev-parse", "--path-format=absolute", "--git-path", preparedFile)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// prepared is what markPrepared set down for the worktree at dir, as a set
// of paths from its top; none for a worktree that has no list.
func (g Git) prepared(ctx context.Context, dir string) (map[string]bool, error) {
	path, err := g.line(ctx, dir, "rev-parse", "--path-format=absolute", "--git-path", preparedFile)
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, p := range strings.Split(string(body), "\x00") {
		if p != "" {
			set[p] = true
		}
	}
	return set, nil
}

// unstagePrepared takes the worktree's own files out of the index an
// add -A just filled, env naming the index when it is not the worktree's.
func (g Git) unstagePrepared(ctx context.Context, dir string, env []string) error {
	own, err := g.prepared(ctx, dir)
	if err != nil || len(own) == 0 {
		return err
	}
	args := []string{"rm", "-q", "--cached", "--ignore-unmatch", "--"}
	for p := range own {
		args = append(args, pathspec(p))
	}
	_, err = g.runEnv(ctx, dir, env, args...)
	return err
}

// withoutPrepared is changes less the worktree's own files.
func (g Git) withoutPrepared(ctx context.Context, dir string, changes []Change) ([]Change, error) {
	own, err := g.prepared(ctx, dir)
	if err != nil || len(own) == 0 {
		return changes, err
	}
	out := changes[:0:0]
	for _, c := range changes {
		if !own[c.Path] {
			out = append(out, c)
		}
	}
	return out, nil
}
