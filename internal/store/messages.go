package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// ErrInvalidInput is returned when a request is well-formed but makes no
// sense, such as replying in a thread that belongs to another room.
var ErrInvalidInput = errors.New("store: invalid input")

// SenderKind says who posted a message.
type SenderKind string

const (
	SenderUser   SenderKind = "user"
	SenderAgent  SenderKind = "agent"
	SenderSystem SenderKind = "system"
)

// MentionKind says what an @-mention points at.
type MentionKind string

const (
	MentionUser  MentionKind = "user"
	MentionAgent MentionKind = "agent"
)

// Mention is one structured @-mention in a message.
type Mention struct {
	Kind MentionKind `json:"kind"`
	ID   string      `json:"id"`
}

// Message is one post in a room.
type Message struct {
	ID   string `json:"id"`
	Seq  int64  `json:"seq"`
	Room string `json:"room_id"`
	// ThreadID is empty for a top-level message.
	ThreadID   string     `json:"thread_id,omitempty"`
	SenderKind SenderKind `json:"sender_kind"`
	// UserID is set when SenderKind is SenderUser.
	UserID string `json:"user_id,omitempty"`
	// AgentInstanceID is set when SenderKind is SenderAgent.
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
	Body            string    `json:"body"`
	Mentions        []Mention `json:"mentions"`
	CreatedAt       time.Time `json:"created_at"`
}

// NewMessage is the input to CreateMessage.
type NewMessage struct {
	RoomID string
	// ThreadID makes the message a reply in that thread; empty posts it at
	// the top level of the room.
	ThreadID   string
	SenderKind SenderKind
	// UserID is required for SenderUser and must be empty otherwise.
	UserID string
	// AgentInstanceID is required for SenderAgent and must be empty
	// otherwise.
	AgentInstanceID string
	Body            string
	Mentions        []Mention
}

// Thread groups the replies to one top-level message.
type Thread struct {
	ID            string    `json:"id"`
	RoomID        string    `json:"room_id"`
	RootMessageID string    `json:"root_message_id"`
	CreatedAt     time.Time `json:"created_at"`
}

// Page limits for message listings.
const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

// CreateMessage posts a message. An unknown room, thread or user is
// ErrNotFound; a thread from another room is ErrInvalidInput.
func (s *Store) CreateMessage(ctx context.Context, m NewMessage) (Message, error) {
	roomID, err := parseUUID(m.RoomID)
	if err != nil {
		return Message{}, err
	}
	var threadID pgtype.UUID
	if m.ThreadID != "" {
		thread, err := s.GetThread(ctx, m.ThreadID)
		if err != nil {
			return Message{}, err
		}
		if thread.RoomID != m.RoomID {
			return Message{}, fmt.Errorf("%w: thread %s belongs to room %s, not %s", ErrInvalidInput, m.ThreadID, thread.RoomID, m.RoomID)
		}
		if threadID, err = parseUUID(m.ThreadID); err != nil {
			return Message{}, err
		}
	}
	var userID, agentID pgtype.UUID
	if m.UserID != "" {
		if userID, err = parseUUID(m.UserID); err != nil {
			return Message{}, err
		}
	}
	if m.AgentInstanceID != "" {
		if agentID, err = parseUUID(m.AgentInstanceID); err != nil {
			return Message{}, err
		}
	}
	mentions, err := marshalMentions(m.Mentions)
	if err != nil {
		return Message{}, err
	}

	row, err := s.q.CreateMessage(ctx, db.CreateMessageParams{
		RoomID:          roomID,
		ThreadID:        threadID,
		SenderKind:      string(m.SenderKind),
		UserID:          userID,
		AgentInstanceID: agentID,
		Body:            m.Body,
		Mentions:        mentions,
	})
	if err != nil {
		return Message{}, mapMessageError(err)
	}
	return toMessage(row)
}

// mapMessageError turns constraint failures on messages into the sentinel
// errors callers can act on.
func mapMessageError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("create message: %w", err)
	}
	switch pgErr.Code {
	case "23503": // foreign_key_violation
		switch pgErr.ConstraintName {
		case "messages_room_id_fkey":
			return fmt.Errorf("room: %w", ErrNotFound)
		case "messages_user_id_fkey":
			return fmt.Errorf("user: %w", ErrNotFound)
		case "messages_thread_id_fkey":
			return fmt.Errorf("thread: %w", ErrNotFound)
		case "messages_agent_instance_id_fkey":
			return fmt.Errorf("agent instance: %w", ErrNotFound)
		}
	case "23514": // check_violation, e.g. blank body or sender/user mismatch
		return fmt.Errorf("%w: %s", ErrInvalidInput, pgErr.ConstraintName)
	}
	return fmt.Errorf("create message: %w", err)
}

// GetMessage returns one message, or ErrNotFound.
func (s *Store) GetMessage(ctx context.Context, id string) (Message, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Message{}, err
	}
	row, err := s.q.GetMessage(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, fmt.Errorf("message %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Message{}, fmt.Errorf("get message %s: %w", id, err)
	}
	return toMessage(row)
}

