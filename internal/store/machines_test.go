package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

func TestRegisterMachine_FirstConnectionCreates(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	runtimes := []runtime.Info{{Name: "claude", Status: runtime.StatusError}}
	id, err := s.RegisterMachine(ctx, "", "laptop", runtimes)
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected a non-empty id")
	}

	rec, err := s.GetMachine(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "laptop" || len(rec.Runtimes) != 1 || rec.Runtimes[0].Status != runtime.StatusError {
		t.Errorf("unexpected record: %+v", rec)
	}
	if rec.DisconnectedAt != nil {
		t.Error("a freshly registered machine must not be marked disconnected")
	}
}

func TestRegisterMachine_KnownIDReconnects(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	id, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkMachineDisconnected(ctx, id); err != nil {
		t.Fatal(err)
	}

	// The machine was renamed and logged in since the last connection.
	again, err := s.RegisterMachine(ctx, id, "laptop-renamed", []runtime.Info{{Name: "claude", Status: runtime.StatusReady}})
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Errorf("reconnect returned id %s, want the presented %s", again, id)
	}

	rec, err := s.GetMachine(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Name != "laptop-renamed" {
		t.Errorf("reconnect must refresh the label, got %q", rec.Name)
	}
	if rec.DisconnectedAt != nil {
		t.Error("reconnecting must clear DisconnectedAt")
	}
	if rec.Runtimes[0].Status != runtime.StatusReady {
		t.Errorf("reconnecting must replace runtimes, got %+v", rec.Runtimes)
	}

	recs, _ := s.ListMachines(ctx)
	if len(recs) != 1 {
		t.Errorf("reconnect must not create a second row, got %d", len(recs))
	}
}

func TestRegisterMachine_UnknownIDGetsFreshIdentity(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	stale := "11111111-2222-3333-4444-555555555555"
	id, err := s.RegisterMachine(ctx, stale, "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id == stale || id == "" {
		t.Errorf("got id %q, want a newly assigned one", id)
	}
	if _, err := s.GetMachine(ctx, id); err != nil {
		t.Errorf("new machine should exist: %v", err)
	}
}

func TestRegisterMachine_MalformedIDRejected(t *testing.T) {
	s := storetest.New(t)

	if _, err := s.RegisterMachine(context.Background(), "not-a-uuid", "laptop", nil); err == nil {
		t.Error("a malformed id should be an error, not silently replaced")
	}
}

func TestRegisterMachine_NamesNeedNotBeUnique(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	a, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two first-time machines with the same name must get different ids")
	}
}

func TestRegisterMachine_NilRuntimesStoredAsEmptyList(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	id, err := s.RegisterMachine(ctx, "", "bare", nil)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.GetMachine(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Runtimes == nil || len(rec.Runtimes) != 0 {
		t.Errorf("Runtimes = %#v, want an empty, non-nil slice", rec.Runtimes)
	}
}

func TestTouchMachine_AdvancesLastSeen(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	id, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.GetMachine(ctx, id)

	time.Sleep(10 * time.Millisecond)
	if err := s.TouchMachine(ctx, id); err != nil {
		t.Fatal(err)
	}

	after, _ := s.GetMachine(ctx, id)
	if !after.LastSeenAt.After(before.LastSeenAt) {
		t.Errorf("LastSeenAt did not advance: before %v, after %v", before.LastSeenAt, after.LastSeenAt)
	}
	if !after.ConnectedAt.Equal(before.ConnectedAt) {
		t.Error("ConnectedAt must not move on touch")
	}
}

func TestUpdateMachineRuntimes(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	id, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMachineRuntimes(ctx, id, []runtime.Info{{Name: "pi", Status: runtime.StatusReady}}); err != nil {
		t.Fatal(err)
	}

	rec, err := s.GetMachine(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Runtimes) != 1 || rec.Runtimes[0].Name != "pi" {
		t.Errorf("unexpected runtimes: %+v", rec.Runtimes)
	}
}

func TestListMachines_OrderedByName(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	for _, name := range []string{"zeta", "alpha", "mid"} {
		if _, err := s.RegisterMachine(ctx, "", name, nil); err != nil {
			t.Fatal(err)
		}
	}

	recs, err := s.ListMachines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{recs[0].Name, recs[1].Name, recs[2].Name}
	if got[0] != "alpha" || got[1] != "mid" || got[2] != "zeta" {
		t.Errorf("order = %v, want alphabetical", got)
	}
}

func TestGetMachine_NotFoundAndInvalidID(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	_, err := s.GetMachine(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown id: got %v, want ErrNotFound", err)
	}

	if _, err := s.GetMachine(ctx, "not-a-uuid"); err == nil {
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
