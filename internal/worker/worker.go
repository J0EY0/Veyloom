package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/protocol"
)

// Worker represents this machine to the hub. It introduces itself with its
// identity and the engines it found, keeps the connection alive with
// heartbeats and re-runs discovery when the hub asks.
type Worker struct {
	cfg       Config
	discovery *Discovery
	identity  Identity
	runners   map[string]engine.Runner
}

// New creates a Worker. identity remembers the hub-assigned ID between
// runs; discovery finds the engines reported to the hub; runners are the
// engines this worker can execute turns on, keyed by engine name.
func New(cfg Config, discovery *Discovery, identity Identity, runners map[string]engine.Runner) *Worker {
	return &Worker{cfg: cfg.withDefaults(), discovery: discovery, identity: identity, runners: runners}
}

// Run connects to the hub over conn and serves it until ctx is cancelled or
// the hub disconnects. A clean disconnect returns nil.
func (w *Worker) Run(ctx context.Context, conn protocol.Conn) error {
	defer conn.Close()

	savedID, err := w.identity.Load()
	if err != nil {
		return fmt.Errorf("load worker identity: %w", err)
	}

	hello := protocol.Hello{WorkerID: savedID, Name: w.cfg.Name, Engines: w.discovery.Run(ctx)}
	if err := conn.Send(ctx, hello); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}
	welcome, err := awaitWelcome(ctx, conn, w.cfg.HandshakeTimeout)
	if err != nil {
		return err
	}
	// The hub may hand out a new id, for example on first contact or after
	// its database was reset. Remember it before doing anything else so a
	// crash later in this run cannot lose it.
	if welcome.WorkerID != savedID {
		if err := w.identity.Save(welcome.WorkerID); err != nil {
			return fmt.Errorf("save worker identity: %w", err)
		}
	}

	interval := time.Duration(welcome.HeartbeatInterval)
	if interval <= 0 {
		interval = w.cfg.HeartbeatInterval
	}

	turns := newTurnRunner(w.runners, conn, w.cfg.EventFlushInterval)
	defer turns.shutdown()

	// Recv blocks, so it runs in its own goroutine and feeds the select
	// below. The goroutine ends when the connection or ctx ends.
	inbound := make(chan protocol.Message)
	recvErr := make(chan error, 1)
	go func() {
		for {
			m, err := conn.Recv(ctx)
			if err != nil {
				recvErr <- err
				return
			}
			select {
			case inbound <- m:
			case <-ctx.Done():
				return
			}
		}
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-recvErr:
			if errors.Is(err, protocol.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("receive: %w", err)
		case <-ticker.C:
			if err := conn.Send(ctx, protocol.Heartbeat{}); err != nil {
				return fmt.Errorf("send heartbeat: %w", err)
			}
		case m := <-inbound:
			if err := w.handle(ctx, conn, turns, m); err != nil {
				return err
			}
		}
	}
}

// awaitWelcome reads the hub's reply to Hello and insists that it is a
// Welcome.
func awaitWelcome(ctx context.Context, conn protocol.Conn, timeout time.Duration) (protocol.Welcome, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	m, err := conn.Recv(ctx)
	if err != nil {
		return protocol.Welcome{}, fmt.Errorf("await welcome: %w", err)
	}
	welcome, ok := m.(protocol.Welcome)
	if !ok {
		return protocol.Welcome{}, fmt.Errorf("await welcome: got %s instead", m.Kind())
	}
	return welcome, nil
}

// handle reacts to one message from the hub. Unknown kinds are ignored so a
// newer hub cannot take down an older worker.
//
// Discovery runs inline, which pauses heartbeats for its duration. That is
// well within any sensible heartbeat timeout; it can move to a goroutine if
// discovery ever grows slower. Turns never block the loop: the runner puts
// each one on its own goroutine.
func (w *Worker) handle(ctx context.Context, conn protocol.Conn, turns *turnRunner, m protocol.Message) error {
	switch m := m.(type) {
	case protocol.Probe:
		report := protocol.EnginesReport{Engines: w.discovery.Run(ctx)}
		if err := conn.Send(ctx, report); err != nil {
			return fmt.Errorf("send engines report: %w", err)
		}
	case protocol.StartTurn:
		turns.start(ctx, m)
	case protocol.CancelTurn:
		turns.cancel(m.TurnID)
	case protocol.ApprovalDecision:
		turns.answer(m.TurnID, m.ApprovalID, m.Decision)
	}
	return nil
}
