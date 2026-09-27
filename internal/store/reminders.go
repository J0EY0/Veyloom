package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// ReminderStatus is where a reminder stands.
type ReminderStatus string

const (
	ReminderPending   ReminderStatus = "pending"
	ReminderFired     ReminderStatus = "fired"
	ReminderCancelled ReminderStatus = "cancelled"
	// ReminderDropped: its member was taken out of the project or switched
	// off by the time it came due.
	ReminderDropped ReminderStatus = "dropped"
)

// Reminder is a member's reminder to itself (docs/design.md 5.23.4): the
// hub wakes it, in the topic it set it in, once it comes due.
type Reminder struct {
	ID       string `json:"id"`
	MemberID string `json:"member_id"`
	RoomID   string `json:"room_id"`
	ThreadID string `json:"thread_id"`
	// TurnID is the turn that set it, whose piece of work the wake it
	// makes carries on; empty once that turn is gone.
	TurnID string         `json:"turn_id,omitempty"`
	Note   string         `json:"note"`
	DueAt  time.Time      `json:"due_at"`
	Status ReminderStatus `json:"status"`
	// SetMessageID is the note that told of it as it was set;
	// FiredMessageID the message it came due as.
	SetMessageID   string `json:"set_message_id,omitempty"`
	FiredMessageID string `json:"fired_message_id,omitempty"`
	// CancelledBy is the person who cancelled it; empty when the member
	// took it back, or it was not cancelled.
	CancelledBy string     `json:"cancelled_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	SettledAt   *time.Time `json:"settled_at,omitempty"`
}

// NewReminder is what CreateReminder records.
type NewReminder struct {
	MemberID, RoomID, ThreadID, TurnID string
	Note                               string
	DueAt                              time.Time
}

// CreateReminder records a reminder not yet due.
func (s *Store) CreateReminder(ctx context.Context, r NewReminder) (Reminder, error) {
	member, err := parseUUID(r.MemberID)
	if err != nil {
		return Reminder{}, err
	}
	room, err := parseUUID(r.RoomID)
	if err != nil {
		return Reminder{}, err
	}
	thread, err := parseUUID(r.ThreadID)
	if err != nil {
		return Reminder{}, err
	}
	turn, err := optionalUUID(r.TurnID)
	if err != nil {
		return Reminder{}, err
	}
	row, err := s.q.CreateReminder(ctx, db.CreateReminderParams{
		MemberID: member, RoomID: room, ThreadID: thread, TurnID: turn, Note: r.Note, DueAt: pgtype.Timestamptz{Time: r.DueAt, Valid: true},
	})
	if err != nil {
		return Reminder{}, fmt.Errorf("create a reminder: %w", err)
	}
	return reminderFromRow(row), nil
}

// SetReminderMessage records the note that told of a reminder as it was
// set.
func (s *Store) SetReminderMessage(ctx context.Context, id, messageID string) error {
	rid, mid, err := reminderLink(id, messageID)
	if err != nil {
		return err
	}
	if err := s.q.SetReminderMessage(ctx, db.SetReminderMessageParams{ID: rid, MessageID: mid}); err != nil {
		return fmt.Errorf("reminder %s: %w", id, err)
	}
	return nil
}

// SetReminderFired records the message a reminder came due as.
func (s *Store) SetReminderFired(ctx context.Context, id, messageID string) error {
	rid, mid, err := reminderLink(id, messageID)
	if err != nil {
		return err
	}
	if err := s.q.SetReminderFired(ctx, db.SetReminderFiredParams{ID: rid, MessageID: mid}); err != nil {
		return fmt.Errorf("reminder %s: %w", id, err)
	}
	return nil
}

// reminderLink parses a reminder's id and a message's.
func reminderLink(id, messageID string) (pgtype.UUID, pgtype.UUID, error) {
	rid, err := parseUUID(id)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	mid, err := parseUUID(messageID)
	return rid, mid, err
}

// GetReminder reads a reminder.
func (s *Store) GetReminder(ctx context.Context, id string) (Reminder, error) {
	rid, err := parseUUID(id)
	if err != nil {
		return Reminder{}, err
	}
	row, err := s.q.GetReminder(ctx, rid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, fmt.Errorf("reminder %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Reminder{}, fmt.Errorf("get reminder %s: %w", id, err)
	}
	return reminderFromRow(row), nil
}

// CountPendingReminders counts memberID's reminders not yet due.
func (s *Store) CountPendingReminders(ctx context.Context, memberID string) (int, error) {
	id, err := parseUUID(memberID)
	if err != nil {
		return 0, err
	}
	n, err := s.q.CountPendingReminders(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("count the reminders of member %s: %w", memberID, err)
	}
	return int(n), nil
}

// ListPendingReminders lists the reminders not yet due of the members
// machineID runs, the soonest first.
func (s *Store) ListPendingReminders(ctx context.Context, machineID string) ([]Reminder, error) {
	id, err := parseUUID(machineID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListPendingReminders(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list the reminders on machine %s: %w", machineID, err)
	}
	return remindersFromRows(rows), nil
}

// ListMemberPendingReminders lists memberID's reminders not yet due, the
// soonest first.
func (s *Store) ListMemberPendingReminders(ctx context.Context, memberID string) ([]Reminder, error) {
	id, err := parseUUID(memberID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListMemberPendingReminders(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list the reminders of member %s: %w", memberID, err)
	}
	return remindersFromRows(rows), nil
}

// ListThreadReminders lists the reminders set in a topic, in the order
// set.
func (s *Store) ListThreadReminders(ctx context.Context, threadID string) ([]Reminder, error) {
	id, err := parseUUID(threadID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListThreadReminders(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list the reminders of topic %s: %w", threadID, err)
	}
	return remindersFromRows(rows), nil
}

// FireReminder has a reminder come due, once. ErrNotFound when it is not
// pending any more: it came due or was cancelled already.
func (s *Store) FireReminder(ctx context.Context, id string) (Reminder, error) {
	rid, err := parseUUID(id)
	if err != nil {
		return Reminder{}, err
	}
	row, err := s.q.FireReminder(ctx, rid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, fmt.Errorf("reminder %s is not pending: %w", id, ErrNotFound)
	}
	if err != nil {
		return Reminder{}, fmt.Errorf("fire reminder %s: %w", id, err)
	}
	return reminderFromRow(row), nil
}

// CancelReminder takes back a reminder not yet due, by the person userID,
// or by its member when userID is empty. ErrNotFound when it is not
// pending any more.
func (s *Store) CancelReminder(ctx context.Context, id, userID string) (Reminder, error) {
	rid, err := parseUUID(id)
	if err != nil {
		return Reminder{}, err
	}
	by, err := optionalUUID(userID)
	if err != nil {
		return Reminder{}, err
	}
	row, err := s.q.CancelReminder(ctx, db.CancelReminderParams{ID: rid, CancelledBy: by})
	if errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, fmt.Errorf("reminder %s is not pending: %w", id, ErrNotFound)
	}
	if err != nil {
		return Reminder{}, fmt.Errorf("cancel reminder %s: %w", id, err)
	}
	return reminderFromRow(row), nil
}

// DropReminder settles a reminder whose member is gone or switched off as
// it comes due. ErrNotFound when it is not pending any more.
func (s *Store) DropReminder(ctx context.Context, id string) (Reminder, error) {
	rid, err := parseUUID(id)
	if err != nil {
		return Reminder{}, err
	}
	row, err := s.q.DropReminder(ctx, rid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reminder{}, fmt.Errorf("reminder %s is not pending: %w", id, ErrNotFound)
	}
	if err != nil {
		return Reminder{}, fmt.Errorf("drop reminder %s: %w", id, err)
	}
	return reminderFromRow(row), nil
}

func remindersFromRows(rows []db.Reminder) []Reminder {
	out := make([]Reminder, len(rows))
	for i, row := range rows {
		out[i] = reminderFromRow(row)
	}
	return out
}

func reminderFromRow(row db.Reminder) Reminder {
	r := Reminder{
		ID: uuidString(row.ID), MemberID: uuidString(row.MemberID), RoomID: uuidString(row.RoomID), ThreadID: uuidString(row.ThreadID),
		TurnID: uuidString(row.TurnID), Note: row.Note, DueAt: row.DueAt.Time, Status: ReminderStatus(row.Status),
		SetMessageID: uuidString(row.SetMessageID), FiredMessageID: uuidString(row.FiredMessageID), CancelledBy: uuidString(row.CancelledBy),
		CreatedAt: row.CreatedAt.Time,
	}
	if row.SettledAt.Valid {
		at := row.SettledAt.Time
		r.SettledAt = &at
	}
	return r
}
