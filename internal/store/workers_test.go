package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

func TestRegisterWorker_FirstConnectionCreates(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	engines := []engine.Info{{Name: "claude", Status: engine.StatusNotLoggedIn}}
	id, err := s.RegisterWorker(ctx, "", "laptop", engines)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected a non-empty id")
	}

	rec, err := s.GetWorker(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "laptop" || len(rec.Engines) != 1 || rec.Engines[0].Status != engine.StatusNotLoggedIn {
		t.Errorf("unexpected record: %+v", rec)
	}
	if rec.DisconnectedAt != nil {
		t.Error("a freshly registered worker must not be marked disconnected")
	}
}

func TestRegisterWorker_KnownIDReconnects(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	id, err := s.RegisterWorker(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkWorkerDisconnected(ctx, id); err != nil {
		t.Fatal(err)
	}

	// The machine was renamed and logged in since the last connection.
	again, err := s.RegisterWorker(ctx, id, "laptop-renamed", []engine.Info{{Name: "claude", Status: engine.StatusReady}})
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Errorf("reconnect returned id %s, want the presented %s", again, id)
	}

	rec, err := s.GetWorker(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "laptop-renamed" {
		t.Errorf("reconnect must refresh the label, got %q", rec.Name)
	}
	if rec.DisconnectedAt != nil {
		t.Error("reconnecting must clear DisconnectedAt")
	}
	if rec.Engines[0].Status != engine.StatusReady {
		t.Errorf("reconnecting must replace engines, got %+v", rec.Engines)
	}

	recs, _ := s.ListWorkers(ctx)
	if len(recs) != 1 {
		t.Errorf("reconnect must not create a second row, got %d", len(recs))
	}
}

func TestRegisterWorker_UnknownIDGetsFreshIdentity(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	stale := "11111111-2222-3333-4444-555555555555"
	id, err := s.RegisterWorker(ctx, stale, "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id == stale || id == "" {
		t.Errorf("got id %q, want a newly assigned one", id)
	}
	if _, err := s.GetWorker(ctx, id); err != nil {
		t.Errorf("new worker should exist: %v", err)
	}
}

func TestRegisterWorker_MalformedIDRejected(t *testing.T) {
	s := storetest.New(t)

	if _, err := s.RegisterWorker(context.Background(), "not-a-uuid", "laptop", nil); err == nil {
		t.Error("a malformed id should be an error, not silently replaced")
	}
}

func TestRegisterWorker_NamesNeedNotBeUnique(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	a, err := s.RegisterWorker(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.RegisterWorker(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two first-time workers with the same name must get different ids")
	}
}

func TestRegisterWorker_NilEnginesStoredAsEmptyList(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	id, err := s.RegisterWorker(ctx, "", "bare", nil)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.GetWorker(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Engines == nil || len(rec.Engines) != 0 {
		t.Errorf("Engines = %#v, want an empty, non-nil slice", rec.Engines)
	}
}

func TestTouchWorker_AdvancesLastSeen(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	id, err := s.RegisterWorker(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetWorker(ctx, id)

	time.Sleep(10 * time.Millisecond)
	if err := s.TouchWorker(ctx, id); err != nil {
		t.Fatal(err)
	}

	after, _ := s.GetWorker(ctx, id)
	if !after.LastSeenAt.After(before.LastSeenAt) {
		t.Errorf("LastSeenAt did not advance: before %v, after %v", before.LastSeenAt, after.LastSeenAt)
	}
	if !after.ConnectedAt.Equal(before.ConnectedAt) {
		t.Error("ConnectedAt must not move on touch")
	}
}

func TestUpdateWorkerEngines(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	id, err := s.RegisterWorker(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateWorkerEngines(ctx, id, []engine.Info{{Name: "pi", Status: engine.StatusAuthUnknown}}); err != nil {
		t.Fatal(err)
	}

	rec, err := s.GetWorker(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Engines) != 1 || rec.Engines[0].Name != "pi" {
		t.Errorf("unexpected engines: %+v", rec.Engines)
	}
}

func TestListWorkers_OrderedByName(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	for _, name := range []string{"zeta", "alpha", "mid"} {
		if _, err := s.RegisterWorker(ctx, "", name, nil); err != nil {
			t.Fatal(err)
		}
	}

	recs, err := s.ListWorkers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{recs[0].Name, recs[1].Name, recs[2].Name}
	if got[0] != "alpha" || got[1] != "mid" || got[2] != "zeta" {
		t.Errorf("order = %v, want alphabetical", got)
	}
}

func TestGetWorker_NotFoundAndInvalidID(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	_, err := s.GetWorker(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown id: got %v, want ErrNotFound", err)
	}

	if _, err := s.GetWorker(ctx, "not-a-uuid"); err == nil {
		t.Error("invalid id should be rejected")
	}
}

func TestMigrate_IsIdempotent(t *testing.T) {
	s := storetest.New(t)

	// storetest already migrated once; a second run must be a no-op.
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}
}
