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

func TestUsers_Rename(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	me, err := s.CreateUser(ctx, "jinghao")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := s.RenameUser(ctx, me.ID, "Jinghao Xian")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != me.ID || renamed.Name != "Jinghao Xian" || !renamed.CreatedAt.Equal(me.CreatedAt) {
		t.Errorf("unexpected rename: %+v", renamed)
	}
	if got, _ := s.GetUser(ctx, me.ID); got.Name != "Jinghao Xian" {
		t.Errorf("user after rename: %+v", got)
	}
	if _, err := s.RenameUser(ctx, me.ID, "  "); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("blank name: got %v, want ErrInvalidInput", err)
	}
	if _, err := s.RenameUser(ctx, "00000000-0000-0000-0000-000000000000", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown id: got %v, want ErrNotFound", err)
	}
}
