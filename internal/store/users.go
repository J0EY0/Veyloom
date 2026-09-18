package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// User is a human member of the team.
type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateUser adds a user.
func (s *Store) CreateUser(ctx context.Context, name string) (User, error) {
	row, err := s.q.CreateUser(ctx, name)
	if err != nil {
		return User{}, fmt.Errorf("create user %q: %w", name, err)
	}
	return toUser(row), nil
}

// GetUser returns one user, or ErrNotFound.
func (s *Store) GetUser(ctx context.Context, id string) (User, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return User{}, err
	}
	row, err := s.q.GetUser(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("user %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return User{}, fmt.Errorf("get user %s: %w", id, err)
	}
	return toUser(row), nil
}

// ListUsers returns every user in creation order.
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	out := make([]User, 0, len(rows))
	for _, row := range rows {
		out = append(out, toUser(row))
	}
	return out, nil
}

// RenameUser changes a user's name, or returns ErrNotFound.
func (s *Store) RenameUser(ctx context.Context, id, name string) (User, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return User{}, err
	}
	row, err := s.q.RenameUser(ctx, db.RenameUserParams{ID: uid, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("user %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return User{}, mapPGError(fmt.Sprintf("rename user %s", id), err)
	}
	return toUser(row), nil
}

func toUser(row db.User) User {
	return User{ID: uuidString(row.ID), Name: row.Name, CreatedAt: row.CreatedAt.Time}
}
