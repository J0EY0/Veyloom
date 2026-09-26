package machine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// workspaceTimeout bounds one git operation; prepare's command has the
// machine's WorkspaceSetupTimeout on top.
const workspaceTimeout = 2 * time.Minute

// workspaces does what the hub asks of the project checkouts and the
// members' worktrees on this machine (docs/design.md 5.21). Each request
// runs on a goroutine of its own, so a long setup command holds nothing
// else up; the requests on one checkout take turns, as two merges onto one
// main line must not cross. It only ever works in worktrees under its own
// folder: a worktree is reset, merged into and removed, which the person's
// checkout must never be.
type workspaces struct {
	git   worktree.Git
	root  string
	setup time.Duration
	conn  protocol.Conn

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newWorkspaces(ctx context.Context, cfg Config, conn protocol.Conn) *workspaces {
	ctx, cancel := context.WithCancel(ctx)
	return &workspaces{root: cfg.WorktreeDir, setup: cfg.WorkspaceSetupTimeout, conn: conn, ctx: ctx, cancel: cancel, locks: map[string]*sync.Mutex{}}
}

// start does req and sends the hub its result.
func (w *workspaces) start(req protocol.WorkspaceRequest) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		res := w.do(w.ctx, req)
		res.RequestID = req.RequestID
		// A connection gone takes the answer with it; the hub gives up on
		// its own.
		_ = w.conn.Send(w.ctx, res)
	}()
}

// shutdown stops what is under way and waits for it.
func (w *workspaces) shutdown() {
	w.cancel()
	w.wg.Wait()
}

// lock takes the checkout's turn; the returned func gives it back.
func (w *workspaces) lock(checkout string) func() {
	w.mu.Lock()
	l := w.locks[checkout]
	if l == nil {
		l = &sync.Mutex{}
		w.locks[checkout] = l
	}
	w.mu.Unlock()
	l.Lock()
	return l.Unlock
}

func (w *workspaces) do(ctx context.Context, req protocol.WorkspaceRequest) protocol.WorkspaceResult {
	if !filepath.IsAbs(req.Checkout) {
		return failed(fmt.Errorf("the checkout must be an absolute path, not %q", req.Checkout))
	}
	defer w.lock(filepath.Clean(req.Checkout))()
	timeout := workspaceTimeout
	if req.Op == protocol.WorkspacePrepare {
		timeout += w.setup
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var res protocol.WorkspaceResult
	var err error
	switch req.Op {
	case protocol.WorkspaceInspect:
		var repo worktree.Repo
		repo, err = w.git.Inspect(ctx, req.Checkout)
		res.Repo = &repo
	case protocol.WorkspaceCreate:
		if err = w.checkName(req.Name); err == nil {
			var ws worktree.Workspace
			ws, err = w.git.Create(ctx, req.Checkout, w.root, req.Name, req.Branch)
			res.Workspace = &ws
		}
	case protocol.WorkspacePrepare:
		if err = w.inside(req.WorkDir); err == nil {
			setup, cancel := context.WithTimeout(ctx, w.setup)
			res.Text, err = w.git.Prepare(setup, req.Checkout, req.WorkDir, req.Copy, req.Run)
			cancel()
			if err != nil {
				// What was done up to the failure goes back with it.
				return withText(failed(err), res.Text)
			}
		}
	case protocol.WorkspaceStatus:
		if err = w.there(req.Dir); err == nil {
			var st worktree.Status
			st, err = w.git.Status(ctx, req.Checkout, req.Dir)
			res.Status = &st
		}
	case protocol.WorkspaceDiff:
		if err = w.there(req.Dir); err == nil {
			res.Text, res.Cut, err = w.git.Diff(ctx, req.Checkout, req.Dir, req.MaxBytes)
		}
	case protocol.WorkspaceSquash:
		if err = w.there(req.Dir); err == nil {
			var m worktree.MergeResult
			m, err = w.git.Squash(ctx, req.Checkout, req.Dir, req.Message, req.Leave)
			res.Merge = &m
		}
	case protocol.WorkspaceSync:
		if err = w.there(req.Dir); err == nil {
			var s worktree.SyncResult
			s, err = w.git.Sync(ctx, req.Checkout, req.Dir, req.FastForwardOnly)
			res.Sync = &s
		}
	case protocol.WorkspaceRemove:
		if err = w.inside(req.Dir); err == nil {
			err = w.git.Remove(ctx, req.Checkout, req.Dir, req.Message)
		}
	case protocol.WorkspaceConclude:
		if err = w.there(req.Dir); err == nil {
			var c worktree.ConcludeResult
			c, err = w.git.Conclude(ctx, req.Dir)
			res.Conclude = &c
		}
	case protocol.WorkspaceAbortMerge:
		if err = w.there(req.Dir); err == nil {
			err = w.git.AbortMerge(ctx, req.Dir)
		}
	case protocol.WorkspaceCheckoutDiff:
		res.Text, res.Cut, err = w.git.CheckoutDiff(ctx, req.Checkout, req.MaxBytes)
	case protocol.WorkspaceCheckoutCommit:
		res.Commit, err = w.git.CommitCheckout(ctx, req.Checkout, req.Message, req.Paths)
	case protocol.WorkspaceSetAside:
		if err = w.there(req.Dir); err == nil {
			res.Ref, err = w.git.SetAside(ctx, req.Checkout, req.Dir, req.Message)
		}
	case protocol.WorkspaceOverlap:
		if len(req.Sides) != 2 {
			err = fmt.Errorf("overlap sets two worktrees side by side, not %d", len(req.Sides))
		} else {
			res.Files, err = w.git.Overlap(ctx, req.Checkout, req.Sides[0], req.Sides[1])
		}
	default:
		err = fmt.Errorf("no workspace operation %q here", req.Op)
	}
	if err != nil {
		return failed(err)
	}
	return res
}

// checkName insists that a new worktree's name stays in the folder for
// them.
func (w *workspaces) checkName(name string) error {
	if w.root == "" {
		return errNoFolder
	}
	clean := filepath.Clean(name)
	if name == "" || filepath.IsAbs(name) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("a worktree is named by a path inside the folder for them, not %q", name)
	}
	return nil
}

