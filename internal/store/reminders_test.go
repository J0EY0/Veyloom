package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

// A member's reminders: set, listed for the machine and the topic, come
// due once or cancelled once.
func TestReminders(t *testing.T) {
	s := storetest.New(t)
	ctx := context.Background()
	machineID, err := s.RegisterMachine(ctx, "", "laptop", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, room, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := s.CreateAgent(ctx, store.NewAgent{Name: "Coder", MachineID: machineID, Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: agent.ID, DisplayName: "Coder"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	asked, err := s.CreateMessage(ctx, store.NewMessage{RoomID: room.ID, SenderKind: store.SenderUser, UserID: user.ID, Body: "watch the build"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.ThreadForMessage(ctx, asked.ID)
	if err != nil {
		t.Fatal(err)
	}

	later := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	sooner := time.Now().Add(time.Hour).Truncate(time.Second)
	first, err := s.CreateReminder(ctx, store.NewReminder{MemberID: member.ID, RoomID: room.ID, ThreadID: thread.ID, Note: "check CI", DueAt: later})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateReminder(ctx, store.NewReminder{MemberID: member.ID, RoomID: room.ID, ThreadID: thread.ID, Note: "check again", DueAt: sooner})
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != store.ReminderPending || !first.DueAt.Equal(later) || first.TurnID != "" {
		t.Errorf("created: %+v", first)
	}
	note, err := s.CreateMessage(ctx, store.NewMessage{RoomID: room.ID, ThreadID: thread.ID, SenderKind: store.SenderSystem, Body: "set"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetReminderMessage(ctx, first.ID, note.ID); err != nil {
		t.Fatal(err)
	}

	if n, err := s.CountPendingReminders(ctx, member.ID); err != nil || n != 2 {
		t.Errorf("pending: %d %v", n, err)
	}
	pending, err := s.ListPendingReminders(ctx, machineID)
	if err != nil || len(pending) != 2 || pending[0].ID != second.ID {
		t.Errorf("the machine's, the soonest first: %+v %v", pending, err)
	}

	fired, err := s.FireReminder(ctx, second.ID)
	if err != nil || fired.Status != store.ReminderFired || fired.SettledAt == nil {
		t.Fatalf("fired: %+v %v", fired, err)
	}
	if _, err := s.FireReminder(ctx, second.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("fired twice: %v", err)
	}
	if err := s.SetReminderFired(ctx, second.ID, note.ID); err != nil {
		t.Fatal(err)
	}
	// The one account lives outside users (00011): whoever cancels needs
	// no row there.
	account := store.NewID()
	cancelled, err := s.CancelReminder(ctx, first.ID, account)
	if err != nil || cancelled.Status != store.ReminderCancelled || cancelled.CancelledBy != account {
		t.Fatalf("cancelled: %+v %v", cancelled, err)
	}
	if _, err := s.CancelReminder(ctx, second.ID, ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a reminder come due is not cancelled: %v", err)
	}

	all, err := s.ListThreadReminders(ctx, thread.ID)
	if err != nil || len(all) != 2 || all[0].SetMessageID != note.ID || all[1].FiredMessageID != note.ID {
		t.Errorf("the topic's, in the order set: %+v %v", all, err)
	}
	if n, _ := s.CountPendingReminders(ctx, member.ID); n != 0 {
		t.Errorf("none pending: %d", n)
	}
	if got, err := s.GetReminder(ctx, first.ID); err != nil || got.Status != store.ReminderCancelled {
		t.Errorf("get: %+v %v", got, err)
	}

	// One whose member is gone as it comes due is dropped, once.
	third, err := s.CreateReminder(ctx, store.NewReminder{MemberID: member.ID, RoomID: room.ID, ThreadID: thread.ID, Note: "gone", DueAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if dropped, err := s.DropReminder(ctx, third.ID); err != nil || dropped.Status != store.ReminderDropped || dropped.SettledAt == nil {
		t.Errorf("dropped: %+v %v", dropped, err)
	}
	if _, err := s.DropReminder(ctx, third.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("dropped twice: %v", err)
	}
}
