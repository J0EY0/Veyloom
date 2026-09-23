// Package auth signs the one person in. The account is a password hash on
// their users row; a signed-in browser holds a random token in a cookie
// and the sessions table holds the token's hash (docs/webui.md §4.8).
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/J0EY0/veyloom/internal/store"
)

// Store persists accounts and sessions; it is the store.
type Store interface {
	HasAccount(ctx context.Context) (bool, error)
	CreateAccount(ctx context.Context, name, passwordHash string) (store.User, error)
	AccountByName(ctx context.Context, name string) (store.User, string, error)
	PasswordHash(ctx context.Context, userID string) (string, error)
	SetPasswordHash(ctx context.Context, userID, hash string) error
	CreateSession(ctx context.Context, tokenHash, userID string, expires time.Time) error
	SessionUser(ctx context.Context, tokenHash string) (store.User, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteUserSessions(ctx context.Context, userID string) error
	DeleteExpiredSessions(ctx context.Context) (int64, error)
}

// MinPasswordLen is the shortest password accepted.
const MinPasswordLen = 8

// MaxPasswordBytes is the longest password accepted, in bytes of UTF-8:
// bcrypt takes no more.
const MaxPasswordBytes = 72

// DefaultSessionTTL is how long a sign-in lasts.
const DefaultSessionTTL = 30 * 24 * time.Hour

var (
	// ErrSetupDone means the account already exists; sign in instead.
	ErrSetupDone = errors.New("auth: the account already exists")
	// ErrBadCredentials covers an unknown username and a wrong password alike.
	ErrBadCredentials = errors.New("auth: wrong username or password")
	// ErrNoSession means the token names no live session.
	ErrNoSession = errors.New("auth: not signed in")
	// ErrWeakPassword means the password is too short.
	ErrWeakPassword = fmt.Errorf("auth: the password needs at least %d characters", MinPasswordLen)
	// ErrLongPassword means the password is longer than bcrypt takes.
	ErrLongPassword = fmt.Errorf("auth: the password can be at most %d bytes", MaxPasswordBytes)
)

// Service is the sign-in logic over a Store.
type Service struct {
	store Store
	ttl   time.Duration
	// Cost is the bcrypt cost; tests lower it.
	Cost int
	now  func() time.Time
}

// New returns a Service whose sessions last ttl (DefaultSessionTTL when
// zero).
func New(s Store, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}
	return &Service{store: s, ttl: ttl, Cost: bcrypt.DefaultCost, now: time.Now}
}

// TTL is how long a new session lasts; the cookie gets the same age.
func (s *Service) TTL() time.Duration { return s.ttl }

// SetupRequired reports whether nobody has registered yet.
func (s *Service) SetupRequired(ctx context.Context) (bool, error) {
	has, err := s.store.HasAccount(ctx)
	return !has, err
}

// Setup registers the one account and signs it in, returning the session
// token for the cookie. ErrSetupDone when an account exists already.
func (s *Service) Setup(ctx context.Context, name, password string) (store.User, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.User{}, "", fmt.Errorf("%w: name is required", store.ErrInvalidInput)
	}
	if err := checkPassword(password); err != nil {
		return store.User{}, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.Cost)
	if err != nil {
		return store.User{}, "", fmt.Errorf("hash password: %w", err)
	}
	user, err := s.store.CreateAccount(ctx, name, string(hash))
	if errors.Is(err, store.ErrConflict) {
		return store.User{}, "", ErrSetupDone
	}
	if err != nil {
		return store.User{}, "", err
	}
	token, err := s.openSession(ctx, user.ID)
	return user, token, err
}

// Login checks the password and opens a session.
func (s *Service) Login(ctx context.Context, name, password string) (store.User, string, error) {
	user, hash, err := s.store.AccountByName(ctx, strings.TrimSpace(name))
	if errors.Is(err, store.ErrNotFound) {
		// Take as long as a real comparison would, so a name cannot be
		// probed by timing.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return store.User{}, "", ErrBadCredentials
	}
	if err != nil {
		return store.User{}, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return store.User{}, "", ErrBadCredentials
	}
	token, err := s.openSession(ctx, user.ID)
	return user, token, err
}

// Logout ends the session behind token; an unknown token is fine.
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, hashToken(token))
}

// UserForToken returns who the token signs in, or ErrNoSession.
func (s *Service) UserForToken(ctx context.Context, token string) (store.User, error) {
	if token == "" {
		return store.User{}, ErrNoSession
	}
	user, err := s.store.SessionUser(ctx, hashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, ErrNoSession
	}
	return user, err
}

// ChangePassword checks the current password, sets the new one and signs
// every browser out except this one, which gets the returned fresh token.
func (s *Service) ChangePassword(ctx context.Context, userID, current, next string) (string, error) {
	hash, err := s.store.PasswordHash(ctx, userID)
	if err != nil {
		return "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(current)) != nil {
		return "", ErrBadCredentials
	}
	if err := checkPassword(next); err != nil {
		return "", err
	}
	fresh, err := bcrypt.GenerateFromPassword([]byte(next), s.Cost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	if err := s.store.SetPasswordHash(ctx, userID, string(fresh)); err != nil {
		return "", err
	}
	if err := s.store.DeleteUserSessions(ctx, userID); err != nil {
		return "", err
	}
	return s.openSession(ctx, userID)
}

// Sweep drops expired sessions; serve calls it at startup.
func (s *Service) Sweep(ctx context.Context) (int64, error) {
	return s.store.DeleteExpiredSessions(ctx)
}

func (s *Service) openSession(ctx context.Context, userID string) (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw[:])
	if err := s.store.CreateSession(ctx, hashToken(token), userID, s.now().Add(s.ttl)); err != nil {
		return "", err
	}
	return token, nil
}

func checkPassword(password string) error {
	if len([]rune(password)) < MinPasswordLen {
		return ErrWeakPassword
	}
	if len(password) > MaxPasswordBytes {
		return ErrLongPassword
	}
	return nil
}

// hashToken is what the sessions table stores instead of the token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// dummyHash is compared against when the name is unknown, so both branches
// of Login cost the same. Its cost matches DefaultCost.
var dummyHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("veyloom"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return h
}()
