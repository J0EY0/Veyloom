package worktree

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// A merge left under way in a member's worktree (docs/design.md 5.21). A
// member trusted with git merges the main line in itself when its work
// conflicts with it; one that settles the conflicts but does not commit,
// or leaves some unsettled, leaves the worktree in the middle of a merge,
// where its work neither goes onto the main line nor takes it in. As its
// turn ends, the merge is seen to: committed when nothing is left to
// settle, named file by file when something is.

// ConcludeResult is how a merge under way in a worktree was seen to.
type ConcludeResult struct {
	// Commit is the merge commit made for the member: its conflicts were
	// settled, the merge not committed.
	Commit string `json:"commit,omitempty"`
	// Unresolved are the files that still have conflict markers; nothing
	// was done.
	Unresolved []string `json:"unresolved,omitempty"`
}

// Conclude sees to a merge left under way in the worktree at dir: with no
// file left with conflict markers it is committed, the settled files
// added and git's own message kept; otherwise it is left as it is and the
// files named. No merge under way is nothing to do.
func (g Git) Conclude(ctx context.Context, dir string) (ConcludeResult, error) {
	if !g.merging(ctx, dir) {
		return ConcludeResult{}, nil
	}
	unresolved, err := g.unresolved(ctx, dir)
	if err != nil || len(unresolved) > 0 {
		return ConcludeResult{Unresolved: unresolved}, err
	}
	unmerged, err := g.unmerged(ctx, dir)
	if err != nil {
		return ConcludeResult{}, err
	}
	if len(unmerged) > 0 {
		// Settled in the files, not yet marked so: added, a file taken away
		// by the settling as well.
		if _, err := g.run(ctx, dir, append([]string{"add", "-A", "--"}, unmerged...)...); err != nil {
			return ConcludeResult{}, err
		}
	}
	if _, err := g.runEnv(ctx, dir, g.identity(ctx, dir), "commit", "-q", "--no-edit", "--no-verify"); err != nil {
		return ConcludeResult{}, err
	}
	commit, err := g.head(ctx, dir)
	return ConcludeResult{Commit: commit}, err
}

// AbortMerge gives up a merge under way in the worktree at dir: the
// worktree is as it was before the merge began. No merge under way is
// nothing to do.
func (g Git) AbortMerge(ctx context.Context, dir string) error {
	if !g.merging(ctx, dir) {
		return nil
	}
	_, err := g.run(ctx, dir, "merge", "--abort")
	return err
}

// unmerged are the files git still has as conflicting in dir's index.
func (g Git) unmerged(ctx context.Context, dir string) ([]string, error) {
	out, err := g.run(ctx, dir, "diff", "--name-only", "-z", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	return splitNull(out), nil
}

// unresolved are the files of a merge under way in dir that still have
// conflict markers: those git has as conflicting, and those the merge
// changed and a member marked settled with the markers still in.
func (g Git) unresolved(ctx context.Context, dir string) ([]string, error) {
	unmerged, err := g.unmerged(ctx, dir)
	if err != nil {
		return nil, err
	}
	changed, err := g.run(ctx, dir, "diff", "--name-only", "-z", "HEAD")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, name := range slices.Compact(slices.Sorted(slices.Values(append(unmerged, splitNull(changed)...)))) {
		marked, err := hasConflictMarkers(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if marked {
			out = append(out, name)
		}
	}
	return out, nil
}

// markerScanLimit caps how much of a file is looked at for conflict
// markers.
const markerScanLimit = 4 << 20

// hasConflictMarkers says the file at path has a line git writes around a
// conflict: "<<<<<<<", "|||||||" or ">>>>>>>", then a space or the end of
// the line. "=======" alone is left out, being a heading's underline in
// markdown too. A file that is not there, or not text, has none.
func hasConflictMarkers(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false, err
	}
	data, err := io.ReadAll(io.LimitReader(f, markerScanLimit))
	if err != nil {
		return false, err
	}
	if bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
		return false, nil
	}
	lines := bufio.NewScanner(bytes.NewReader(data))
	lines.Buffer(make([]byte, 64<<10), markerScanLimit)
	for lines.Scan() {
		line := strings.TrimRight(lines.Text(), "\r")
		for _, marker := range []string{"<<<<<<<", "|||||||", ">>>>>>>"} {
			if rest, ok := strings.CutPrefix(line, marker); ok && (rest == "" || rest[0] == ' ') {
				return true, nil
			}
		}
	}
	return false, lines.Err()
}

// splitNull reads git's NUL-separated names.
func splitNull(out string) []string {
	var names []string
	for _, name := range strings.Split(out, "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}
