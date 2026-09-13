package hub

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/protocol"
)

// fakeClock is an adjustable time source so tests can assert on LastSeen.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// connect starts Serve on one end of a pipe and returns the worker's end
// plus a channel that yields Serve's result.
func connect(t *testing.T, h *Hub) (protocol.Conn, <-chan error) {
	t.Helper()
	hubEnd, workerEnd := protocol.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() { errCh <- h.Serve(ctx, hubEnd) }()
	return workerEnd, errCh
}

// handshake sends Hello from the worker's end and returns the Welcome.
func handshake(t *testing.T, conn protocol.Conn, hello protocol.Hello) protocol.Welcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := conn.Send(ctx, hello); err != nil {
		t.Fatalf("send hello: %v", err)
	}
	m, err := conn.Recv(ctx)
	if err != nil {
		t.Fatalf("recv welcome: %v", err)
	}
	welcome, ok := m.(protocol.Welcome)
	if !ok {
		t.Fatalf("got %T, want Welcome", m)
	}
	return welcome
}

// eventually polls cond until it is true or the deadline passes.
func eventually(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// awaitServe waits for Serve to return and hands back its error.
func awaitServe(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
		return nil
	}
}

func TestServe_RegistersWorkerOnHello(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)}
	store := newFakeStore()
	h := New(store, Config{HeartbeatInterval: 3 * time.Second}, WithClock(clock.now))
	conn, _ := connect(t, h)

	engines := []engine.Info{{Name: "claude", Status: engine.StatusReady, Version: "2.1.85"}}
	welcome := handshake(t, conn, protocol.Hello{Name: "laptop", Engines: engines})

	if welcome.WorkerID != "w1" {
		t.Errorf("WorkerID = %q, want the store's w1", welcome.WorkerID)
	}
	if time.Duration(welcome.HeartbeatInterval) != 3*time.Second {
		t.Errorf("HeartbeatInterval = %v, want 3s", time.Duration(welcome.HeartbeatInterval))
	}

	workers := h.Workers()
	if len(workers) != 1 {
		t.Fatalf("got %d workers, want 1", len(workers))
	}
	w := workers[0]
	if w.ID != "w1" || w.Name != "laptop" || len(w.Engines) != 1 || w.Engines[0].Name != "claude" {
		t.Errorf("unexpected worker: %+v", w)
	}
	if !w.ConnectedAt.Equal(clock.t) || !w.LastSeen.Equal(clock.t) {
		t.Errorf("timestamps should come from the clock: %+v", w)
	}
	if got := store.storedEngines("w1"); len(got) != 1 || got[0].Name != "claude" {
		t.Errorf("engines from Hello should be persisted, store has %+v", got)
	}
}

func TestServe_RejectsNonHelloFirstMessage(t *testing.T) {
	h := New(newFakeStore(), Config{})
	conn, errCh := connect(t, h)

	if err := conn.Send(context.Background(), protocol.Heartbeat{}); err != nil {
		t.Fatal(err)
	}

	err := awaitServe(t, errCh)
	if err == nil || !strings.Contains(err.Error(), "heartbeat") {
		t.Errorf("Serve returned %v, want an error naming the bad kind", err)
	}
	if len(h.Workers()) != 0 {
		t.Error("a rejected connection must not be registered")
	}
}

func TestServe_StoreFailureOnRegister(t *testing.T) {
	store := newFakeStore()
	store.failRegister = errors.New("database down")
	h := New(store, Config{})
	conn, errCh := connect(t, h)

	if err := conn.Send(context.Background(), protocol.Hello{Name: "laptop"}); err != nil {
		t.Fatal(err)
	}

	err := awaitServe(t, errCh)
	if err == nil || !strings.Contains(err.Error(), "database down") {
		t.Errorf("Serve returned %v, want the store error", err)
	}
	if len(h.Workers()) != 0 {
		t.Error("a worker the store could not register must not be tracked")
	}
}

func TestServe_RefusesDuplicateConnection(t *testing.T) {
	h := New(newFakeStore(), Config{})
	first, _ := connect(t, h)
	welcome := handshake(t, first, protocol.Hello{Name: "laptop"})

	second, errCh := connect(t, h)
	if err := second.Send(context.Background(), protocol.Hello{WorkerID: welcome.WorkerID, Name: "laptop"}); err != nil {
		t.Fatal(err)
	}

	if err := awaitServe(t, errCh); !errors.Is(err, ErrAlreadyConnected) {
		t.Errorf("second connection got %v, want ErrAlreadyConnected", err)
	}
	if len(h.Workers()) != 1 {
		t.Errorf("the first connection must stay tracked, got %d workers", len(h.Workers()))
	}
}

func TestServe_HeartbeatRefreshesLastSeenAndTouchesStore(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)}
	store := newFakeStore()
	h := New(store, Config{}, WithClock(clock.now))
	conn, _ := connect(t, h)
	handshake(t, conn, protocol.Hello{Name: "laptop"})

	clock.t = clock.t.Add(time.Minute)
	if err := conn.Send(context.Background(), protocol.Heartbeat{}); err != nil {
		t.Fatal(err)
	}

	eventually(t, func() bool {
		return h.Workers()[0].LastSeen.Equal(clock.t)
	}, "LastSeen to advance")
	if got := h.Workers()[0].ConnectedAt; got.Equal(clock.t) {
		t.Error("ConnectedAt must not move with heartbeats")
	}
	if store.touchCount("w1") != 1 {
		t.Errorf("store touched %d times, want 1", store.touchCount("w1"))
	}
}

