package worktree_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/worktree"
)

// repo is a git repository made for a test, on branch main, with a first
// commit; git's own settings on this machine are kept out.
type repo struct {
	t    *testing.T
	dir  string
	root string
	g    worktree.Git
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r := &repo{t: t, dir: realDir(t), root: realDir(t)}
	r.git(r.dir, "init", "-q", "-b", "main")
	r.git(r.dir, "config", "user.name", "Alice")
	r.git(r.dir, "config", "user.email", "alice@example.com")
	r.write(r.dir, ".gitignore", ".env\nnode_modules/\ndocs/\n*.cfg\n")
	r.write(r.dir, "a.txt", "one\ntwo\nthree\n")
	r.write(r.dir, "b.txt", "bee\n")
	r.commit(r.dir, "first")
	return r
}

// realDir is a temporary folder by the name git gives it, symbolic links
// resolved (macOS keeps them under /private).
func realDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func (r *repo) git(dir string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(dir, name, body string) {
	r.t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) read(dir, name string) string {
	r.t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		r.t.Fatal(err)
	}
	return string(body)
}

func (r *repo) commit(dir, message string) string {
	r.t.Helper()
	r.git(dir, "add", "-A")
	r.git(dir, "commit", "-q", "-m", message)
	return r.git(dir, "rev-parse", "HEAD")
}

// create makes a member's worktree of the repository.
func (r *repo) create(name, branch string) worktree.Workspace {
	r.t.Helper()
	ws, err := r.g.Create(context.Background(), r.dir, r.root, name, branch)
	if err != nil {
		r.t.Fatal(err)
	}
	return ws
}

func paths(changes []worktree.Change) []string {
	var out []string
	for _, c := range changes {
		mark := c.Status
		if c.Uncommitted {
			mark += "*"
		}
		out = append(out, mark+" "+c.Path)
	}
	slices.Sort(out)
	return out
}

func TestInspect(t *testing.T) {
	r := newRepo(t)
	ctx := context.Background()
	if _, err := r.g.Inspect(ctx, t.TempDir()); !errors.Is(err, worktree.ErrNotRepo) {
		t.Errorf("a folder with no repository: %v", err)
	}

	r.write(r.dir, "a.txt", "one\n2\nthree\n")
	r.write(r.dir, "new.txt", "new\n")
	r.write(r.dir, ".env", "SECRET=1\n")
	got, err := r.g.Inspect(ctx, r.dir)
	if err != nil {
		t.Fatal(err)
	}
	head := r.git(r.dir, "rev-parse", "HEAD")
	if got.Root != r.dir || got.Sub != "" || got.Branch != "main" || got.Head != head {
		t.Errorf("the checkout: %+v", got)
	}
	// What is ignored is none of it.
	if want := []string{"A* new.txt", "M* a.txt"}; !slices.Equal(paths(got.Changed), want) {
		t.Errorf("uncommitted %v, want %v", paths(got.Changed), want)
	}

	// A checkout below the repository's top says where.
	r.write(r.dir, "web/app.ts", "x\n")
	if got, _ := r.g.Inspect(ctx, filepath.Join(r.dir, "web")); got.Sub != "web" || got.Root != r.dir {
		t.Errorf("a checkout in a folder of the repository: %+v", got)
	}

	// Detached, there is no branch; before a commit, no head.
	r.git(r.dir, "checkout", "-q", "--detach")
	if got, _ := r.g.Inspect(ctx, r.dir); got.Branch != "" || got.Head != head {
		t.Errorf("detached: %+v", got)
	}
	fresh := realDir(t)
	r.git(fresh, "init", "-q", "-b", "main")
	if got, err := r.g.Inspect(ctx, fresh); err != nil || got.Head != "" || got.Branch != "main" {
		t.Errorf("no commit yet: %+v %v", got, err)
	}
	if _, err := r.g.Create(ctx, fresh, r.root, "p/m", "veyloom/m"); !errors.Is(err, worktree.ErrNoCommits) {
		t.Errorf("a worktree before a commit: %v", err)
	}
}

func TestCreate(t *testing.T) {
	r := newRepo(t)
	head := r.git(r.dir, "rev-parse", "HEAD")
	ws := r.create("demo/coder", "veyloom/coder")
	if ws.Dir != filepath.Join(r.root, "demo/coder") || ws.Branch != "veyloom/coder" || ws.WorkDir != ws.Dir || ws.Base != head {
		t.Errorf("the worktree: %+v", ws)
	}
	if r.read(ws.Dir, "a.txt") != "one\ntwo\nthree\n" || r.git(ws.Dir, "rev-parse", "--abbrev-ref", "HEAD") != "veyloom/coder" {
		t.Error("the worktree holds the checkout's commit on its own branch")
	}

	// Taken names get a number; what is there is left alone.
	again := r.create("demo/coder", "veyloom/coder")
	if again.Dir != ws.Dir+"-2" || again.Branch != "veyloom/coder-2" {
		t.Errorf("names taken: %+v", again)
	}

	// A checkout in a folder of the repository: the member works in the
	// same folder of its worktree.
	r.write(r.dir, "web/app.ts", "x\n")
	r.commit(r.dir, "web")
	web, err := r.g.Create(context.Background(), filepath.Join(r.dir, "web"), r.root, "demo/web", "veyloom/web")
	if err != nil {
		t.Fatal(err)
	}
	if web.WorkDir != filepath.Join(web.Dir, "web") {
		t.Errorf("the working folder: %+v", web)
	}
}

