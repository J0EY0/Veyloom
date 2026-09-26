package hub

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
)

// fakeClock is an adjustable time source so tests can assert on LastSeen.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// connect starts Serve on one end of a pipe and returns the machine's end
// plus a channel that yields Serve's result.
func connect(t *testing.T, h *Hub) (protocol.Conn, <-chan error) {
	t.Helper()
	hubEnd, machineEnd := protocol.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() { errCh <- h.Serve(ctx, hubEnd) }()
	return machineEnd, errCh
}

// handshake sends Hello from the machine's end and returns the Welcome.
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
	eventuallyWithin(t, 2*time.Second, cond, what)
}

// eventuallyWithin is eventually with its own deadline, for what takes
// long on purpose.
func eventuallyWithin(t *testing.T, within time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(within)
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

func TestServe_RegistersMachineOnHello(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)}
	store := newFakeStore()
	h := New(store, Config{HeartbeatInterval: 3 * time.Second}, WithClock(clock.now))
	conn, _ := connect(t, h)

	runtimes := []runtime.Info{{Name: "claude", Status: runtime.StatusReady, Version: "2.1.85"}}
	welcome := handshake(t, conn, protocol.Hello{Name: "laptop", Runtimes: runtimes})

	if welcome.MachineID != "w1" {
		t.Errorf("MachineID = %q, want the store's w1", welcome.MachineID)
	}
	if time.Duration(welcome.HeartbeatInterval) != 3*time.Second {
		t.Errorf("HeartbeatInterval = %v, want 3s", time.Duration(welcome.HeartbeatInterval))
	}

	machines := h.Machines()
	if len(machines) != 1 {
		t.Fatalf("got %d machines, want 1", len(machines))
	}
	w := machines[0]
	if w.ID != "w1" || w.Name != "laptop" || len(w.Runtimes) != 1 || w.Runtimes[0].Name != "claude" {
		t.Errorf("unexpected machine: %+v", w)
	}
	if !w.ConnectedAt.Equal(clock.t) || !w.LastSeen.Equal(clock.t) {
		t.Errorf("timestamps should come from the clock: %+v", w)
	}
	if got := store.storedRuntimes("w1"); len(got) != 1 || got[0].Name != "claude" {
		t.Errorf("runtimes from Hello should be persisted, store has %+v", got)
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
	if len(h.Machines()) != 0 {
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
	if len(h.Machines()) != 0 {
		t.Error("a machine the store could not register must not be tracked")
	}
}

func TestServe_RefusesDuplicateConnection(t *testing.T) {
	h := New(newFakeStore(), Config{})
	first, _ := connect(t, h)
	welcome := handshake(t, first, protocol.Hello{Name: "laptop"})

	second, errCh := connect(t, h)
	if err := second.Send(context.Background(), protocol.Hello{MachineID: welcome.MachineID, Name: "laptop"}); err != nil {
		t.Fatal(err)
	}

	if err := awaitServe(t, errCh); !errors.Is(err, ErrAlreadyConnected) {
		t.Errorf("second connection got %v, want ErrAlreadyConnected", err)
	}
	if len(h.Machines()) != 1 {
		t.Errorf("the first connection must stay tracked, got %d machines", len(h.Machines()))
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
		return h.Machines()[0].LastSeen.Equal(clock.t)
	}, "LastSeen to advance")
	if got := h.Machines()[0].ConnectedAt; got.Equal(clock.t) {
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
	if len(h.Machines()) != 0 {
		t.Error("machine should be untracked once its connection ends")
	}
}

func TestServe_RuntimesReportReplacesRuntimes(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)}
	store := newFakeStore()
	h := New(store, Config{}, WithClock(clock.now))
	conn, _ := connect(t, h)
	handshake(t, conn, protocol.Hello{
		Name:     "laptop",
		Runtimes: []runtime.Info{{Name: "claude", Status: runtime.StatusError}},
	})
	if got := h.Machines()[0].ProbedAt; !got.Equal(clock.t) {
		t.Errorf("ProbedAt at connect = %v, want %v: Hello carries the first discovery", got, clock.t)
	}

	clock.t = clock.t.Add(time.Minute)
	report := protocol.RuntimesReport{Runtimes: []runtime.Info{{Name: "claude", Status: runtime.StatusReady}}}
	if err := conn.Send(context.Background(), report); err != nil {
		t.Fatal(err)
	}

	eventually(t, func() bool {
		return h.Machines()[0].Runtimes[0].Status == runtime.StatusReady
	}, "runtimes to be replaced in the live view")
	if got := store.storedRuntimes("w1"); got[0].Status != runtime.StatusReady {
		t.Errorf("runtimes should be persisted too, store has %+v", got)
	}
	if got := h.Machines()[0].ProbedAt; !got.Equal(clock.t) {
		t.Errorf("ProbedAt = %v, want %v once the report is in", got, clock.t)
	}
}

