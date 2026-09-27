package hub

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// An upload nobody sent goes a day later, with its thumbnail; one a
// message carries stays.
func TestSweepAttachments(t *testing.T) {
	dir := t.TempDir()
	l := newLoopWith(t, Config{AttachmentDir: dir})
	write := func(rel string) {
		t.Helper()
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	upload := func(name string, thumb bool) store.Attachment {
		t.Helper()
		id := store.NewID()
		rel, thumbRel := l.room.ID+"/"+id+".png", ""
		write(rel)
		if thumb {
			thumbRel = l.room.ID + "/" + id + ".thumb.jpg"
			write(thumbRel)
		}
		a, err := l.s.CreateAttachment(l.ctx, store.NewAttachment{
			ID: id, RoomID: l.room.ID, Filename: name, MediaType: "image/png", Kind: store.AttachmentImage, Size: 5, Path: rel, ThumbnailPath: thumbRel,
		})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	loose := upload("draft.png", true)
	sent := upload("shot.png", false)
	if _, err := l.s.CreateMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, SenderKind: store.SenderUser, UserID: l.user.ID, AttachmentIDs: []string{sent.ID}}); err != nil {
		t.Fatal(err)
	}

	if n := l.h.sweepAttachments(l.ctx, time.Now().Add(-time.Hour)); n != 0 {
		t.Errorf("swept %d uploads younger than the cut", n)
	}
	if n := l.h.sweepAttachments(l.ctx, time.Now().Add(time.Minute)); n != 1 {
		t.Errorf("swept %d uploads, want the one nobody sent", n)
	}
	if _, err := l.s.GetAttachment(l.ctx, loose.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the swept upload is still recorded: %v", err)
	}
	for _, rel := range []string{loose.Path, loose.ThumbnailPath} {
		if _, err := os.Stat(filepath.Join(dir, rel)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still on disk: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, sent.Path)); err != nil {
		t.Errorf("the sent attachment's file went: %v", err)
	}
	if _, err := l.s.GetAttachment(l.ctx, sent.ID); err != nil {
		t.Errorf("the sent attachment went: %v", err)
	}
}