func TestPrepare(t *testing.T) {
	r := newRepo(t)
	r.write(r.dir, ".env", "SECRET=1\n")
	r.write(r.dir, "docs/design.md", "# design\n")
	r.write(r.dir, "docs/notes/a.md", "a\n")
	r.write(r.dir, "local.cfg", "cfg\n")
	ws := r.create("demo/coder", "veyloom/coder")
	r.write(ws.WorkDir, "local.cfg", "mine\n")
	ctx := context.Background()

	log, err := r.g.Prepare(ctx, r.dir, ws.WorkDir, []string{".env", "docs/", "*.cfg", "missing.txt"}, `echo "from $VEYLOOM_REPO" > ready.txt; echo done`)
	if err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	if r.read(ws.WorkDir, ".env") != "SECRET=1\n" || r.read(ws.WorkDir, "docs/notes/a.md") != "a\n" {
		t.Error("the files git does not carry are copied, folders whole")
	}
	if r.read(ws.WorkDir, "local.cfg") != "mine\n" {
		t.Error("a copy replaced what the worktree had")
	}
	if r.read(ws.WorkDir, "ready.txt") != "from "+r.dir+"\n" {
		t.Error("the command runs in the worktree, told where the checkout is")
	}
	for _, want := range []string{"copied .env", "copied docs", "already there: local.cfg", "not in the checkout: missing.txt", "done"} {
		if !strings.Contains(log, want) {
			t.Errorf("the log lacks %q:\n%s", want, log)
		}
	}
	// What getting ready made is the worktree's own, not the member's work:
	// what .gitignore leaves out, and ready.txt as well.
	if st, err := r.g.Status(ctx, r.dir, ws.Dir); err != nil || len(st.Files) != 0 || st.Uncommitted != 0 {
		t.Errorf("after preparing: %+v %v", st, err)
	}
	r.write(ws.WorkDir, "work.txt", "mine\n")
	if st, _ := r.g.Status(ctx, r.dir, ws.Dir); !slices.Equal(paths(st.Files), []string{"A* work.txt"}) {
		t.Errorf("the member's work: %v", paths(st.Files))
	}
	if patch, _, _ := r.g.Diff(ctx, r.dir, ws.Dir, 0); strings.Contains(patch, "ready.txt") {
		t.Errorf("the patch shows what getting ready made:\n%s", patch)
	}
	merged, err := r.g.Squash(ctx, r.dir, ws.Dir, "Work", nil)
	if err != nil || merged.Commit == "" {
		t.Fatalf("squash: %+v %v", merged, err)
	}
	if files := r.git(r.dir, "show", "--name-only", "--format=", "HEAD"); files != "work.txt" {
		t.Errorf("the merge carried %q", files)
	}
	if r.read(ws.WorkDir, "ready.txt") == "" {
		t.Error("the worktree lost what getting ready made")
	}

	if _, err := r.g.Prepare(ctx, r.dir, ws.WorkDir, []string{"../outside"}, ""); err == nil {
		t.Error("a path out of the checkout is refused")
	}
	log, err = r.g.Prepare(ctx, r.dir, ws.WorkDir, nil, "echo about to fail; exit 3")
	if err == nil || !strings.Contains(log, "about to fail") {
		t.Errorf("a failing command: %v\n%s", err, log)
	}
}

func TestRemove(t *testing.T) {
	r := newRepo(t)
	ws := r.create("demo/coder", "veyloom/coder")
	r.write(ws.Dir, "a.txt", "one\nTWO\nthree\n")
	r.write(ws.Dir, "new.txt", "new\n")
	ctx := context.Background()
	if err := r.g.Remove(ctx, r.dir, ws.Dir, "Work Coder left"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ws.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Error("the worktree is still there")
	}
	// Nothing is lost: the branch has it, committed.
	if got := r.git(r.dir, "log", "-1", "--format=%s", "veyloom/coder"); got != "Work Coder left" {
		t.Errorf("the last commit of the branch: %q", got)
	}
	if got := r.git(r.dir, "show", "veyloom/coder:new.txt"); got != "new" {
		t.Errorf("new.txt on the branch: %q", got)
	}
	// Gone already, it is only forgotten.
	if err := r.g.Remove(ctx, r.dir, ws.Dir, "again"); err != nil {
		t.Errorf("removing it twice: %v", err)
	}
}
