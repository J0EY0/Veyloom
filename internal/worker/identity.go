package worker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Identity persists the ID the hub assigned to this worker, so the hub
// recognises the same machine across restarts. The ID is presented in Hello
// and updated from Welcome whenever the hub hands out a different one.
type Identity interface {
	// Load returns the saved ID, or "" when the worker has never connected.
	Load() (string, error)
	// Save records a newly assigned ID.
	Save(id string) error
}

// FileIdentity keeps the ID in a small JSON file, creating parent
// directories as needed. It is what real workers use.
type FileIdentity struct {
	Path string
}

// identityFile is the on-disk format.
type identityFile struct {
	WorkerID string `json:"worker_id"`
}

// Load implements Identity. A missing file means "never connected".
func (f FileIdentity) Load() (string, error) {
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read worker identity %s: %w", f.Path, err)
	}

	var v identityFile
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("parse worker identity %s: %w", f.Path, err)
	}
	return v.WorkerID, nil
}

// Save implements Identity. It writes to a temporary file and renames it
// into place so a crash cannot leave a half-written identity behind.
func (f FileIdentity) Save(id string) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return fmt.Errorf("create worker identity dir: %w", err)
	}
	data, err := json.MarshalIndent(identityFile{WorkerID: id}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode worker identity: %w", err)
	}

	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write worker identity: %w", err)
	}
	if err := os.Rename(tmp, f.Path); err != nil {
		return fmt.Errorf("commit worker identity: %w", err)
	}
	return nil
}

// MemoryIdentity keeps the ID in memory only. Tests use it, and so can
// throwaway workers that should not leave files behind.
type MemoryIdentity struct {
	mu sync.Mutex
	id string
}

// Load implements Identity.
func (m *MemoryIdentity) Load() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.id, nil
}

// Save implements Identity.
func (m *MemoryIdentity) Save(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.id = id
	return nil
}
