package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
)

// MessageStore reads messages and threads. Writing goes through Chat.
type MessageStore interface {
	GetMessage(ctx context.Context, id string) (store.Message, error)
	ListRoomMessages(ctx context.Context, roomID string, after int64, limit int) ([]store.Message, error)
	ListRoomMessagesBefore(ctx context.Context, roomID string, before int64, limit int) ([]store.Message, error)
	ListThreadMessages(ctx context.Context, threadID string, after int64, limit int) ([]store.Message, error)
	ThreadForMessage(ctx context.Context, messageID string) (store.Thread, error)
	GetThread(ctx context.Context, id string) (store.Thread, error)
	ThreadSummaries(ctx context.Context, rootMessageIDs []string) (map[string]store.ThreadSummary, error)
	ListUserMentions(ctx context.Context, userID string, before int64, limit int) ([]store.InboxItem, error)
	CountUnreadMentions(ctx context.Context, userID string) (int, error)
	// ChainWork is a piece of work across its topics.
	ChainWork(ctx context.Context, chain string) (store.WorkSummary, error)
}

// InboxResponse is the body of GET /api/v1/users/{id}/inbox: messages that
// mention the user, newest first, and how many of all of them the user has
// not read.
type InboxResponse struct {
	Items  []store.InboxItem `json:"items"`
	Unread int               `json:"unread"`
}

// InboxReadResponse answers POST /api/v1/users/{id}/inbox/read: how many
// were marked, and how many are left unread.
type InboxReadResponse struct {
	Marked int `json:"marked"`
	Unread int `json:"unread"`
}

// PostMessageRequest is the body of POST /api/v1/rooms/{id}/messages.
//
// The signed-in user is the sender; user_id in the body counts only when
// the API runs without sign-in (tests).
// At most one of ThreadID and ReplyTo may be set: ThreadID posts into an
// existing thread, ReplyTo starts or continues the thread of a message.
type PostMessageRequest struct {
	UserID   string          `json:"user_id"`
	Body     string          `json:"body"`
	Mentions []store.Mention `json:"mentions"`
	// AttachmentIDs are uploads (POST /rooms/{id}/attachments) the message
	// carries. A message may be attachments alone.
	AttachmentIDs []string `json:"attachment_ids"`
	ThreadID      string   `json:"thread_id"`
	ReplyTo       string   `json:"reply_to"`
}

// MessageResponse is the body of single-message endpoints.
type MessageResponse struct {
	Message store.Message `json:"message"`
}

// MessagesResponse is the body of thread message listings, oldest first.
type MessagesResponse struct {
	Messages []store.Message `json:"messages"`
}

// RoomMessage is a top-level message as the room timeline lists it: the
// message plus, when a topic hangs off it, that topic's summary.
type RoomMessage struct {
	store.Message
	Thread *store.ThreadSummary `json:"thread,omitempty"`
}

// RoomMessagesResponse is the body of GET /api/v1/rooms/{id}/messages,
// oldest first.
type RoomMessagesResponse struct {
	Messages []RoomMessage `json:"messages"`
}

// ThreadResponse is the body of GET /api/v1/threads/{id}: the thread, the
// message it is rooted at and its turns, oldest first. Replies come from
// the messages sub-resource.
type ThreadResponse struct {
	Thread store.Thread  `json:"thread"`
	Root   store.Message `json:"root"`
	Turns  []store.Turn  `json:"turns"`
	// Work is the piece of work the topic's latest turn is part of, which
	// may have begun in another topic.
	Work *store.WorkSummary `json:"work,omitempty"`
}

func (h *handlers) postMessage(w http.ResponseWriter, r *http.Request) {
	var req PostMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	if user, ok := userFrom(r.Context()); ok {
		req.UserID = user.ID
	}
	if err := validatePostMessage(req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}

	threadID := req.ThreadID
	if req.ReplyTo != "" {
		thread, err := h.deps.Messages.ThreadForMessage(r.Context(), req.ReplyTo)
		if err != nil {
			h.writeStoreError(w, r, err)
			return
		}
		threadID = thread.ID
	}

	msg, err := h.deps.Chat.PostUserMessage(r.Context(), store.NewMessage{
		RoomID:        r.PathValue("id"),
		ThreadID:      threadID,
		UserID:        req.UserID,
		Body:          req.Body,
		Mentions:      req.Mentions,
		AttachmentIDs: req.AttachmentIDs,
	})
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, MessageResponse{Message: msg})
}

// validatePostMessage checks what the store cannot: field presence and the
// shape of mentions.
func validatePostMessage(req PostMessageRequest) error {
	if strings.TrimSpace(req.UserID) == "" {
		return fmt.Errorf("user_id is required")
	}
	if strings.TrimSpace(req.Body) == "" && len(req.AttachmentIDs) == 0 {
		return fmt.Errorf("body or attachment_ids is required")
	}
	for i, id := range req.AttachmentIDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("attachment_ids[%d] is required", i)
		}
	}
	if req.ThreadID != "" && req.ReplyTo != "" {
		return fmt.Errorf("thread_id and reply_to are mutually exclusive")
	}
	for i, m := range req.Mentions {
		if m.Kind != store.MentionUser && m.Kind != store.MentionAgent {
			return fmt.Errorf("mentions[%d].kind must be %q or %q", i, store.MentionUser, store.MentionAgent)
		}
		if strings.TrimSpace(m.ID) == "" {
			return fmt.Errorf("mentions[%d].id is required", i)
		}
	}
	return nil
}

