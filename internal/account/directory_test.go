package account_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/J0EY0/veyloom/internal/account"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

func TestDirectory_LaysTheAccountOverTheTable(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	file, err := account.Open(filepath.Join(t.TempDir(), "account.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := &account.Directory{Store: s, File: file}

	alice, err := s.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if users, _ := dir.ListUsers(ctx); len(users) != 1 || users[0].Name != "alice" {
		t.Errorf("without an account: %+v", users)
	}

	me, err := file.CreateAccount(ctx, "jinghao", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := dir.GetUser(ctx, me.ID); err != nil || got.Name != "jinghao" {
		t.Errorf("account by id: %+v %v", got, err)
	}
	if got, err := dir.GetUser(ctx, alice.ID); err != nil || got.Name != "alice" {
		t.Errorf("table user by id: %+v %v", got, err)
	}
	if _, err := dir.GetUser(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown id: got %v, want ErrNotFound", err)
	}
	if users, _ := dir.ListUsers(ctx); len(users) != 2 || users[0].ID != me.ID || users[1].Name != "alice" {
		t.Errorf("account should come first: %+v", users)
	}

	if renamed, err := dir.RenameUser(ctx, me.ID, "Jinghao Xian"); err != nil || renamed.Name != "Jinghao Xian" {
		t.Errorf("rename account: %+v %v", renamed, err)
	}
	if u, _ := file.Account(); u.Name != "Jinghao Xian" {
		t.Error("the account rename should reach the file")
	}
	if renamed, err := dir.RenameUser(ctx, alice.ID, "Alice"); err != nil || renamed.Name != "Alice" {
		t.Errorf("rename table user: %+v %v", renamed, err)
	}
	if got, _ := s.GetUser(ctx, alice.ID); got.Name != "Alice" {
		t.Error("the table rename should reach the table")
	}

	// The account can write to the tables without a users row.
	_, room, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateMessage(ctx, store.NewMessage{RoomID: room.ID, SenderKind: store.SenderUser, UserID: me.ID, Body: "hi"}); err != nil {
		t.Errorf("message from the account: %v", err)
	}
}
