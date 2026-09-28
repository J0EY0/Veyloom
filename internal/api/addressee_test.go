package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
)

// Where a message that names nobody would go, in a room or its topic
// (docs/design.md 4.2), for the person signed in: whom they talk with in a
// topic is theirs.
func TestRooms_Addressee(t *testing.T) {
	ctx := context.Background()
	projects := newFakeProjects()
	users := newFakeUsers()
	messages := newFakeMessages(projects, users)
	_, room, _ := projects.CreateProject(ctx, store.NewProject{Name: "p"})
	_, other, _ := projects.CreateProject(ctx, store.NewProject{Name: "q"})
	user, _ := users.CreateUser(ctx, "alice")
	chat := &fakeChat{messages: messages, addressee: hub.Addressee{Member: store.Member{ID: "m1"}, Reason: hub.AddresseeLeader}}
	auth := newFakeAuth()
	token := auth.open(user)
	handler := NewHandler(Deps{Projects: projects, Users: users, Messages: messages, Turns: fakeTurns{}, Chat: chat, Auth: auth})
	do := func(path string, out any) *httptest.ResponseRecorder {
		t.Helper()
		rec := send(t, handler, http.MethodGet, path, "", token)
		if out != nil && rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
		}
		return rec
	}
	topicOf := func(roomID string) store.Thread {
		t.Helper()
		msg, err := messages.CreateMessage(ctx, store.NewMessage{RoomID: roomID, SenderKind: store.SenderUser, UserID: user.ID, Body: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		thread, err := messages.ThreadForMessage(ctx, msg.ID)
		if err != nil {
			t.Fatal(err)
		}
		return thread
	}
	topic, elsewhere := topicOf(room.ID), topicOf(other.ID)
	const nowhere = "00000000-0000-0000-0000-000000000000"

	var got AddresseeResponse
	if rec := do("/api/v1/rooms/"+room.ID+"/addressee", &got); rec.Code != http.StatusOK || got.MemberID != "m1" || got.Reason != hub.AddresseeLeader {
		t.Errorf("the room: %d %+v", rec.Code, got)
	}
	if rec := do("/api/v1/rooms/"+room.ID+"/addressee?thread_id="+topic.ID, nil); rec.Code != http.StatusOK {
		t.Errorf("a topic: %d", rec.Code)
	}
	if want := []string{room.ID + "/ for " + user.ID, room.ID + "/" + topic.ID + " for " + user.ID}; strings.Join(chat.addressed, "|") != strings.Join(want, "|") {
		t.Errorf("asked about %v, want %v", chat.addressed, want)
	}
	// Nobody takes it: no member named.
	chat.addressee = hub.Addressee{Reason: hub.AddresseeNone}
	if rec := do("/api/v1/rooms/"+room.ID+"/addressee", nil); rec.Code != http.StatusOK || rec.Body.String() != "{\"reason\":\"none\"}\n" {
		t.Errorf("nobody: %d %s", rec.Code, rec.Body)
	}
	for name, path := range map[string]string{
		"unknown room":         "/api/v1/rooms/" + nowhere + "/addressee",
		"another room's topic": "/api/v1/rooms/" + room.ID + "/addressee?thread_id=" + elsewhere.ID,
		"unknown topic":        "/api/v1/rooms/" + room.ID + "/addressee?thread_id=" + nowhere,
	} {
		if rec := do(path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", name, rec.Code)
		}
	}
}
