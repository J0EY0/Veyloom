package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// HoldReason says which limit on agents waking one another held a wake
// back (docs/design.md 5.22).
type HoldReason string

const (
	// HoldIdle: the last turns agents woke in the piece of work did no
	// work, only talked.
	HoldIdle HoldReason = "idle"
	// HoldLimit: the piece of work reached the project's relay limit.
	HoldLimit HoldReason = "limit"
)

// RelayHold is a wake a limit held back: the note that told the person,
// who was not woken, where and by what message.
type RelayHold struct {
	MessageID        string     `json:"message_id"`
	MemberID         string     `json:"member_id"`
	ThreadID         string     `json:"thread_id"`
	TriggerMessageID string     `json:"trigger_message_id"`
	Reason           HoldReason `json:"reason"`
	CreatedAt        time.Time  `json:"created_at"`
	// ContinuedAt is when a person let it go on; nil until then.
	ContinuedAt *time.Time `json:"continued_at,omitempty"`
}

// CreateRelayHold records a wake held back, under its note.
func (s *Store) CreateRelayHold(ctx context.Context, h RelayHold) error {
	message, err := parseUUID(h.MessageID)
	if err != nil {
		return err
	}
	member, err := parseUUID(h.MemberID)
	if err != nil {
		return err
	}
	thread, err := parseUUID(h.ThreadID)
	if err != nil {
		return err
	}
	trigger, err := parseUUID(h.TriggerMessageID)
	if err != nil {
		return err
	}
	if err := s.q.CreateRelayHold(ctx, db.CreateRelayHoldParams{MessageID: message, MemberID: member, ThreadID: thread, TriggerMessageID: trigger, Reason: string(h.Reason)}); err != nil {
		return fmt.Errorf("hold a wake: %w", err)
	}
	return nil
}

// GetRelayHold is the wake held back under the note messageID.
func (s *Store) GetRelayHold(ctx context.Context, messageID string) (RelayHold, error) {
	id, err := parseUUID(messageID)
	if err != nil {
		return RelayHold{}, err
	}
	row, err := s.q.GetRelayHold(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return RelayHold{}, ErrNotFound
	}
	if err != nil {
		return RelayHold{}, fmt.Errorf("get the wake held under %s: %w", messageID, err)
	}
	return toRelayHold(row), nil
}

// ContinueRelayHold records that a person let the wake held under the note
// messageID go on, and gives it back; a wake already let go on is a
// conflict.
func (s *Store) ContinueRelayHold(ctx context.Context, messageID string) (RelayHold, error) {
	id, err := parseUUID(messageID)
	if err != nil {
		return RelayHold{}, err
	}
	row, err := s.q.ContinueRelayHold(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := s.GetRelayHold(ctx, messageID); err != nil {
			return RelayHold{}, err
		}
		return RelayHold{}, Conflicting("relayContinued", nil, "this wake was let go on already")
	}
	if err != nil {
		return RelayHold{}, fmt.Errorf("let the wake held under %s go on: %w", messageID, err)
	}
	return toRelayHold(row), nil
}

func toRelayHold(row db.RelayHold) RelayHold {
	h := RelayHold{
		MessageID:        uuidString(row.MessageID),
		MemberID:         uuidString(row.MemberID),
		ThreadID:         uuidString(row.ThreadID),
		TriggerMessageID: uuidString(row.TriggerMessageID),
		Reason:           HoldReason(row.Reason),
		CreatedAt:        row.CreatedAt.Time,
	}
	if row.ContinuedAt.Valid {
		at := row.ContinuedAt.Time
		h.ContinuedAt = &at
	}
	return h
}

// ListThreadRelayHolds lists the wakes held back that notes in a topic
// tell of, oldest first.
func (s *Store) ListThreadRelayHolds(ctx context.Context, threadID string) ([]RelayHold, error) {
	id, err := parseUUID(threadID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListThreadRelayHolds(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list the held wakes of topic %s: %w", threadID, err)
	}
	out := make([]RelayHold, len(rows))
	for i, row := range rows {
		out[i] = toRelayHold(row)
	}
	return out, nil
}