func TestServe_HeartbeatLeavesProbedAt(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)}
	h := New(newFakeStore(), Config{}, WithClock(clock.now))
	conn, _ := connect(t, h)
	handshake(t, conn, protocol.Hello{Name: "laptop"})
	connected := clock.t

	clock.t = clock.t.Add(time.Minute)
	if err := conn.Send(context.Background(), protocol.Heartbeat{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return h.Machines()[0].LastSeen.Equal(clock.t) }, "LastSeen to advance")
	if got := h.Machines()[0].ProbedAt; !got.Equal(connected) {
		t.Errorf("ProbedAt = %v, want %v: a heartbeat is not a discovery", got, connected)
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
	if len(h.Machines()) != 0 {
		t.Error("machine should be gone after disconnect")
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

	// The machine comes back presenting the id it was given, under a new
	// label.
	conn, _ = connect(t, h)
	second := handshake(t, conn, protocol.Hello{MachineID: first.MachineID, Name: "laptop-renamed"})

	if second.MachineID != first.MachineID {
		t.Errorf("reconnect got id %q, want %q", second.MachineID, first.MachineID)
	}
	if store.storedName(first.MachineID) != "laptop-renamed" {
		t.Errorf("label should be refreshed, store has %q", store.storedName(first.MachineID))
	}
}

func TestServe_SameNameWithoutIDIsANewMachine(t *testing.T) {
	h := New(newFakeStore(), Config{})

	conn, errCh := connect(t, h)
	first := handshake(t, conn, protocol.Hello{Name: "laptop"})
	conn.Close()
	awaitServe(t, errCh)

	// Same label, no id: this is a different machine as far as the hub
	// can tell, so it must not take over the first machine's identity.
	conn, _ = connect(t, h)
	second := handshake(t, conn, protocol.Hello{Name: "laptop"})

	if second.MachineID == first.MachineID {
		t.Error("a machine without an id must not be matched by name")
	}
}

func TestProbe_SendsProbeToMachine(t *testing.T) {
	h := New(newFakeStore(), Config{})
	conn, _ := connect(t, h)
	welcome := handshake(t, conn, protocol.Hello{Name: "laptop"})

	if err := h.Probe(context.Background(), welcome.MachineID); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	m, err := conn.Recv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(protocol.Probe); !ok {
		t.Errorf("machine received %T, want Probe", m)
	}
}

func TestProbe_UnknownMachine(t *testing.T) {
	err := New(newFakeStore(), Config{}).Probe(context.Background(), "w404")
	if !errors.Is(err, ErrUnknownMachine) {
		t.Errorf("got %v, want ErrUnknownMachine", err)
	}
}

func TestMachines_SortedAndCopied(t *testing.T) {
	h := New(newFakeStore(), Config{})
	for _, name := range []string{"first", "second"} {
		conn, _ := connect(t, h)
		handshake(t, conn, protocol.Hello{Name: name, Runtimes: []runtime.Info{{Name: "pi"}}})
	}

	machines := h.Machines()
	if len(machines) != 2 || machines[0].ID != "w1" || machines[1].ID != "w2" {
		t.Fatalf("unexpected order: %+v", machines)
	}

	// Mutating the snapshot must not leak into the hub.
	machines[0].Runtimes[0].Name = "mutated"
	if h.Machines()[0].Runtimes[0].Name != "pi" {
		t.Error("Machines returned a slice aliasing internal state")
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
