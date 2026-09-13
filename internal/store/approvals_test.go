package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// approvalFixture extends turnFixture with a running turn.
type approvalFixture struct {
	turnFixture
	turn store.Turn
}

func newApprovalFixture(t *testing.T) approvalFixture {
	t.Helper()
	f := newTurnFixture(t)
	turn, err := f.s.CreateTurn(context.Background(), f.newTurn())
	if err != nil {
		t.Fatal(err)
	}
	return approvalFixture{turnFixture: f, turn: turn}
}

func (f approvalFixture) newApproval(requestID string) store.NewApproval {
	return store.NewApproval{
		TurnID:          f.turn.ID,
		RoomID:          f.room.ID,
		ThreadID:        f.thread.ID,
		AgentInstanceID: f.instance.ID,
		RequestID:       requestID,
		Tool:            "Bash",
		Input:           `{"command":"make test"}`,
	}
}

func TestApprovals_CreateDecideGet(t *testing.T) {
	f := newApprovalFixture(t)
	ctx := context.Background()

	note, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderSystem, Body: "wants to run make test"})
	if err != nil {
		t.Fatal(err)
	}
	in := f.newApproval("r1")
	in.MessageID = note.ID
	a, err := f.s.CreateApproval(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != store.ApprovalPending || a.Kind != store.ApprovalToolUse || a.Tool != "Bash" || string(a.Input) != `{"command":"make test"}` {
		t.Errorf("unexpected approval: %+v", a)
	}
	if a.MessageID != note.ID || a.TurnID != f.turn.ID || a.AgentInstanceID != f.instance.ID || a.DecidedAt != nil || a.DecidedBy != "" {
		t.Errorf("unexpected approval: %+v", a)
	}

	decided, err := f.s.DecideApproval(ctx, a.ID, store.ApprovalOutcome{Status: store.ApprovalAllowed, Message: "go ahead", DecidedBy: f.user.ID})
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != store.ApprovalAllowed || decided.Message != "go ahead" || decided.DecidedBy != f.user.ID || decided.DecidedAt == nil {
		t.Errorf("unexpected decided approval: %+v", decided)
	}

	got, err := f.s.GetApproval(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.ApprovalAllowed || got.DecidedBy != f.user.ID {
		t.Errorf("GetApproval = %+v", got)
	}

	// The first decision wins.
	_, err = f.s.DecideApproval(ctx, a.ID, store.ApprovalOutcome{Status: store.ApprovalDenied, DecidedBy: f.user.ID})
	if !errors.Is(err, store.ErrConflict) {
		t.Errorf("second decision: got %v, want ErrConflict", err)
	}
	// A system decision has no user.
	b, _ := f.s.CreateApproval(ctx, f.newApproval("r2"))
	expired, err := f.s.DecideApproval(ctx, b.ID, store.ApprovalOutcome{Status: store.ApprovalExpired, Message: "no answer in time"})
	if err != nil {
		t.Fatal(err)
	}
	if expired.Status != store.ApprovalExpired || expired.DecidedBy != "" || expired.DecidedAt == nil {
		t.Errorf("unexpected expired approval: %+v", expired)
	}
}

func TestApprovals_Errors(t *testing.T) {
	f := newApprovalFixture(t)
	ctx := context.Background()

	if _, err := f.s.GetApproval(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown id: got %v, want ErrNotFound", err)
	}
	if _, err := f.s.GetApproval(ctx, "nope"); !errors.Is(err, store.ErrInvalidID) {
		t.Errorf("bad id: got %v, want ErrInvalidID", err)
	}
	if _, err := f.s.DecideApproval(ctx, "00000000-0000-0000-0000-000000000000", store.ApprovalOutcome{Status: store.ApprovalAllowed}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("decide unknown: got %v, want ErrNotFound", err)
	}

	a, err := f.s.CreateApproval(ctx, f.newApproval("r1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DecideApproval(ctx, a.ID, store.ApprovalOutcome{Status: store.ApprovalPending}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("decide to pending: got %v, want ErrInvalidInput", err)
	}
	if _, err := f.s.DecideApproval(ctx, a.ID, store.ApprovalOutcome{Status: "maybe"}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("unknown status: got %v, want ErrInvalidInput", err)
	}
	if _, err := f.s.CreateApproval(ctx, f.newApproval("r1")); !errors.Is(err, store.ErrConflict) {
		t.Errorf("duplicate request id: got %v, want ErrConflict", err)
	}
	blank := f.newApproval("")
	if _, err := f.s.CreateApproval(ctx, blank); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("blank request id: got %v, want ErrInvalidInput", err)
	}
	orphan := f.newApproval("r3")
	orphan.TurnID = "00000000-0000-0000-0000-000000000000"
	if _, err := f.s.CreateApproval(ctx, orphan); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown turn: got %v, want ErrNotFound", err)
	}
}

func TestApprovals_CallerSuppliedID(t *testing.T) {
	f := newApprovalFixture(t)
	in := f.newApproval("r1")
	in.ID = store.NewID()
	a, err := f.s.CreateApproval(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != in.ID {
		t.Errorf("ID = %s, want the supplied %s", a.ID, in.ID)
	}
	if _, err := f.s.CreateApproval(context.Background(), in); !errors.Is(err, store.ErrConflict) {
		t.Errorf("reusing an id: got %v, want ErrConflict", err)
	}
	bad := f.newApproval("r2")
	bad.ID = "nope"
	if _, err := f.s.CreateApproval(context.Background(), bad); !errors.Is(err, store.ErrInvalidID) {
		t.Errorf("bad id: got %v, want ErrInvalidID", err)
	}
}

func TestApprovals_NonJSONInputIsStoredAsString(t *testing.T) {
	f := newApprovalFixture(t)
	in := f.newApproval("r1")
	in.Input = "not json"
	a, err := f.s.CreateApproval(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if string(a.Input) != `"not json"` {
		t.Errorf("Input = %s, want a JSON string", a.Input)
	}
}

func TestApprovals_ListAndResolve(t *testing.T) {
	f := newApprovalFixture(t)
	ctx := context.Background()

	first, _ := f.s.CreateApproval(ctx, f.newApproval("r1"))
	second, _ := f.s.CreateApproval(ctx, f.newApproval("r2"))
	if _, err := f.s.DecideApproval(ctx, first.ID, store.ApprovalOutcome{Status: store.ApprovalDenied, DecidedBy: f.user.ID}); err != nil {
		t.Fatal(err)
	}

	pending, err := f.s.ListPendingRoomApprovals(ctx, f.room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != second.ID {
		t.Errorf("pending = %+v, want only the second", pending)
	}

	resolved, err := f.s.ResolveTurnApprovals(ctx, f.turn.ID, store.ApprovalCancelled, "turn ended")
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].ID != second.ID || resolved[0].Status != store.ApprovalCancelled || resolved[0].Message != "turn ended" {
		t.Errorf("resolved = %+v", resolved)
	}
	if again, _ := f.s.ResolveTurnApprovals(ctx, f.turn.ID, store.ApprovalCancelled, ""); len(again) != 0 {
		t.Errorf("nothing should be left to resolve, got %+v", again)
	}

	all, err := f.s.ListTurnApprovals(ctx, f.turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].ID != first.ID || all[0].Status != store.ApprovalDenied || all[1].Status != store.ApprovalCancelled {
		t.Errorf("turn approvals = %+v", all)
	}
	if pending, _ := f.s.ListPendingRoomApprovals(ctx, f.room.ID); len(pending) != 0 {
		t.Errorf("nothing should be pending, got %+v", pending)
	}
}
