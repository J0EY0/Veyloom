package worktree

import (
	"context"
	"slices"
	"strings"
)

// Overlap is the files the work of two worktrees both changes, each its
// own way (docs/design.md 5.21): changed on both sides since the two last
// shared history, and not alike now. Work one of them has from the other,
// having merged it, is none of it, however the main line has moved since;
// nor is a file both changed the same way, which merges cleanly. a and b
// are where each stands, as its status says.
func (g Git) Overlap(ctx context.Context, checkout string, a, b Side) ([]string, error) {
	base, err := g.line(ctx, checkout, "merge-base", a.Head, b.Head)
	if err != nil {
		return nil, err
	}
	mine, err := g.changedNames(ctx, checkout, base, a.Tree)
	if err != nil || len(mine) == 0 {
		return nil, err
	}
	theirs, err := g.changedNames(ctx, checkout, base, b.Tree)
	if err != nil || len(theirs) == 0 {
		return nil, err
	}
	unlike, err := g.changedNames(ctx, checkout, a.Tree, b.Tree)
	if err != nil {
		return nil, err
	}
	var both []string
	for path := range mine {
		if theirs[path] && unlike[path] {
			both = append(both, path)
		}
	}
	slices.Sort(both)
	return both, nil
}

// changedNames are the paths that differ from tree-ish from to to, as a set.
func (g Git) changedNames(ctx context.Context, dir, from, to string) (map[string]bool, error) {
	out, err := g.run(ctx, dir, "diff-tree", "-r", "-z", "--name-only", "--no-renames", from, to)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, path := range strings.Split(out, "\x00") {
		if path != "" {
			names[path] = true
		}
	}
	return names, nil
}
