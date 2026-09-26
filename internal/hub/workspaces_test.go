package hub

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// A request on a checkout goes to its machine and waits for the answer
// with its id; a machine gone, or not there, fails it at once.
func TestWorkspace_AsksTheMachineAndWaits(t *testing.T) {
	h := New(newFakeStore(), Config{HeartbeatInterval: time.Hour})
	conn, _ := connect(t, h)
	machineID := handshake(t, conn, protocol.Hello{Name: "laptop"}).MachineID
	ctx := context.Background()

	type answer struct {
		res protocol.WorkspaceResult
		err error
	}
	ask := func(req protocol.WorkspaceRequest) <-chan answer {
		out := make(chan answer, 1)
		go func() {
			res, err := h.workspace(ctx, machineID, req)
			out <- answer{res, err}
		}()
		return out
	}
	recv := func() protocol.WorkspaceRequest {
		t.Helper()
		rctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		m, err := conn.Recv(rctx)
		if err != nil {
			t.Fatal(err)
		}
		req, ok := m.(protocol.WorkspaceRequest)
		if !ok {
			t.Fatalf("got %T", m)
		}
		return req
	}

	got := ask(protocol.WorkspaceRequest{Op: protocol.WorkspaceInspect, Checkout: "/src/app"})
	req := recv()
	if req.RequestID == "" || req.Op != protocol.WorkspaceInspect || req.Checkout != "/src/app" {
		t.Fatalf("the machine got %+v", req)
	}
	// An answer to nobody is dropped; the one asked for arrives.
	if err := conn.Send(ctx, protocol.WorkspaceResult{RequestID: "stray"}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Send(ctx, protocol.WorkspaceResult{RequestID: req.RequestID, Repo: &worktree.Repo{Branch: "main"}}); err != nil {
		t.Fatal(err)
	}
	select {
	case a := <-got:
		if a.err != nil || a.res.Repo == nil || a.res.Repo.Branch != "main" {
			t.Errorf("the answer: %+v %v", a.res, a.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no answer")
	}

	// Waiting, the machine goes away.
	got = ask(protocol.WorkspaceRequest{Op: protocol.WorkspaceStatus, Checkout: "/src/app", Dir: "/wt/app/coder"})
	recv()
	conn.Close()
	select {
	case a := <-got:
		if !errors.Is(a.err, ErrMachineOffline) {
			t.Errorf("the machine went away: %v", a.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("still waiting for a machine that is gone")
	}
	eventually(t, func() bool { return len(h.Machines()) == 0 }, "the machine to be forgotten")
	if _, err := h.workspace(ctx, machineID, protocol.WorkspaceRequest{Op: protocol.WorkspaceInspect}); !errors.Is(err, ErrMachineOffline) {
		t.Errorf("a machine not connected: %v", err)
	}
}
