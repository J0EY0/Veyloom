package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
)

// stubDetector reports a fixed status after an optional delay, or an error if
// its context expires first. It stands in for real CLI detectors.
type stubDetector struct {
	name   string
	delay  time.Duration
	status engine.Status
	// barrier, when set, is released in Detect and then waited on. All stubs
	// sharing a barrier can only finish if they run at the same time, which
	// is how TestDiscovery_RunsConcurrently proves concurrency without timing
	// assertions.
	barrier *sync.WaitGroup
}

func (s stubDetector) Name() string { return s.name }

func (s stubDetector) Detect(ctx context.Context) engine.Info {
	if s.barrier != nil {
		s.barrier.Done()
		done := make(chan struct{})
		go func() { s.barrier.Wait(); close(done) }()
		select {
		case <-done:
		case <-ctx.Done():
			return engine.Info{Name: s.name, Status: engine.StatusError, Detail: ctx.Err().Error()}
		}
	}

	select {
	case <-time.After(s.delay):
		return engine.Info{Name: s.name, Status: s.status}
	case <-ctx.Done():
		return engine.Info{Name: s.name, Status: engine.StatusError, Detail: ctx.Err().Error()}
	}
}

func TestDiscovery_PreservesOrder(t *testing.T) {
	detectors := []engine.Detector{
		stubDetector{name: "a", delay: 30 * time.Millisecond, status: engine.StatusReady},
		stubDetector{name: "b", status: engine.StatusNotInstalled},
		stubDetector{name: "c", delay: 10 * time.Millisecond, status: engine.StatusAuthUnknown},
	}

	infos := NewDiscovery(detectors, time.Second).Run(context.Background())

	if len(infos) != 3 {
		t.Fatalf("got %d results, want 3", len(infos))
	}
	for i, want := range []string{"a", "b", "c"} {
		if infos[i].Name != want {
			t.Errorf("infos[%d].Name = %q, want %q", i, infos[i].Name, want)
		}
	}
	if infos[0].Status != engine.StatusReady || infos[1].Status != engine.StatusNotInstalled {
		t.Errorf("statuses not passed through: %+v", infos)
	}
}

func TestDiscovery_RunsConcurrently(t *testing.T) {
	var barrier sync.WaitGroup
	barrier.Add(3)
	detectors := []engine.Detector{
		stubDetector{name: "a", status: engine.StatusReady, barrier: &barrier},
		stubDetector{name: "b", status: engine.StatusReady, barrier: &barrier},
		stubDetector{name: "c", status: engine.StatusReady, barrier: &barrier},
	}

	// If Run were sequential the first detector would block on the barrier
	// until its timeout and report an error.
	infos := NewDiscovery(detectors, 2*time.Second).Run(context.Background())

	for _, info := range infos {
		if info.Status != engine.StatusReady {
			t.Errorf("%s: status = %q, want ready (detail: %s)", info.Name, info.Status, info.Detail)
		}
	}
}

func TestDiscovery_SlowDetectorTimesOutAlone(t *testing.T) {
	detectors := []engine.Detector{
		stubDetector{name: "fast", status: engine.StatusReady},
		stubDetector{name: "slow", delay: time.Second, status: engine.StatusReady},
	}

	infos := NewDiscovery(detectors, 50*time.Millisecond).Run(context.Background())

	if infos[0].Status != engine.StatusReady {
		t.Errorf("fast detector should succeed, got %+v", infos[0])
	}
	if infos[1].Status != engine.StatusError {
		t.Errorf("slow detector should time out, got %+v", infos[1])
	}
}

func TestNewDiscovery_DefaultTimeout(t *testing.T) {
	if d := NewDiscovery(nil, 0); d.timeout != DefaultConfig().DetectTimeout {
		t.Errorf("timeout = %v, want %v", d.timeout, DefaultConfig().DetectTimeout)
	}
}
