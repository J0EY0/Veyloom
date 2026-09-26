package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Workspace is a member's worktree.
type Workspace struct {
	// Dir is the worktree, Branch the branch checked out in it.
	Dir    string `json:"dir"`
	Branch string `json:"branch"`
	// WorkDir is where the member works: the same place in the worktree as
	// the checkout has in its repository.
	WorkDir string `json:"work_dir"`
	// Base is the commit it branched off.
	Base string `json:"base"`
}

// Create makes a worktree of the checkout's repository at root/name, on a
// new branch named branch, from the commit the checkout is at. A name or
// branch already taken gets -2, -3 and so on: nothing there is touched.
func (g Git) Create(ctx context.Context, checkout, root, name, branch string) (Workspace, error) {
	repo, err := g.Inspect(ctx, checkout)
	if err != nil {
		return Workspace{}, err
	}
	if repo.Head == "" {
		return Workspace{}, ErrNoCommits
	}
	dir, err := freeDir(filepath.Join(root, name))
	if err != nil {
		return Workspace{}, err
	}
	if branch, err = g.freeBranch(ctx, repo.Root, branch); err != nil {
		return Workspace{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return Workspace{}, err
	}
	if _, err := g.run(ctx, repo.Root, "worktree", "add", "-q", "-b", branch, dir, repo.Head); err != nil {
		return Workspace{}, err
	}
	return Workspace{Dir: dir, Branch: branch, WorkDir: filepath.Join(dir, repo.Sub), Base: repo.Head}, nil
}

// freeDir is path, or path-2, path-3... the first nothing is at.
func freeDir(path string) (string, error) {
	for n := 1; n < 1000; n++ {
		try := path
		if n > 1 {
			try = path + "-" + strconv.Itoa(n)
		}
		switch _, err := os.Lstat(try); {
		case errors.Is(err, fs.ErrNotExist):
			return try, nil
		case err != nil:
			return "", err
		}
	}
	return "", fmt.Errorf("no free folder for %s", path)
}

// freeBranch is branch, or branch-2, branch-3... the first no branch has.
func (g Git) freeBranch(ctx context.Context, root, branch string) (string, error) {
	for n := 1; n < 1000; n++ {
		try := branch
		if n > 1 {
			try = branch + "-" + strconv.Itoa(n)
		}
		_, err := g.run(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+try)
		switch code := exitCode(err); {
		case err == nil:
			continue
		case code == 1:
			return try, nil
		default:
			return "", err
		}
	}
	return "", fmt.Errorf("no free branch name for %s", branch)
}

// outputTail caps what Prepare keeps of the command's output: its end,
// where a failure says what went wrong.
const outputTail = 8 << 10

// Prepare gets a new worktree ready the way the project's leader wrote
// down (docs/design.md 5.21): it copies from the checkout what git does not
// carry over, files or folders named relative to the checkout, patterns
// allowed; then it runs the command in the member's working directory,
// with VEYLOOM_REPO set to the checkout. A copy never replaces what the
// worktree has. What it did and the command's output come back in words,
// the output cut to its end; a command that fails is the error too.
//
// What getting ready left in the worktree that git neither tracks nor
// ignores is set down as the worktree's own (see prepared): it is none of
// the member's work, and is never counted, shown or merged as such.
func (g Git) Prepare(ctx context.Context, checkout, workDir string, copies []string, run string) (string, error) {
	log, err := prepare(ctx, checkout, workDir, copies, run)
	if err != nil {
		return log, err
	}
	return log, g.markPrepared(ctx, workDir)
}

func prepare(ctx context.Context, checkout, workDir string, copies []string, run string) (string, error) {
	var log strings.Builder
	for _, entry := range copies {
		if err := copyEntry(&log, checkout, workDir, entry); err != nil {
			return log.String(), err
		}
	}
	if strings.TrimSpace(run) == "" {
		return log.String(), nil
	}
	fmt.Fprintf(&log, "$ %s\n", run)
	cmd := exec.CommandContext(ctx, "sh", "-c", run)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), "VEYLOOM_REPO="+checkout, "VEYLOOM_WORKTREE="+workDir)
	out := &tail{max: outputTail}
	cmd.Stdout, cmd.Stderr = out, out
	err := cmd.Run()
	log.WriteString(out.String())
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return log.String(), fmt.Errorf("the setup command failed: %w", err)
	}
	return log.String(), nil
}

