// Package account keeps the one account of this Veyloom in the state dir:
// name, password hash and live sessions in a JSON file, like the machine's
// identity. Nothing about the person is in the database; messages and
// approvals name them by id, and Directory answers for that id.
package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// contents is the on-disk format.
type contents struct {
	Account  *entry    `json:"account,omitempty"`
	Sessions []session `json:"sessions,omitempty"`
}

type entry struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

type session struct {
	TokenHash string    `json:"token_hash"`
	ExpiresAt time.Time `json:"expires_at"`
}

// File is the account store: it satisfies auth.Store and hands the
// account to Directory. Every change is written through at once.
type File struct {
	path string
	mu   sync.Mutex
	data contents
}

// Open reads the file at path; a missing file is an empty store.
func Open(path string) (*File, error) {
	f := &File{path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read account %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &f.data); err != nil {
		return nil, fmt.Errorf("parse account %s: %w", path, err)
	}
	return f, nil
}

// Account returns the account, if one has been created.
func (f *File) Account() (store.User, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data.Account == nil {
		return store.User{}, false
	}
	return f.data.Account.user(), true
}

// Rename changes the account's name.
func (f *File) Rename(name string) (store.User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.User{}, fmt.Errorf("rename account: %w: name is required", store.ErrInvalidInput)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data.Account == nil {
		return store.User{}, fmt.Errorf("rename account: %w", store.ErrNotFound)
	}
	f.data.Account.Name = name
	if err := f.save(); err != nil {
		return store.User{}, err
	}
	return f.data.Account.user(), nil
}

// HasAccount implements auth.Store.
func (f *File) HasAccount(context.Context) (bool, error) {
	_, ok := f.Account()
	return ok, nil
}

// CreateAccount implements auth.Store: ErrConflict once an account
// exists, ErrInvalidInput on a blank name.
func (f *File) CreateAccount(_ context.Context, name, passwordHash string) (store.User, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.User{}, fmt.Errorf("create account: %w: name is required", store.ErrInvalidInput)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data.Account != nil {
		return store.User{}, fmt.Errorf("create account: %w", store.ErrConflict)
	}
	f.data.Account = &entry{ID: store.NewID(), Name: name, PasswordHash: passwordHash, CreatedAt: time.Now().UTC()}
	if err := f.save(); err != nil {
		f.data.Account = nil
		return store.User{}, err
	}
	return f.data.Account.user(), nil
}

// AccountByName implements auth.Store, matching the name case-insensitively.
func (f *File) AccountByName(_ context.Context, name string) (store.User, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data.Account == nil || !strings.EqualFold(f.data.Account.Name, strings.TrimSpace(name)) {
		return store.User{}, "", fmt.Errorf("account %q: %w", name, store.ErrNotFound)
	}
	return f.data.Account.user(), f.data.Account.PasswordHash, nil
}

// PasswordHash implements auth.Store.
func (f *File) PasswordHash(_ context.Context, userID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data.Account == nil || f.data.Account.ID != userID {
		return "", fmt.Errorf("account %s: %w", userID, store.ErrNotFound)
	}
	return f.data.Account.PasswordHash, nil
}

// SetPasswordHash implements auth.Store.
func (f *File) SetPasswordHash(_ context.Context, userID, hash string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data.Account == nil || f.data.Account.ID != userID {
		return fmt.Errorf("account %s: %w", userID, store.ErrNotFound)
	}
	previous := f.data.Account.PasswordHash
	f.data.Account.PasswordHash = hash
	if err := f.save(); err != nil {
		f.data.Account.PasswordHash = previous
		return err
	}
	return nil
}

// CreateSession implements auth.Store.
func (f *File) CreateSession(_ context.Context, tokenHash, userID string, expires time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data.Account == nil || f.data.Account.ID != userID {
		return fmt.Errorf("session for %s: %w", userID, store.ErrNotFound)
	}
	f.data.Sessions = append(f.data.Sessions, session{TokenHash: tokenHash, ExpiresAt: expires.UTC()})
	if err := f.save(); err != nil {
		f.data.Sessions = f.data.Sessions[:len(f.data.Sessions)-1]
		return err
	}
	return nil
}

// SessionUser implements auth.Store: ErrNotFound for an unknown or
// expired token.
func (f *File) SessionUser(_ context.Context, tokenHash string) (store.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.data.Sessions {
		if s.TokenHash == tokenHash && s.ExpiresAt.After(time.Now()) && f.data.Account != nil {
			return f.data.Account.user(), nil
		}
	}
	return store.User{}, fmt.Errorf("session: %w", store.ErrNotFound)
}

// DeleteSession implements auth.Store; an unknown token is fine.
func (f *File) DeleteSession(_ context.Context, tokenHash string) error {
	return f.dropSessions(func(s session) bool { return s.TokenHash == tokenHash })
}

// DeleteUserSessions implements auth.Store: there is one user, so every
// session goes.
func (f *File) DeleteUserSessions(_ context.Context, userID string) error {
	return f.dropSessions(func(session) bool { return f.data.Account != nil && f.data.Account.ID == userID })
}

// DeleteExpiredSessions implements auth.Store.
func (f *File) DeleteExpiredSessions(context.Context) (int64, error) {
	now := time.Now()
	var n int64
	err := f.dropSessions(func(s session) bool {
		if !s.ExpiresAt.After(now) {
			n++
			return true
		}
		return false
	})
	return n, err
}

// dropSessions removes the sessions drop says to, and saves when any went.
func (f *File) dropSessions(drop func(session) bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.data.Sessions[:0:0]
	for _, s := range f.data.Sessions {
		if !drop(s) {
			kept = append(kept, s)
		}
	}
	if len(kept) == len(f.data.Sessions) {
		return nil
	}
	previous := f.data.Sessions
	f.data.Sessions = kept
	if err := f.save(); err != nil {
		f.data.Sessions = previous
		return err
	}
	return nil
}

func (e *entry) user() store.User {
	return store.User{ID: e.ID, Name: e.Name, CreatedAt: e.CreatedAt}
}

// save writes to a temporary file and renames it into place, so a crash
// cannot leave a half-written account behind. Callers hold the lock.
func (f *File) save() error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return fmt.Errorf("create account dir: %w", err)
	}
	data, err := json.MarshalIndent(f.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode account: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".account-*.json")
	if err != nil {
		return fmt.Errorf("write account: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write account: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write account: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write account: %w", err)
	}
	if err := os.Rename(tmp.Name(), f.path); err != nil {
		return fmt.Errorf("write account: %w", err)
	}
	return nil
}
