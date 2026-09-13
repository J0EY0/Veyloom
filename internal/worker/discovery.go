// Package worker hosts the runtime that lives on each machine where agent
// CLIs are installed. For now it only knows how to discover those CLIs; running
// turns and talking to the hub come in later steps.
package worker

import (
	"context"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
)

// Discovery finds engines on the local machine.
type Discovery struct {
	detectors []engine.Detector
	timeout   time.Duration
}

// NewDiscovery builds a Discovery over the given detectors. A non-positive
// timeout falls back to the DefaultConfig detect timeout.
func NewDiscovery(detectors []engine.Detector, timeout time.Duration) *Discovery {
	if timeout <= 0 {
		timeout = DefaultConfig().DetectTimeout
	}
	return &Discovery{detectors: detectors, timeout: timeout}
}

// Run probes every detector concurrently and returns one Info per detector,
// in the order the detectors were registered. Each detector gets its own
// timeout, so one slow CLI cannot delay or fail the others.
func (d *Discovery) Run(ctx context.Context) []engine.Info {
	results := make([]engine.Info, len(d.detectors))

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
