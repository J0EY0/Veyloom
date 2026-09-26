package machine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// gitRepo is a repository with one commit, in a folder named the way git
// names it.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "Alice"},
		{"config", "user.email", "alice@example.com"},
	} {
		gitIn(t, dir, args...)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "first")
	return dir
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// ask sends req and waits for its answer.
func ask(t *testing.T, hub protocol.Conn, req protocol.WorkspaceRequest) protocol.WorkspaceResult {
	t.Helper()
	if err := hub.Send(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	res := recvKind[protocol.WorkspaceResult](t, hub)
	if res.RequestID != req.RequestID {
		t.Fatalf("answer to %q, want %q", res.RequestID, req.RequestID)
	}
	return res
}

func TestRun_WorkspaceRequests(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	checkout := gitRepo(t)
	w := New(Config{Name: "laptop", WorktreeDir: root, WorkspaceSetupTimeout: time.Minute}, NewDiscovery(nil, time.Second), &MemoryIdentity{}, runtime.BuiltinRunners())
	hub, _, _ := startMachine(t, w)
	recvKind[protocol.Hello](t, hub)
	if err := hub.Send(context.Background(), protocol.Welcome{MachineID: "w1", HeartbeatInterval: protocol.Duration(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r1", Op: protocol.WorkspaceInspect, Checkout: checkout})
	if res.Error != "" || res.Repo == nil || res.Repo.Branch != "main" {
		t.Fatalf("inspect: %+v", res)
	}
	res = ask(t, hub, protocol.WorkspaceRequest{RequestID: "r2", Op: protocol.WorkspaceCreate, Checkout: checkout, Name: "demo/coder", Branch: "veyloom/coder"})
	if res.Error != "" || res.Workspace == nil || res.Workspace.Dir != filepath.Join(root, "demo/coder") {
		t.Fatalf("create: %+v", res)
	}
	dir := res.Workspace.Dir
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = ask(t, hub, protocol.WorkspaceRequest{RequestID: "r3", Op: protocol.WorkspaceStatus, Checkout: checkout, Dir: dir})
	if res.Error != "" || res.Status == nil || res.Status.Uncommitted != 1 {
		t.Fatalf("status: %+v", res)
	}
	res = ask(t, hub, protocol.WorkspaceRequest{RequestID: "r4", Op: protocol.WorkspacePrepare, Checkout: checkout, WorkDir: dir, Run: "echo ready; exit 2"})
	if res.Error == "" || !strings.Contains(res.Text, "ready") {
		t.Errorf("a failing setup comes back with what it said: %+v", res)
	}
	res = ask(t, hub, protocol.WorkspaceRequest{RequestID: "r5", Op: protocol.WorkspaceSquash, Checkout: checkout, Dir: dir, Message: "Two"})
	if res.Error != "" || res.Merge == nil || res.Merge.Commit == "" {
		t.Fatalf("squash: %+v", res)
	}

	// The person's checkout is no worktree to reset or merge into.
	for i, req := range []protocol.WorkspaceRequest{
		{Op: protocol.WorkspaceSquash, Checkout: checkout, Dir: checkout, Message: "no"},
		{Op: protocol.WorkspaceRemove, Checkout: checkout, Dir: filepath.Join(root, "..", "elsewhere")},
		{Op: protocol.WorkspaceCreate, Checkout: checkout, Name: "../escape", Branch: "veyloom/x"},
		{Op: "rename", Checkout: checkout},
	} {
		req.RequestID = "bad" + string(rune('a'+i))
		if res := ask(t, hub, req); res.Error == "" {
			t.Errorf("%s was done: %+v", req.Op, res)
		}
	}
	// Failures the hub tells apart are named.
	if res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r6", Op: protocol.WorkspaceInspect, Checkout: t.TempDir()}); res.Code != protocol.WorkspaceNotRepo {
		t.Errorf("not a repository: %+v", res)
	}
	if res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r7", Op: protocol.WorkspaceSquash, Checkout: checkout, Dir: dir, Message: "again"}); res.Code != protocol.WorkspaceNoChanges {
		t.Errorf("nothing to squash: %+v", res)
	}
	if res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r8", Op: protocol.WorkspaceSync, Checkout: checkout, Dir: filepath.Join(root, "demo/gone"), FastForwardOnly: true}); res.Code != protocol.WorkspaceGone {
		t.Errorf("a worktree taken away: %+v", res)
	}

	// A merge the member left under way: seen to, guarded, given up.
	if res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r9", Op: protocol.WorkspaceConclude, Checkout: checkout, Dir: dir}); res.Error != "" || res.Conclude == nil || res.Conclude.Commit != "" {
		t.Errorf("nothing to conclude: %+v", res)
	}
	for _, step := range []struct {
		dir, body string
	}{{dir, "member\n"}, {checkout, "person\n"}} {
		if err := os.WriteFile(filepath.Join(step.dir, "a.txt"), []byte(step.body), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, step.dir, "commit", "-qam", strings.TrimSpace(step.body))
	}
	merge := exec.Command("git", "merge", "-q", "main")
	merge.Dir = dir
	if err := merge.Run(); err == nil {
		t.Fatal("the merge should stop on a conflict")
	}
	if res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r10", Op: protocol.WorkspaceSquash, Checkout: checkout, Dir: dir, Message: "no"}); res.Code != protocol.WorkspaceMergeUnderway {
		t.Errorf("squash during a merge: %+v", res)
	}
	if res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r11", Op: protocol.WorkspaceConclude, Checkout: checkout, Dir: dir}); res.Error != "" || res.Conclude == nil || len(res.Conclude.Unresolved) != 1 {
		t.Errorf("a merge with conflicts left: %+v", res)
	}
	if res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r12", Op: protocol.WorkspaceAbortMerge, Checkout: checkout, Dir: dir}); res.Error != "" {
		t.Errorf("give the merge up: %+v", res)
	}
	if res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r13", Op: protocol.WorkspaceStatus, Checkout: checkout, Dir: dir}); res.Status == nil || res.Status.Merging {
		t.Errorf("after giving up: %+v", res)
	}
	if res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r14", Op: protocol.WorkspaceAbortMerge, Checkout: checkout, Dir: checkout}); res.Error == "" {
		t.Errorf("the checkout is no worktree to give a merge up in: %+v", res)
	}

	// Only new files are left out of a merge; the work is set aside, and
	// never the person's checkout.
	res = ask(t, hub, protocol.WorkspaceRequest{RequestID: "r15", Op: protocol.WorkspaceSquash, Checkout: checkout, Dir: dir, Message: "no", Leave: []string{"a.txt"}})
	if res.Code != protocol.WorkspaceNotNew || !slices.Equal(res.Files, []string{"a.txt"}) {
		t.Errorf("leaving out a committed file: %+v", res)
	}
	if res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r16", Op: protocol.WorkspaceSetAside, Checkout: checkout, Dir: checkout, Message: "no"}); res.Error == "" {
		t.Errorf("the checkout was set aside: %+v", res)
	}
	if res := ask(t, hub, protocol.WorkspaceRequest{RequestID: "r18", Op: protocol.WorkspaceOverlap, Checkout: checkout, Sides: []worktree.Side{{Head: "x"}}}); res.Error == "" {
		t.Errorf("an overlap of one side: %+v", res)
	}
	res = ask(t, hub, protocol.WorkspaceRequest{RequestID: "r17", Op: protocol.WorkspaceSetAside, Checkout: checkout, Dir: dir, Message: "Coder's"})
	if res.Error != "" || !strings.HasPrefix(res.Ref, worktree.SetAsideRefs) {
		t.Errorf("set aside: %+v", res)
	}
}
