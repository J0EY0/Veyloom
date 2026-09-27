package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestAttachmentKindOf(t *testing.T) {
	for _, c := range []struct{ mediaType, name, want string }{
		{"image/png", "shot.png", store.AttachmentImage},
		{"image/svg+xml", "logo.svg", store.AttachmentImage},
		{"video/mp4", "hang.mp4", store.AttachmentVideo},
		{"audio/mp4", "notes.m4a", store.AttachmentAudio},
		// Sound in an MP4 box sniffs as video.
		{"video/mp4", "补充.m4a", store.AttachmentAudio},
		{"application/pdf", "spec.pdf", store.AttachmentPDF},
		{"application/octet-stream", "spec.PDF", store.AttachmentPDF},
		{"text/plain; charset=utf-8", "tags_test.go", store.AttachmentText},
		{"application/octet-stream", "main.rs", store.AttachmentText},
		{"application/json", "data.json", store.AttachmentText},
		{"text/html", "bookmarks.html", store.AttachmentText},
		// An office file is a zip inside; its name says what it is.
		{"application/zip", "plan.xlsx", store.AttachmentOffice},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", "spec", store.AttachmentOffice},
		{"application/zip", "release.zip", store.AttachmentArchive},
		{"application/gzip", "logs.tar.gz", store.AttachmentArchive},
		{"application/octet-stream", "blob.bin", store.AttachmentOther},
	} {
		if got := store.AttachmentKindOf(c.mediaType, c.name); got != c.want {
			t.Errorf("%s %s: %s, want %s", c.mediaType, c.name, got, c.want)
		}
	}
}

