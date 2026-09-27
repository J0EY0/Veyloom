package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// PauseReason says why a pause keeps turns from starting.
type PauseReason string

const (
	// An account's: signed out or its credentials refused; its usage
	// limit or balance used up; too many requests; its provider failing.
	PauseAuth      PauseReason = "auth"
	PauseQuota     PauseReason = "quota"
	PauseRateLimit PauseReason = "rate_limit"
	PauseServer    PauseReason = "server"
	// A member's: its turns keep failing.
	PauseFailing PauseReason = "failing"
)

// Pause keeps turns from starting for a while (docs/design.md 5.23.3):
// those of every member running on a machine with a runtime whose account
// cannot take turns now, or those of one member whose turns keep failing.
// What they are asked meanwhile waits, and goes on once it is lifted.
type Pause struct {
	ID string `json:"id"`
	// MachineID and Runtime name an account's pause; MemberID a member's.
	MachineID string      `json:"machine_id,omitempty"`
	Runtime   string      `json:"runtime,omitempty"`
	MemberID  string      `json:"member_id,omitempty"`
	Reason    PauseReason `json:"reason"`
	// Detail is what the runtime said, the last time it failed.
	Detail string `json:"detail"`
	// EndsAt is when it runs out; nil waits for a person to lift it.
	EndsAt    *time.Time `json:"ends_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

// Account reports whether the pause is an account's, not a member's.
func (p Pause) Account() bool { return p.MemberID == "" }

// PauseAccount pauses runtime's account on machineID, or updates why and
// until when; a pause in effect keeps the time it began.
func (s *Store) PauseAccount(ctx context.Context, machineID, runtime string, reason PauseReason, detail string, endsAt *time.Time) (Pause, error) {
	mid, err := parseUUID(machineID)
	if err != nil {
		return Pause{}, err
	}
	row, err := s.q.PauseAccount(ctx, db.PauseAccountParams{MachineID: mid, Runtime: runtime, Reason: string(reason), Detail: detail, EndsAt: timestamp(endsAt)})
	if err != nil {
		return Pause{}, fmt.Errorf("pause %s on machine %s: %w", runtime, machineID, err)
	}
	return pauseFromRow(row), nil
}

// PauseMember pauses memberID, or updates why and until when.
func (s *Store) PauseMember(ctx context.Context, memberID string, reason PauseReason, detail string, endsAt *time.Time) (Pause, error) {
	id, err := parseUUID(memberID)
	if err != nil {
		return Pause{}, err
	}
	row, err := s.q.PauseMember(ctx, db.PauseMemberParams{MemberID: id, Reason: string(reason), Detail: detail, EndsAt: timestamp(endsAt)})
	if err != nil {
		return Pause{}, fmt.Errorf("pause member %s: %w", memberID, err)
	}
	return pauseFromRow(row), nil
}

// ListPauses returns the pauses in effect, the oldest first.
func (s *Store) ListPauses(ctx context.Context) ([]Pause, error) {
	rows, err := s.q.ListPauses(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pauses: %w", err)
	}
	out := make([]Pause, len(rows))
	for i, row := range rows {
		out[i] = pauseFromRow(row)
	}
	return out, nil
}

// LiftAccountPause lifts the pause of runtime's account on machineID; it
// reports whether there was one.
func (s *Store) LiftAccountPause(ctx context.Context, machineID, runtime string) (bool, error) {
	mid, err := parseUUID(machineID)
	if err != nil {
		return false, err
	}
	n, err := s.q.LiftAccountPause(ctx, db.LiftAccountPauseParams{MachineID: mid, Runtime: runtime})
	if err != nil {
		return false, fmt.Errorf("lift the pause of %s on machine %s: %w", runtime, machineID, err)
	}
	return n > 0, nil
}

// LiftMemberPause lifts memberID's pause; it reports whether there was one.
func (s *Store) LiftMemberPause(ctx context.Context, memberID string) (bool, error) {
	id, err := parseUUID(memberID)
	if err != nil {
		return false, err
	}
	n, err := s.q.LiftMemberPause(ctx, id)
	if err != nil {
		return false, fmt.Errorf("lift the pause of member %s: %w", memberID, err)
	}
	return n > 0, nil
}

func pauseFromRow(row db.Pause) Pause {
	p := Pause{
		ID: uuidString(row.ID), MachineID: uuidString(row.MachineID), Runtime: row.Runtime, MemberID: uuidString(row.MemberID),
		Reason: PauseReason(row.Reason), Detail: row.Detail, CreatedAt: row.CreatedAt.Time,
	}
	if row.EndsAt.Valid {
		ends := row.EndsAt.Time
		p.EndsAt = &ends
	}
	return p
}

// timestamp is t for a nullable column.
func timestamp(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}
