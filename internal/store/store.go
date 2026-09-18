// Package store persists Veyloom's state in Postgres.
//
// Database access goes through sqlc-generated queries in the db subpackage.
// This package wraps them with the types the rest of the code uses, owns the
// connection pool and applies schema migrations.
package store

import (
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/J0EY0/veyloom/internal/store/db"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("store: not found")

// ErrInvalidID is returned when an id is not a well-formed UUID.
var ErrInvalidID = errors.New("store: invalid id")

// ErrConflict is returned when a row would violate a uniqueness rule, such
// as a second agent with the same name.
var ErrConflict = errors.New("store: conflict")

// NewID returns a fresh random UUID in the canonical text form, for rows
// whose id the caller needs to know before the insert.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("store: random: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Store is a handle to the database. It is safe for concurrent use.
type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// Open connects to Postgres using a URL such as
// postgres://user:pass@host:5432/dbname and verifies that the database is
// reachable.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return &Store{pool: pool, q: db.New(pool)}, nil
}

// Migrate applies every pending schema migration embedded in the binary. It
// is idempotent, so it runs at each startup.
func (s *Store) Migrate(ctx context.Context) error {
	migrations, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}

	// goose speaks database/sql; borrow connections from the pool for the
	// duration of the migration.
	sqlDB := stdlib.OpenDBFromPool(s.pool)
	defer sqlDB.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations)
	if err != nil {
		return fmt.Errorf("prepare migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Close releases the connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Exec runs a raw statement. It is intended for tests and one-off tooling;
// application code goes through typed methods.
func (s *Store) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := s.pool.Exec(ctx, sql, args...)
	return err
}
