package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/protocol"
)

// countingDetector reports ready and counts how many times it was asked, so
// tests can tell an initial discovery from a re-run triggered by a Probe.
type countingDetector struct{ calls atomic.Int32 }

func (d *countingDetector) Name() string { return "counting" }

func (d *countingDetector) Detect(context.Context) engine.Info {
	d.calls.Add(1)
	return engine.Info{Name: "counting", Status: engine.StatusReady}
}

// startWorker runs a Worker over a pipe and returns the hub's end plus a
// channel that yields Run's result.
func startWorker(t *testing.T, w *Worker) (protocol.Conn, <-chan error, context.CancelFunc) {
	t.Helper()
	hubEnd, workerEnd := protocol.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	errCh := make(chan error, 1)
	go func() { errCh <- w.Run(ctx, workerEnd) }()
	return hubEnd, errCh, cancel
}

// recvKind reads the next message from conn and asserts its type.
func recvKind[T protocol.Message](t *testing.T, conn protocol.Conn) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	m, err := conn.Recv(ctx)
	if err != nil {
		t.Fatalf("recv: %v", err)
	}
	typed, ok := m.(T)
	if !ok {
		var want T
		t.Fatalf("got %T, want %T", m, want)
	}
	return typed
}

func TestRun_HandshakeSendsEngines(t *testing.T) {
	det := &countingDetector{}
	w := New(Config{Name: "laptop"}, NewDiscovery([]engine.Detector{det}, time.Second), &MemoryIdentity{}, engine.BuiltinRunners())
	hubEnd, _, _ := startWorker(t, w)

	hello := recvKind[protocol.Hello](t, hubEnd)

	if hello.Name != "laptop" {
		t.Errorf("Name = %q, want laptop", hello.Name)
	}
	if len(hello.Engines) != 1 || hello.Engines[0].Name != "counting" {
		t.Errorf("Hello should carry discovered engines, got %+v", hello.Engines)
	}
	if det.calls.Load() != 1 {
		t.Errorf("discovery ran %d times before Hello, want 1", det.calls.Load())
	}
}

func TestRun_SendsHeartbeatsAtHubInterval(t *testing.T) {
	w := New(Config{Name: "laptop"}, NewDiscovery(nil, time.Second), &MemoryIdentity{}, engine.BuiltinRunners())
	hubEnd, _, _ := startWorker(t, w)
	recvKind[protocol.Hello](t, hubEnd)

	welcome := protocol.Welcome{WorkerID: "w1", HeartbeatInterval: protocol.Duration(20 * time.Millisecond)}
	if err := hubEnd.Send(context.Background(), welcome); err != nil {
		t.Fatal(err)
	}

	// Two consecutive heartbeats prove the ticker keeps firing.
	recvKind[protocol.Heartbeat](t, hubEnd)
	recvKind[protocol.Heartbeat](t, hubEnd)
}

func TestRun_ProbeTriggersRediscovery(t *testing.T) {
	det := &countingDetector{}
	w := New(Config{Name: "laptop"}, NewDiscovery([]engine.Detector{det}, time.Second), &MemoryIdentity{}, engine.BuiltinRunners())
	hubEnd, _, _ := startWorker(t, w)
	recvKind[protocol.Hello](t, hubEnd)

	// A long heartbeat interval keeps heartbeats out of the way.
	welcome := protocol.Welcome{WorkerID: "w1", HeartbeatInterval: protocol.Duration(time.Hour)}
	if err := hubEnd.Send(context.Background(), welcome); err != nil {
		t.Fatal(err)
	}
	if err := hubEnd.Send(context.Background(), protocol.Probe{}); err != nil {
		t.Fatal(err)
	}

	report := recvKind[protocol.EnginesReport](t, hubEnd)

	if len(report.Engines) != 1 || report.Engines[0].Status != engine.StatusReady {
		t.Errorf("unexpected report: %+v", report)
	}
	if det.calls.Load() != 2 {
		t.Errorf("discovery ran %d times, want 2 (initial + probe)", det.calls.Load())
	}
}

func TestRun_RejectsNonWelcomeReply(t *testing.T) {
	w := New(Config{Name: "laptop"}, NewDiscovery(nil, time.Second), &MemoryIdentity{}, engine.BuiltinRunners())
	hubEnd, errCh, _ := startWorker(t, w)
	recvKind[protocol.Hello](t, hubEnd)

	if err := hubEnd.Send(context.Background(), protocol.Probe{}); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("Run should fail when the hub does not answer with Welcome")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestRun_StopsCleanlyWhenHubDisconnects(t *testing.T) {
	w := New(Config{Name: "laptop"}, NewDiscovery(nil, time.Second), &MemoryIdentity{}, engine.BuiltinRunners())
	hubEnd, errCh, _ := startWorker(t, w)
	recvKind[protocol.Hello](t, hubEnd)
	if err := hubEnd.Send(context.Background(), protocol.Welcome{WorkerID: "w1"}); err != nil {
		t.Fatal(err)
	}

	hubEnd.Close()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the hub closed")
	}
}

func TestRun_StopsCleanlyOnCancel(t *testing.T) {
	w := New(Config{Name: "laptop"}, NewDiscovery(nil, time.Second), &MemoryIdentity{}, engine.BuiltinRunners())
	hubEnd, errCh, cancel := startWorker(t, w)
	recvKind[protocol.Hello](t, hubEnd)
	if err := hubEnd.Send(context.Background(), protocol.Welcome{WorkerID: "w1"}); err != nil {
		t.Fatal(err)
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run returned %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestRun_PresentsSavedIDAndAdoptsAssignedOne(t *testing.T) {
	identity := &MemoryIdentity{}
	w := New(Config{Name: "laptop"}, NewDiscovery(nil, time.Second), identity, engine.BuiltinRunners())

	// First run: nothing saved yet, so Hello carries no id and the id from
	// Welcome is remembered.
	hubEnd, _, cancel := startWorker(t, w)
	hello := recvKind[protocol.Hello](t, hubEnd)
	if hello.WorkerID != "" {
		t.Errorf("first Hello carried id %q, want empty", hello.WorkerID)
	}
	if err := hubEnd.Send(context.Background(), protocol.Welcome{WorkerID: "assigned-1"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if id, _ := identity.Load(); id == "assigned-1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not save the assigned id")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()

	// Second run: the saved id is presented.
	hubEnd, _, _ = startWorker(t, w)
	hello = recvKind[protocol.Hello](t, hubEnd)
	if hello.WorkerID != "assigned-1" {
		t.Errorf("second Hello carried id %q, want assigned-1", hello.WorkerID)
	}
}

func TestConfig_ZeroFieldsFilledFromDefaults(t *testing.T) {
	got := Config{Name: "explicit"}.withDefaults()
	def := DefaultConfig()

	if got.Name != "explicit" {
		t.Errorf("explicit Name was overwritten: %q", got.Name)
	}
	if got.DetectTimeout != def.DetectTimeout || got.HandshakeTimeout != def.HandshakeTimeout || got.HeartbeatInterval != def.HeartbeatInterval {
		t.Errorf("zero fields should take defaults, got %+v", got)
	}
	if def.Name == "" {
		t.Error("default Name must never be empty")
	}
}
