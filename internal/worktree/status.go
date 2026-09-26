package worktree

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Status is how a member's worktree stands against the main line.
type Status struct {
	// Branch is the branch checked out in the worktree.
	Branch string `json:"branch"`
	// Base is the main line, the branch the checkout has; empty while the
	// checkout's HEAD is detached, when the commit it is at stands in.
	Base string `json:"base,omitempty"`
	// Ahead counts the worktree's commits the main line lacks; Behind the
	// main line's the worktree lacks.
	Ahead  int `json:"ahead"`
	Behind int `json:"behind"`
	// Files are what the worktree changed since it branched off the main
	// line, committed or not; Uncommitted counts those with changes not
	// committed.
	Files       []Change `json:"files,omitempty"`
	Uncommitted int      `json:"uncommitted"`
	// Merging says a merge is under way in the worktree, stopped on
	// conflicts; Conflicts are the files that still have conflict markers.
	Merging   bool     `json:"merging,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`
	// Commits are the worktree's own commits the main line lacks, oldest
	// first, merges of the main line left out; the latest ownCommits of
	// them at most.
	Commits []Commit `json:"commits,omitempty"`
	// At is where the worktree stands, what two worktrees' work is set
	// side by side by (Overlap).
	At Side `json:"at"`
}

// Side is where a worktree stands: its branch's last commit, and a tree of
// what it has, uncommitted changes and new files in.
type Side struct {
	Head string `json:"head"`
	Tree string `json:"tree"`
}

// Commit is one commit made in a worktree: what its message says.
type Commit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	Body    string `json:"body,omitempty"`
}

// ownCommits caps the commits a status names.
const ownCommits = 30

// mainLine is where the checkout stands: its branch, empty when detached,
// and the commit.
func (g Git) mainLine(ctx context.Context, checkout string) (branch, commit string, err error) {
	if commit, err = g.head(ctx, checkout); err != nil {
		return "", "", err
	}
	if commit == "" {
		return "", "", ErrNoCommits
	}
	branch, err = g.branch(ctx, checkout)
	return branch, commit, err
}

// Status says how the worktree at dir stands against the checkout's
// branch.
func (g Git) Status(ctx context.Context, checkout, dir string) (Status, error) {
	base, commit, err := g.mainLine(ctx, checkout)
	if err != nil {
		return Status{}, err
	}
	st := Status{Base: base, Merging: g.merging(ctx, dir)}
	if st.Merging {
		if st.Conflicts, err = g.unresolved(ctx, dir); err != nil {
			return Status{}, err
		}
	}
	if st.Branch, err = g.branch(ctx, dir); err != nil {
		return Status{}, err
	}
	if st.Behind, st.Ahead, err = g.counts(ctx, dir, commit); err != nil {
		return Status{}, err
	}
	if st.Ahead > 0 {
		if st.Commits, err = g.ownCommits(ctx, dir, commit); err != nil {
			return Status{}, err
		}
	}
	fork, tree, err := g.sinceFork(ctx, dir, commit)
	if err != nil {
		return Status{}, err
	}
	if st.At.Head, err = g.head(ctx, dir); err != nil {
		return Status{}, err
	}
	st.At.Tree = tree
	if st.Files, err = g.changes(ctx, dir, fork, tree); err != nil {
		return Status{}, err
	}
	uncommitted, err := g.uncommitted(ctx, dir)
	if err != nil {
		return Status{}, err
	}
	if uncommitted, err = g.withoutPrepared(ctx, dir, uncommitted); err != nil {
		return Status{}, err
	}
	pending := make(map[string]Change, len(uncommitted))
	for _, c := range uncommitted {
		pending[c.Path] = c
	}
	for i := range st.Files {
		if c, ok := pending[st.Files[i].Path]; ok {
			st.Files[i].Uncommitted, st.Files[i].New = true, c.New
			st.Uncommitted++
		}
	}
	return st, nil
}

