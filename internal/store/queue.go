package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// QueuedWake is a message a member was asked while it was busy, waiting
// for its turn. It is kept in the store so a hub that stops does not lose
// it: the member is woken for it once its machine is back.
type QueuedWake struct {
	MemberID  string
	MessageID string
	// ThreadID is the topic it is answered in; empty for a message to the
	// room, whose turn opens a topic of its own.
	ThreadID string
	// AnchorID starts a piece of work of its own at this message, when a
	// person let a held wake go on (docs/design.md 5.22).
	AnchorID string
	QueuedAt time.Time
}

// QueueWake keeps what a busy member was asked until its turn starts.
func (s *Store) QueueWake(ctx context.Context, w QueuedWake) error {
	member, err := parseUUID(w.MemberID)
	if err != nil {
		return err
	}
	message, err := parseUUID(w.MessageID)
	if err != nil {
		return err
	}
	thread, err := optionalUUID(w.ThreadID)
	if err != nil {
		return err
	}
	anchor, err := optionalUUID(w.AnchorID)
	if err != nil {
		return err
	}
	if err := s.q.QueueWake(ctx, db.QueueWakeParams{MemberID: member, MessageID: message, ThreadID: thread, AnchorID: anchor}); err != nil {
		return fmt.Errorf("queue a wake: %w", err)
	}
	return nil
}

// UnqueueWakes forgets what memberID waited to be asked by messageIDs, as
// the turn that answers them starts.
func (s *Store) UnqueueWakes(ctx context.Context, memberID string, messageIDs []string) error {
	member, err := parseUUID(memberID)
	if err != nil {
		return err
	}
	ids := make([]pgtype.UUID, len(messageIDs))
	for i, id := range messageIDs {
		if ids[i], err = parseUUID(id); err != nil {
			return err
		}
	}
	if err := s.q.UnqueueWakes(ctx, db.UnqueueWakesParams{MemberID: member, MessageIds: ids}); err != nil {
		return fmt.Errorf("unqueue wakes: %w", err)
	}
	return nil
}

// ListQueuedWakes lists what the members running on machineID wait to be
// asked, in the order they were asked.
func (s *Store) ListQueuedWakes(ctx context.Context, machineID string) ([]QueuedWake, error) {
	id, err := parseUUID(machineID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListQueuedWakes(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list the wakes waiting on machine %s: %w", machineID, err)
	}
	out := make([]QueuedWake, len(rows))
	for i, row := range rows {
		out[i] = QueuedWake{
			MemberID:  uuidString(row.MemberID),
			MessageID: uuidString(row.MessageID),
			ThreadID:  uuidString(row.ThreadID),
			AnchorID:  uuidString(row.AnchorID),
			QueuedAt:  row.QueuedAt.Time,
		}
	}
	return out, nil
}
