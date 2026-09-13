// Package storetest provides a real Postgres database for tests.
//
// Each call to New creates a throwaway database on the server named by
// VEYLOOM_TEST_DATABASE_URL, migrates it, and drops it when the test ends.
// Tests are skipped when the variable is unset, so the rest of the suite
// runs without a database.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/J0EY0/veyloom/internal/store"
)

// EnvDatabaseURL names the environment variable holding the admin
// connection URL, for example the one from docker-compose.yml:
//
//	postgres://veyloom:veyloom@localhost:5432/veyloom?sslmode=disable
const EnvDatabaseURL = "VEYLOOM_TEST_DATABASE_URL"

// New returns a migrated Store on a fresh database, or skips the test when
// no database server is configured.
func New(t *testing.T) *store.Store {
	t.Helper()

	adminURL := os.Getenv(EnvDatabaseURL)
	if adminURL == "" {
		t.Skipf("%s not set; run `docker compose up -d` and export it to enable database tests", EnvDatabaseURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to test database server: %v", err)
	}

	name := "veyloom_test_" + randomHex(6)
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	s, err := store.Open(ctx, withDatabase(t, adminURL, name))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}

	t.Cleanup(func() {
		s.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// FORCE terminates any straggling connections so the drop cannot
		// hang a test run.
		if _, err := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)"); err != nil {
			t.Logf("drop test database %s: %v", name, err)
		}
		admin.Close(ctx)
	})
	return s
}

// withDatabase returns adminURL pointing at a different database name.
func withDatabase(t *testing.T, adminURL, name string) string {
	t.Helper()
	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse %s: %v", EnvDatabaseURL, err)
	}
	u.Path = "/" + name
	return u.String()
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Exec runs a raw SQL statement against the test database. It exists for
// setup and schema assertions that the Store deliberately has no method for.
func Exec(t *testing.T, s *store.Store, sql string, args ...any) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.Exec(ctx, sql, args...)
}
