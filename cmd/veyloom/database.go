package main

import (
	"context"
	"fmt"

	"github.com/J0EY0/veyloom/internal/store"
)

// openStore connects to the database, brings its schema up to date and
// seeds the builtin agent templates. Doing this on every start keeps a
// single binary self-sufficient.
func openStore(ctx context.Context, url string) (*store.Store, error) {
	s, err := store.Open(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := s.Migrate(ctx); err != nil {
		s.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	if err := s.EnsureBuiltinAgentTemplates(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}
