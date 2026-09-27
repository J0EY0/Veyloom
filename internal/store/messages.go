package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
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
	// MemberID is set when SenderKind is SenderAgent.
	MemberID string    `json:"member_id,omitempty"`
	Body     string    `json:"body"`
	Mentions []Mention `json:"mentions"`
	// Attachments are the files a person posted with the message; empty
	// on hub-written messages.
	Attachments []Attachment `json:"attachments"`
	// TurnID is set on messages the hub writes for a turn: a topic root once
	// its text is known, the agent's later replies, and system notes.
	TurnID string `json:"turn_id,omitempty"`
	// Title is what a member handing work on called it (send_message's
	// title), the task of the members the message names; empty otherwise.
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
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
	// MemberID is required for SenderAgent and must be empty
	// otherwise.
	MemberID string
	Body     string
	Mentions []Mention
	// AttachmentIDs are uploads of the room to carry with the message; they
	// must be unclaimed. A person may post attachments without words.
	AttachmentIDs []string
	// TurnID links a hub-written message to its turn; empty for people.
	TurnID string
	// Title names the task a member hands on with the message.
	Title string
}

// InboxItem is a message that mentions a user, with the names the inbox
// shows next to it.
type InboxItem struct {
	Message
	RoomName    string `json:"room_name"`
	ProjectName string `json:"project_name"`
	SenderName  string `json:"sender_name"`
	// Read says the user has read it: in the inbox, in its topic, or all
	// at once.
	Read bool `json:"read"`
}

// InboxRead picks what of a user's inbox to mark read: the messages
// named, those in a topic, or all up to a seq. Only messages that mention
// the user are ever marked.
type InboxRead struct {
	MessageIDs []string `json:"message_ids,omitempty"`
	ThreadID   string   `json:"thread_id,omitempty"`
	UpTo       int64    `json:"up_to,omitempty"`
}

// ThreadSummary is what a room timeline shows under a topic root without
// opening it: how much was said and how the latest turn is doing.
type ThreadSummary struct {
	ID string `json:"id"`
	// Number is what the topic is called in its room, written #12. Zero
	// where a summary is made without asking the store (a topic announced
	// the moment it opens).
	Number      int          `json:"number,omitempty"`
	ReplyCount  int          `json:"reply_count"`
	LastReplyAt *time.Time   `json:"last_reply_at,omitempty"`
	Turns       int          `json:"turns"`
	LastTurn    *TurnSummary `json:"last_turn,omitempty"`
	// Work is, under the topic a piece of work began in, the piece of work
	// its latest turn is part of, counted across all its topics.
	Work *WorkSummary `json:"work,omitempty"`
}

// WorkSummary is a piece of work (docs/design.md 5.22) as a timeline
// shows it: from what a person said, every turn it took, in whichever
// topic.
type WorkSummary struct {
	// ThreadID is the topic the work began in, ThreadNumber what that
	// topic is called; Chain the message that began it.
	ThreadID     string `json:"thread_id"`
	ThreadNumber int    `json:"thread_number,omitempty"`
	Chain        string `json:"chain"`
	Turns        int    `json:"turns"`
	// Members are the members that took turns in it, in the order they
	// first did; a timeline's summary leaves them out.
	Members []string `json:"members,omitempty"`
	// StartedAt is when its first turn began; EndedAt when its last one
	// ended, once none runs.
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Running   bool       `json:"running"`
}

