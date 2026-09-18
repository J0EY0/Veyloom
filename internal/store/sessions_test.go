package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

func (f turnFixture) newSession() store.NewMemberSession {
	return store.NewMemberSession{
		MemberID:  f.member.ID,
		Runtime:   "fake",
		MachineID: f.machineID,
		WorkDir:   "/repo",
	}
}

func TestSessions_StartGetSetRef(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	if _, err := f.s.GetOpenSession(ctx, f.member.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("open session of a new member: got %v, want ErrNotFound", err)
	}

	sess, err := f.s.StartSession(ctx, f.newSession())
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID == "" || sess.MemberID != f.member.ID || sess.Runtime != "fake" || sess.MachineID != f.machineID || sess.WorkDir != "/repo" {
		t.Errorf("started session = %+v", sess)
	}
	if sess.Started() || sess.EndedAt != nil || sess.EndReason != "" {
		t.Errorf("a fresh session is neither started nor ended: %+v", sess)
	}

	if err := f.s.SetSessionRef(ctx, sess.ID, "ref-1"); err != nil {
		t.Fatal(err)
	}
	open, err := f.s.GetOpenSession(ctx, f.member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if open.ID != sess.ID || open.Ref != "ref-1" || !open.Started() {
		t.Errorf("open session = %+v", open)
	}
	got, err := f.s.GetSession(ctx, sess.ID)
	if err != nil || got.Ref != "ref-1" {
		t.Errorf("GetSession = %+v, %v", got, err)
	}

	if err := f.s.SetSessionRef(ctx, store.NewID(), "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("set ref of an unknown session: got %v, want ErrNotFound", err)
	}
	if _, err := f.s.GetSession(ctx, store.NewID()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("get unknown session: got %v, want ErrNotFound", err)
	}
}

func TestSessions_CallerPicksTheID(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	n := f.newSession()
	n.ID, n.Ref = store.NewID(), "reported-early"
	sess, err := f.s.StartSession(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ID != n.ID || sess.Ref != "reported-early" {
		t.Errorf("session = %+v, want id %s and the ref", sess, n.ID)
	}
}

func TestSessions_OneOpenPerMember(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	first, err := f.s.StartSession(ctx, f.newSession())
	if err != nil {
		t.Fatal(err)
	}
	// Without saying what happens to the open one, a second is refused
	// and the first stays as it was.
	if _, err := f.s.StartSession(ctx, f.newSession()); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("second open session: got %v, want ErrConflict", err)
	}
	if open, err := f.s.GetOpenSession(ctx, f.member.ID); err != nil || open.ID != first.ID {
		t.Fatalf("open session after the refusal = %+v, %v", open, err)
	}

	n := f.newSession()
	n.WorkDir, n.Replaces = "/moved", store.SessionDirChanged
	second, err := f.s.StartSession(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	open, err := f.s.GetOpenSession(ctx, f.member.ID)
	if err != nil || open.ID != second.ID || open.WorkDir != "/moved" {
		t.Fatalf("open session = %+v, %v; want the second", open, err)
	}
	old, err := f.s.GetSession(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.EndedAt == nil || old.EndReason != store.SessionDirChanged {
		t.Errorf("replaced session = %+v, want ended as dir_changed", old)
	}

	all, err := f.s.ListMemberSessions(ctx, f.member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("sessions of the member = %d, want 2", len(all))
	}
}

func TestSessions_EndOpen(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	// Nothing open: nothing to end, and no complaint.
	if err := f.s.EndOpenSession(ctx, f.member.ID, store.SessionManual); err != nil {
		t.Fatal(err)
	}
	sess, err := f.s.StartSession(ctx, f.newSession())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.EndOpenSession(ctx, f.member.ID, "because"); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("unknown reason: got %v, want ErrInvalidInput", err)
	}
	if err := f.s.EndOpenSession(ctx, f.member.ID, store.SessionManual); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GetOpenSession(ctx, f.member.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("open session after ending: got %v, want ErrNotFound", err)
	}
	ended, _ := f.s.GetSession(ctx, sess.ID)
	if ended.EndedAt == nil || ended.EndReason != store.SessionManual {
		t.Errorf("ended session = %+v", ended)
	}
	// The member can start over.
	if _, err := f.s.StartSession(ctx, f.newSession()); err != nil {
		t.Errorf("session after an ended one: %v", err)
	}
}

func TestSessions_RemovingTheMemberEndsIt(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	sess, err := f.s.StartSession(ctx, f.newSession())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RemoveMember(ctx, f.member.ID); err != nil {
		t.Fatal(err)
	}
	ended, err := f.s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ended.EndedAt == nil || ended.EndReason != store.SessionMemberRemoved {
		t.Errorf("session of a removed member = %+v, want ended as member_removed", ended)
	}
}

func TestSessions_RemovalRefusedLeavesTheSessionOpen(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	sess, err := f.s.StartSession(ctx, f.newSession())
	if err != nil {
		t.Fatal(err)
	}
	turn := f.newTurn()
	turn.SessionID = sess.ID
	if _, err := f.s.CreateTurn(ctx, turn); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RemoveMember(ctx, f.member.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("remove a member with a running turn: got %v, want ErrConflict", err)
	}
	if open, err := f.s.GetOpenSession(ctx, f.member.ID); err != nil || open.ID != sess.ID {
		t.Errorf("open session = %+v, %v; want it untouched", open, err)
	}
}

func TestTurns_RememberTheirSession(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	sess, err := f.s.StartSession(ctx, f.newSession())
	if err != nil {
		t.Fatal(err)
	}
	n := f.newTurn()
	n.SessionID = sess.ID
	turn, err := f.s.CreateTurn(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	if turn.SessionID != sess.ID {
		t.Errorf("SessionID = %q, want %q", turn.SessionID, sess.ID)
	}
	got, err := f.s.GetTurn(ctx, turn.ID)
	if err != nil || got.SessionID != sess.ID {
		t.Errorf("GetTurn = %+v, %v", got, err)
	}

	// A turn that never got a session has none.
	bare, err := f.s.CreateTurn(ctx, f.newTurn())
	if err != nil {
		t.Fatal(err)
	}
	if bare.SessionID != "" {
		t.Errorf("SessionID of a turn without one = %q", bare.SessionID)
	}
}

func TestTurns_MoveToAnotherSession(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	first, err := f.s.StartSession(ctx, f.newSession())
	if err != nil {
		t.Fatal(err)
	}
	n := f.newTurn()
	n.SessionID = first.ID
	turn, err := f.s.CreateTurn(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	replacement := f.newSession()
	replacement.Replaces = store.SessionNotFound
	second, err := f.s.StartSession(ctx, replacement)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetTurnSession(ctx, turn.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.s.GetTurn(ctx, turn.ID); got.SessionID != second.ID {
		t.Errorf("SessionID = %q, want the new session %q", got.SessionID, second.ID)
	}
	if err := f.s.SetTurnSession(ctx, store.NewID(), second.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown turn: got %v, want ErrNotFound", err)
	}
	if err := f.s.SetTurnSession(ctx, turn.ID, store.NewID()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown session: got %v, want ErrNotFound", err)
	}
}

func TestSessions_ResetOnRequest(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	// Nothing open: nothing to do, and no complaint.
	if err := f.s.ResetSession(ctx, f.member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.LatestSession(ctx, f.member.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("latest session of a member that never had one: got %v, want ErrNotFound", err)
	}

	sess, err := f.s.StartSession(ctx, f.newSession())
	if err != nil {
		t.Fatal(err)
	}
	n := f.newTurn()
	n.SessionID = sess.ID
	turn, err := f.s.CreateTurn(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.s.SessionTurns(ctx, sess.ID); err != nil || got != 1 {
		t.Errorf("SessionTurns = %d, %v; want 1", got, err)
	}

	// Not from under a running turn: it would go on in a session that is
	// no longer the member's.
	if err := f.s.ResetSession(ctx, f.member.ID); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("reset while a turn runs: got %v, want ErrConflict", err)
	}
	if open, err := f.s.GetOpenSession(ctx, f.member.ID); err != nil || open.ID != sess.ID {
		t.Fatalf("the refused reset must leave the session: %+v, %v", open, err)
	}

	if _, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: store.TurnDone}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.ResetSession(ctx, f.member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GetOpenSession(ctx, f.member.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("open session after the reset: got %v, want ErrNotFound", err)
	}
	last, err := f.s.LatestSession(ctx, f.member.ID)
	if err != nil || last.ID != sess.ID || last.EndReason != store.SessionManual {
		t.Errorf("latest session = %+v, %v; want the one just ended, as manual", last, err)
	}
}

func TestTopic_SummariesCarryTheNumber(t *testing.T) {
	f := newTurnFixture(t)
	summaries, err := f.s.ThreadSummaries(context.Background(), []string{f.root.ID})
	if err != nil {
		t.Fatal(err)
	}
	if got := summaries[f.root.ID]; got.ID != f.thread.ID || got.Number != 1 {
		t.Errorf("summary = %+v, want topic #1", got)
	}
}
