package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// BranchEventKind says what became of a member's branch.
type BranchEventKind string

const (
	// BranchMerged: its work went on the main line in a commit.
	BranchMerged BranchEventKind = "merged"
	// BranchReset: it was reset to the main line, its work archived under
	// a ref first.
	BranchReset BranchEventKind = "reset"
)

// BranchEvent is what became of a member's branch at one moment
// (docs/design.md 5.21): the task board and a piece of work's page tell
// from these what came of the work a member did on it (docs/webui.md 4.20).
type BranchEvent struct {
	ID       string          `json:"id"`
	MemberID string          `json:"member_id"`
	Kind     BranchEventKind `json:"kind"`
	// Commit is the merge's commit on the main line; Ref where a reset
	// archived the branch's work.
	Commit string `json:"commit,omitempty"`
	Ref    string `json:"ref,omitempty"`
	// ViaMemberID is the member whose branch was merged, when it was not
	// this one's but held its work.
	ViaMemberID string    `json:"via_member_id,omitempty"`
	At          time.Time `json:"at"`
}

// RecordBranchEvent records what became of a member's branch.
func (s *Store) RecordBranchEvent(ctx context.Context, e BranchEvent) (BranchEvent, error) {
	mid, err := parseUUID(e.MemberID)
	if err != nil {
		return BranchEvent{}, err
	}
	var via pgtype.UUID
	if e.ViaMemberID != "" {
		if via, err = parseUUID(e.ViaMemberID); err != nil {
			return BranchEvent{}, err
		}
	}
	switch e.Kind {
	case BranchMerged, BranchReset:
	default:
		return BranchEvent{}, fmt.Errorf("%w: branch event %q", ErrInvalidInput, e.Kind)
	}
	row, err := s.q.RecordBranchEvent(ctx, db.RecordBranchEventParams{
		MemberID: mid, Kind: string(e.Kind), CommitSha: e.Commit, Ref: e.Ref, ViaMemberID: via,
	})
	if err != nil {
		return BranchEvent{}, mapPGError("record branch event", err)
	}
	return toBranchEvent(row), nil
}

// ListRoomBranchEvents returns what became of the branches of a room's
// members since a moment, oldest first.
func (s *Store) ListRoomBranchEvents(ctx context.Context, roomID string, since time.Time) ([]BranchEvent, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListRoomBranchEvents(ctx, db.ListRoomBranchEventsParams{RoomID: rid, Since: pgtype.Timestamptz{Time: since, Valid: true}})
	if err != nil {
		return nil, fmt.Errorf("list branch events of room %s: %w", roomID, err)
	}
	out := make([]BranchEvent, len(rows))
	for i, row := range rows {
		out[i] = toBranchEvent(row)
	}
	return out, nil
}

func toBranchEvent(row db.BranchEvent) BranchEvent {
	return BranchEvent{
		ID:          uuidString(row.ID),
		MemberID:    uuidString(row.MemberID),
		Kind:        BranchEventKind(row.Kind),
		Commit:      row.CommitSha,
		Ref:         row.Ref,
		ViaMemberID: uuidString(row.ViaMemberID),
		At:          row.CreatedAt.Time,
	}
}
