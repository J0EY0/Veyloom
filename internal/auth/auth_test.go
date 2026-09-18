package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/J0EY0/veyloom/internal/store"
)

// memStore is an in-memory Store with the same rules as the real one: one
// account, sessions that expire.
type memStore struct {
	account  *store.User
	hash     string
	sessions map[string]session
}

type session struct {
	userID  string
	expires time.Time
}

func newMemStore() *memStore { return &memStore{sessions: map[string]session{}} }

func (m *memStore) HasAccount(context.Context) (bool, error) { return m.account != nil, nil }

func (m *memStore) CreateAccount(_ context.Context, name, hash string) (store.User, error) {
	if m.account != nil {
		return store.User{}, fmt.Errorf("create account: %w", store.ErrConflict)
	}
	u := store.User{ID: "u1", Name: name}
	m.account, m.hash = &u, hash
	return u, nil
}

func (m *memStore) AccountByName(_ context.Context, name string) (store.User, string, error) {
	if m.account == nil || m.account.Name != name {
		return store.User{}, "", fmt.Errorf("account %q: %w", name, store.ErrNotFound)
	}
	return *m.account, m.hash, nil
}

func (m *memStore) PasswordHash(_ context.Context, id string) (string, error) {
	if m.account == nil || m.account.ID != id {
		return "", fmt.Errorf("account %s: %w", id, store.ErrNotFound)
	}
	return m.hash, nil
}

func (m *memStore) SetPasswordHash(_ context.Context, id, hash string) error {
	if m.account == nil || m.account.ID != id {
		return fmt.Errorf("account %s: %w", id, store.ErrNotFound)
	}
	m.hash = hash
	return nil
}

func (m *memStore) CreateSession(_ context.Context, tokenHash, userID string, expires time.Time) error {
	m.sessions[tokenHash] = session{userID: userID, expires: expires}
	return nil
}

func (m *memStore) SessionUser(_ context.Context, tokenHash string) (store.User, error) {
	s, ok := m.sessions[tokenHash]
	if !ok || !s.expires.After(time.Now()) || m.account == nil || m.account.ID != s.userID {
		return store.User{}, fmt.Errorf("session: %w", store.ErrNotFound)
	}
	return *m.account, nil
}

func (m *memStore) DeleteSession(_ context.Context, tokenHash string) error {
	delete(m.sessions, tokenHash)
	return nil
}

func (m *memStore) DeleteUserSessions(_ context.Context, userID string) error {
	for k, s := range m.sessions {
		if s.userID == userID {
			delete(m.sessions, k)
		}
	}
	return nil
}

func (m *memStore) DeleteExpiredSessions(context.Context) (int64, error) {
	var n int64
	for k, s := range m.sessions {
		if !s.expires.After(time.Now()) {
			delete(m.sessions, k)
			n++
		}
	}
	return n, nil
}

func newService(m *memStore) *Service {
	s := New(m, time.Hour)
	s.Cost = bcrypt.MinCost
	return s
}

func TestSetupThenLogin(t *testing.T) {
	ctx := context.Background()
	m := newMemStore()
	s := newService(m)

	if required, _ := s.SetupRequired(ctx); !required {
		t.Fatal("setup should be required before the first account")
	}
	if _, _, err := s.Setup(ctx, "jinghao", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("short password: got %v, want ErrWeakPassword", err)
	}
	if _, _, err := s.Setup(ctx, "  ", "long enough"); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("blank name: got %v, want ErrInvalidInput", err)
	}

	user, token, err := s.Setup(ctx, " jinghao ", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if user.Name != "jinghao" || token == "" {
		t.Errorf("unexpected setup result: %+v %q", user, token)
	}
	if m.hash == "correct horse" || bcrypt.CompareHashAndPassword([]byte(m.hash), []byte("correct horse")) != nil {
		t.Error("the password should be stored hashed")
	}
	if _, ok := m.sessions[token]; ok {
		t.Error("the raw token should not be stored")
	}
	if required, _ := s.SetupRequired(ctx); required {
		t.Error("setup should be done")
	}
	if _, _, err := s.Setup(ctx, "again", "correct horse"); !errors.Is(err, ErrSetupDone) {
		t.Errorf("second setup: got %v, want ErrSetupDone", err)
	}

	if got, err := s.UserForToken(ctx, token); err != nil || got.ID != user.ID {
		t.Errorf("token after setup: %+v, %v", got, err)
	}
	if _, err := s.UserForToken(ctx, ""); !errors.Is(err, ErrNoSession) {
		t.Errorf("empty token: got %v, want ErrNoSession", err)
	}
	if _, err := s.UserForToken(ctx, "nope"); !errors.Is(err, ErrNoSession) {
		t.Errorf("unknown token: got %v, want ErrNoSession", err)
	}

	if _, _, err := s.Login(ctx, "jinghao", "wrong"); !errors.Is(err, ErrBadCredentials) {
		t.Errorf("wrong password: got %v, want ErrBadCredentials", err)
	}
	if _, _, err := s.Login(ctx, "nobody", "correct horse"); !errors.Is(err, ErrBadCredentials) {
		t.Errorf("unknown name: got %v, want ErrBadCredentials", err)
	}
	_, second, err := s.Login(ctx, "jinghao", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if second == token {
		t.Error("each sign-in should get its own token")
	}

	if err := s.Logout(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserForToken(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Error("a signed-out token should not work")
	}
	if _, err := s.UserForToken(ctx, second); err != nil {
		t.Error("the other session should survive a logout")
	}
}

func TestChangePassword_SignsOtherBrowsersOut(t *testing.T) {
	ctx := context.Background()
	m := newMemStore()
	s := newService(m)
	user, first, err := s.Setup(ctx, "jinghao", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	_, second, _ := s.Login(ctx, "jinghao", "correct horse")

	if _, err := s.ChangePassword(ctx, user.ID, "wrong", "battery staple"); !errors.Is(err, ErrBadCredentials) {
		t.Errorf("wrong current password: got %v", err)
	}
	if _, err := s.ChangePassword(ctx, user.ID, "correct horse", "short"); !errors.Is(err, ErrWeakPassword) {
		t.Errorf("weak new password: got %v", err)
	}
	fresh, err := s.ChangePassword(ctx, user.ID, "correct horse", "battery staple")
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{first, second} {
		if _, err := s.UserForToken(ctx, old); !errors.Is(err, ErrNoSession) {
			t.Error("old sessions should be gone after a password change")
		}
	}
	if _, err := s.UserForToken(ctx, fresh); err != nil {
		t.Error("the fresh token should work")
	}
	if _, _, err := s.Login(ctx, "jinghao", "correct horse"); !errors.Is(err, ErrBadCredentials) {
		t.Error("the old password should no longer work")
	}
	if _, _, err := s.Login(ctx, "jinghao", "battery staple"); err != nil {
		t.Error("the new password should work")
	}
}

func TestSessionsExpire(t *testing.T) {
	ctx := context.Background()
	m := newMemStore()
	s := New(m, time.Hour)
	s.Cost = bcrypt.MinCost
	s.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	_, token, err := s.Setup(ctx, "jinghao", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserForToken(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Error("an expired session should not sign anyone in")
	}
	if n, _ := s.Sweep(ctx); n != 1 {
		t.Errorf("sweep removed %d sessions, want 1", n)
	}
}
