package main

import (
	"context"
	"fmt"

	"github.com/J0EY0/veyloom/internal/store"
)

// openStore connects to the database and brings its schema up to date.
// Doing this on every start keeps a single binary self-sufficient. It
// seeds nothing: a fresh install has no agents, and the Agents page asks
// you to make the first one (2026-09-16).
func openStore(ctx context.Context, url string) (*store.Store, error) {
	s, err := store.Open(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := s.Migrate(ctx); err != nil {
		s.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	return s, nil
}
