package worktree

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Repo is how a project's checkout stands: the main line the members'
// worktrees branch off.
type Repo struct {
	// Root is the top of the repository the checkout is in, Sub the
	// checkout's place under it: "" when the checkout is the top.
	Root string `json:"root"`
	Sub  string `json:"sub,omitempty"`
	// Branch is the branch checked out, the main line; empty while HEAD is
	// detached. Head is the commit it is at; empty before the first commit.
	Branch string `json:"branch,omitempty"`
	Head   string `json:"head,omitempty"`
	// Changed are the files changed in the checkout and not committed:
	// what the members' worktrees do not have.
	Changed []Change `json:"changed,omitempty"`
}

// Change is a file changed: in a worktree since it branched off, or in the
// checkout and not committed.
type Change struct {
	Path string `json:"path"`
	// Status is A added, M modified, D deleted, R renamed (from From), or
	// T its type changed.
	Status string `json:"status"`
	From   string `json:"from,omitempty"`
	// Added and Deleted count lines; Binary says they could not be counted.
	Added   int  `json:"added"`
	Deleted int  `json:"deleted"`
	Binary  bool `json:"binary,omitempty"`
	// Uncommitted says the change, all of it or some, is not committed.
	// New says the file is in no commit at all, made since and not
	// committed: such as what a build or a run wrote. A merge may leave it
	// out, and it stays where it is.
	Uncommitted bool `json:"uncommitted,omitempty"`
	New         bool `json:"new,omitempty"`
}

// Inspect says how a checkout stands. ErrNotRepo when it is in no git
// repository.
func (g Git) Inspect(ctx context.Context, checkout string) (Repo, error) {
	root, err := g.root(ctx, checkout)
	if err != nil {
		return Repo{}, err
	}
	repo := Repo{Root: root}
	if repo.Sub, err = sub(root, checkout); err != nil {
		return Repo{}, err
	}
	if repo.Head, err = g.head(ctx, checkout); err != nil {
		return Repo{}, err
	}
	if repo.Branch, err = g.branch(ctx, checkout); err != nil {
		return Repo{}, err
	}
	if repo.Changed, err = g.uncommitted(ctx, checkout); err != nil {
		return Repo{}, err
	}
	return repo, nil
}

// root is the top of the repository dir is in.
func (g Git) root(ctx context.Context, dir string) (string, error) {
	top, err := g.line(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if exitCode(err) > 0 {
			return "", fmt.Errorf("%s: %w", dir, ErrNotRepo)
		}
		return "", err
	}
	return top, nil
}

// sub is where dir lies under the repository's top, which git gives with
// symbolic links resolved: dir is resolved the same way first.
func sub(root, dir string) (string, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, real)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "", nil
	}
	return rel, nil
}

// head is the commit dir's HEAD is at; empty before the first commit.
func (g Git) head(ctx context.Context, dir string) (string, error) {
	sha, err := g.line(ctx, dir, "rev-parse", "-q", "--verify", "HEAD^{commit}")
	if exitCode(err) == 1 {
		return "", nil
	}
	return sha, err
}

// branch is the branch checked out in dir; empty while HEAD is detached.
func (g Git) branch(ctx context.Context, dir string) (string, error) {
	name, err := g.line(ctx, dir, "symbolic-ref", "-q", "--short", "HEAD")
	if exitCode(err) == 1 {
		return "", nil
	}
	return name, err
}

// uncommitted lists what is changed in dir and not committed, new files
// included, and nothing .gitignore leaves out. Taking no optional locks,
// it does not get in the way of a person working there.
func (g Git) uncommitted(ctx context.Context, dir string) ([]Change, error) {
	out, err := g.run(ctx, dir, "--no-optional-locks", "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	return parseStatus(out), nil
}

// parseStatus reads `git status --porcelain=v1 -z`: "XY path", NUL, and
// for renames and copies the old path after another NUL.
func parseStatus(out string) []Change {
	var changes []Change
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		entry := fields[i]
		if len(entry) < 4 {
			continue
		}
		xy, path := entry[:2], entry[3:]
		c := Change{Path: path, Status: statusLetter(xy), Uncommitted: true}
		c.New = c.Status == "A"
		if xy[0] == 'R' || xy[0] == 'C' {
			if i+1 < len(fields) {
				c.From = fields[i+1]
				i++
			}
		}
		changes = append(changes, c)
	}
	return changes
}

// statusLetter is the one letter of a porcelain XY pair that matters here.
func statusLetter(xy string) string {
	switch {
	case xy == "??":
		return "A"
	case strings.ContainsRune(xy, 'D'):
		return "D"
	case xy[0] == 'R' || xy[0] == 'C':
		return "R"
	case strings.ContainsRune(xy, 'A'):
		return "A"
	case strings.ContainsRune(xy, 'T'):
		return "T"
	default:
		return "M"
	}
}
