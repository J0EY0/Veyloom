// Package machine is what runs on each machine where agent CLIs are
// installed: it finds the runtimes there, keeps a connection to the hub and
// runs the turns the hub sends.
package machine

import (
	"context"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
)

// Discovery finds runtimes on the local machine.
type Discovery struct {
	detectors []runtime.Detector
	timeout   time.Duration
}

// NewDiscovery builds a Discovery over the given detectors. A non-positive
// timeout falls back to the DefaultConfig detect timeout.
func NewDiscovery(detectors []runtime.Detector, timeout time.Duration) *Discovery {
	if timeout <= 0 {
		timeout = DefaultConfig().DetectTimeout
	}
	return &Discovery{detectors: detectors, timeout: timeout}
}

// Run probes every detector concurrently and returns one Info per detector,
// in the order the detectors were registered. Each detector gets its own
// timeout, so one slow CLI cannot delay or fail the others.
func (d *Discovery) Run(ctx context.Context) []runtime.Info {
	results := make([]runtime.Info, len(d.detectors))

	var wg sync.WaitGroup
	for i, det := range d.detectors {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dctx, cancel := context.WithTimeout(ctx, d.timeout)
			defer cancel()
			results[i] = det.Detect(dctx)
		}()
	}
	wg.Wait()

	return results
}
