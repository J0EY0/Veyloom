package protocol

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPipe_DeliversBothDirections(t *testing.T) {
	ctx := context.Background()
	a, b := Pipe()

	if err := a.Send(ctx, Hello{Name: "from-a"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Send(ctx, Welcome{WorkerID: "from-b"}); err != nil {
		t.Fatal(err)
	}

	got, err := b.Recv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hello, ok := got.(Hello); !ok || hello.Name != "from-a" {
		t.Errorf("b received %#v, want Hello from a", got)
	}

	got, err = a.Recv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if welcome, ok := got.(Welcome); !ok || welcome.WorkerID != "from-b" {
		t.Errorf("a received %#v, want Welcome from b", got)
	}
}

func TestPipe_PreservesOrder(t *testing.T) {
	ctx := context.Background()
	a, b := Pipe()

	for _, name := range []string{"1", "2", "3"} {
		if err := a.Send(ctx, Hello{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"1", "2", "3"} {
		got, err := b.Recv(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if got.(Hello).Name != want {
			t.Errorf("got %q, want %q", got.(Hello).Name, want)
		}
	}
}

func TestPipe_CloseUnblocksPeer(t *testing.T) {
	a, b := Pipe()

	errCh := make(chan error, 1)
	go func() {
		_, err := b.Recv(context.Background())
		errCh <- err
	}()

	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrClosed) {
			t.Errorf("Recv returned %v, want ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Recv did not return after the peer closed")
	}

	// The closing side is closed too, and sending anywhere now fails.
	if err := a.Send(context.Background(), Heartbeat{}); !errors.Is(err, ErrClosed) {
		t.Errorf("Send after Close returned %v, want ErrClosed", err)
	}
	if err := b.Send(context.Background(), Heartbeat{}); !errors.Is(err, ErrClosed) {
		t.Errorf("peer Send after Close returned %v, want ErrClosed", err)
	}
}

func TestPipe_BufferedMessageSurvivesClose(t *testing.T) {
	a, b := Pipe()
	if err := a.Send(context.Background(), Heartbeat{}); err != nil {
		t.Fatal(err)
	}
	a.Close()

	// The message was accepted before Close, so the peer must still get it.
	if _, err := b.Recv(context.Background()); err != nil {
		t.Errorf("Recv returned %v, want the buffered message", err)
	}
	if _, err := b.Recv(context.Background()); !errors.Is(err, ErrClosed) {
		t.Errorf("second Recv returned %v, want ErrClosed", err)
	}
}

func TestPipe_RecvHonoursContext(t *testing.T) {
	_, b := Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := b.Recv(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Recv returned %v, want DeadlineExceeded", err)
	}
}
