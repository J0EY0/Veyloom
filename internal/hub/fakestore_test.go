package hub

import (
	"context"
	"fmt"
	"sync"

	"github.com/J0EY0/veyloom/internal/engine"
)

// fakeStore is an in-memory WorkerStore for tests. It mirrors the real
// store's identity rule (a known id reconnects, anything else gets a new
// sequential id) and records what was persisted so tests can assert on it.
// The embedded Store is nil: the other methods are never reached by the
// tests that use this fake, and calling one would panic loudly.
type fakeStore struct {
	Store
	mu           sync.Mutex
	names        map[string]string // id → name
	touches      map[string]int
	engines      map[string][]engine.Info
	disconnected map[string]int
	// failRegister, when set, is returned from RegisterWorker.
	failRegister error
	// failTouch, when set, is returned from TouchWorker.
	failTouch error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		names:        make(map[string]string),
		touches:      make(map[string]int),
		engines:      make(map[string][]engine.Info),
		disconnected: make(map[string]int),
	}
}

func (s *fakeStore) RegisterWorker(_ context.Context, id, name string, engines []engine.Info) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failRegister != nil {
		return "", s.failRegister
	}
	if _, known := s.names[id]; !known {
		id = fmt.Sprintf("w%d", len(s.names)+1)
	}
	s.names[id] = name
	s.engines[id] = engines
	return id, nil
}

func (s *fakeStore) TouchWorker(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failTouch != nil {
		return s.failTouch
	}
	s.touches[id]++
	return nil
}

func (s *fakeStore) UpdateWorkerEngines(_ context.Context, id string, engines []engine.Info) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.engines[id] = engines
	return nil
}

func (s *fakeStore) MarkWorkerDisconnected(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disconnected[id]++
	return nil
}

func (s *fakeStore) touchCount(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.touches[id]
}

func (s *fakeStore) disconnectCount(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disconnected[id]
}

func (s *fakeStore) storedEngines(id string) []engine.Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.engines[id]
}

func (s *fakeStore) storedName(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.names[id]
}
