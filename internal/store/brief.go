package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// What a brief is put together from: what a session has not read yet. A
// brief is taken at one position of the room (RoomPosition), every listing
// is bounded by it, and the session's positions move up to it afterwards,
// so that nothing posted while the brief was being made falls in between.

// NewsQuery selects what is new to a session in a room.
type NewsQuery struct {
	RoomID string
	// After and UpTo bound the messages by seq: After < seq <= UpTo.
	After, UpTo int64
	// SessionID, when set, leaves out what the session said itself: the
	// messages of turns that ran in it, which it remembers untold.
	SessionID string
	// Limit caps the rows returned; the newest are kept.
	Limit int
}

// RoomNewsItem is a top-level message with the number of the topic it
// heads, 0 when it heads none.
type RoomNewsItem struct {
	Message
	TopicNumber int
}

// TopicNewsItem is a topic with replies the session has not read.
type TopicNewsItem struct {
	ThreadID string
	Number   int
	// NewCount is how many replies are new; Root heads the topic and Last
	// is the newest of the replies.
	NewCount int
	Root     Message
	Last     Message
}

// RoomPosition returns the seq of a room's newest message, 0 for an empty
// room: where the room stands now.
func (s *Store) RoomPosition(ctx context.Context, roomID string) (int64, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return 0, err
	}
	pos, err := s.q.RoomPosition(ctx, rid)
	if err != nil {
		return 0, fmt.Errorf("position of room %s: %w", roomID, err)
	}
	return pos, nil
}

// RoomNews returns the room's top-level messages new to a session, oldest
// first, and how many there are in all; when that is more than came back,
// it is the oldest that were left out.
func (s *Store) RoomNews(ctx context.Context, q NewsQuery) ([]RoomNewsItem, int, error) {
	roomID, sessionID, err := q.ids()
	if err != nil {
		return nil, 0, err
	}
	rows, err := s.q.ListRoomNews(ctx, db.ListRoomNewsParams{RoomID: roomID, After: q.After, UpTo: q.UpTo, SessionID: sessionID, MaxRows: clampLimit(q.Limit)})
	if err != nil {
		return nil, 0, fmt.Errorf("news of room %s: %w", q.RoomID, err)
	}
	total := 0
	raw := make([]db.Message, 0, len(rows))
	numbers := make(map[string]int, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		total = int(rows[i].Total)
		raw = append(raw, rows[i].Message)
		numbers[uuidString(rows[i].Message.ID)] = int(rows[i].TopicNumber)
	}
	msgs, err := s.listWithAttachments(ctx, raw)
	if err != nil {
		return nil, 0, err
	}
	out := make([]RoomNewsItem, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, RoomNewsItem{Message: m, TopicNumber: numbers[m.ID]})
	}
	return out, total, nil
}

// TopicNews returns the room's topics with replies new to a session, other
// than exceptThreadID, most recently active first, and how many such topics
// there are in all.
func (s *Store) TopicNews(ctx context.Context, q NewsQuery, exceptThreadID string) ([]TopicNewsItem, int, error) {
	roomID, sessionID, err := q.ids()
	if err != nil {
		return nil, 0, err
	}
	var except pgtype.UUID
	if exceptThreadID != "" {
		if except, err = parseUUID(exceptThreadID); err != nil {
			return nil, 0, err
		}
	} else {
		// No topic to leave out: a UUID no thread has.
		except = pgtype.UUID{Valid: true}
	}
	rows, err := s.q.ListTopicNews(ctx, db.ListTopicNewsParams{RoomID: roomID, After: q.After, UpTo: q.UpTo, ExceptThreadID: except, SessionID: sessionID, MaxRows: clampLimit(q.Limit)})
	if err != nil {
		return nil, 0, fmt.Errorf("topic news of room %s: %w", q.RoomID, err)
	}
	total := 0
	out := make([]TopicNewsItem, 0, len(rows))
	for _, row := range rows {
		total = int(row.Total)
		root, err := toMessage(row.Message)
		if err != nil {
			return nil, 0, err
		}
		last, err := toMessage(row.Message_2)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, TopicNewsItem{ThreadID: uuidString(row.ThreadID), Number: int(row.Number), NewCount: int(row.NewCount), Root: root, Last: last})
	}
	return out, total, nil
}

// ThreadNews returns a thread's replies new to a session, oldest first, and
// how many there are in all; when that is more than came back, it is the
// oldest that were left out. With After 0 and no SessionID it is the tail
// of the whole thread. RoomID is not used.
func (s *Store) ThreadNews(ctx context.Context, threadID string, q NewsQuery) ([]Message, int, error) {
	tid, err := parseUUID(threadID)
	if err != nil {
		return nil, 0, err
	}
	var sessionID pgtype.UUID
	if q.SessionID != "" {
		if sessionID, err = parseUUID(q.SessionID); err != nil {
			return nil, 0, err
		}
	}
	rows, err := s.q.ListThreadNews(ctx, db.ListThreadNewsParams{ThreadID: tid, After: q.After, UpTo: q.UpTo, SessionID: sessionID, MaxRows: clampLimit(q.Limit)})
	if err != nil {
		return nil, 0, fmt.Errorf("news of thread %s: %w", threadID, err)
	}
	total := 0
	raw := make([]db.Message, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		total = int(rows[i].Total)
		raw = append(raw, rows[i].Message)
	}
	msgs, err := s.listWithAttachments(ctx, raw)
	if err != nil {
		return nil, 0, err
	}
	return msgs, total, nil
}

// ids parses the query's room and optional session.
func (q NewsQuery) ids() (room, session pgtype.UUID, err error) {
	if room, err = parseUUID(q.RoomID); err != nil {
		return room, session, err
	}
	if q.SessionID != "" {
		session, err = parseUUID(q.SessionID)
	}
	return room, session, err
}