// inside insists that dir is in the folder for worktrees: the work done
// there resets and merges, which the person's own checkout must never get.
func (w *workspaces) inside(dir string) error {
	if w.root == "" {
		return errNoFolder
	}
	rel, err := filepath.Rel(filepath.Clean(w.root), filepath.Clean(dir))
	if err != nil || !filepath.IsAbs(dir) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s is not a worktree of this machine's", dir)
	}
	return nil
}

// there is inside, and the worktree still there: one taken away outside
// Veyloom is errGone.
func (w *workspaces) there(dir string) error {
	if err := w.inside(dir); err != nil {
		return err
	}
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s: %w", dir, errGone)
	}
	return nil
}

var (
	errNoFolder = errors.New("this machine has no folder for worktrees")
	errGone     = errors.New("the worktree is not there any more")
)

// failed is the result of an operation that failed with err, named by a
// code where the hub tells it apart.
func failed(err error) protocol.WorkspaceResult {
	res := protocol.WorkspaceResult{Error: err.Error()}
	switch {
	case errors.Is(err, worktree.ErrNotRepo):
		res.Code = protocol.WorkspaceNotRepo
	case errors.Is(err, worktree.ErrNoCommits):
		res.Code = protocol.WorkspaceNoCommits
	case errors.Is(err, worktree.ErrDetached):
		res.Code = protocol.WorkspaceDetached
	case errors.Is(err, worktree.ErrNoChanges):
		res.Code = protocol.WorkspaceNoChanges
	case errors.Is(err, worktree.ErrMainLineMoved):
		res.Code = protocol.WorkspaceMainLineMoved
	case errors.Is(err, worktree.ErrMergeUnderway):
		res.Code = protocol.WorkspaceMergeUnderway
	case errors.Is(err, errNoFolder):
		res.Code = protocol.WorkspaceNoFolder
	case errors.Is(err, errGone):
		res.Code = protocol.WorkspaceGone
	}
	var changed *worktree.CheckoutChangedError
	if errors.As(err, &changed) {
		res.Code, res.Files = protocol.WorkspaceCheckoutChanged, changed.Files
	}
	var notNew *worktree.NotNewError
	if errors.As(err, &notNew) {
		res.Code, res.Files = protocol.WorkspaceNotNew, notNew.Files
	}
	return res
}

func withText(res protocol.WorkspaceResult, text string) protocol.WorkspaceResult {
	res.Text = text
	return res
}
