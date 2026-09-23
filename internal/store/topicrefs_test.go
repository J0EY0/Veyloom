package store_test

import (
	"context"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestTopicRefs(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	turn, err := f.s.CreateTurn(ctx, f.newTurn())
	if err != nil {
		t.Fatal(err)
	}
	// By the turns that ran in them; a turn not there is left out.
	refs, err := f.s.ListTurnTopics(ctx, []string{turn.ID, store.NewID()})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("refs %+v", refs)
	}
	if r := refs[0]; r.TurnID != turn.ID || r.ThreadID != f.thread.ID || r.RoomID != f.room.ID || r.Number != f.thread.Number || r.RootBody != "@agent go" {
		t.Errorf("the topic a turn ran in: %+v", r)
	}
	// By their numbers; a number with no topic is left out.
	refs, err = f.s.ListTopicsByNumber(ctx, f.room.ID, []int{f.thread.Number, f.thread.Number + 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].ThreadID != f.thread.ID || refs[0].TurnID != "" || refs[0].RootBody != "@agent go" {
		t.Errorf("the topic by number: %+v", refs)
	}
	if refs, err := f.s.ListTurnTopics(ctx, nil); err != nil || refs != nil {
		t.Errorf("nothing asked: %+v %v", refs, err)
	}
	if refs, err := f.s.ListTurnTopics(ctx, []string{"not an id", turn.ID}); err != nil || len(refs) != 1 {
		t.Errorf("what is no id names no turn: %+v %v", refs, err)
	}
}
