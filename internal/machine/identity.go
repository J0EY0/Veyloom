package machine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Identity persists the ID the hub assigned to this machine, so the hub
// recognises the same machine across restarts. The ID is presented in Hello
// and updated from Welcome whenever the hub hands out a different one.
type Identity interface {
	// Load returns the saved ID, or "" when the machine has never connected.
	Load() (string, error)
	// Save records a newly assigned ID.
	Save(id string) error
}

// FileIdentity keeps the ID in a small JSON file, creating parent
// directories as needed. It is what real machines use.
type FileIdentity struct {
	Path string
}

// identityFile is the on-disk format.
type identityFile struct {
	MachineID string `json:"machine_id"`
}

// Load implements Identity. A missing file means "never connected".
func (f FileIdentity) Load() (string, error) {
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read machine identity %s: %w", f.Path, err)
	}

	var v identityFile
	if err := json.Unmarshal(data, &v); err != nil {
		return "", fmt.Errorf("parse machine identity %s: %w", f.Path, err)
	}
	return v.MachineID, nil
}

// Save implements Identity. It writes to a temporary file and renames it
// into place so a crash cannot leave a half-written identity behind.
func (f FileIdentity) Save(id string) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return fmt.Errorf("create machine identity dir: %w", err)
	}
	data, err := json.MarshalIndent(identityFile{MachineID: id}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode machine identity: %w", err)
	}

	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write machine identity: %w", err)
	}
	if err := os.Rename(tmp, f.Path); err != nil {
		return fmt.Errorf("commit machine identity: %w", err)
	}
	return nil
}

// MemoryIdentity keeps the ID in memory only. Tests use it, and so can
// throwaway machines that should not leave files behind.
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
