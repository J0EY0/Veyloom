package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/protocol"
)

// turnRunner executes turns on this worker's engines and streams their
// events to the hub. One runner serves one hub connection; Worker.Run owns
// it and shuts it down when the connection ends.
//
// Turns run concurrently, each in its own goroutine. Events flow engine →
// pump → conn; the engine blocks when the pump is slow, so a slow hub
// applies backpressure instead of growing memory.
type turnRunner struct {
	runners map[string]engine.Runner
	conn    protocol.Conn
	flush   time.Duration

	mu     sync.Mutex
	active map[string]engine.Turn
	wg     sync.WaitGroup
}

func newTurnRunner(runners map[string]engine.Runner, conn protocol.Conn, flush time.Duration) *turnRunner {
	return &turnRunner{
		runners: runners,
		conn:    conn,
		flush:   flush,
		active:  make(map[string]engine.Turn),
	}
}

// start launches a turn. Problems that prevent it from starting, such as
// an engine this worker cannot run, are reported to the hub as an
// immediate TurnDone with an error rather than returned, because the hub
// is waiting for exactly that message.
func (r *turnRunner) start(ctx context.Context, req protocol.StartTurn) {
	runner, ok := r.runners[req.Engine]
	if !ok {
		r.reportFailure(ctx, req.TurnID, fmt.Errorf("engine %q has no runner on this worker", req.Engine))
		return
	}
	if !r.reserve(req.TurnID) {
		r.reportFailure(ctx, req.TurnID, fmt.Errorf("turn %s is already running", req.TurnID))
		return
	}

	turn, err := runner.StartTurn(ctx, req.Spec)
	if err != nil {
		r.release(req.TurnID)
		r.reportFailure(ctx, req.TurnID, fmt.Errorf("start %s turn: %w", req.Engine, err))
		return
	}
	r.track(req.TurnID, turn)

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer r.release(req.TurnID)
		r.pump(ctx, req.TurnID, turn)
	}()
}

// cancel stops a running turn. Unknown ids are ignored: the turn may have
// finished while the hub's request was in flight.
func (r *turnRunner) cancel(turnID string) {
	if turn := r.lookup(turnID); turn != nil {
		turn.Cancel()
	}
}

// answer passes the hub's decision to the turn that asked. As with cancel,
// a decision for a turn or request that is gone is dropped: the turn ended
// while the person was deciding, and nothing is waiting any more.
func (r *turnRunner) answer(turnID, approvalID string, d engine.Decision) {
	if turn := r.lookup(turnID); turn != nil {
		_ = turn.Answer(approvalID, d)
	}
}

// lookup returns a running turn, or nil while it is starting or gone.
func (r *turnRunner) lookup(turnID string) engine.Turn {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active[turnID]
}

// shutdown cancels every turn and waits for their pumps to finish.
func (r *turnRunner) shutdown() {
	r.mu.Lock()
	for _, turn := range r.active {
		if turn != nil {
			turn.Cancel()
		}
	}
	r.mu.Unlock()
	r.wg.Wait()
}

// reserve claims a turn id before the engine starts, so a duplicate
// StartTurn cannot launch a second engine process.
func (r *turnRunner) reserve(turnID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.active[turnID]; exists {
		return false
	}
	r.active[turnID] = nil
	return true
}

func (r *turnRunner) track(turnID string, turn engine.Turn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active[turnID] = turn
}

func (r *turnRunner) release(turnID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, turnID)
}

// pump forwards a turn's events to the hub, coalescing text, and finishes
// with a TurnDone. If the connection fails mid-turn the turn is cancelled:
// nobody is listening any more.
func (r *turnRunner) pump(ctx context.Context, turnID string, turn engine.Turn) {
	buf := newTextBuffer(r.flush)
	send := func(ev engine.Event) bool {
		return r.conn.Send(ctx, outbound(turnID, ev)) == nil
	}

	events := turn.Events()
	for events != nil {
		select {
		case ev, ok := <-events:
			if !ok {
				events = nil
				break
			}
			for _, out := range buf.add(ev) {
				if !send(out) {
					r.abandon(turn)
					return
				}
			}
		case <-buf.deadline():
			if ev, ok := buf.take(); ok && !send(ev) {
				r.abandon(turn)
				return
			}
		}
	}
	if ev, ok := buf.take(); ok && !send(ev) {
		r.abandon(turn)
		return
	}

	res, err := turn.Result()
	done := protocol.TurnDone{TurnID: turnID, Result: res}
	if err != nil {
		done.Error = err.Error()
		done.Cancelled = errors.Is(err, engine.ErrTurnCancelled)
	}
	_ = r.conn.Send(ctx, done)
}

// outbound is the protocol message for one engine event. Approval requests
// travel as their own kind because the hub must act on them and reply;
// everything else is a fire-and-forget TurnEvent.
func outbound(turnID string, ev engine.Event) protocol.Message {
	if ev.Kind == engine.EventApprovalRequest {
		return protocol.ApprovalRequest{TurnID: turnID, ApprovalID: ev.ApprovalID, Tool: ev.Tool, Input: ev.Input, At: ev.At}
	}
	return protocol.TurnEvent{TurnID: turnID, Event: ev}
}

// abandon stops a turn whose events can no longer be delivered and drains
// it so the engine goroutine can exit.
func (r *turnRunner) abandon(turn engine.Turn) {
	turn.Cancel()
	for range turn.Events() {
	}
}

func (r *turnRunner) reportFailure(ctx context.Context, turnID string, err error) {
	_ = r.conn.Send(ctx, protocol.TurnDone{TurnID: turnID, Error: err.Error()})
}

// textBuffer coalesces consecutive text events into one message per flush
// window. Engines stream replies in small chunks; without this every chunk
// would cost a protocol message. Non-text events flush the buffer first so
// ordering is preserved.
type textBuffer struct {
	window time.Duration
	text   strings.Builder
	first  time.Time
	timer  *time.Timer
}

func newTextBuffer(window time.Duration) *textBuffer {
	return &textBuffer{window: window}
}

// add absorbs ev and returns whatever must be sent now.
func (b *textBuffer) add(ev engine.Event) []engine.Event {
	if ev.Kind != engine.EventText || b.window <= 0 {
		out := make([]engine.Event, 0, 2)
		if pending, ok := b.take(); ok {
			out = append(out, pending)
		}
		return append(out, ev)
	}
	if b.text.Len() == 0 {
		b.first = ev.At
		b.timer = time.NewTimer(b.window)
	}
	b.text.WriteString(ev.Text)
	return nil
}

// deadline is the channel that fires when buffered text has waited long
// enough; nil (never fires) when nothing is buffered.
func (b *textBuffer) deadline() <-chan time.Time {
	if b.timer == nil {
		return nil
	}
	return b.timer.C
}

// take returns the buffered text as one event and resets the buffer.
func (b *textBuffer) take() (engine.Event, bool) {
	if b.text.Len() == 0 {
		return engine.Event{}, false
	}
	ev := engine.Event{Kind: engine.EventText, At: b.first, Text: b.text.String()}
	b.text.Reset()
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	return ev, true
}
