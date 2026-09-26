package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The project's checkout itself (docs/design.md 5.21): the changes made
// there and not committed, which the members' worktrees do not have and
// which stop work going on the main line when it changes the same files. A
// person reads them and commits them from the branch tab.

// CheckoutChangedError says work could not go on the main line: it changes
// files that have changes in the checkout not committed, which git will not
// write over.
type CheckoutChangedError struct {
	Files []string
}

func (e *CheckoutChangedError) Error() string {
	return fmt.Sprintf("the checkout has changes not committed to %s, which the merge changes too", strings.Join(e.Files, ", "))
}

// inTheWay are the files that moving the checkout from commit to target
// would write, and that have changes there not committed: git refuses the
// move while there are any.
func (g Git) inTheWay(ctx context.Context, checkout, commit, target string) ([]string, error) {
	changed, err := g.uncommitted(ctx, checkout)
	if err != nil || len(changed) == 0 {
		return nil, err
	}
	written, err := g.changedNames(ctx, checkout, commit, target)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, c := range changed {
		for _, path := range []string{c.Path, c.From} {
			if path != "" && written[path] && !slices.Contains(files, path) {
				files = append(files, path)
			}
		}
	}
	slices.Sort(files)
	return files, nil
}

// CheckoutDiff is the patch of what the checkout changed and did not
// commit, new files too, less what .gitignore leaves out; cut to max bytes
// when longer, and then cut says so.
func (g Git) CheckoutDiff(ctx context.Context, checkout string, max int) (patch string, cut bool, err error) {
	if _, _, err := g.mainLine(ctx, checkout); err != nil {
		return "", false, err
	}
	tree, err := g.snapshot(ctx, checkout)
	if err != nil {
		return "", false, err
	}
	patch, err = g.run(ctx, checkout, "diff", "--no-color", "--no-ext-diff", "-M", "HEAD", tree)
	if err != nil {
		return "", false, err
	}
	if max > 0 && len(patch) > max {
		patch = strings.ToValidUTF8(patch[:max], "")
		cut = true
	}
	return patch, cut, nil
}

// CommitCheckout commits, on the checkout's branch, the changes to paths
// made there and not committed, with message: the files a person was shown,
// paths from the top of the repository, and nothing else, whatever else is
// staged. It is the new commit.
func (g Git) CommitCheckout(ctx context.Context, checkout, message string, paths []string) (string, error) {
	base, _, err := g.mainLine(ctx, checkout)
	if err != nil {
		return "", err
	}
	if base == "" {
		return "", ErrDetached
	}
	root, err := g.root(ctx, checkout)
	if err != nil {
		return "", err
	}
	changed, err := g.uncommitted(ctx, checkout)
	if err != nil {
		return "", err
	}
	// Every file picked is committed as it stands; those still there are
	// added first, so that new ones are known to git. A file gone is
	// committed gone without that, which git would refuse for one it no
	// longer tracks.
	var specs, adds []string
	for _, c := range changed {
		for _, path := range []string{c.Path, c.From} {
			if path == "" || !slices.Contains(paths, path) {
				continue
			}
			specs = append(specs, pathspec(path))
			if _, err := os.Lstat(filepath.Join(root, path)); err == nil {
				adds = append(adds, pathspec(path))
			}
		}
	}
	if len(specs) == 0 {
		return "", ErrNoChanges
	}
	if len(adds) > 0 {
		if _, err := g.run(ctx, checkout, append([]string{"add", "-A", "--"}, adds...)...); err != nil {
			return "", err
		}
	}
	args := append([]string{"commit", "-q", "-m", message, "--"}, specs...)
	if _, err := g.runEnv(ctx, checkout, g.identity(ctx, checkout), args...); err != nil {
		return "", err
	}
	return g.head(ctx, checkout)
}
