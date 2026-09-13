package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// turnFixture extends agentFixture with an instance, a user and a thread
// rooted at one of the user's messages.
type turnFixture struct {
	agentFixture
	instance store.AgentInstance
	user     store.User
	thread   store.Thread
	root     store.Message
}

func newTurnFixture(t *testing.T) turnFixture {
	t.Helper()
	f := newAgentFixture(t)
	ctx := context.Background()
	instance, err := f.s.CreateAgentInstance(ctx, store.NewAgentInstance{RoomID: f.room.ID, TemplateID: f.template.ID, WorkerID: f.workerID})
	if err != nil {
		t.Fatal(err)
	}
	user, err := f.s.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	root, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: user.ID, Body: "@agent go"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := f.s.ThreadForMessage(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	return turnFixture{agentFixture: f, instance: instance, user: user, thread: thread, root: root}
}

func (f turnFixture) newTurn() store.NewTurn {
	return store.NewTurn{
		AgentInstanceID:  f.instance.ID,
		RoomID:           f.room.ID,
		ThreadID:         f.thread.ID,
		TriggerMessageID: f.root.ID,
		WorkerID:         f.workerID,
	}
}

func TestTurns_CreateFinishGet(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	turn, err := f.s.CreateTurn(ctx, f.newTurn())
	if err != nil {
		t.Fatal(err)
	}
	if turn.Status != store.TurnRunning || turn.EndedAt != nil || turn.TriggerMessageID != f.root.ID {
		t.Errorf("unexpected turn: %+v", turn)
	}

	reply, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderAgent, AgentInstanceID: f.instance.ID, Body: "done"})
	if err != nil {
		t.Fatal(err)
	}
	finished, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: store.TurnDone, ReplyMessageID: reply.ID, TranscriptPath: "/tmp/t.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != store.TurnDone || finished.EndedAt == nil || finished.ReplyMessageID != reply.ID || finished.TranscriptPath != "/tmp/t.jsonl" {
		t.Errorf("unexpected finished turn: %+v", finished)
	}

	got, err := f.s.GetTurn(ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TurnDone {
		t.Errorf("GetTurn = %+v", got)
	}

	failed, err := f.s.CreateTurn(ctx, f.newTurn())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FinishTurn(ctx, failed.ID, store.TurnOutcome{Status: store.TurnFailed, Error: "boom"}); err != nil {
		t.Fatal(err)
	}
	turns, err := f.s.ListRoomTurns(ctx, f.room.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 || turns[0].ID != failed.ID || turns[0].Error != "boom" {
		t.Errorf("ListRoomTurns should be newest first: %+v", turns)
	}
}

func TestTurns_Errors(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	unknown := "00000000-0000-0000-0000-000000000000"

	bad := f.newTurn()
	bad.AgentInstanceID = unknown
	if _, err := f.s.CreateTurn(ctx, bad); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown instance: got %v, want ErrNotFound", err)
	}
	if _, err := f.s.FinishTurn(ctx, unknown, store.TurnOutcome{Status: store.TurnDone}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("finish unknown: got %v, want ErrNotFound", err)
	}
	turn, _ := f.s.CreateTurn(ctx, f.newTurn())
	if _, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: "exploded"}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("bad status: got %v, want ErrInvalidInput", err)
	}
}

func TestUpdateAgentInstanceSession(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	if err := f.s.UpdateAgentInstanceSession(ctx, f.instance.ID, "sess-42"); err != nil {
		t.Fatal(err)
	}
	inst, err := f.s.GetAgentInstance(ctx, f.instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inst.EngineSessionRef != "sess-42" {
		t.Errorf("EngineSessionRef = %q", inst.EngineSessionRef)
	}
}

func TestLastAgentMessageInThread(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	if _, err := f.s.LastAgentMessageInThread(ctx, f.thread.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("empty thread: got %v, want ErrNotFound", err)
	}

	f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderAgent, AgentInstanceID: f.instance.ID, Body: "first"})
	f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "human"})
	last, _ := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderAgent, AgentInstanceID: f.instance.ID, Body: "second"})

	got, err := f.s.LastAgentMessageInThread(ctx, f.thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != last.ID {
		t.Errorf("got %q, want the latest agent message %q", got.Body, last.Body)
	}
}