func TestServe_StoreFailureOnHeartbeatEndsConnection(t *testing.T) {
	store := newFakeStore()
	h := New(store, Config{})
	conn, errCh := connect(t, h)
	handshake(t, conn, protocol.Hello{Name: "laptop"})

	store.failTouch = errors.New("database down")
	if err := conn.Send(context.Background(), protocol.Heartbeat{}); err != nil {
		t.Fatal(err)
	}

	err := awaitServe(t, errCh)
	if err == nil || !strings.Contains(err.Error(), "database down") {
		t.Errorf("Serve returned %v, want the store error", err)
	}
	if len(h.Workers()) != 0 {
		t.Error("worker should be untracked once its connection ends")
	}
}

func TestServe_EnginesReportReplacesEngines(t *testing.T) {
	store := newFakeStore()
	h := New(store, Config{})
	conn, _ := connect(t, h)
	handshake(t, conn, protocol.Hello{
		Name:    "laptop",
		Engines: []engine.Info{{Name: "claude", Status: engine.StatusNotLoggedIn}},
	})

	report := protocol.EnginesReport{Engines: []engine.Info{{Name: "claude", Status: engine.StatusReady}}}
	if err := conn.Send(context.Background(), report); err != nil {
		t.Fatal(err)
	}

	eventually(t, func() bool {
		return h.Workers()[0].Engines[0].Status == engine.StatusReady
	}, "engines to be replaced in the live view")
	if got := store.storedEngines("w1"); got[0].Status != engine.StatusReady {
		t.Errorf("engines should be persisted too, store has %+v", got)
	}
}

func TestServe_UnregistersOnDisconnect(t *testing.T) {
	store := newFakeStore()
	h := New(store, Config{})
	conn, errCh := connect(t, h)
	handshake(t, conn, protocol.Hello{Name: "laptop"})

	conn.Close()

	if err := awaitServe(t, errCh); err != nil {
		t.Errorf("Serve returned %v on a clean disconnect, want nil", err)
	}
	if len(h.Workers()) != 0 {
		t.Error("worker should be gone after disconnect")
	}
	if store.disconnectCount("w1") != 1 {
		t.Errorf("store marked disconnected %d times, want 1", store.disconnectCount("w1"))
	}
}

func TestServe_ReconnectWithPresentedIDKeepsIt(t *testing.T) {
	store := newFakeStore()
	h := New(store, Config{})

	conn, errCh := connect(t, h)
	first := handshake(t, conn, protocol.Hello{Name: "laptop"})
	conn.Close()
	awaitServe(t, errCh)

	// The worker comes back presenting the id it was given, under a new
	// label.
	conn, _ = connect(t, h)
	second := handshake(t, conn, protocol.Hello{WorkerID: first.WorkerID, Name: "laptop-renamed"})

	if second.WorkerID != first.WorkerID {
		t.Errorf("reconnect got id %q, want %q", second.WorkerID, first.WorkerID)
	}
	if store.storedName(first.WorkerID) != "laptop-renamed" {
		t.Errorf("label should be refreshed, store has %q", store.storedName(first.WorkerID))
	}
}

func TestServe_SameNameWithoutIDIsANewWorker(t *testing.T) {
	h := New(newFakeStore(), Config{})

	conn, errCh := connect(t, h)
	first := handshake(t, conn, protocol.Hello{Name: "laptop"})
	conn.Close()
	awaitServe(t, errCh)

	// Same label, no id: this is a different machine as far as the hub
	// can tell, so it must not take over the first worker's identity.
	conn, _ = connect(t, h)
	second := handshake(t, conn, protocol.Hello{Name: "laptop"})

	if second.WorkerID == first.WorkerID {
		t.Error("a worker without an id must not be matched by name")
	}
}

func TestProbe_SendsProbeToWorker(t *testing.T) {
	h := New(newFakeStore(), Config{})
	conn, _ := connect(t, h)
	welcome := handshake(t, conn, protocol.Hello{Name: "laptop"})

	if err := h.Probe(context.Background(), welcome.WorkerID); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	m, err := conn.Recv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(protocol.Probe); !ok {
		t.Errorf("worker received %T, want Probe", m)
	}
}

func TestProbe_UnknownWorker(t *testing.T) {
	err := New(newFakeStore(), Config{}).Probe(context.Background(), "w404")
	if !errors.Is(err, ErrUnknownWorker) {
		t.Errorf("got %v, want ErrUnknownWorker", err)
	}
}

func TestWorkers_SortedAndCopied(t *testing.T) {
	h := New(newFakeStore(), Config{})
	for _, name := range []string{"first", "second"} {
		conn, _ := connect(t, h)
		handshake(t, conn, protocol.Hello{Name: name, Engines: []engine.Info{{Name: "pi"}}})
	}

	workers := h.Workers()
	if len(workers) != 2 || workers[0].ID != "w1" || workers[1].ID != "w2" {
		t.Fatalf("unexpected order: %+v", workers)
	}

	// Mutating the snapshot must not leak into the hub.
	workers[0].Engines[0].Name = "mutated"
	if h.Workers()[0].Engines[0].Name != "pi" {
		t.Error("Workers returned a slice aliasing internal state")
	}
}

func TestConfig_ZeroFieldsFilledFromDefaults(t *testing.T) {
	got := Config{HeartbeatInterval: time.Second}.withDefaults()
	def := DefaultConfig()

	if got.HeartbeatInterval != time.Second {
		t.Errorf("explicit HeartbeatInterval was overwritten: %v", got.HeartbeatInterval)
	}
	if got.HandshakeTimeout != def.HandshakeTimeout || got.StoreTimeout != def.StoreTimeout || got.BriefMessages != def.BriefMessages {
		t.Errorf("zero fields should take defaults, got %+v", got)
	}
}
