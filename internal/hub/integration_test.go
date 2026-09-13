package hub

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/worker"
)

// flippingDetector reports "not logged in" the first time and "ready" after
// that, standing in for a user who logs in between two discoveries.
type flippingDetector struct{ calls atomic.Int32 }

func (d *flippingDetector) Name() string { return "claude" }

func (d *flippingDetector) Detect(context.Context) engine.Info {
	if d.calls.Add(1) == 1 {
		return engine.Info{Name: "claude", Status: engine.StatusNotLoggedIn}
	}
	return engine.Info{Name: "claude", Status: engine.StatusReady}
}

// TestHubAndWorker_InProcess wires a real Hub to a real Worker over a Pipe,
// exactly as `veyloom serve` does on a single machine.
func TestHubAndWorker_InProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newFakeStore()
	h := New(store, Config{HeartbeatInterval: 20 * time.Millisecond})
	det := &flippingDetector{}
	identity := &worker.MemoryIdentity{}
	w := worker.New(worker.Config{Name: "laptop"}, worker.NewDiscovery([]engine.Detector{det}, time.Second), identity, engine.BuiltinRunners())

	hubEnd, workerEnd := protocol.Pipe()
	go h.Serve(ctx, hubEnd)
	go w.Run(ctx, workerEnd)

	// Registration: the worker shows up with its initial engines and
	// remembers the id it was given.
	eventually(t, func() bool { return len(h.Workers()) == 1 }, "worker to register")
	info := h.Workers()[0]
	if info.Name != "laptop" || info.Engines[0].Status != engine.StatusNotLoggedIn {
		t.Fatalf("unexpected registration: %+v", info)
	}
	eventually(t, func() bool { id, _ := identity.Load(); return id == info.ID }, "worker to save its id")

	// Liveness: heartbeats keep LastSeen moving and reach the store.
	first := info.LastSeen
	eventually(t, func() bool { return h.Workers()[0].LastSeen.After(first) }, "a heartbeat")
	eventually(t, func() bool { return store.touchCount(info.ID) > 0 }, "a heartbeat to be persisted")

	// Probe: the hub asks, the worker re-discovers, both views update.
	if err := h.Probe(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return h.Workers()[0].Engines[0].Status == engine.StatusReady
	}, "engines to refresh after probe")
	if got := store.storedEngines(info.ID); got[0].Status != engine.StatusReady {
		t.Errorf("refreshed engines should be persisted, store has %+v", got)
	}

	// Shutdown: cancelling the shared context tears everything down.
	cancel()
	eventually(t, func() bool { return len(h.Workers()) == 0 }, "worker to unregister")
	eventually(t, func() bool { return store.disconnectCount(info.ID) == 1 }, "disconnect to be persisted")

	// Restart: the worker presents its saved id and is recognised.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	hubEnd, workerEnd = protocol.Pipe()
	go h.Serve(ctx2, hubEnd)
	go w.Run(ctx2, workerEnd)
	eventually(t, func() bool { return len(h.Workers()) == 1 }, "worker to register again")
	if got := h.Workers()[0].ID; got != info.ID {
		t.Errorf("restarted worker got id %s, want %s", got, info.ID)
	}
}