func TestRoomAttachments_ListSearchSortAndSweep(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	upload := func(name, mediaType string, size int64) string {
		t.Helper()
		id := store.NewID()
		_, err := f.s.CreateAttachment(ctx, store.NewAttachment{
			ID: id, RoomID: f.room.ID, Filename: name, MediaType: mediaType, Kind: store.AttachmentKindOf(mediaType, name), Size: size,
			Width: 1280, Height: 800, Path: f.room.ID + "/" + id, ThumbnailPath: f.room.ID + "/" + id + ".thumb.jpg",
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	shot := upload("screen.png", "image/png", 3000)
	clip := upload("hang.mp4", "video/mp4", 9000)
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID,
		Body: "\nlist 折行了\n截图和录屏在这", AttachmentIDs: []string{shot, clip}}); err != nil {
		t.Fatal(err)
	}
	spec := upload("spec-v2.pdf", "application/pdf", 5000)
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID,
		Body: "按这份文档补测试", AttachmentIDs: []string{spec}}); err != nil {
		t.Fatal(err)
	}
	loose := upload("draft.txt", "text/plain", 10)

	list := func(q store.AttachmentQuery) ([]string, int) {
		t.Helper()
		found, total, err := f.s.ListRoomAttachments(ctx, f.room.ID, q)
		if err != nil {
			t.Fatal(err)
		}
		names := make([]string, len(found))
		for i, a := range found {
			names[i] = a.Filename
		}
		return names, total
	}

	// The newest first, a message's last file before its first; an upload
	// nobody sent is not there.
	all, total, err := f.s.ListRoomAttachments(ctx, f.room.ID, store.AttachmentQuery{})
	if err != nil || total != 3 || len(all) != 3 || all[0].ID != spec || all[1].ID != clip || all[2].ID != shot {
		t.Fatalf("all: %+v %d %v", all, total, err)
	}
	pdf, picture := all[0], all[2]
	if pdf.SenderKind != store.SenderAgent || pdf.MemberID != f.member.ID || pdf.SenderName != f.member.DisplayName || pdf.ThreadID != f.thread.ID ||
		pdf.ThreadNumber != f.thread.Number || pdf.Said != "按这份文档补测试" || pdf.Kind != store.AttachmentPDF {
		t.Errorf("the member's pdf: %+v", pdf)
	}
	// A person's name is not in the database; the query was not told it.
	if picture.SenderKind != store.SenderUser || picture.UserID != f.user.ID || picture.SenderName != "" || picture.ThreadID != "" ||
		picture.Said != "list 折行了" || picture.Width != 1280 || !picture.Thumbnail || picture.MessageSeq == 0 {
		t.Errorf("the person's picture: %+v", picture)
	}

	named, _, err := f.s.ListRoomAttachments(ctx, f.room.ID, store.AttachmentQuery{People: map[string]string{f.user.ID: "alice"}})
	if err != nil || named[2].SenderName != "alice" || named[0].SenderName != f.member.DisplayName {
		t.Errorf("named: %+v %v", named, err)
	}

	for _, c := range []struct {
		name string
		q    store.AttachmentQuery
		want []string
	}{
		{"pictures and video", store.AttachmentQuery{Kinds: []string{store.AttachmentImage, store.AttachmentVideo}}, []string{"hang.mp4", "screen.png"}},
		{"a file's name, any case", store.AttachmentQuery{Words: "PNG"}, []string{"screen.png"}},
		{"what the message said", store.AttachmentQuery{Words: "折行"}, []string{"hang.mp4", "screen.png"}},
		{"a member's name", store.AttachmentQuery{Words: f.member.DisplayName}, []string{"spec-v2.pdf"}},
		{"a person's name, from the account file", store.AttachmentQuery{Words: "ALI", People: map[string]string{f.user.ID: "alice"}}, []string{"hang.mp4", "screen.png"}},
		{"every word, each anywhere", store.AttachmentQuery{Words: " 折行  png ", People: map[string]string{f.user.ID: "alice"}}, []string{"screen.png"}},
		{"a word nothing has", store.AttachmentQuery{Words: "折行 spec"}, []string{}},
		{"a word the person's name has", store.AttachmentQuery{Words: "alice mp4", People: map[string]string{f.user.ID: "alice"}}, []string{"hang.mp4"}},
		{"a percent sign is just a character", store.AttachmentQuery{Words: "%"}, []string{}},
		{"what the person sent", store.AttachmentQuery{UserID: f.user.ID}, []string{"hang.mp4", "screen.png"}},
		{"what the member sent", store.AttachmentQuery{MemberID: f.member.ID}, []string{"spec-v2.pdf"}},
		{"biggest first", store.AttachmentQuery{Sort: store.AttachmentsBySize}, []string{"hang.mp4", "spec-v2.pdf", "screen.png"}},
		{"by name", store.AttachmentQuery{Sort: store.AttachmentsByName}, []string{"hang.mp4", "screen.png", "spec-v2.pdf"}},
		// Oldest first is newest first turned round.
		{"oldest first", store.AttachmentQuery{Sort: store.AttachmentsOldest}, []string{"screen.png", "hang.mp4", "spec-v2.pdf"}},
		{"a first page", store.AttachmentQuery{Limit: 2}, []string{"spec-v2.pdf", "hang.mp4"}},
		{"the next page", store.AttachmentQuery{Offset: 2, Limit: 2}, []string{"screen.png"}},
		{"past the end, however far", store.AttachmentQuery{Offset: 1 << 32, Limit: 2}, nil},
	} {
		got, n := list(c.q)
		if !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
		if c.q.Limit == 0 && n != len(c.want) {
			t.Errorf("%s: total %d, want %d", c.name, n, len(c.want))
		}
		if c.q.Limit != 0 && n != 3 {
			t.Errorf("%s: total %d counts every page, want 3", c.name, n)
		}
	}
	if _, _, err := f.s.ListRoomAttachments(ctx, f.room.ID, store.AttachmentQuery{Kinds: []string{"photo"}}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("unknown kind: %v", err)
	}
	if _, _, err := f.s.ListRoomAttachments(ctx, f.room.ID, store.AttachmentQuery{Sort: "colour"}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("unknown sort: %v", err)
	}

	// Picked for downloading together: only what messages of the room carry.
	picked, err := f.s.RoomAttachmentsByID(ctx, f.room.ID, []string{spec, loose, shot})
	if err != nil || len(picked) != 2 || picked[0].ID != shot || picked[1].ID != spec {
		t.Errorf("picked: %+v %v", picked, err)
	}

	// The upload nobody sent is swept once it is old enough.
	if left, err := f.s.UnclaimedAttachments(ctx, time.Now().Add(-time.Hour), 10); err != nil || len(left) != 0 {
		t.Errorf("too young to sweep: %+v %v", left, err)
	}
	left, err := f.s.UnclaimedAttachments(ctx, time.Now().Add(time.Minute), 10)
	if err != nil || len(left) != 1 || left[0].ID != loose {
		t.Fatalf("left behind: %+v %v", left, err)
	}
	if gone, err := f.s.DeleteUnclaimedAttachment(ctx, loose); err != nil || gone.Path == "" {
		t.Errorf("sweep: %+v %v", gone, err)
	}
	if _, err := f.s.DeleteUnclaimedAttachment(ctx, loose); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("sweep twice: %v", err)
	}
	if _, err := f.s.DeleteUnclaimedAttachment(ctx, shot); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a sent attachment must not be swept: %v", err)
	}
}
