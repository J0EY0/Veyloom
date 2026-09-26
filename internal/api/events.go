package api

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/J0EY0/veyloom/internal/hub"
)

// EventsOptions tunes the WebSocket event stream.
type EventsOptions struct {
	// AllowedOrigins lists host patterns of browser origins allowed to call
	// the API from another origin (CORS) and to
	// connect cross-origin. Same-origin requests and clients that send no
	// Origin header are always accepted.
	AllowedOrigins []string
	// WriteTimeout bounds each event write; zero means unbounded.
	WriteTimeout time.Duration
}

// roomEvents upgrades GET /api/v1/rooms/{id}/events to a WebSocket and
// streams the room's live events as JSON, one hub.Event per message. The
// stream is one-way: a client that sends data is disconnected. A client
// too slow to keep up is closed with StatusPolicyViolation and should
// reload over REST before reconnecting.
func (h *handlers) roomEvents(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	if _, err := h.deps.Projects.GetRoom(r.Context(), roomID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.stream(w, r, func() hub.Subscription { return h.deps.Chat.Subscribe(roomID) })
}

// inboxEvents upgrades GET /api/v1/users/{id}/inbox/events to a WebSocket
// and streams what reaches the user's inbox as it happens, in every
// project: the messages mentioning them, and the requests waiting for a
// person as they are asked and decided. Otherwise it is roomEvents.
func (h *handlers) inboxEvents(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("id")
	if _, err := h.deps.Users.GetUser(r.Context(), userID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.stream(w, r, func() hub.Subscription { return h.deps.Chat.SubscribeInbox(userID) })
}

// stream upgrades the request to a WebSocket and writes the events of the
// subscription it opens, one hub.Event per message, until either side
// goes away.
func (h *handlers) stream(w http.ResponseWriter, r *http.Request, subscribe func() hub.Subscription) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.deps.Events.AllowedOrigins})
	if err != nil {
		// Accept has already answered with the reason.
		return
	}
	defer conn.CloseNow()

	sub := subscribe()
	defer sub.Close()

	// CloseRead keeps reading so pings are answered and a closed peer is
	// noticed; its context ends when the connection does.
	ctx := conn.CloseRead(r.Context())
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Events():
			if !ok {
				if sub.Lagged() {
					_ = conn.Close(websocket.StatusPolicyViolation, "too slow: events were dropped, resync over REST")
				} else {
					_ = conn.Close(websocket.StatusNormalClosure, "")
				}
				return
			}
			if err := h.writeEvent(ctx, conn, ev); err != nil {
				return
			}
		}
	}
}

// writeEvent sends one event within the configured write timeout.
func (h *handlers) writeEvent(ctx context.Context, conn *websocket.Conn, ev any) error {
	if h.deps.Events.WriteTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.deps.Events.WriteTimeout)
		defer cancel()
	}
	return wsjson.Write(ctx, conn, ev)
}