// ownCommits are the commits of dir's HEAD the main line at commit lacks,
// merges left out, oldest first.
func (g Git) ownCommits(ctx context.Context, dir, commit string) ([]Commit, error) {
	out, err := g.run(ctx, dir, "log", "--no-merges", "--reverse", fmt.Sprintf("--max-count=%d", ownCommits),
		"--format=%h%x1f%s%x1f%b%x1e", commit+"..HEAD")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, record := range strings.Split(out, "\x1e") {
		fields := strings.SplitN(strings.TrimSpace(record), "\x1f", 3)
		if len(fields) < 2 || fields[0] == "" {
			continue
		}
		c := Commit{Hash: fields[0], Subject: strings.TrimSpace(fields[1])}
		if len(fields) == 3 {
			c.Body = strings.TrimSpace(fields[2])
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// counts are the commits the main line at commit has that dir's HEAD
// lacks, its own only, along its first parents, so that a squashed merge
// counts once however its work is grafted; and the commits of dir's HEAD
// the main line lacks.
func (g Git) counts(ctx context.Context, dir, commit string) (behind, ahead int, err error) {
	if behind, err = g.count(ctx, dir, "--first-parent", "HEAD.."+commit); err != nil {
		return 0, 0, err
	}
	ahead, err = g.count(ctx, dir, commit+"..HEAD")
	return behind, ahead, err
}

// count is how many commits git rev-list lists with args.
func (g Git) count(ctx context.Context, dir string, args ...string) (int, error) {
	out, err := g.line(ctx, dir, append([]string{"rev-list", "--count"}, args...)...)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(out)
	if err != nil {
		return 0, fmt.Errorf("git rev-list --count: unexpected %q", out)
	}
	return n, nil
}

// sinceFork is where dir's branch forked off the main line at commit, and
// the tree of dir as it stands, less the files named in leave.
func (g Git) sinceFork(ctx context.Context, dir, commit string, leave ...string) (fork, tree string, err error) {
	if fork, err = g.line(ctx, dir, "merge-base", commit, "HEAD"); err != nil {
		return "", "", err
	}
	tree, err = g.snapshot(ctx, dir, leave...)
	return fork, tree, err
}

// snapshot writes the worktree at dir, as it stands, into a tree: committed
// or not, new files too, less what .gitignore leaves out and the new files
// named in leave. It works on a copy of the worktree's index, which it
// leaves as it was; the copy keeps the index's record of what is
// unchanged, so only changed files are read.
func (g Git) snapshot(ctx context.Context, dir string, leave ...string) (string, error) {
	tmp, err := os.MkdirTemp("", "veyloom-index-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	index := filepath.Join(tmp, "index")
	env := []string{"GIT_INDEX_FILE=" + index}
	own, err := g.line(ctx, dir, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return "", err
	}
	if err := copyPlain(own, index); err != nil {
		// No index of its own yet: start from what HEAD has.
		if _, err := g.runEnv(ctx, dir, env, "read-tree", "HEAD"); err != nil {
			return "", err
		}
	}
	if _, err := g.runEnv(ctx, dir, env, "add", "-A"); err != nil {
		return "", err
	}
	// The worktree's own files are none of the member's work.
	if err := g.unstagePrepared(ctx, dir, env); err != nil {
		return "", err
	}
	if len(leave) > 0 {
		if _, err := g.runEnv(ctx, dir, env, append([]string{"rm", "-q", "--cached", "--ignore-unmatch", "--"}, literal(leave)...)...); err != nil {
			return "", err
		}
	}
	return lineOf(g.runEnv(ctx, dir, env, "write-tree"))
}

// copyPlain copies one file's bytes and its time of change. An index needs
// its time: git reads again the files changed in the same second as the
// index was written, since their size and time alone cannot tell; a copy
// dated now would take them for unchanged.
func copyPlain(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	dst, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	if err := dst.Close(); err != nil {
		return err
	}
	return os.Chtimes(to, info.ModTime(), info.ModTime())
}

func lineOf(out string, err error) (string, error) {
	return strings.TrimSpace(out), err
}

// changes lists what differs from tree-ish a to b, renames found, with line
// counts.
func (g Git) changes(ctx context.Context, dir, a, b string) ([]Change, error) {
	names, err := g.run(ctx, dir, "diff-tree", "-r", "-z", "-M", "--name-status", a, b)
	if err != nil {
		return nil, err
	}
	counts, err := g.run(ctx, dir, "diff-tree", "-r", "-z", "-M", "--numstat", a, b)
	if err != nil {
		return nil, err
	}
	changes := parseNameStatus(names)
	byPath := make(map[string]*Change, len(changes))
	for i := range changes {
		byPath[changes[i].Path] = &changes[i]
	}
	for _, n := range parseNumstat(counts) {
		if c := byPath[n.Path]; c != nil {
			c.Added, c.Deleted, c.Binary = n.Added, n.Deleted, n.Binary
		}
	}
	return changes, nil
}

// parseNameStatus reads `git diff-tree -z --name-status`: a status, NUL,
// the path, NUL; renames and copies have a score in the status and the old
// path before the new one.
func parseNameStatus(out string) []Change {
	var changes []Change
	fields := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	for i := 0; i+1 < len(fields); i++ {
		status := fields[i]
		if status == "" {
			continue
		}
		c := Change{Status: status[:1], Path: fields[i+1]}
		i++
		if (c.Status == "R" || c.Status == "C") && i+1 < len(fields) {
			c.From, c.Path = c.Path, fields[i+1]
			c.Status = "R"
			i++
		}
		changes = append(changes, c)
	}
	return changes
}

// parseNumstat reads `git diff-tree -z --numstat`: "added TAB deleted TAB
// path" NUL, or for a rename "added TAB deleted TAB" NUL old NUL new NUL;
// binary files count "-".
func parseNumstat(out string) []Change {
	var changes []Change
	fields := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	for i := 0; i < len(fields); i++ {
		parts := strings.SplitN(fields[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		c := Change{Path: parts[2]}
		if c.Path == "" && i+2 < len(fields) {
			c.From, c.Path = fields[i+1], fields[i+2]
			i += 2
		}
		if parts[0] == "-" {
			c.Binary = true
		} else {
			c.Added, _ = strconv.Atoi(parts[0])
			c.Deleted, _ = strconv.Atoi(parts[1])
		}
		changes = append(changes, c)
	}
	return changes
}

// Diff is the patch of what the worktree at dir changed since it branched
// off the checkout's branch, committed or not, new files too; cut to max
// bytes when longer, and then cut says so.
func (g Git) Diff(ctx context.Context, checkout, dir string, max int) (patch string, cut bool, err error) {
	_, commit, err := g.mainLine(ctx, checkout)
	if err != nil {
		return "", false, err
	}
	fork, tree, err := g.sinceFork(ctx, dir, commit)
	if err != nil {
		return "", false, err
	}
	patch, err = g.run(ctx, dir, "diff", "--no-color", "--no-ext-diff", "-M", fork, tree)
	if err != nil {
		return "", false, err
	}
	if max > 0 && len(patch) > max {
		patch = strings.ToValidUTF8(patch[:max], "")
		cut = true
	}
	return patch, cut, nil
}
