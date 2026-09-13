package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
)

// fakeSub is an in-memory hub.Subscription the test feeds by hand.
type fakeSub struct {
	ch     chan hub.Event
	lagged bool
	closed chan struct{}
}

func newFakeSub() *fakeSub {
	return &fakeSub{ch: make(chan hub.Event, 8), closed: make(chan struct{})}
}

func (s *fakeSub) Events() <-chan hub.Event { return s.ch }
func (s *fakeSub) Lagged() bool             { return s.lagged }
func (s *fakeSub) Close() {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
}

func (c *fakeChat) Subscribe(roomID string) hub.Subscription {
	c.subscribed = append(c.subscribed, roomID)
	return c.sub
}

func eventsServer(t *testing.T, opts EventsOptions) (*httptest.Server, store.Room, *fakeChat) {
	t.Helper()
	projects := newFakeProjects()
	_, room, _ := projects.CreateProject(context.Background(), store.NewProject{Name: "p"})
	chat := &fakeChat{sub: newFakeSub()}
	srv := httptest.NewServer(NewHandler(Deps{Projects: projects, Chat: chat, Events: opts}))
	t.Cleanup(srv.Close)
	return srv, room, chat
}

func wsURL(srv *httptest.Server, path string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + path
}

func TestEvents_StreamsRoomEvents(t *testing.T) {
	srv, room, chat := eventsServer(t, EventsOptions{WriteTimeout: time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/rooms/"+room.ID+"/events"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	msg := store.Message{ID: "m1", Room: room.ID, Body: "hello"}
	chat.sub.ch <- hub.Event{Kind: hub.EventMessage, RoomID: room.ID, Message: &msg}
	chat.sub.ch <- hub.Event{Kind: hub.EventTurnStarted, RoomID: room.ID, Turn: &store.Turn{ID: "t1", Status: store.TurnRunning}}

	var first, second hub.Event
	if err := wsjson.Read(ctx, conn, &first); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, conn, &second); err != nil {
		t.Fatal(err)
	}
	if first.Kind != hub.EventMessage || first.Message == nil || first.Message.Body != "hello" {
		t.Errorf("first event = %+v", first)
	}
	if second.Kind != hub.EventTurnStarted || second.Turn == nil || second.Turn.ID != "t1" {
		t.Errorf("second event = %+v", second)
	}
	if len(chat.subscribed) != 1 || chat.subscribed[0] != room.ID {
		t.Errorf("subscribed to %v, want the room", chat.subscribed)
	}

	// The client leaving releases the subscription.
	conn.Close(websocket.StatusNormalClosure, "bye")
	select {
	case <-chat.sub.closed:
	case <-time.After(2 * time.Second):
		t.Error("the subscription should be closed when the client disconnects")
	}
}

func TestEvents_LaggedClientIsToldToResync(t *testing.T) {
	srv, room, chat := eventsServer(t, EventsOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/rooms/"+room.ID+"/events"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()

	// The broker dropping a subscriber looks like this: lagged, then closed.
	chat.sub.lagged = true
	close(chat.sub.ch)

	var ev hub.Event
	err = wsjson.Read(ctx, conn, &ev)
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) || closeErr.Code != websocket.StatusPolicyViolation || !strings.Contains(closeErr.Reason, "resync") {
		t.Errorf("expected a policy-violation close asking to resync, got %v", err)
	}
}

func TestEvents_UnknownRoomAndOrigins(t *testing.T) {
	srv, room, _ := eventsServer(t, EventsOptions{AllowedOrigins: []string{"ui.example.com"}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, res, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/rooms/r404/events"), nil); err == nil || res == nil || res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown room: err %v, status %v, want 404", err, res)
	}

	allowed := &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http://ui.example.com"}}}
	if conn, _, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/rooms/"+room.ID+"/events"), allowed); err != nil {
		t.Errorf("allowed origin should connect: %v", err)
	} else {
		conn.CloseNow()
	}
	denied := &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"http://evil.example.com"}}}
	if _, res, err := websocket.Dial(ctx, wsURL(srv, "/api/v1/rooms/"+room.ID+"/events"), denied); err == nil || res == nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("foreign origin: err %v, status %v, want 403", err, res)
	}
}