// copyEntry copies what entry names in the checkout into the worktree, at
// the same place, noting each copy or why there was none.
func copyEntry(log io.Writer, checkout, workDir, entry string) error {
	entry = filepath.Clean(strings.TrimSpace(entry))
	if entry == "." || filepath.IsAbs(entry) || entry == ".." || strings.HasPrefix(entry, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%q is not a path inside the checkout", entry)
	}
	matches, err := filepath.Glob(filepath.Join(checkout, entry))
	if err != nil {
		return fmt.Errorf("%q: %w", entry, err)
	}
	if len(matches) == 0 {
		fmt.Fprintf(log, "not in the checkout: %s\n", entry)
		return nil
	}
	for _, from := range matches {
		rel, err := filepath.Rel(checkout, from)
		if err != nil {
			return err
		}
		to := filepath.Join(workDir, rel)
		if _, err := os.Lstat(to); err == nil {
			fmt.Fprintf(log, "already there: %s\n", rel)
			continue
		}
		if err := copyTree(from, to); err != nil {
			return fmt.Errorf("copy %s: %w", rel, err)
		}
		fmt.Fprintf(log, "copied %s\n", rel)
	}
	return nil
}

// copyTree copies a file, a folder with all it holds, or a symbolic link as
// the link it is, keeping modes.
func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			return os.Symlink(target, dst)
		case info.IsDir():
			return os.MkdirAll(dst, info.Mode().Perm())
		default:
			return copyFile(path, dst, info.Mode().Perm())
		}
	})
}

func copyFile(from, to string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// tail keeps the last max bytes written to it.
type tail struct {
	max int
	buf []byte
	cut bool
}

func (t *tail) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
		t.cut = true
	}
	return len(p), nil
}

func (t *tail) String() string {
	out := string(bytes.ToValidUTF8(t.buf, nil))
	if t.cut {
		out = "…\n" + out
	}
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

// Remove takes a member's worktree away. What was not committed there is
// first committed on its branch with message, a merge stopped on conflicts
// given up, so the branch keeps all the work; the branch itself stays. A
// worktree already gone is only forgotten.
func (g Git) Remove(ctx context.Context, checkout, dir, message string) error {
	root, err := g.root(ctx, checkout)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		_, err := g.run(ctx, root, "worktree", "prune")
		return err
	}
	if err := g.commitAll(ctx, dir, message); err != nil {
		return err
	}
	_, err = g.run(ctx, root, "worktree", "remove", "--force", dir)
	return err
}

// commitAll commits everything changed in dir, new files included, on the
// branch there; nothing when nothing is. A merge stopped on conflicts is
// given up first: what the branch had before it is what is kept.
func (g Git) commitAll(ctx context.Context, dir, message string) error {
	if g.merging(ctx, dir) {
		if _, err := g.run(ctx, dir, "merge", "--abort"); err != nil {
			return err
		}
	}
	changed, err := g.uncommitted(ctx, dir)
	if err != nil || len(changed) == 0 {
		return err
	}
	if _, err := g.run(ctx, dir, "add", "-A"); err != nil {
		return err
	}
	if err := g.unstagePrepared(ctx, dir, nil); err != nil {
		return err
	}
	if staged, err := g.line(ctx, dir, "diff", "--cached", "--name-only"); err != nil || staged == "" {
		// Nothing but what getting ready made.
		return err
	}
	_, err = g.runEnv(ctx, dir, g.identity(ctx, dir), "commit", "-q", "--no-verify", "-m", message)
	return err
}

// merging reports whether a merge is under way in dir, stopped on
// conflicts.
func (g Git) merging(ctx context.Context, dir string) bool {
	_, err := g.run(ctx, dir, "rev-parse", "-q", "--verify", "MERGE_HEAD")
	return err == nil
}
