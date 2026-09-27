package hub

import (
	"context"
	"fmt"
	"sync"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// fakeStore is an in-memory MachineStore for tests. It mirrors the real
// store's identity rule (a known id reconnects, anything else gets a new
// sequential id) and records what was persisted so tests can assert on it.
// The embedded Store is nil: the other methods are never reached by the
// tests that use this fake, and calling one would panic loudly.
type fakeStore struct {
	Store
	mu           sync.Mutex
	names        map[string]string // id → name
	touches      map[string]int
	runtimes     map[string][]runtime.Info
	disconnected map[string]int
	// failRegister, when set, is returned from RegisterMachine.
	failRegister error
	// failTouch, when set, is returned from TouchMachine.
	failTouch error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		names:        make(map[string]string),
		touches:      make(map[string]int),
		runtimes:     make(map[string][]runtime.Info),
		disconnected: make(map[string]int),
	}
}

func (s *fakeStore) RegisterMachine(_ context.Context, id, name string, runtimes []runtime.Info) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failRegister != nil {
		return "", s.failRegister
	}
	if _, known := s.names[id]; !known {
		id = fmt.Sprintf("w%d", len(s.names)+1)
	}
	s.names[id] = name
	s.runtimes[id] = runtimes
	return id, nil
}

func (s *fakeStore) TouchMachine(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failTouch != nil {
		return s.failTouch
	}
	s.touches[id]++
	return nil
}

func (s *fakeStore) UpdateMachineRuntimes(_ context.Context, id string, runtimes []runtime.Info) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimes[id] = runtimes
	return nil
}

func (s *fakeStore) MarkMachineDisconnected(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disconnected[id]++
	return nil
}

// ListPendingReminders has no reminders: a machine connecting looks.
func (s *fakeStore) ListPendingReminders(context.Context, string) ([]store.Reminder, error) {
	return nil, nil
}

// ListQueuedWakes has nothing waiting: a machine connecting looks.
func (s *fakeStore) ListQueuedWakes(context.Context, string) ([]store.QueuedWake, error) {
	return nil, nil
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

func (s *fakeStore) storedRuntimes(id string) []runtime.Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runtimes[id]
}

func (s *fakeStore) storedName(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.names[id]
}
