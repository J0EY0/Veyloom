package api

import (
	"context"
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
}

// PostMessageRequest is the body of POST /api/v1/rooms/{id}/messages.
//
// There is no authentication yet, so the client names the sending user.
// At most one of ThreadID and ReplyTo may be set: ThreadID posts into an
// existing thread, ReplyTo starts or continues the thread of a message.
type PostMessageRequest struct {
	UserID   string          `json:"user_id"`
	Body     string          `json:"body"`
	Mentions []store.Mention `json:"mentions"`
	ThreadID string          `json:"thread_id"`
	ReplyTo  string          `json:"reply_to"`
}

// MessageResponse is the body of single-message endpoints.
type MessageResponse struct {
	Message store.Message `json:"message"`
}

// MessagesResponse is the body of message listings, oldest first.
type MessagesResponse struct {
	Messages []store.Message `json:"messages"`
}

// ThreadResponse is the body of GET /api/v1/threads/{id}: the thread and
// the message it is rooted at. Replies come from the messages sub-resource.
type ThreadResponse struct {
	Thread store.Thread  `json:"thread"`
	Root   store.Message `json:"root"`
}

func (h *handlers) postMessage(w http.ResponseWriter, r *http.Request) {
	var req PostMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validatePostMessage(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
		RoomID:   r.PathValue("id"),
		ThreadID: threadID,
		UserID:   req.UserID,
		Body:     req.Body,
		Mentions: req.Mentions,
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
	if strings.TrimSpace(req.Body) == "" {
		return fmt.Errorf("body is required")
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
		writeError(w, http.StatusBadRequest, err.Error())
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
	writeJSON(w, http.StatusOK, MessagesResponse{Messages: messages})
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
	writeJSON(w, http.StatusOK, ThreadResponse{Thread: thread, Root: root})
}

func (h *handlers) listThreadMessages(w http.ResponseWriter, r *http.Request) {
	threadID := r.PathValue("id")
	if _, err := h.deps.Messages.GetThread(r.Context(), threadID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	page, err := parsePage(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
