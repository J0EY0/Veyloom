package hub

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/machine"
	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
)

// flippingDetector reports "not logged in" the first time and "ready" after
// that, standing in for a user who logs in between two discoveries.
type flippingDetector struct{ calls atomic.Int32 }

func (d *flippingDetector) Name() string { return "claude" }

func (d *flippingDetector) Detect(context.Context) runtime.Info {
	if d.calls.Add(1) == 1 {
		return runtime.Info{Name: "claude", Status: runtime.StatusError}
	}
	return runtime.Info{Name: "claude", Status: runtime.StatusReady}
}

// TestHubAndMachine_InProcess wires a real Hub to a real Machine over a Pipe,
// exactly as `veyloom serve` does on a single machine.
func TestHubAndMachine_InProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := newFakeStore()
	h := New(store, Config{HeartbeatInterval: 20 * time.Millisecond})
	det := &flippingDetector{}
	identity := &machine.MemoryIdentity{}
	w := machine.New(machine.Config{Name: "laptop"}, machine.NewDiscovery([]runtime.Detector{det}, time.Second), identity, runtime.BuiltinRunners())

	hubEnd, machineEnd := protocol.Pipe()
	go h.Serve(ctx, hubEnd)
	go w.Run(ctx, machineEnd)

	// Registration: the machine shows up with its initial runtimes and
	// remembers the id it was given.
	eventually(t, func() bool { return len(h.Machines()) == 1 }, "machine to register")
	info := h.Machines()[0]
	if info.Name != "laptop" || info.Runtimes[0].Status != runtime.StatusError {
		t.Fatalf("unexpected registration: %+v", info)
	}
	eventually(t, func() bool { id, _ := identity.Load(); return id == info.ID }, "machine to save its id")

	// Liveness: heartbeats keep LastSeen moving and reach the store.
	first := info.LastSeen
	eventually(t, func() bool { return h.Machines()[0].LastSeen.After(first) }, "a heartbeat")
	eventually(t, func() bool { return store.touchCount(info.ID) > 0 }, "a heartbeat to be persisted")

	// Probe: the hub asks, the machine re-discovers, both views update.
	if err := h.Probe(ctx, info.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return h.Machines()[0].Runtimes[0].Status == runtime.StatusReady
	}, "runtimes to refresh after probe")
	if got := store.storedRuntimes(info.ID); got[0].Status != runtime.StatusReady {
		t.Errorf("refreshed runtimes should be persisted, store has %+v", got)
	}

	// Shutdown: cancelling the shared context tears everything down.
	cancel()
	eventually(t, func() bool { return len(h.Machines()) == 0 }, "machine to unregister")
	eventually(t, func() bool { return store.disconnectCount(info.ID) == 1 }, "disconnect to be persisted")

	// Restart: the machine presents its saved id and is recognised.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	hubEnd, machineEnd = protocol.Pipe()
	go h.Serve(ctx2, hubEnd)
	go w.Run(ctx2, machineEnd)
	eventually(t, func() bool { return len(h.Machines()) == 1 }, "machine to register again")
	if got := h.Machines()[0].ID; got != info.ID {
		t.Errorf("restarted machine got id %s, want %s", got, info.ID)
	}
}
