package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// A piece of work: the turns agents woke in it, whether the latest did
// work, and a wake held back that a person lets go on once (docs/design.md
// 5.22).
func TestRelays_PieceOfWork(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	chain := f.root.ID
	start := func(wokenBy string) store.Turn {
		t.Helper()
		nt := f.newTurn()
		nt.ChainMessageID, nt.WokenByTurnID = chain, wokenBy
		turn, err := f.s.CreateTurn(ctx, nt)
		if err != nil {
			t.Fatal(err)
		}
		return turn
	}
	finish := func(turn store.Turn, worked bool) {
		t.Helper()
		if _, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: store.TurnDone, Worked: worked}); err != nil {
			t.Fatal(err)
		}
	}

	asked := start("")
	finish(asked, true)
	if woken, worked, err := f.s.ChainWakes(ctx, chain, 3); err != nil || woken != 0 || len(worked) != 0 {
		t.Errorf("a person's turn only: %d %v %v", woken, worked, err)
	}
	one := start(asked.ID)
	finish(one, true)
	two := start(one.ID)
	finish(two, false)
	running := start(two.ID)
	woken, worked, err := f.s.ChainWakes(ctx, chain, 3)
	if err != nil || woken != 3 || !slices.Equal(worked, []bool{false, true}) {
		t.Errorf("three woken, one under way: %d %v %v", woken, worked, err)
	}
	if running.ChainMessageID != chain || running.WokenByTurnID != two.ID || running.Worked {
		t.Errorf("the turn under way: %+v", running)
	}

	// A wake held back, let go on once.
	note, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderSystem, Body: "held"})
	if err != nil {
		t.Fatal(err)
	}
	hold := store.RelayHold{MessageID: note.ID, MemberID: f.member.ID, ThreadID: f.thread.ID, TriggerMessageID: f.root.ID, Reason: store.HoldIdle}
	if err := f.s.CreateRelayHold(ctx, hold); err != nil {
		t.Fatal(err)
	}
	if got, err := f.s.GetRelayHold(ctx, note.ID); err != nil || got.Reason != store.HoldIdle || got.ContinuedAt != nil || got.TriggerMessageID != f.root.ID {
		t.Errorf("the held wake: %+v %v", got, err)
	}
	if got, err := f.s.ContinueRelayHold(ctx, note.ID); err != nil || got.ContinuedAt == nil {
		t.Errorf("let go on: %+v %v", got, err)
	}
	if _, err := f.s.ContinueRelayHold(ctx, note.ID); !errors.Is(err, store.ErrConflict) {
		t.Errorf("let go on twice: %v", err)
	}
	if _, err := f.s.ContinueRelayHold(ctx, f.root.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("no wake held there: %v", err)
	}

	// The project's relay limit: 30 at first, set, kept in bounds.
	if p, _ := f.s.GetProject(ctx, f.room.ProjectID); p.RelayLimit != 30 {
		t.Errorf("a new project's relay limit: %d", p.RelayLimit)
	}
	for set, want := range map[int]int{0: 0, 12: 12, -5: -1} {
		p, err := f.s.UpdateProject(ctx, f.room.ProjectID, store.ProjectPatch{RelayLimit: &set})
		if err != nil || p.RelayLimit != want {
			t.Errorf("relay limit %d: %d %v", set, p.RelayLimit, err)
		}
	}
}
