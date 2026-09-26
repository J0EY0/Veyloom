// Package worktree does what the members' worktrees need of git
// (docs/design.md 5.21): a worktree and a branch of its own for each member
// of a project, got ready for work the way the project's leader wrote down,
// how far each has come against the main line, its work squashed onto the
// main line as one commit, the main line brought into it, and the worktree
// taken away again. The main line is whatever branch the project's checkout
// has: the person's own.
//
// It runs the git executable. What it asks of git needs 2.38 or newer, for
// merge-tree --write-tree.
package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ReplaceRefBase is the namespace of Veyloom's own replace refs: the
// grafts that let git follow a squashed merge (see Squash). Only git run
// with GIT_REPLACE_REF_BASE set to it reads them, as every run of this
// package's is; people's own git, reading refs/replace/, sees the main
// line's history as it is.
const ReplaceRefBase = "refs/veyloom/replace/"

// AgentEnv is what the environment of an agent working in a project needs
// for its git to follow squashed merges the way this package's does.
var AgentEnv = []string{"GIT_REPLACE_REF_BASE=" + ReplaceRefBase}

// Git runs git for the operations of this package.
type Git struct {
	// Bin is the git executable; empty is "git" on PATH.
	Bin string
}

// Why an operation does not go ahead.
var (
	// ErrNotRepo says the checkout is not in a git repository.
	ErrNotRepo = errors.New("the checkout is not in a git repository")
	// ErrNoCommits says the repository has no commit to branch off yet.
	ErrNoCommits = errors.New("the repository has no commit yet")
	// ErrDetached says the checkout is on no branch, so there is no main
	// line to put work on.
	ErrDetached = errors.New("the checkout is on no branch")
	// ErrNoChanges says the worktree has nothing the main line lacks.
	ErrNoChanges = errors.New("the worktree changed nothing")
	// ErrMainLineMoved says the main line got a commit while work was
	// being put on it; trying again takes it in.
	ErrMainLineMoved = errors.New("the main line moved meanwhile")
	// ErrMergeUnderway says a merge is under way in the worktree, stopped
	// on conflicts: until it is concluded or given up, the worktree's work
	// neither goes onto the main line nor takes the main line in.
	ErrMergeUnderway = errors.New("a merge is under way in the worktree")
)

// Error is git failing: the command, how it exited and what it said.
type Error struct {
	Args   []string
	Code   int
	Stderr string
}

func (e *Error) Error() string {
	said := strings.TrimSpace(e.Stderr)
	if said == "" {
		return fmt.Sprintf("git %s: exit status %d", strings.Join(e.Args, " "), e.Code)
	}
	return fmt.Sprintf("git %s: %s", e.Args[0], said)
}

// exitCode is how git exited in err, or -1 when err is not git's exit.
func exitCode(err error) int {
	var gitErr *Error
	if errors.As(err, &gitErr) {
		return gitErr.Code
	}
	return -1
}

// call is one run of git.
type call struct {
	dir  string
	env  []string
	args []string
}

// run runs git in dir and returns what it wrote to stdout, trailing newline
// and all. Git is asked not to prompt and to speak plain English, so what
// it says can be read back, and to read Veyloom's grafts.
func (g Git) run(ctx context.Context, dir string, args ...string) (string, error) {
	return g.exec(ctx, call{dir: dir, args: args})
}

// runEnv is run with env added to git's environment.
func (g Git) runEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	return g.exec(ctx, call{dir: dir, env: env, args: args})
}

func (g Git) exec(ctx context.Context, c call) (string, error) {
	bin := g.Bin
	if bin == "" {
		bin = "git"
	}
	cmd := exec.CommandContext(ctx, bin, c.args...)
	cmd.Dir = c.dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "LANGUAGE=C")
	cmd.Env = append(append(cmd.Env, AgentEnv...), c.env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), nil
	}
	if ctx.Err() != nil {
		return stdout.String(), fmt.Errorf("git %s: %w", c.args[0], ctx.Err())
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), &Error{Args: c.args, Code: exitErr.ExitCode(), Stderr: stderr.String()}
	}
	return stdout.String(), fmt.Errorf("git %s: %w", c.args[0], err)
}

// line is one line of git's output, trimmed.
func (g Git) line(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := g.run(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

// pathspec names one path, from the top of the repository, to git as it
// is written: no pattern in it is expanded.
func pathspec(path string) string {
	return ":(top,literal)" + path
}

// literal is pathspec of each of paths.
func literal(paths []string) []string {
	specs := make([]string, len(paths))
	for i, p := range paths {
		specs[i] = pathspec(p)
	}
	return specs
}

// identity is what a commit made here needs of the environment: nothing
// when the repository says who commits, else Veyloom's name, so a checkout
// with no git identity set up still gets its commits.
func (g Git) identity(ctx context.Context, dir string) []string {
	var env []string
	if name, _ := g.line(ctx, dir, "config", "user.name"); name == "" {
		env = append(env, "GIT_AUTHOR_NAME=Veyloom", "GIT_COMMITTER_NAME=Veyloom")
	}
	if email, _ := g.line(ctx, dir, "config", "user.email"); email == "" {
		env = append(env, "GIT_AUTHOR_EMAIL=veyloom@localhost", "GIT_COMMITTER_EMAIL=veyloom@localhost")
	}
	return env
}
