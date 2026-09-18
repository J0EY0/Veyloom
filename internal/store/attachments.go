package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// Attachment is a file a person attached to a message. The bytes live on
// the hub's disk; the API serves them by id.
type Attachment struct {
	ID     string `json:"id"`
	RoomID string `json:"room_id"`
	// MessageID is empty until the message carrying the upload is posted.
	MessageID string `json:"message_id,omitempty"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
	// Path is where the bytes are, relative to the attachment directory.
	// It is the hub's business, not the client's.
	Path      string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}

// NewAttachment is the input to CreateAttachment. The caller picks the id
// (see NewID) because the file is named after it before the row exists.
type NewAttachment struct {
	ID        string
	RoomID    string
	Filename  string
	MediaType string
	Size      int64
	Path      string
}

// CreateAttachment records an upload that no message carries yet. An
// unknown room is ErrNotFound.
func (s *Store) CreateAttachment(ctx context.Context, a NewAttachment) (Attachment, error) {
	id, err := parseUUID(a.ID)
	if err != nil {
		return Attachment{}, err
	}
	roomID, err := parseUUID(a.RoomID)
	if err != nil {
		return Attachment{}, err
	}
	row, err := s.q.CreateAttachment(ctx, db.CreateAttachmentParams{
		ID: id, RoomID: roomID, Filename: a.Filename, MediaType: a.MediaType, Size: a.Size, Path: a.Path,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23503": // foreign_key_violation: the room
				return Attachment{}, fmt.Errorf("room: %w", ErrNotFound)
			case "23514": // check_violation: blank filename, negative size
				return Attachment{}, fmt.Errorf("%w: %s", ErrInvalidInput, pgErr.ConstraintName)
			case "23505": // unique_violation: the id
				return Attachment{}, fmt.Errorf("attachment %s: %w", a.ID, ErrConflict)
			}
		}
		return Attachment{}, fmt.Errorf("create attachment: %w", err)
	}
	return toAttachment(row), nil
}

// GetAttachment returns one attachment by id.
func (s *Store) GetAttachment(ctx context.Context, id string) (Attachment, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Attachment{}, err
	}
	row, err := s.q.GetAttachment(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, fmt.Errorf("attachment %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("get attachment %s: %w", id, err)
	}
	return toAttachment(row), nil
}

// claimAttachments ties uploads to the message that carries them, inside
// the caller's transaction. Every id must be an unclaimed upload of the
// message's room; otherwise the message is not created.
func claimAttachments(ctx context.Context, q *db.Queries, messageID, roomID pgtype.UUID, ids []string) ([]Attachment, error) {
	seen := make(map[string]bool, len(ids))
	uuids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		uid, err := parseUUID(id)
		if err != nil {
			return nil, err
		}
		uuids = append(uuids, uid)
	}
	rows, err := q.ClaimAttachments(ctx, db.ClaimAttachmentsParams{MessageID: messageID, Ids: uuids, RoomID: roomID})
	if err != nil {
		return nil, fmt.Errorf("claim attachments: %w", err)
	}
	if len(rows) != len(uuids) {
		return nil, fmt.Errorf("%w: an attachment is unknown, belongs to another room, or is already on a message", ErrInvalidInput)
	}
	return toAttachments(rows), nil
}

// withAttachments fills in the attachments of the given messages with one
// query, and returns the same slice.
func (s *Store) withAttachments(ctx context.Context, msgs []Message) ([]Message, error) {
	if len(msgs) == 0 {
		return msgs, nil
	}
	ids := make([]pgtype.UUID, 0, len(msgs))
	for _, m := range msgs {
		uid, err := parseUUID(m.ID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, uid)
	}
	rows, err := s.q.ListAttachmentsByMessages(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	byMessage := make(map[string][]Attachment, len(rows))
	for _, row := range rows {
		a := toAttachment(row)
		byMessage[a.MessageID] = append(byMessage[a.MessageID], a)
	}
	for i := range msgs {
		if found, ok := byMessage[msgs[i].ID]; ok {
			msgs[i].Attachments = found
		}
	}
	return msgs, nil
}

func toAttachment(row db.Attachment) Attachment {
	return Attachment{
		ID:        uuidString(row.ID),
		RoomID:    uuidString(row.RoomID),
		MessageID: uuidString(row.MessageID),
		Filename:  row.Filename,
		MediaType: row.MediaType,
		Size:      row.Size,
		Path:      row.Path,
		CreatedAt: row.CreatedAt.Time,
	}
}

func toAttachments(rows []db.Attachment) []Attachment {
	out := make([]Attachment, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAttachment(row))
	}
	return out
}