// ListRoomMessages returns a room's top-level messages with seq greater
// than after, oldest first. It is the call for polling and for "what
// happened since". An unknown room yields an empty list.
func (s *Store) ListRoomMessages(ctx context.Context, roomID string, after int64, limit int) ([]Message, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListRoomMessagesAfter(ctx, db.ListRoomMessagesAfterParams{RoomID: rid, Seq: after, Limit: clampLimit(limit)})
	if err != nil {
		return nil, fmt.Errorf("list messages of room %s: %w", roomID, err)
	}
	return toMessages(rows)
}

// ListRoomMessagesBefore returns a room's top-level messages with seq less
// than before, still oldest first, so a client can walk back through
// history page by page. A before of 0 means "the latest".
func (s *Store) ListRoomMessagesBefore(ctx context.Context, roomID string, before int64, limit int) ([]Message, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	if before <= 0 {
		before = math.MaxInt64
	}
	rows, err := s.q.ListRoomMessagesBefore(ctx, db.ListRoomMessagesBeforeParams{RoomID: rid, Seq: before, Limit: clampLimit(limit)})
	if err != nil {
		return nil, fmt.Errorf("list messages of room %s: %w", roomID, err)
	}
	// The query returns newest first so LIMIT picks the right end; flip it.
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return toMessages(rows)
}

// ListThreadMessages returns a thread's replies with seq greater than
// after, oldest first. The root message is not included; see GetThread.
func (s *Store) ListThreadMessages(ctx context.Context, threadID string, after int64, limit int) ([]Message, error) {
	tid, err := parseUUID(threadID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListThreadMessagesAfter(ctx, db.ListThreadMessagesAfterParams{ThreadID: tid, Seq: after, Limit: clampLimit(limit)})
	if err != nil {
		return nil, fmt.Errorf("list messages of thread %s: %w", threadID, err)
	}
	return toMessages(rows)
}

// ThreadForMessage returns the thread a message belongs to, creating it if
// the message is top-level and has no thread yet. Replying to a reply
// therefore lands in the same thread as the original.
func (s *Store) ThreadForMessage(ctx context.Context, messageID string) (Thread, error) {
	msg, err := s.GetMessage(ctx, messageID)
	if err != nil {
		return Thread{}, err
	}
	if msg.ThreadID != "" {
		return s.GetThread(ctx, msg.ThreadID)
	}

	roomID, _ := parseUUID(msg.Room)
	rootID, _ := parseUUID(msg.ID)
	row, err := s.q.UpsertThread(ctx, db.UpsertThreadParams{RoomID: roomID, RootMessageID: rootID})
	if err != nil {
		return Thread{}, fmt.Errorf("create thread for message %s: %w", messageID, err)
	}
	return toThread(row), nil
}

// GetThread returns one thread, or ErrNotFound.
func (s *Store) GetThread(ctx context.Context, id string) (Thread, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Thread{}, err
	}
	row, err := s.q.GetThread(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Thread{}, fmt.Errorf("thread %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Thread{}, fmt.Errorf("get thread %s: %w", id, err)
	}
	return toThread(row), nil
}

// clampLimit applies the default and maximum page size.
func clampLimit(limit int) int32 {
	if limit <= 0 {
		return DefaultPageLimit
	}
	if limit > MaxPageLimit {
		return MaxPageLimit
	}
	return int32(limit)
}

// marshalMentions encodes mentions for the jsonb column; nil becomes [].
func marshalMentions(m []Mention) ([]byte, error) {
	if m == nil {
		m = []Mention{}
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode mentions: %w", err)
	}
	return raw, nil
}

func toMessage(row db.Message) (Message, error) {
	mentions := []Mention{}
	if err := json.Unmarshal(row.Mentions, &mentions); err != nil {
		return Message{}, fmt.Errorf("decode mentions of message %s: %w", uuidString(row.ID), err)
	}
	return Message{
		ID:              uuidString(row.ID),
		Seq:             row.Seq,
		Room:            uuidString(row.RoomID),
		ThreadID:        uuidString(row.ThreadID),
		SenderKind:      SenderKind(row.SenderKind),
		UserID:          uuidString(row.UserID),
		AgentInstanceID: uuidString(row.AgentInstanceID),
		Body:            row.Body,
		Mentions:        mentions,
		CreatedAt:       row.CreatedAt.Time,
	}, nil
}

func toMessages(rows []db.Message) ([]Message, error) {
	out := make([]Message, 0, len(rows))
	for _, row := range rows {
		m, err := toMessage(row)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func toThread(row db.Thread) Thread {
	return Thread{
		ID:            uuidString(row.ID),
		RoomID:        uuidString(row.RoomID),
		RootMessageID: uuidString(row.RootMessageID),
		CreatedAt:     row.CreatedAt.Time,
	}
}

// ListThreadMessagesBefore returns a thread's most recent replies with seq
// less than before, oldest first. A before of 0 means "the latest". It is
// the call for showing an agent the tail of a long conversation.
func (s *Store) ListThreadMessagesBefore(ctx context.Context, threadID string, before int64, limit int) ([]Message, error) {
	tid, err := parseUUID(threadID)
	if err != nil {
		return nil, err
	}
	if before <= 0 {
		before = math.MaxInt64
	}
	rows, err := s.q.ListThreadMessagesBefore(ctx, db.ListThreadMessagesBeforeParams{ThreadID: tid, Seq: before, Limit: clampLimit(limit)})
	if err != nil {
		return nil, fmt.Errorf("list messages of thread %s: %w", threadID, err)
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return toMessages(rows)
}
