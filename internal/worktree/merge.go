package worktree

import (
	"context"
	"strconv"
	"strings"
)

// MergeResult is how putting a member's work on the main line went.
type MergeResult struct {
	// Commit is the new commit on the main line; empty when nothing went on.
	Commit string `json:"commit,omitempty"`
	// Conflicts are the files the worktree and the main line both changed
	// in ways git cannot put together; nothing went on then.
	Conflicts []string `json:"conflicts,omitempty"`
	// Unsettled says why the worktree did not start over from the new
	// commit, which went on all the same; a person sees to it.
	Unsettled string `json:"unsettled,omitempty"`
}

// Squash puts everything the worktree at dir changed since it branched
// off, committed or not, on the checkout's branch as one commit with
// message (docs/design.md 5.21), but for the new files named in leave. The
// merge is worked out apart from both working trees; when it conflicts
// nothing changes and the files are named. When it does not, the checkout
// moves forward onto the new commit, unless changes there not committed
// are in the way, which a CheckoutChangedError names, and the worktree's
// branch starts over from it, keeping the files left out.
//
// A squashed commit has the main line alone for parent: the member's
// commits are not in its history. Yet another member may have merged them,
// building on the work as its brief says to, and git would count, diff and
// merge them again as that member's own, conflicting with themselves. So
// the commit is grafted, under ReplaceRefBase, to have the member's last
// commit as a second parent: to this package's git, and the agents', it is
// the merge it amounts to.
func (g Git) Squash(ctx context.Context, checkout, dir, message string, leave []string) (MergeResult, error) {
	if g.merging(ctx, dir) {
		return MergeResult{}, ErrMergeUnderway
	}
	if err := g.checkLeave(ctx, dir, leave); err != nil {
		return MergeResult{}, err
	}
	base, commit, err := g.mainLine(ctx, checkout)
	if err != nil {
		return MergeResult{}, err
	}
	if base == "" {
		return MergeResult{}, ErrDetached
	}
	fork, tree, err := g.sinceFork(ctx, dir, commit, leave...)
	if err != nil {
		return MergeResult{}, err
	}
	forkTree, err := g.line(ctx, dir, "rev-parse", fork+"^{tree}")
	if err != nil {
		return MergeResult{}, err
	}
	if tree == forkTree {
		return MergeResult{}, ErrNoChanges
	}
	head, err := g.head(ctx, dir)
	if err != nil {
		return MergeResult{}, err
	}
	who := g.identity(ctx, checkout)
	work, err := lineOf(g.runEnv(ctx, dir, who, "commit-tree", tree, "-p", head, "-m", "Work in progress"))
	if err != nil {
		return MergeResult{}, err
	}
	merged, conflicts, err := g.mergeTree(ctx, checkout, commit, work)
	if err != nil || len(conflicts) > 0 {
		return MergeResult{Conflicts: conflicts}, err
	}
	squashed, err := lineOf(g.runEnv(ctx, checkout, who, "commit-tree", merged, "-p", commit, "-m", message))
	if err != nil {
		return MergeResult{}, err
	}
	// Said here in so many words, rather than in git's, which differ with
	// its version and language.
	if files, err := g.inTheWay(ctx, checkout, commit, squashed); err != nil {
		return MergeResult{}, err
	} else if len(files) > 0 {
		return MergeResult{}, &CheckoutChangedError{Files: files}
	}
	// Grafted before it goes on the main line, so it is never there
	// without; the graft goes again should it not get there.
	grafted, err := g.graft(ctx, checkout, squashed, commit, head)
	if err != nil {
		return MergeResult{}, err
	}
	if _, err := g.run(ctx, checkout, "merge", "-q", "--ff-only", squashed); err != nil {
		if grafted {
			_, _ = g.run(ctx, checkout, "replace", "-d", squashed)
		}
		if now, _ := g.head(ctx, checkout); now != commit {
			return MergeResult{}, ErrMainLineMoved
		}
		return MergeResult{}, err
	}
	if err := g.startOver(ctx, dir, squashed, leave); err != nil {
		return MergeResult{Commit: squashed, Unsettled: err.Error()}, nil
	}
	return MergeResult{Commit: squashed}, nil
}

