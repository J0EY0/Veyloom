package account

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestFile_AccountLivesAcrossReopens(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state", "account.json")
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if has, _ := f.HasAccount(ctx); has {
		t.Fatal("a missing file should mean no account")
	}
	if _, _, err := f.AccountByName(ctx, "anyone"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("no account by name: got %v, want ErrNotFound", err)
	}
	if _, err := f.CreateAccount(ctx, "  ", "hash"); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("blank name: got %v, want ErrInvalidInput", err)
	}

	me, err := f.CreateAccount(ctx, " Jinghao ", "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if me.ID == "" || me.Name != "Jinghao" || me.CreatedAt.IsZero() {
		t.Errorf("unexpected account: %+v", me)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v, want 0600", info.Mode().Perm())
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), `"password_hash": "hash1"`) {
		t.Errorf("file should hold the hash: %s", raw)
	}
	if _, err := f.CreateAccount(ctx, "other", "hash2"); !errors.Is(err, store.ErrConflict) {
		t.Errorf("second account: got %v, want ErrConflict", err)
	}

	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, hash, err := again.AccountByName(ctx, "jinghao")
	if err != nil || got.ID != me.ID || hash != "hash1" || !got.CreatedAt.Equal(me.CreatedAt) {
		t.Errorf("reopened account: %+v %q %v", got, hash, err)
	}
	if h, _ := again.PasswordHash(ctx, me.ID); h != "hash1" {
		t.Errorf("hash by id: %q", h)
	}
	if _, err := again.PasswordHash(ctx, "someone"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("hash of stranger: got %v, want ErrNotFound", err)
	}
	if err := again.SetPasswordHash(ctx, me.ID, "hash2"); err != nil {
		t.Fatal(err)
	}
	if err := again.SetPasswordHash(ctx, "someone", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("set hash of stranger: got %v, want ErrNotFound", err)
	}
	renamed, err := again.Rename(" Jinghao Xian ")
	if err != nil || renamed.Name != "Jinghao Xian" || renamed.ID != me.ID {
		t.Errorf("rename: %+v %v", renamed, err)
	}
	if _, err := again.Rename("  "); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("blank rename: got %v", err)
	}
	third, _ := Open(path)
	if u, ok := third.Account(); !ok || u.Name != "Jinghao Xian" {
		t.Errorf("rename should persist: %+v", u)
	}
	if h, _ := third.PasswordHash(ctx, me.ID); h != "hash2" {
		t.Errorf("new hash should persist: %q", h)
	}
}

func TestFile_Sessions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "account.json")
	f, _ := Open(path)
	if err := f.CreateSession(ctx, "early", "nobody", time.Now().Add(time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("session before an account: got %v, want ErrNotFound", err)
	}
	me, err := f.CreateAccount(ctx, "jinghao", "hash")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.CreateSession(ctx, "live", me.ID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.CreateSession(ctx, "stale", me.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.CreateSession(ctx, "x", "someone", time.Now().Add(time.Hour)); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("session for a stranger: got %v, want ErrNotFound", err)
	}

	// Sessions survive a restart.
	again, _ := Open(path)
	if u, err := again.SessionUser(ctx, "live"); err != nil || u.ID != me.ID {
		t.Errorf("live session: %+v %v", u, err)
	}
	if _, err := again.SessionUser(ctx, "stale"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expired session: got %v, want ErrNotFound", err)
	}
	if _, err := again.SessionUser(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown session: got %v, want ErrNotFound", err)
	}
	if n, _ := again.DeleteExpiredSessions(ctx); n != 1 {
		t.Errorf("swept %d, want 1", n)
	}
	if err := again.DeleteSession(ctx, "live"); err != nil {
		t.Fatal(err)
	}
	if _, err := again.SessionUser(ctx, "live"); !errors.Is(err, store.ErrNotFound) {
		t.Error("a deleted session should be gone")
	}
	if err := again.DeleteSession(ctx, "live"); err != nil {
		t.Error("deleting twice should be fine")
	}
	_ = again.CreateSession(ctx, "a", me.ID, time.Now().Add(time.Hour))
	_ = again.CreateSession(ctx, "b", me.ID, time.Now().Add(time.Hour))
	if err := again.DeleteUserSessions(ctx, me.ID); err != nil {
		t.Fatal(err)
	}
	third, _ := Open(path)
	for _, token := range []string{"a", "b"} {
		if _, err := third.SessionUser(ctx, token); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("session %s should be gone", token)
		}
	}
}

// The memory switches (docs/design.md 5.19) start all on and are kept
// with the account.
func TestFile_MemoryPrefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account.json")
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.MemoryPrefs(); got != store.DefaultMemoryPrefs {
		t.Errorf("before any are set: %+v", got)
	}
	off := store.MemoryPrefs{Enabled: true, Personal: false, Project: true}
	if err := f.SetMemoryPrefs(off); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.MemoryPrefs(); got != off || got.UsesPersonal() || !got.UsesProject() {
		t.Errorf("after a reopen: %+v", got)
	}
	if all := (store.MemoryPrefs{Personal: true, Project: true}); all.UsesAny() {
		t.Error("the switch for memory as a whole turns both off")
	}
}

func TestOpen_RejectsABrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "account.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Error("a broken file should fail to open")
	}
}
