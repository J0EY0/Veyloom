package store

import (
	"context"
	"fmt"

	"github.com/J0EY0/veyloom/internal/store/db"
	"github.com/jackc/pgx/v5/pgtype"
)

// TopicRef is a topic as something else names it: a turn that ran in it, or
// its number in its room. It carries the topic's first message, which says
// what the topic is about.
type TopicRef struct {
	// TurnID is the turn it was found by, when it was.
	TurnID   string `json:"turn_id,omitempty"`
	ThreadID string `json:"thread_id"`
	RoomID   string `json:"room_id"`
	Number   int    `json:"number"`
	RootBody string `json:"root_body"`
}

// ListTurnTopics finds the topics the given turns ran in. Turns not found
// are left out, and so is what is no id at all, as a page written by hand
// may give.
func (s *Store) ListTurnTopics(ctx context.Context, turnIDs []string) ([]TopicRef, error) {
	ids := make([]pgtype.UUID, 0, len(turnIDs))
	for _, id := range turnIDs {
		if uid, err := parseUUID(id); err == nil {
			ids = append(ids, uid)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.q.ListTurnTopics(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list the topics of turns: %w", err)
	}
	out := make([]TopicRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, TopicRef{
			TurnID: uuidString(row.TurnID), ThreadID: uuidString(row.ThreadID), RoomID: uuidString(row.RoomID),
			Number: int(row.Number), RootBody: row.RootBody,
		})
	}
	return out, nil
}

// ListTopicsByNumber finds topics of a room by their numbers; numbers the
// room has no topic for are left out.
func (s *Store) ListTopicsByNumber(ctx context.Context, roomID string, numbers []int) ([]TopicRef, error) {
	if len(numbers) == 0 {
		return nil, nil
	}
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	ns := make([]int32, len(numbers))
	for i, n := range numbers {
		ns[i] = int32(n)
	}
	rows, err := s.q.ListTopicsByNumber(ctx, db.ListTopicsByNumberParams{RoomID: rid, Numbers: ns})
	if err != nil {
		return nil, fmt.Errorf("list topics of room %s: %w", roomID, err)
	}
	out := make([]TopicRef, 0, len(rows))
	for _, row := range rows {
		out = append(out, TopicRef{ThreadID: uuidString(row.ThreadID), RoomID: uuidString(row.RoomID), Number: int(row.Number), RootBody: row.RootBody})
	}
	return out, nil
}
