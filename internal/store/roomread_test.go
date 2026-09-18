package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

func TestRoomRead_ThreadByNumber(t *testing.T) {
	f := newBriefFixture(t)
	ctx := context.Background()

	got, err := f.s.ThreadByNumber(ctx, f.room.ID, 2)
	if err != nil || got.ID != f.topic2.ID {
		t.Errorf("#2 = %+v, %v; want %s", got, err, f.topic2.ID)
	}
	if _, err := f.s.ThreadByNumber(ctx, f.room.ID, 9); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("#9: got %v, want ErrNotFound", err)
	}
	// Numbers belong to a room.
	_, other, err := f.s.CreateProject(ctx, store.NewProject{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ThreadByNumber(ctx, other.ID, 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("#1 of a room without topics: got %v, want ErrNotFound", err)
	}
}

func TestRoomRead_ListRoomTopics(t *testing.T) {
	f := newBriefFixture(t)
	ctx := context.Background()

	// A third topic nobody has replied in: its root is its last message.
	quiet, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "anyone?"})
	if err != nil {
		t.Fatal(err)
	}
	topic3, err := f.s.ThreadForMessage(ctx, quiet.ID)
	if err != nil {
		t.Fatal(err)
	}

	topics, err := f.s.ListRoomTopics(ctx, f.room.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) != 3 || topics[0].Number != 3 || topics[1].Number != 2 || topics[2].Number != 1 {
		t.Fatalf("topics = %+v, want #3, #2, #1 by last activity", topics)
	}
	if got := topics[0]; got.ThreadID != topic3.ID || got.ReplyCount != 0 || got.Last.ID != quiet.ID || got.Root.ID != quiet.ID || got.LastSeq != quiet.Seq {
		t.Errorf("a topic without replies = %+v, want its root as its last message", got)
	}
	if got := topics[2]; got.ReplyCount != 2 || got.Root.ID != f.root.ID || got.Last.ID != f.reply.ID {
		t.Errorf("#1 = %+v, want two replies, alice's the last", got)
	}

	// Paged by the last message's seq.
	page, err := f.s.ListRoomTopics(ctx, f.room.ID, topics[0].LastSeq, 1)
	if err != nil || len(page) != 1 || page[0].Number != 2 {
		t.Errorf("page after #3 = %+v, %v; want #2", page, err)
	}
}

func TestRoomRead_Search(t *testing.T) {
	f := newBriefFixture(t)
	ctx := context.Background()
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.topic2.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "the rate is 100% of_quota"}); err != nil {
		t.Fatal(err)
	}

	hits, err := f.s.SearchRoomMessages(ctx, f.room.ID, "THING", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != f.second.ID || hits[0].TopicNumber != 2 {
		t.Errorf("search for THING = %+v, want the root of #2, whatever the case", hits)
	}
	// A reply is found with the number of the topic it is in.
	if hits, _ = f.s.SearchRoomMessages(ctx, f.room.ID, "details", 0, 10); len(hits) != 1 || hits[0].ID != f.more.ID || hits[0].TopicNumber != 2 {
		t.Errorf("search for details = %+v, want the reply in #2", hits)
	}
	// A message outside every topic has none.
	if hits, _ = f.s.SearchRoomMessages(ctx, f.room.ID, "all done", 0, 10); len(hits) != 1 || hits[0].TopicNumber != 0 {
		t.Errorf("search for all done = %+v, want the closing message, topic 0", hits)
	}
	// The phrase holds no wildcards: % and _ match themselves.
	if hits, _ = f.s.SearchRoomMessages(ctx, f.room.ID, "100% of_q", 0, 10); len(hits) != 1 {
		t.Errorf("search for a phrase with %% and _ = %+v, want the one message", hits)
	}
	if hits, _ = f.s.SearchRoomMessages(ctx, f.room.ID, "1_0", 0, 10); len(hits) != 0 {
		t.Errorf("_ must not match any character: %+v", hits)
	}
	if hits, _ = f.s.SearchRoomMessages(ctx, f.room.ID, "%", 0, 1); len(hits) != 1 {
		t.Errorf("%% alone finds the one message that holds it, got %+v", hits)
	}
	if _, err := f.s.SearchRoomMessages(ctx, f.room.ID, "   ", 0, 10); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("an empty phrase: got %v, want ErrInvalidInput", err)
	}
	// Another room's messages are not found.
	_, other, _ := f.s.CreateProject(ctx, store.NewProject{Name: "other"})
	if hits, _ = f.s.SearchRoomMessages(ctx, other.ID, "thing", 0, 10); len(hits) != 0 {
		t.Errorf("search in another room = %+v, want nothing", hits)
	}
}

func TestTurns_FilesChanged(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	turn, err := f.s.CreateTurn(ctx, f.newTurn())
	if err != nil {
		t.Fatal(err)
	}
	if len(turn.FilesChanged) != 0 {
		t.Errorf("a running turn has changed %v", turn.FilesChanged)
	}
	done, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: store.TurnDone, Usage: runtime.Usage{OutputTokens: 1}, FilesChanged: []string{"cmd/main.go", "README.md"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(done.FilesChanged) != 2 || done.FilesChanged[0] != "cmd/main.go" || done.FilesChanged[1] != "README.md" {
		t.Errorf("FilesChanged = %v, want them in the order given", done.FilesChanged)
	}
	// None is none, not a null the column refuses.
	other, _ := f.s.CreateTurn(ctx, f.newTurn())
	if got, err := f.s.FinishTurn(ctx, other.ID, store.TurnOutcome{Status: store.TurnFailed}); err != nil || len(got.FilesChanged) != 0 {
		t.Errorf("a turn that changed nothing = %v, %v", got.FilesChanged, err)
	}
}
