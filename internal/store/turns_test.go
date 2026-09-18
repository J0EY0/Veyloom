package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// turnFixture extends agentFixture with a member, a user and a thread
// rooted at one of the user's messages.
type turnFixture struct {
	agentFixture
	member store.Member
	user   store.User
	thread store.Thread
	root   store.Message
}

func newTurnFixture(t *testing.T) turnFixture {
	t.Helper()
	f := newAgentFixture(t)
	ctx := context.Background()
	member, err := f.s.CreateMember(ctx, store.NewMember{RoomID: f.room.ID, AgentID: f.agent.ID})
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
	return turnFixture{agentFixture: f, member: member, user: user, thread: thread, root: root}
}

func (f turnFixture) newTurn() store.NewTurn {
	return store.NewTurn{
		MemberID:         f.member.ID,
		RoomID:           f.room.ID,
		ThreadID:         f.thread.ID,
		TriggerMessageID: f.root.ID,
		MachineID:        f.machineID,
		Runtime:          "fake",
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

	reply, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if turn.Runtime != "fake" {
		t.Errorf("runtime = %q, want the one it runs on", turn.Runtime)
	}
	if turn.Usage != (runtime.Usage{}) {
		t.Errorf("a running turn has spent nothing yet: %+v", turn.Usage)
	}
	usage := runtime.Usage{InputTokens: 120, CacheReadTokens: 800, CacheWriteTokens: 40, OutputTokens: 60}
	finished, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: store.TurnDone, ReplyMessageID: reply.ID, TranscriptPath: "/tmp/t.jsonl", Usage: usage})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != store.TurnDone || finished.EndedAt == nil || finished.ReplyMessageID != reply.ID || finished.TranscriptPath != "/tmp/t.jsonl" || finished.Usage != usage {
		t.Errorf("unexpected finished turn: %+v", finished)
	}

	got, err := f.s.GetTurn(ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.TurnDone || got.Usage != usage {
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
	bad.MemberID = unknown
	if _, err := f.s.CreateTurn(ctx, bad); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown member: got %v, want ErrNotFound", err)
	}
	if _, err := f.s.FinishTurn(ctx, unknown, store.TurnOutcome{Status: store.TurnDone}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("finish unknown: got %v, want ErrNotFound", err)
	}
	turn, _ := f.s.CreateTurn(ctx, f.newTurn())
	if _, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: "exploded"}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("bad status: got %v, want ErrInvalidInput", err)
	}
}

func TestLastAgentMessageInThread(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	if _, err := f.s.LastAgentMessageInThread(ctx, f.thread.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("empty thread: got %v, want ErrNotFound", err)
	}

	f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "first"})
	f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "human"})
	last, _ := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "second"})

	got, err := f.s.LastAgentMessageInThread(ctx, f.thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != last.ID {
		t.Errorf("got %q, want the latest agent message %q", got.Body, last.Body)
	}
}

func TestFailRunningTurns_CutsOffWhatTheLastStopLeft(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	running, err := f.s.CreateTurn(ctx, store.NewTurn{MemberID: f.member.ID, RoomID: f.room.ID, ThreadID: f.thread.ID, TriggerMessageID: f.root.ID, MachineID: f.machineID})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := f.s.CreateApproval(ctx, store.NewApproval{TurnID: running.ID, RoomID: f.room.ID, ThreadID: f.thread.ID, MemberID: f.member.ID, RequestID: "q1", Tool: "Bash", Input: `{"command":"make"}`})
	if err != nil {
		t.Fatal(err)
	}
	done, err := f.s.CreateTurn(ctx, store.NewTurn{MemberID: f.member.ID, RoomID: f.room.ID, ThreadID: f.thread.ID, TriggerMessageID: f.root.ID, MachineID: f.machineID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FinishTurn(ctx, done.ID, store.TurnOutcome{Status: store.TurnDone}); err != nil {
		t.Fatal(err)
	}

	cut, err := f.s.FailRunningTurns(ctx, "stopped")
	if err != nil {
		t.Fatal(err)
	}
	if len(cut) != 1 || cut[0].ID != running.ID || cut[0].Status != store.TurnFailed || cut[0].Error != "stopped" || cut[0].EndedAt == nil {
		t.Errorf("unexpected cut-off turns: %+v", cut)
	}
	if got, _ := f.s.GetTurn(ctx, done.ID); got.Status != store.TurnDone {
		t.Errorf("a finished turn should be left alone, got %+v", got)
	}
	if got, _ := f.s.GetApproval(ctx, approval.ID); got.Status != store.ApprovalCancelled {
		t.Errorf("the pending approval should be cancelled, got %+v", got)
	}
	if again, _ := f.s.FailRunningTurns(ctx, "stopped"); len(again) != 0 {
		t.Errorf("nothing should be left to cut off, got %d", len(again))
	}
}