// graft has squashed, which took in the member's work on top of main, read
// as the merge of the member's commits, at head, into main. None is needed
// when the member made no commit of its own, or when squashed is the one
// commit it made, said and made the same way in the same second: a graft
// would make it its own ancestor.
func (g Git) graft(ctx context.Context, checkout, squashed, main, head string) (bool, error) {
	for _, pair := range [][2]string{{head, main}, {squashed, head}} {
		if has, err := g.isAncestor(ctx, checkout, pair[0], pair[1]); err != nil || has {
			return false, err
		}
	}
	_, err := g.run(ctx, checkout, "replace", "--graft", squashed, main, head)
	return err == nil, err
}

// isAncestor says commit a is in the history of commit b, b included.
func (g Git) isAncestor(ctx context.Context, dir, a, b string) (bool, error) {
	_, err := g.run(ctx, dir, "merge-base", "--is-ancestor", a, b)
	if exitCode(err) == 1 {
		return false, nil
	}
	return err == nil, err
}

// mergeTree works out the merge of two commits without a working tree:
// the merged tree, or the files that conflict.
func (g Git) mergeTree(ctx context.Context, dir, ours, theirs string) (tree string, conflicts []string, err error) {
	out, err := g.run(ctx, dir, "merge-tree", "--write-tree", "--name-only", "--no-messages", ours, theirs)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	switch exitCode(err) {
	case -1:
		if err != nil {
			return "", nil, err
		}
		return strings.TrimSpace(lines[0]), nil, nil
	case 1:
		for _, name := range lines[1:] {
			if name = unquote(name); name != "" {
				conflicts = append(conflicts, name)
			}
		}
		return "", conflicts, nil
	default:
		return "", nil, err
	}
}

// unquote reads a path git quoted, C style, for having unusual characters
// in it.
func unquote(name string) string {
	if strings.HasPrefix(name, `"`) {
		if plain, err := strconv.Unquote(name); err == nil {
			return plain
		}
	}
	return name
}

// SyncResult is how bringing the main line into a member's worktree went.
type SyncResult struct {
	// Updated says the worktree's branch took the main line in.
	Updated bool `json:"updated,omitempty"`
	// Skipped says it was left alone: it has work of its own and only a
	// fast-forward was asked for.
	Skipped bool `json:"skipped,omitempty"`
	// Conflicts are the files the merge stopped on; it was given up, and
	// the worktree is as it was.
	Conflicts []string `json:"conflicts,omitempty"`
}

// Sync brings the checkout's branch into the worktree at dir. With
// fastForwardOnly it does so only while the worktree has no work of its
// own, committed or not, which is what happens before each of a member's
// turns; otherwise it merges, and a merge that conflicts is given up and
// the files named. A worktree that has the main line already is left be;
// one with a merge of its own under way is not touched.
func (g Git) Sync(ctx context.Context, checkout, dir string, fastForwardOnly bool) (SyncResult, error) {
	if g.merging(ctx, dir) {
		if fastForwardOnly {
			return SyncResult{Skipped: true}, nil
		}
		return SyncResult{}, ErrMergeUnderway
	}
	base, commit, err := g.mainLine(ctx, checkout)
	if err != nil {
		return SyncResult{}, err
	}
	if has, err := g.isAncestor(ctx, dir, commit, "HEAD"); err != nil || has {
		return SyncResult{}, err
	}
	_, ahead, err := g.counts(ctx, dir, commit)
	if err != nil {
		return SyncResult{}, err
	}
	changed, err := g.uncommitted(ctx, dir)
	if err == nil {
		changed, err = g.withoutPrepared(ctx, dir, changed)
	}
	if err != nil {
		return SyncResult{}, err
	}
	if ahead == 0 && len(changed) == 0 {
		if _, err := g.run(ctx, dir, "merge", "-q", "--ff-only", commit); err != nil {
			return SyncResult{}, err
		}
		return SyncResult{Updated: true}, nil
	}
	if fastForwardOnly {
		return SyncResult{Skipped: true}, nil
	}
	message := "Merge the main line"
	if base != "" {
		message = "Merge " + base
	}
	_, err = g.runEnv(ctx, dir, g.identity(ctx, dir), "merge", "-q", "--no-edit", "-m", message, commit)
	if err == nil {
		return SyncResult{Updated: true}, nil
	}
	if !g.merging(ctx, dir) {
		// Refused before it began, as when uncommitted changes are in the
		// way: git's words say which.
		return SyncResult{}, err
	}
	conflicts, uerr := g.unmerged(ctx, dir)
	if _, aerr := g.run(ctx, dir, "merge", "--abort"); aerr != nil {
		return SyncResult{}, aerr
	}
	if uerr != nil {
		return SyncResult{}, uerr
	}
	return SyncResult{Conflicts: conflicts}, nil
}
