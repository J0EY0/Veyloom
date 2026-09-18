package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// topic builds what the hub builds for a top-level mention: an empty agent
// message heading a thread, with a turn running in it.
func (f turnFixture) topic(t *testing.T) (store.Message, store.Thread, store.Turn) {
	t.Helper()
	ctx := context.Background()
	root, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID})
	if err != nil {
		t.Fatalf("an agent root may be empty: %v", err)
	}
	thread, err := f.s.ThreadForMessage(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := f.s.CreateTurn(ctx, store.NewTurn{MemberID: f.member.ID, RoomID: f.room.ID, ThreadID: thread.ID, TriggerMessageID: f.root.ID, MachineID: f.machineID})
	if err != nil {
		t.Fatal(err)
	}
	return root, thread, turn
}

func TestTopic_RootFilledInLater(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	root, _, turn := f.topic(t)

	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: " "}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("people still have to say something: got %v", err)
	}

	filled, err := f.s.UpdateMessageBody(ctx, root.ID, "on it", turn.ID, []store.Mention{{Kind: store.MentionUser, ID: f.user.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if filled.ID != root.ID || filled.Seq != root.Seq || filled.Body != "on it" || filled.TurnID != turn.ID || len(filled.Mentions) != 1 || filled.Mentions[0].ID != f.user.ID {
		t.Errorf("filled root = %+v", filled)
	}
	got, _ := f.s.GetMessage(ctx, root.ID)
	if got.Body != "on it" || got.TurnID != turn.ID {
		t.Errorf("stored root = %+v", got)
	}
	if _, err := f.s.UpdateMessageBody(ctx, "00000000-0000-0000-0000-000000000000", "x", "", nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown message: got %v, want ErrNotFound", err)
	}
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderSystem, Body: "note", TurnID: "00000000-0000-0000-0000-000000000000"}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown turn: got %v, want ErrNotFound", err)
	}
}

func TestTopic_LastAgentMessageCountsTheRoot(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	root, thread, turn := f.topic(t)

	last, err := f.s.LastAgentMessageInThread(ctx, thread.ID)
	if err != nil || last.ID != root.ID {
		t.Fatalf("with no replies the root is the last agent message: %+v, %v", last, err)
	}
	reply, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "more", TurnID: turn.ID})
	if err != nil {
		t.Fatal(err)
	}
	if last, _ = f.s.LastAgentMessageInThread(ctx, thread.ID); last.ID != reply.ID {
		t.Errorf("a reply supersedes the root, got %+v", last)
	}
	// The user's own thread (rooted at a user message) has no agent voice.
	if _, err := f.s.LastAgentMessageInThread(ctx, f.thread.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("thread without agents: got %v, want ErrNotFound", err)
	}
}

func TestTopic_SummariesAndTurns(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	root, thread, first := f.topic(t)

	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "done", TurnID: first.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FinishTurn(ctx, first.ID, store.TurnOutcome{Status: store.TurnDone}); err != nil {
		t.Fatal(err)
	}
	second, err := f.s.CreateTurn(ctx, store.NewTurn{MemberID: f.member.ID, RoomID: f.room.ID, ThreadID: thread.ID, MachineID: f.machineID})
	if err != nil {
		t.Fatal(err)
	}

	summaries, err := f.s.ThreadSummaries(ctx, []string{root.ID, f.root.ID})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := summaries[root.ID]
	if !ok || got.ID != thread.ID || got.ReplyCount != 1 || got.LastReplyAt == nil || got.Turns != 2 {
		t.Errorf("summary = %+v", got)
	}
	if got.LastTurn == nil || got.LastTurn.ID != second.ID || got.LastTurn.Status != store.TurnRunning || got.LastTurn.EndedAt != nil {
		t.Errorf("last turn = %+v", got.LastTurn)
	}
	// The user's thread exists but has no replies and no turns.
	if plain, ok := summaries[f.root.ID]; !ok || plain.ReplyCount != 0 || plain.Turns != 0 || plain.LastTurn != nil || plain.LastReplyAt != nil {
		t.Errorf("summary of an untouched thread = %+v (present %v)", plain, ok)
	}
	if empty, err := f.s.ThreadSummaries(ctx, nil); err != nil || len(empty) != 0 {
		t.Errorf("no ids: %v, %v", empty, err)
	}

	turns, err := f.s.ListThreadTurns(ctx, thread.ID)
	if err != nil || len(turns) != 2 || turns[0].ID != first.ID || turns[1].ID != second.ID {
		t.Errorf("thread turns = %+v, %v", turns, err)
	}
}

func TestTopic_NumbersRisePerRoom(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	// The fixture's thread is the room's first topic.
	if f.thread.Number != 1 {
		t.Fatalf("first topic is #%d, want #1", f.thread.Number)
	}
	// Asking again for a thread that exists returns it and takes no number.
	for range 3 {
		again, err := f.s.ThreadForMessage(ctx, f.root.ID)
		if err != nil {
			t.Fatal(err)
		}
		if again.ID != f.thread.ID || again.Number != 1 {
			t.Fatalf("same root gave thread %s #%d, want %s #1", again.ID, again.Number, f.thread.ID)
		}
	}
	// A reply lands in the root's thread, not in one of its own.
	reply, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "more"})
	if err != nil {
		t.Fatal(err)
	}
	if of, err := f.s.ThreadForMessage(ctx, reply.ID); err != nil || of.ID != f.thread.ID {
		t.Fatalf("thread of a reply = %+v, %v", of, err)
	}

	second, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "another thing"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := f.s.ThreadForMessage(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.Number != 2 {
		t.Errorf("second topic is #%d, want #2: looking the first up again must not burn numbers", next.Number)
	}
	if got, err := f.s.GetThread(ctx, next.ID); err != nil || got.Number != 2 {
		t.Errorf("GetThread = %+v, %v", got, err)
	}

	// Numbers are per room: another project's first topic is #1 again.
	_, otherRoom, err := f.s.CreateProject(ctx, store.NewProject{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: otherRoom.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.s.ThreadForMessage(ctx, elsewhere.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Number != 1 {
		t.Errorf("another room's first topic is #%d, want #1", first.Number)
	}
}
