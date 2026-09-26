package hub

import (
	"context"
	"crypto/rand"
	"sync"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/store"
)

// Asking a machine to work on a project's checkout or a member's worktree
// (docs/design.md 5.21): the checkout is on the machine the members run
// on, and so is git. One request, one answer, matched by the request's id.

// ErrMachineOffline says the machine a checkout is on is not connected, or
// went away before it answered.
var ErrMachineOffline = store.Conflicting("machineOffline", nil, "the machine the project's checkout is on is not connected")

// workspaceCalls are the requests waiting for their machine's answer.
type workspaceCalls struct {
	mu      sync.Mutex
	waiting map[string]waitingCall
}

type waitingCall struct {
	machine string
	answer  chan protocol.WorkspaceResult
}

// workspace sends req to the machine and waits for the answer as long as
// ctx lets it. A failure the machine reports comes back in the result, not
// as the error: the error is only for the answer not coming.
func (h *Hub) workspace(ctx context.Context, machineID string, req protocol.WorkspaceRequest) (protocol.WorkspaceResult, error) {
	conn, ok := h.connFor(machineID)
	if !ok {
		return protocol.WorkspaceResult{}, ErrMachineOffline
	}
	req.RequestID = rand.Text()
	answer := make(chan protocol.WorkspaceResult, 1)
	h.calls.mu.Lock()
	if h.calls.waiting == nil {
		h.calls.waiting = map[string]waitingCall{}
	}
	h.calls.waiting[req.RequestID] = waitingCall{machine: machineID, answer: answer}
	h.calls.mu.Unlock()
	defer func() {
		h.calls.mu.Lock()
		delete(h.calls.waiting, req.RequestID)
		h.calls.mu.Unlock()
	}()

	if err := conn.Send(ctx, req); err != nil {
		return protocol.WorkspaceResult{}, err
	}
	select {
	case res, ok := <-answer:
		if !ok {
			return protocol.WorkspaceResult{}, ErrMachineOffline
		}
		return res, nil
	case <-ctx.Done():
		return protocol.WorkspaceResult{}, ctx.Err()
	}
}

// onWorkspaceResult hands an answer to the request waiting for it; one
// nobody waits for any more is dropped.
func (h *Hub) onWorkspaceResult(res protocol.WorkspaceResult) {
	h.calls.mu.Lock()
	call, ok := h.calls.waiting[res.RequestID]
	delete(h.calls.waiting, res.RequestID)
	h.calls.mu.Unlock()
	if ok {
		call.answer <- res
	}
}

// workspaceMachineGone fails the requests a machine that went away will
// never answer.
func (h *Hub) workspaceMachineGone(machineID string) {
	h.calls.mu.Lock()
	defer h.calls.mu.Unlock()
	for id, call := range h.calls.waiting {
		if call.machine == machineID {
			close(call.answer)
			delete(h.calls.waiting, id)
		}
	}
}