// TurnSummary is the slice of a Turn a timeline needs.
type TurnSummary struct {
	ID string `json:"id"`
	// MemberID is whose turn it is.
	MemberID  string     `json:"member_id,omitempty"`
	Status    TurnStatus `json:"status"`
	Error     string     `json:"error,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// Thread groups the replies to one top-level message.
type Thread struct {
	ID     string `json:"id"`
	RoomID string `json:"room_id"`
	// Number is what the topic is called in its room, written #12. It rises
	// with every new topic; gaps are possible and mean nothing.
	Number        int       `json:"number"`
	RootMessageID string    `json:"root_message_id"`
	CreatedAt     time.Time `json:"created_at"`
}

// Page limits for message listings.
const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

// CreateMessage posts a message. An unknown room, thread or user is
// ErrNotFound; a thread from another room, a person's message with neither
// words nor attachments, or an attachment that cannot be claimed is
// ErrInvalidInput.
func (s *Store) CreateMessage(ctx context.Context, m NewMessage) (Message, error) {
	roomID, err := parseUUID(m.RoomID)
	if err != nil {
		return Message{}, err
	}
	if m.SenderKind == SenderUser && strings.TrimSpace(m.Body) == "" && len(m.AttachmentIDs) == 0 {
		return Message{}, fmt.Errorf("%w: a message needs words or attachments", ErrInvalidInput)
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
	var userID, memberID pgtype.UUID
	if m.UserID != "" {
		if userID, err = parseUUID(m.UserID); err != nil {
			return Message{}, err
		}
	}
	if m.MemberID != "" {
		if memberID, err = parseUUID(m.MemberID); err != nil {
			return Message{}, err
		}
	}
	mentions, err := marshalMentions(m.Mentions)
	if err != nil {
		return Message{}, err
	}
	var turnID pgtype.UUID
	if m.TurnID != "" {
		if turnID, err = parseUUID(m.TurnID); err != nil {
			return Message{}, err
		}
	}

	// The message and the claim of its attachments land together or not
	// at all.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Message{}, fmt.Errorf("create message: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	q := s.q.WithTx(tx)

	row, err := q.CreateMessage(ctx, db.CreateMessageParams{
		RoomID:     roomID,
		ThreadID:   threadID,
		SenderKind: string(m.SenderKind),
		UserID:     userID,
		MemberID:   memberID,
		Body:       m.Body,
		Mentions:   mentions,
		TurnID:     turnID,
		Title:      strings.TrimSpace(m.Title),
	})
	if err != nil {
		return Message{}, mapMessageError(err)
	}
	msg, err := toMessage(row)
	if err != nil {
		return Message{}, err
	}
	if len(m.AttachmentIDs) > 0 {
		if msg.Attachments, err = claimAttachments(ctx, q, row.ID, roomID, m.AttachmentIDs); err != nil {
			return Message{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Message{}, fmt.Errorf("create message: %w", err)
	}
	return msg, nil
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
		case "messages_member_id_fkey":
			return fmt.Errorf("member: %w", ErrNotFound)
		case "messages_turn_id_fkey":
			return fmt.Errorf("turn: %w", ErrNotFound)
		}
	case "23514": // check_violation, e.g. blank body or sender/user mismatch
		return constraintProblem(pgErr)
	}
	return fmt.Errorf("create message: %w", err)
}

// ListUserMentions returns the messages that mention userID across every
// room, newest first, with seq less than before (0 means the latest).
func (s *Store) ListUserMentions(ctx context.Context, userID string, before int64, limit int) ([]InboxItem, error) {
	uid, needle, err := mentionOf(userID)
	if err != nil {
		return nil, err
	}
	if before <= 0 {
		before = math.MaxInt64
	}
	rows, err := s.q.ListUserMentions(ctx, db.ListUserMentionsParams{UserID: uid, Needle: needle, Before: before, Max: clampLimit(limit)})
	if err != nil {
		return nil, fmt.Errorf("inbox of %s: %w", userID, err)
	}
	msgs := make([]Message, 0, len(rows))
	for _, row := range rows {
		msg, err := toMessage(db.Message{
			ID: row.ID, Seq: row.Seq, RoomID: row.RoomID, ThreadID: row.ThreadID, SenderKind: row.SenderKind,
			UserID: row.UserID, MemberID: row.MemberID, Body: row.Body, Mentions: row.Mentions,
			CreatedAt: row.CreatedAt, TurnID: row.TurnID,
		})
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
	if msgs, err = s.withAttachments(ctx, msgs); err != nil {
		return nil, err
	}
	out := make([]InboxItem, 0, len(rows))
	for i, row := range rows {
		out = append(out, InboxItem{Message: msgs[i], RoomName: row.RoomName, ProjectName: row.ProjectName, SenderName: row.SenderName, Read: row.Read})
	}
	return out, nil
}

// CountUnreadMentions is how many of the messages that mention userID they
// have not read.
func (s *Store) CountUnreadMentions(ctx context.Context, userID string) (int, error) {
	uid, needle, err := mentionOf(userID)
	if err != nil {
		return 0, err
	}
	n, err := s.q.CountUnreadMentions(ctx, db.CountUnreadMentionsParams{UserID: uid, Needle: needle})
	if err != nil {
		return 0, fmt.Errorf("unread inbox of %s: %w", userID, err)
	}
	return int(n), nil
}

// MarkMentionsRead marks read what read picks of the messages that mention
// userID, and says how many were not read before.
func (s *Store) MarkMentionsRead(ctx context.Context, userID string, read InboxRead) (int, error) {
	uid, needle, err := mentionOf(userID)
	if err != nil {
		return 0, err
	}
	params := db.MarkMentionsReadParams{UserID: uid, Needle: needle, Ids: []pgtype.UUID{}, UpTo: read.UpTo}
	for _, id := range read.MessageIDs {
		mid, err := parseUUID(id)
		if err != nil {
			return 0, err
		}
		params.Ids = append(params.Ids, mid)
	}
	if read.ThreadID != "" {
		if params.ThreadID, err = parseUUID(read.ThreadID); err != nil {
			return 0, err
		}
	}
	n, err := s.q.MarkMentionsRead(ctx, params)
	if err != nil {
		return 0, mapPGError("mark the inbox read", err)
	}
	return int(n), nil
}

// mentionOf is a user's id, parsed, and the mention of them as the
// messages' mentions column is matched against.
func mentionOf(userID string) (pgtype.UUID, []byte, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return pgtype.UUID{}, nil, err
	}
	needle, err := json.Marshal([]Mention{{Kind: MentionUser, ID: userID}})
	if err != nil {
		return pgtype.UUID{}, nil, fmt.Errorf("inbox of %s: %w", userID, err)
	}
	return uid, needle, nil
}

// UpdateMessageBody sets a message's text, its mentions and the turn it
// belongs to. The hub uses it to fill in a topic root once the agent has
// said something; the row is otherwise unchanged. An unknown message is
// ErrNotFound.
func (s *Store) UpdateMessageBody(ctx context.Context, id, body, turnID string, mentions []Mention) (Message, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Message{}, err
	}
	var tid pgtype.UUID
	if turnID != "" {
		if tid, err = parseUUID(turnID); err != nil {
			return Message{}, err
		}
	}
	encoded, err := marshalMentions(mentions)
	if err != nil {
		return Message{}, err
	}
	row, err := s.q.UpdateMessageBody(ctx, db.UpdateMessageBodyParams{ID: uid, Body: body, TurnID: tid, Mentions: encoded})
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, fmt.Errorf("message %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Message{}, mapMessageError(err)
	}
	return toMessage(row)
}

// SetMessageTitle names the task a message hands on.
func (s *Store) SetMessageTitle(ctx context.Context, id, title string) (Message, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Message{}, err
	}
	row, err := s.q.SetMessageTitle(ctx, db.SetMessageTitleParams{ID: uid, Title: title})
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, fmt.Errorf("message %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Message{}, mapMessageError(err)
	}
	return toMessage(row)
}

// ThreadSummaries returns the topic summary for each of the given
// top-level messages that has one, keyed by message id. Messages without
// a thread are simply absent from the result.
func (s *Store) ThreadSummaries(ctx context.Context, rootMessageIDs []string) (map[string]ThreadSummary, error) {
	ids := make([]pgtype.UUID, 0, len(rootMessageIDs))
	for _, id := range rootMessageIDs {
		uid, err := parseUUID(id)
		if err != nil {
			return nil, err
		}
		ids = append(ids, uid)
	}
	rows, err := s.q.ThreadSummaries(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("thread summaries: %w", err)
	}
	out := make(map[string]ThreadSummary, len(rows))
	for _, row := range rows {
		summary := ThreadSummary{
			ID:         uuidString(row.ThreadID),
			Number:     int(row.ThreadNumber),
			ReplyCount: int(row.ReplyCount),
			Turns:      int(row.TurnCount),
		}
		if row.LastReplyAt.Valid {
			t := row.LastReplyAt.Time
			summary.LastReplyAt = &t
		}
		if row.LastTurnID != "" {
			summary.LastTurn = &TurnSummary{
				ID:        row.LastTurnID,
				MemberID:  row.LastTurnMemberID,
				Status:    TurnStatus(row.LastTurnStatus),
				Error:     row.LastTurnError,
				StartedAt: row.LastTurnStartedAt.Time,
			}
			if row.LastTurnEndedAt.Valid {
				t := row.LastTurnEndedAt.Time
				summary.LastTurn.EndedAt = &t
			}
		}
		if row.WorkChain != "" {
			summary.Work = &WorkSummary{
				ThreadID: summary.ID, ThreadNumber: summary.Number, Chain: row.WorkChain, Turns: int(row.WorkTurns),
				StartedAt: row.WorkStartedAt.Time, Running: row.WorkRunning,
			}
			if row.WorkEndedAt.Valid {
				t := row.WorkEndedAt.Time
				summary.Work.EndedAt = &t
			}
		}
		out[uuidString(row.RootMessageID)] = summary
	}
	return out, nil
}

// ChainWork is the piece of work the message chain began, across all its
// topics; ErrNotFound when no turn is part of it.
func (s *Store) ChainWork(ctx context.Context, chain string) (WorkSummary, error) {
	uid, err := parseUUID(chain)
	if err != nil {
		return WorkSummary{}, err
	}
	row, err := s.q.ChainWork(ctx, uid)
	if err != nil {
		return WorkSummary{}, fmt.Errorf("chain work: %w", err)
	}
	if row.Turns == 0 {
		return WorkSummary{}, ErrNotFound
	}
	work := WorkSummary{
		ThreadID: uuidString(row.ThreadID), ThreadNumber: int(row.ThreadNumber), Chain: chain, Turns: int(row.Turns),
		StartedAt: row.StartedAt.Time, Running: row.Running,
	}
	for _, id := range row.Members {
		work.Members = append(work.Members, uuidString(id))
	}
	if row.EndedAt.Valid {
		t := row.EndedAt.Time
		work.EndedAt = &t
	}
	return work, nil
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
	msg, err := toMessage(row)
	if err != nil {
		return Message{}, err
	}
	msgs, err := s.withAttachments(ctx, []Message{msg})
	if err != nil {
		return Message{}, err
	}
	return msgs[0], nil
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
	return s.listWithAttachments(ctx, rows)
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
	return s.listWithAttachments(ctx, rows)
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
	return s.listWithAttachments(ctx, rows)
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
	// Looked up before it is created: creating takes the room's next topic
	// number, and asking again for a thread that exists must not burn one.
	row, err := s.q.GetThreadByRoot(ctx, rootID)
	if errors.Is(err, pgx.ErrNoRows) {
		row, err = s.q.UpsertThread(ctx, db.UpsertThreadParams{RoomID: roomID, RootMessageID: rootID})
	}
	if err != nil {
		return Thread{}, fmt.Errorf("thread for message %s: %w", messageID, err)
	}
	return toThread(row), nil
}

// ThreadOfMessage returns the thread a message is in, or the one it
// started, without starting one: ErrNotFound for a top-level message
// nothing came of.
func (s *Store) ThreadOfMessage(ctx context.Context, messageID string) (Thread, error) {
	msg, err := s.GetMessage(ctx, messageID)
	if err != nil {
		return Thread{}, err
	}
	if msg.ThreadID != "" {
		return s.GetThread(ctx, msg.ThreadID)
	}
	rootID, _ := parseUUID(msg.ID)
	row, err := s.q.GetThreadByRoot(ctx, rootID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Thread{}, fmt.Errorf("thread of message %s: %w", messageID, ErrNotFound)
	}
	if err != nil {
		return Thread{}, fmt.Errorf("thread of message %s: %w", messageID, err)
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
		ID:          uuidString(row.ID),
		Seq:         row.Seq,
		Room:        uuidString(row.RoomID),
		ThreadID:    uuidString(row.ThreadID),
		SenderKind:  SenderKind(row.SenderKind),
		UserID:      uuidString(row.UserID),
		MemberID:    uuidString(row.MemberID),
		Body:        row.Body,
		Mentions:    mentions,
		Attachments: []Attachment{},
		TurnID:      uuidString(row.TurnID),
		Title:       row.Title,
		CreatedAt:   row.CreatedAt.Time,
	}, nil
}

// listWithAttachments converts rows and fills in their attachments.
func (s *Store) listWithAttachments(ctx context.Context, rows []db.Message) ([]Message, error) {
	msgs, err := toMessages(rows)
	if err != nil {
		return nil, err
	}
	return s.withAttachments(ctx, msgs)
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
		Number:        int(row.Number),
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
	return s.listWithAttachments(ctx, rows)
}
