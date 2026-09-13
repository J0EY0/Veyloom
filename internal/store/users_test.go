package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

func TestUsers_CreateGetList(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()

	alice, err := s.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, "bob"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetUser(ctx, alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "alice" {
		t.Errorf("unexpected user: %+v", got)
	}

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].Name != "alice" || users[1].Name != "bob" {
		t.Errorf("unexpected list: %+v", users)
	}

	if _, err := s.GetUser(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown id: got %v, want ErrNotFound", err)
	}
	if _, err := s.CreateUser(ctx, "  "); err == nil {
		t.Error("blank name should be rejected")
	}
}
