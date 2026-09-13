package protocol

import (
	"context"
	"errors"
	"sync"
)

// ErrClosed is returned by Send and Recv once either end of a connection has
// been closed.
var ErrClosed = errors.New("protocol: connection closed")

// Conn is a bidirectional, message-oriented connection between a hub and a
// worker. Implementations must allow Send and Recv to run concurrently, and
// multiple goroutines to call Send at the same time.
type Conn interface {
	// Send delivers m to the peer, blocking until it is accepted, ctx ends
	// or the connection closes.
	Send(ctx context.Context, m Message) error
	// Recv returns the next message from the peer, blocking until one
	// arrives, ctx ends or the connection closes.
	Recv(ctx context.Context) (Message, error)
	// Close releases the connection. Both ends see ErrClosed afterwards.
	Close() error
}

// pipeBuffer is how many messages each direction of a Pipe can hold before
// Send blocks. It only smooths bursts; a peer that stops reading still
// applies backpressure.
const pipeBuffer = 32

// Pipe returns two connected in-memory Conns. Messages sent on one end are
// received on the other, in order. Closing either end closes both.
//
// It is how the hub and worker talk when they share a process, and it lets
// tests exercise both sides without a network.
func Pipe() (Conn, Conn) {
	shared := &pipeState{done: make(chan struct{})}
	aToB := make(chan Message, pipeBuffer)
	bToA := make(chan Message, pipeBuffer)
	return &pipeEnd{state: shared, send: aToB, recv: bToA},
		&pipeEnd{state: shared, send: bToA, recv: aToB}
}

// pipeState is shared by both ends so that closing one closes the other.
type pipeState struct {
	done      chan struct{}
	closeOnce sync.Once
}

func (s *pipeState) close() {
	s.closeOnce.Do(func() { close(s.done) })
}

type pipeEnd struct {
	state *pipeState
	send  chan<- Message
	recv  <-chan Message
}

func (p *pipeEnd) Send(ctx context.Context, m Message) error {
	// Check for closure first so a closed pipe never accepts new messages
	// just because the buffer has room.
	select {
	case <-p.state.done:
		return ErrClosed
	default:
	}
	select {
	case p.send <- m:
		return nil
	case <-p.state.done:
		return ErrClosed
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *pipeEnd) Recv(ctx context.Context) (Message, error) {
	// Deliver anything already buffered before reporting closure, so a
	// message sent just before Close is not lost.
	select {
	case m := <-p.recv:
		return m, nil
	default:
	}
	select {
	case m := <-p.recv:
		return m, nil
	case <-p.state.done:
		return nil, ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *pipeEnd) Close() error {
	p.state.close()
	return nil
}