func (h *handlers) listRoomMessages(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	// Confirm the room exists so an unknown id is a 404, not an empty page.
	if _, err := h.deps.Projects.GetRoom(r.Context(), roomID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	page, err := parsePage(r)
	if err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}

	var messages []store.Message
	if page.hasBefore {
		messages, err = h.deps.Messages.ListRoomMessagesBefore(r.Context(), roomID, page.before, page.limit)
	} else {
		messages, err = h.deps.Messages.ListRoomMessages(r.Context(), roomID, page.after, page.limit)
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	ids := make([]string, len(messages))
	for i, m := range messages {
		ids[i] = m.ID
	}
	summaries, err := h.deps.Messages.ThreadSummaries(r.Context(), ids)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	out := make([]RoomMessage, len(messages))
	for i, m := range messages {
		out[i] = RoomMessage{Message: m}
		if summary, ok := summaries[m.ID]; ok {
			out[i].Thread = &summary
		}
	}
	writeJSON(w, http.StatusOK, RoomMessagesResponse{Messages: out})
}

// userInbox lists what mentions a user across rooms. Only before= pages
// it, newest first, since an inbox is read from the top.
func (h *handlers) userInbox(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if _, err := h.deps.Users.GetUser(r.Context(), userID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	page, err := parsePage(r)
	if err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	items, err := h.deps.Messages.ListUserMentions(r.Context(), userID, page.before, page.limit)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	unread, err := h.deps.Messages.CountUnreadMentions(r.Context(), userID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.fillSenderNames(r.Context(), items)
	writeJSON(w, http.StatusOK, InboxResponse{Items: items, Unread: unread})
}

// readInbox marks read what the body picks of what mentions a user: the
// messages named, those in a topic, or all up to a seq (docs/webui.md 4.19).
func (h *handlers) readInbox(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if user, ok := userFrom(r.Context()); ok && user.ID != userID {
		writeError(w, http.StatusForbidden, "a person marks only their own inbox read")
		return
	}
	var read store.InboxRead
	if err := decodeJSON(r, &read); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	marked, err := h.deps.Chat.MarkInboxRead(r.Context(), userID, read)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	unread, err := h.deps.Messages.CountUnreadMentions(r.Context(), userID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, InboxReadResponse{Marked: marked, Unread: unread})
}

func (h *handlers) getMessage(w http.ResponseWriter, r *http.Request) {
	msg, err := h.deps.Messages.GetMessage(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MessageResponse{Message: msg})
}

func (h *handlers) getThread(w http.ResponseWriter, r *http.Request) {
	thread, err := h.deps.Messages.GetThread(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	root, err := h.deps.Messages.GetMessage(r.Context(), thread.RootMessageID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	turns, err := h.deps.Turns.ListThreadTurns(r.Context(), thread.ID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	res := ThreadResponse{Thread: thread, Root: root, Turns: turns}
	if len(turns) > 0 && turns[len(turns)-1].ChainMessageID != "" {
		work, err := h.deps.Messages.ChainWork(r.Context(), turns[len(turns)-1].ChainMessageID)
		switch {
		case err == nil:
			res.Work = &work
		case !errors.Is(err, store.ErrNotFound):
			h.writeStoreError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *handlers) listThreadMessages(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("id")
	if _, err := h.deps.Messages.GetThread(r.Context(), threadID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	page, err := parsePage(r)
	if err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	if page.hasBefore {
		writeError(w, http.StatusBadRequest, "before is not supported for threads; use after")
		return
	}

	messages, err := h.deps.Messages.ListThreadMessages(r.Context(), threadID, page.after, page.limit)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MessagesResponse{Messages: messages})
}

// page is a parsed message-listing cursor: ?after=SEQ or ?before=SEQ, plus
// ?limit=N. The store applies the default and maximum limit.
type page struct {
	after     int64
	before    int64
	hasBefore bool
	limit     int
}

func parsePage(r *http.Request) (page, error) {
	q := r.URL.Query()
	var p page

	after, hasAfter, err := queryInt(q.Get("after"))
	if err != nil {
		return p, fmt.Errorf("after: %w", err)
	}
	before, hasBefore, err := queryInt(q.Get("before"))
	if err != nil {
		return p, fmt.Errorf("before: %w", err)
	}
	if hasAfter && hasBefore {
		return p, fmt.Errorf("after and before are mutually exclusive")
	}
	limit, _, err := queryInt(q.Get("limit"))
	if err != nil {
		return p, fmt.Errorf("limit: %w", err)
	}

	p.after, p.before, p.hasBefore, p.limit = after, before, hasBefore, int(limit)
	return p, nil
}

// queryInt parses an optional non-negative integer query value.
func queryInt(raw string) (value int64, present bool, err error) {
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0, true, fmt.Errorf("must be a non-negative integer, got %q", raw)
	}
	return n, true, nil
}

// fillSenderNames names senders the listing's join could not: the account
// is not in the users table, so its messages arrive nameless.
func (h *handlers) fillSenderNames(ctx context.Context, items []store.InboxItem) {
	names := map[string]string{}
	for i := range items {
		if items[i].SenderName != "" || items[i].UserID == "" {
			continue
		}
		name, seen := names[items[i].UserID]
		if !seen {
			if u, err := h.deps.Users.GetUser(ctx, items[i].UserID); err == nil {
				name = u.Name
			}
			names[items[i].UserID] = name
		}
		items[i].SenderName = name
	}
}
