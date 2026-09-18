package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestAttachments_ClaimedByTheMessageThatCarriesThem(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	id := store.NewID()
	att, err := f.s.CreateAttachment(ctx, store.NewAttachment{ID: id, RoomID: f.room.ID, Filename: "diagram.png", MediaType: "image/png", Size: 1234, Path: f.room.ID + "/" + id + ".png"})
	if err != nil {
		t.Fatal(err)
	}
	if att.ID != id || att.MessageID != "" || att.Filename != "diagram.png" {
		t.Fatalf("attachment = %+v", att)
	}
	if got, err := f.s.GetAttachment(ctx, id); err != nil || got.Path != att.Path {
		t.Fatalf("get = %+v, %v", got, err)
	}

	// Words are optional when a file comes along.
	msg, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, AttachmentIDs: []string{id, id}})
	if err != nil {
		t.Fatalf("post with attachment: %v", err)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].ID != id || msg.Attachments[0].MessageID != msg.ID {
		t.Fatalf("attachments on the new message = %+v", msg.Attachments)
	}

	// Every read of the message brings its attachments along.
	got, err := f.s.GetMessage(ctx, msg.ID)
	if err != nil || len(got.Attachments) != 1 || got.Attachments[0].Filename != "diagram.png" {
		t.Errorf("GetMessage attachments = %+v, %v", got.Attachments, err)
	}
	page, err := f.s.ListRoomMessagesBefore(ctx, f.room.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	var seen bool
	for _, m := range page {
		if m.ID == msg.ID {
			seen = len(m.Attachments) == 1
		} else if m.Attachments == nil {
			t.Errorf("message %s: attachments should be an empty slice, not nil", m.ID)
		}
	}
	if !seen {
		t.Errorf("room listing lost the attachment: %+v", page)
	}
	inbox, err := f.s.ListUserMentions(ctx, f.user.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range inbox {
		if item.Attachments == nil {
			t.Errorf("inbox item %s: attachments should be an empty slice, not nil", item.ID)
		}
	}

	// A claimed upload cannot be posted again.
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "again", AttachmentIDs: []string{id}}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("reusing an attachment: got %v, want ErrInvalidInput", err)
	}
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "ghost", AttachmentIDs: []string{store.NewID()}}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("unknown attachment: got %v, want ErrInvalidInput", err)
	}
	// And the failed post left nothing behind.
	after, err := f.s.ListRoomMessagesBefore(ctx, f.room.ID, 0, 10)
	if err != nil || len(after) != len(page) {
		t.Errorf("a refused post must not leave a message: %d before, %d after (%v)", len(page), len(after), err)
	}
}

func TestAttachments_PeopleStillHaveToSaySomething(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "  "}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("blank message without attachments: got %v, want ErrInvalidInput", err)
	}
	if _, err := f.s.CreateAttachment(ctx, store.NewAttachment{ID: store.NewID(), RoomID: store.NewID(), Filename: "x", MediaType: "text/plain", Path: "x"}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("attachment in an unknown room: got %v, want ErrNotFound", err)
	}
}
