package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrUnknownApproval is returned by Turn.Answer when the approval id is not
// pending: it was already answered, the turn is over, or it never existed.
var ErrUnknownApproval = errors.New("runtime: unknown approval")

// turnBase implements the Turn plumbing every runtime needs: the event
// channel, the done signal, the guarded result and the approval registry.
// A runtime embeds it, runs its work on a goroutine that emits events, and
// calls finish exactly once at the end.
type turnBase struct {
	events chan Event
	done   chan struct{}
	cancel context.CancelFunc

	mu     sync.Mutex
	result Result
	err    error
	// pending holds one channel per approval request awaiting an Answer.
	// A channel is closed, never sent on, when the turn ends first.
	pending map[string]chan Decision
}

func newTurnBase(cancel context.CancelFunc) *turnBase {
	return &turnBase{
		events:  make(chan Event, 16),
		done:    make(chan struct{}),
		cancel:  cancel,
		pending: make(map[string]chan Decision),
	}
}

// failedTurn is a turn that was over before it began: no events, and the
// given outcome. A runner returns one instead of an error from StartTurn
// when the outcome has something to say beyond the error, its Failure.
func failedTurn(res Result, err error) Turn {
	ctx, cancel := context.WithCancel(context.Background())
	t := newTurnBase(cancel)
	t.finish(ctx, res, err)
	cancel()
	return t
}

// Events implements Turn.
func (t *turnBase) Events() <-chan Event { return t.events }

// Result implements Turn.
func (t *turnBase) Result() (Result, error) {
	<-t.done
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.result, t.err
}

// Cancel implements Turn.
func (t *turnBase) Cancel() { t.cancel() }

// emit delivers ev unless ctx ends first. It stamps the time when the
// runtime did not.
func (t *turnBase) emit(ctx context.Context, ev Event) bool {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	select {
	case t.events <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// requestApproval asks permission to use tool with input and blocks until
// Answer settles it or the turn ends. Runtimes call it from the goroutine
// that needs the decision, which for a CLI is the one serving the CLI's
// permission callback.
func (t *turnBase) requestApproval(ctx context.Context, tool, input string) (Decision, error) {
	id := randomHex(8)
	ch := make(chan Decision, 1)
	t.mu.Lock()
	t.pending[id] = ch
	t.mu.Unlock()

	if !t.emit(ctx, Event{Kind: EventApprovalRequest, ApprovalID: id, Tool: tool, Input: input}) {
		t.forget(id)
		return Decision{}, ErrTurnCancelled
	}
	select {
	case d, ok := <-ch:
		if !ok {
			return Decision{}, ErrTurnCancelled
		}
		return d, nil
	case <-ctx.Done():
		t.forget(id)
		return Decision{}, ErrTurnCancelled
	}
}

// Answer implements Turn. Each request accepts exactly one answer.
func (t *turnBase) Answer(approvalID string, d Decision) error {
	t.mu.Lock()
	ch, ok := t.pending[approvalID]
	delete(t.pending, approvalID)
	t.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownApproval, approvalID)
	}
	ch <- d
	return nil
}

// forget drops a pending request that will not be answered.
func (t *turnBase) forget(approvalID string) {
	t.mu.Lock()
	delete(t.pending, approvalID)
	t.mu.Unlock()
}

// finish records the outcome and closes the channels. If ctx was cancelled
// the error is ErrTurnCancelled regardless of what the runtime reported,
// though the tokens it had spent by then still count. Approval requests
// still pending are released so their waiters return.
func (t *turnBase) finish(ctx context.Context, res Result, err error) {
	if ctx.Err() != nil {
		res, err = Result{Usage: res.Usage}, ErrTurnCancelled
	}
	t.mu.Lock()
	t.result, t.err = res, err
	for id, ch := range t.pending {
		close(ch)
		delete(t.pending, id)
	}
	t.mu.Unlock()
	close(t.events)
	close(t.done)
}
