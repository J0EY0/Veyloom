package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// RoomAttachment is an attachment as the attachments tab lists it (docs/
// webui.md 4.21): the file, and the message that carried it.
type RoomAttachment struct {
	Attachment
	// ThreadID is the topic the message is in, empty for the room itself.
	ThreadID     string `json:"thread_id,omitempty"`
	ThreadNumber int    `json:"thread_number,omitempty"`
	// Who sent it: a person (UserID) or a member (MemberID).
	SenderKind SenderKind `json:"sender_kind"`
	UserID     string     `json:"user_id,omitempty"`
	MemberID   string     `json:"member_id,omitempty"`
	// SenderName is the member's name, or the person's when the query
	// named the people.
	SenderName string `json:"sender_name"`
	// MessageSeq places the message in the chat, for finding it again.
	MessageSeq int64 `json:"message_seq"`
	// Said is what the message said, in a line.
	Said string `json:"said,omitempty"`
}

// How the attachments tab sorts.
const (
	AttachmentsNewest = "newest"
	AttachmentsOldest = "oldest"
	AttachmentsBySize = "size"
	AttachmentsByName = "name"
)

// AttachmentQuery narrows and orders a room's attachments.
type AttachmentQuery struct {
	// Words are split at spaces, and each of them must match the file's
	// name, what the message said, or the member's or the person's name,
	// ignoring case.
	Words string
	// People are the people's names by id: they are in the account file,
	// not the database.
	People map[string]string
	// Kinds keeps these kinds only; none keeps every kind.
	Kinds []string
	// UserID or MemberID keeps what that person or member sent.
	UserID   string
	MemberID string
	// Sort is one of the Attachments… orders; empty is the newest first.
	Sort   string
	Offset int
	Limit  int
}

// saidMax caps the line of a message an attachment is listed with.
const saidMax = 200

// ListRoomAttachments returns a page of the attachments messages of a room
// carry, and how many there are in all the pages.
func (s *Store) ListRoomAttachments(ctx context.Context, roomID string, q AttachmentQuery) ([]RoomAttachment, int, error) {
	room, err := parseUUID(roomID)
	if err != nil {
		return nil, 0, err
	}
	for _, k := range q.Kinds {
		if !slices.Contains(AttachmentKinds, k) {
			return nil, 0, fmt.Errorf("%w: %q is not a kind of attachment", ErrInvalidInput, k)
		}
	}
	sort := q.Sort
	if sort == "" {
		sort = AttachmentsNewest
	}
	if !slices.Contains([]string{AttachmentsNewest, AttachmentsOldest, AttachmentsBySize, AttachmentsByName}, sort) {
		return nil, 0, fmt.Errorf("%w: %q is not a way to sort attachments", ErrInvalidInput, sort)
	}
	user, err := optionalUUID(q.UserID)
	if err != nil {
		return nil, 0, err
	}
	member, err := optionalUUID(q.MemberID)
	if err != nil {
		return nil, 0, err
	}
	ids := make([]pgtype.UUID, 0, len(q.People))
	names := make([]string, 0, len(q.People))
	for id, name := range q.People {
		u, err := parseUUID(id)
		if err != nil {
			return nil, 0, err
		}
		ids = append(ids, u)
		names = append(names, name)
	}
	patterns := []string{}
	for _, word := range strings.Fields(q.Words) {
		patterns = append(patterns, "%"+likeEscaper.Replace(word)+"%")
	}
	kinds := q.Kinds
	if kinds == nil {
		kinds = []string{}
	}
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	rows, err := s.q.ListRoomAttachments(ctx, db.ListRoomAttachmentsParams{
		RoomID: room, Kinds: kinds, UserID: user, MemberID: member, Patterns: patterns, PeopleIds: ids, PeopleNames: names,
		Sort: sort, Skip: int32(min(max(0, q.Offset), math.MaxInt32)), Max: int32(limit),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list attachments: %w", err)
	}
	total, err := s.q.CountRoomAttachments(ctx, db.CountRoomAttachmentsParams{
		RoomID: room, Kinds: kinds, UserID: user, MemberID: member, Patterns: patterns, PeopleIds: ids, PeopleNames: names,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("count attachments: %w", err)
	}
	out := make([]RoomAttachment, 0, len(rows))
	for _, row := range rows {
		out = append(out, RoomAttachment{
			Attachment:   toAttachment(row.Attachment),
			ThreadID:     uuidString(row.ThreadID),
			ThreadNumber: int(row.ThreadNumber),
			SenderKind:   SenderKind(row.SenderKind),
			UserID:       uuidString(row.UserID),
			MemberID:     uuidString(row.MemberID),
			SenderName:   row.SenderName,
			MessageSeq:   row.MessageSeq,
			Said:         firstLine(row.MessageBody, saidMax),
		})
	}
	return out, int(total), nil
}

// likeEscaper keeps what a person types from reading as a LIKE pattern.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// firstLine is a text's first line with words in it, cut to n characters.
func firstLine(text string, n int) string {
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			if utf8.RuneCountInString(l) > n {
				r := []rune(l)
				return string(r[:n]) + "…"
			}
			return l
		}
	}
	return ""
}

// RoomAttachmentsByID returns those of the ids that name attachments
// messages of the room carry, oldest first; the rest are left out.
func (s *Store) RoomAttachmentsByID(ctx context.Context, roomID string, ids []string) ([]Attachment, error) {
	room, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	uuids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		u, err := parseUUID(id)
		if err != nil {
			return nil, err
		}
		uuids = append(uuids, u)
	}
	rows, err := s.q.ListRoomAttachmentsByID(ctx, db.ListRoomAttachmentsByIDParams{RoomID: room, Ids: uuids})
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	return toAttachments(rows), nil
}

// UnclaimedAttachments returns up to limit uploads no message took before
// the given time, the oldest first.
func (s *Store) UnclaimedAttachments(ctx context.Context, before time.Time, limit int) ([]Attachment, error) {
	rows, err := s.q.ListUnclaimedAttachments(ctx, db.ListUnclaimedAttachmentsParams{
		Before: pgtype.Timestamptz{Time: before, Valid: true}, Max: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list unclaimed attachments: %w", err)
	}
	return toAttachments(rows), nil
}

// DeleteUnclaimedAttachment forgets an upload no message took, and returns
// it so its files can go too. One a message took meanwhile is ErrNotFound.
func (s *Store) DeleteUnclaimedAttachment(ctx context.Context, id string) (Attachment, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Attachment{}, err
	}
	row, err := s.q.DeleteUnclaimedAttachment(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Attachment{}, fmt.Errorf("unclaimed attachment %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Attachment{}, fmt.Errorf("delete attachment %s: %w", id, err)
	}
	return toAttachment(row), nil
}
