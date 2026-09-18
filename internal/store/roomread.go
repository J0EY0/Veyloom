package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// What an agent reads of its room on request (the read tools, design.md
// 5.7): the directory of topics, a topic by its number, a search. Briefs
// push what is new; these are for everything else, older or elsewhere.

// TopicListing is one line of a room's directory of topics.
type TopicListing struct {
	ThreadID string
	Number   int
	// ReplyCount leaves out the root; Last is the newest message, the root
	// itself while nobody has replied, and LastSeq its seq, the cursor for
	// the next page.
	ReplyCount int
	LastSeq    int64
	Root       Message
	Last       Message
}

// ThreadByNumber returns the room's topic with that number, or ErrNotFound.
func (s *Store) ThreadByNumber(ctx context.Context, roomID string, number int) (Thread, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return Thread{}, err
	}
	row, err := s.q.GetThreadByNumber(ctx, db.GetThreadByNumberParams{RoomID: rid, Number: int32(number)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Thread{}, fmt.Errorf("topic #%d: %w", number, ErrNotFound)
	}
	if err != nil {
		return Thread{}, fmt.Errorf("topic #%d of room %s: %w", number, roomID, err)
	}
	return toThread(row), nil
}

// ListRoomTopics returns a room's topics, most recently active first, whose
// last message is older than before; 0 means from the newest.
func (s *Store) ListRoomTopics(ctx context.Context, roomID string, before int64, limit int) ([]TopicListing, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	if before <= 0 {
		before = math.MaxInt64
	}
	rows, err := s.q.ListRoomTopics(ctx, db.ListRoomTopicsParams{RoomID: rid, Before: before, MaxRows: clampLimit(limit)})
	if err != nil {
		return nil, fmt.Errorf("topics of room %s: %w", roomID, err)
	}
	out := make([]TopicListing, 0, len(rows))
	for _, row := range rows {
		root, err := toMessage(row.Message)
		if err != nil {
			return nil, err
		}
		last, err := toMessage(row.Message_2)
		if err != nil {
			return nil, err
		}
		out = append(out, TopicListing{ThreadID: uuidString(row.ThreadID), Number: int(row.Number), ReplyCount: int(row.ReplyCount), LastSeq: row.LastSeq, Root: root, Last: last})
	}
	return out, nil
}

// SearchRoomMessages returns the room's messages whose text holds phrase,
// newest first and older than before (0 means from the newest), each with
// the number of the topic it is in or heads. The phrase is matched as
// written, without regard to case; it holds no wildcards.
func (s *Store) SearchRoomMessages(ctx context.Context, roomID, phrase string, before int64, limit int) ([]RoomNewsItem, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return nil, fmt.Errorf("%w: nothing to search for", ErrInvalidInput)
	}
	if before <= 0 {
		before = math.MaxInt64
	}
	rows, err := s.q.SearchRoomMessages(ctx, db.SearchRoomMessagesParams{RoomID: rid, Pattern: "%" + escapeLike(phrase) + "%", Before: before, MaxRows: clampLimit(limit)})
	if err != nil {
		return nil, fmt.Errorf("search room %s: %w", roomID, err)
	}
	raw := make([]db.Message, 0, len(rows))
	numbers := make(map[string]int, len(rows))
	for _, row := range rows {
		raw = append(raw, row.Message)
		numbers[uuidString(row.Message.ID)] = int(row.TopicNumber)
	}
	msgs, err := s.listWithAttachments(ctx, raw)
	if err != nil {
		return nil, err
	}
	out := make([]RoomNewsItem, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, RoomNewsItem{Message: m, TopicNumber: numbers[m.ID]})
	}
	return out, nil
}

// escapeLike makes s match itself in a LIKE pattern, under the default
// escape character.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
