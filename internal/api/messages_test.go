package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
)

// fakeUsers is an in-memory UserStore with ids "u1", "u2", ...
type fakeUsers struct{ users map[string]store.User }

func newFakeUsers() *fakeUsers { return &fakeUsers{users: map[string]store.User{}} }

func (f *fakeUsers) CreateUser(_ context.Context, name string) (store.User, error) {
	u := store.User{ID: fmt.Sprintf("u%d", len(f.users)+1), Name: name}
	f.users[u.ID] = u
	return u, nil
}

func (f *fakeUsers) GetUser(_ context.Context, id string) (store.User, error) {
	if err := checkID(id); err != nil {
		return store.User{}, err
	}
	u, ok := f.users[id]
	if !ok {
		return store.User{}, fmt.Errorf("user %s: %w", id, store.ErrNotFound)
	}
	return u, nil
}

func (f *fakeUsers) ListUsers(context.Context) ([]store.User, error) {
	out := []store.User{}
	for _, u := range f.users {
		out = append(out, u)
	}
	return out, nil
}

// fakeMessages is an in-memory MessageStore that mirrors the real store's
// rules closely enough to exercise the handlers: room and user existence,
// top-level versus thread listings, cursors, and get-or-create threads.
type fakeMessages struct {
	rooms    *fakeProjects
	users    *fakeUsers
	messages []store.Message
	threads  map[string]store.Thread
}

func newFakeMessages(rooms *fakeProjects, users *fakeUsers) *fakeMessages {
	return &fakeMessages{rooms: rooms, users: users, threads: map[string]store.Thread{}}
}

// fakeChat stands in for the hub: it stores the message and remembers what
// was cancelled.
type fakeChat struct {
	messages   *fakeMessages
	running    map[string]bool
	cancelled  []string
	approvals  fakeApprovals
	decided    []string
	sub        *fakeSub
	subscribed []string
}

func (c *fakeChat) PostUserMessage(ctx context.Context, m store.NewMessage) (store.Message, error) {
	m.SenderKind = store.SenderUser
	return c.messages.CreateMessage(ctx, m)
}

func (c *fakeChat) CancelTurn(_ context.Context, turnID string) error {
	if !c.running[turnID] {
		return fmt.Errorf("%w: %s", hub.ErrUnknownTurn, turnID)
	}
	c.cancelled = append(c.cancelled, turnID)
	return nil
}

func (f *fakeMessages) CreateMessage(ctx context.Context, m store.NewMessage) (store.Message, error) {
	if _, err := f.rooms.GetRoom(ctx, m.RoomID); err != nil {
		return store.Message{}, err
	}
	if _, err := f.users.GetUser(ctx, m.UserID); err != nil {
		return store.Message{}, err
	}
	if m.ThreadID != "" {
		if _, err := f.GetThread(ctx, m.ThreadID); err != nil {
			return store.Message{}, err
		}
	}
	msg := store.Message{
		ID: fmt.Sprintf("m%d", len(f.messages)+1), Seq: int64(len(f.messages) + 1),
		Room: m.RoomID, ThreadID: m.ThreadID, SenderKind: m.SenderKind, UserID: m.UserID,
		Body: m.Body, Mentions: m.Mentions,
	}
	if msg.Mentions == nil {
		msg.Mentions = []store.Mention{}
	}
	f.messages = append(f.messages, msg)
	return msg, nil
}

func (f *fakeMessages) GetMessage(_ context.Context, id string) (store.Message, error) {
	if err := checkID(id); err != nil {
		return store.Message{}, err
	}
	for _, m := range f.messages {
		if m.ID == id {
			return m, nil
		}
	}
	return store.Message{}, fmt.Errorf("message %s: %w", id, store.ErrNotFound)
}

func (f *fakeMessages) list(keep func(store.Message) bool, limit int) []store.Message {
	out := []store.Message{}
	for _, m := range f.messages {
		if keep(m) {
			out = append(out, m)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (f *fakeMessages) ListRoomMessages(_ context.Context, roomID string, after int64, limit int) ([]store.Message, error) {
	return f.list(func(m store.Message) bool { return m.Room == roomID && m.ThreadID == "" && m.Seq > after }, limit), nil
}

func (f *fakeMessages) ListRoomMessagesBefore(_ context.Context, roomID string, before int64, limit int) ([]store.Message, error) {
	all := f.list(func(m store.Message) bool {
		return m.Room == roomID && m.ThreadID == "" && (before <= 0 || m.Seq < before)
	}, 0)
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, nil
}

func (f *fakeMessages) ListThreadMessages(_ context.Context, threadID string, after int64, limit int) ([]store.Message, error) {
	return f.list(func(m store.Message) bool { return m.ThreadID == threadID && m.Seq > after }, limit), nil
}

func (f *fakeMessages) ThreadForMessage(ctx context.Context, messageID string) (store.Thread, error) {
	msg, err := f.GetMessage(ctx, messageID)
	if err != nil {
		return store.Thread{}, err
	}
	if msg.ThreadID != "" {
		return f.threads[msg.ThreadID], nil
	}
	for _, t := range f.threads {
		if t.RootMessageID == msg.ID {
			return t, nil
		}
	}
	t := store.Thread{ID: fmt.Sprintf("t%d", len(f.threads)+1), RoomID: msg.Room, RootMessageID: msg.ID}
	f.threads[t.ID] = t
	return t, nil
}

func (f *fakeMessages) GetThread(_ context.Context, id string) (store.Thread, error) {
	if err := checkID(id); err != nil {
		return store.Thread{}, err
	}
	t, ok := f.threads[id]
	if !ok {
		return store.Thread{}, fmt.Errorf("thread %s: %w", id, store.ErrNotFound)
	}
	return t, nil
}

// chatHandler wires all fakes together with one project, its main room and
// one user already present.
func chatHandler(t *testing.T) (http.Handler, store.Room, store.User, *fakeMessages) {
	t.Helper()
	projects := newFakeProjects()
	users := newFakeUsers()
	messages := newFakeMessages(projects, users)
	_, room, _ := projects.CreateProject(context.Background(), store.NewProject{Name: "p"})
	user, _ := users.CreateUser(context.Background(), "alice")
	chat := &fakeChat{messages: messages, running: map[string]bool{}}
	handler := NewHandler(Deps{Projects: projects, Users: users, Messages: messages, Chat: chat})
	return handler, room, user, messages
}

func TestUsers_CreateGetList(t *testing.T) {
	handler, _, _, _ := chatHandler(t)

	var created UserResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/users", `{"name":"bob"}`, &created); rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d; body: %s", rec.Code, rec.Body)
	}
	var one UserResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/users/"+created.User.ID, "", &one); rec.Code != http.StatusOK || one.User.Name != "bob" {
		t.Errorf("get: status = %d, user = %+v", rec.Code, one.User)
	}
	var all UsersResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/users", "", &all); rec.Code != http.StatusOK || len(all.Users) != 2 {
		t.Errorf("list: status = %d, users = %+v", rec.Code, all.Users)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/users", `{"name":" "}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: status = %d, want 400", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/users/u404", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown user: status = %d, want 404", rec.Code)
	}
}

func TestPostMessage_TopLevel(t *testing.T) {
	handler, room, user, _ := chatHandler(t)
	body := fmt.Sprintf(`{"user_id":%q,"body":"hello","mentions":[{"kind":"agent","id":"a1"}]}`, user.ID)

	var resp MessageResponse
	rec := do(t, handler, http.MethodPost, "/api/v1/rooms/"+room.ID+"/messages", body, &resp)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body)
	}
	m := resp.Message
	if m.Room != room.ID || m.ThreadID != "" || m.UserID != user.ID || m.SenderKind != store.SenderUser || len(m.Mentions) != 1 {
		t.Errorf("unexpected message: %+v", m)
	}
}

func TestPostMessage_ReplyToStartsAndContinuesThread(t *testing.T) {
	handler, room, user, _ := chatHandler(t)
	path := "/api/v1/rooms/" + room.ID + "/messages"

	var root MessageResponse
	do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"user_id":%q,"body":"root"}`, user.ID), &root)

	var reply MessageResponse
	rec := do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"user_id":%q,"body":"reply","reply_to":%q}`, user.ID, root.Message.ID), &reply)
	if rec.Code != http.StatusCreated || reply.Message.ThreadID == "" {
		t.Fatalf("reply_to should create a thread: status = %d, message = %+v", rec.Code, reply.Message)
	}

	// Replying to the reply lands in the same thread.
	var second MessageResponse
	do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"user_id":%q,"body":"again","reply_to":%q}`, user.ID, reply.Message.ID), &second)
	if second.Message.ThreadID != reply.Message.ThreadID {
		t.Errorf("reply to a reply went to thread %q, want %q", second.Message.ThreadID, reply.Message.ThreadID)
	}

	// So does posting with the thread id directly.
	var third MessageResponse
	do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"user_id":%q,"body":"direct","thread_id":%q}`, user.ID, reply.Message.ThreadID), &third)
	if third.Message.ThreadID != reply.Message.ThreadID {
		t.Errorf("thread_id post went to %q", third.Message.ThreadID)
	}

	// The thread endpoint returns the root; its messages are the replies.
	var thread ThreadResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/threads/"+reply.Message.ThreadID, "", &thread); rec.Code != http.StatusOK || thread.Root.ID != root.Message.ID {
		t.Errorf("thread: status = %d, root = %+v", rec.Code, thread.Root)
	}
	var replies MessagesResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/threads/"+reply.Message.ThreadID+"/messages", "", &replies); rec.Code != http.StatusOK || len(replies.Messages) != 3 {
		t.Errorf("replies: status = %d, count = %d", rec.Code, len(replies.Messages))
	}
}

func TestPostMessage_BadRequests(t *testing.T) {
	handler, room, user, _ := chatHandler(t)
	path := "/api/v1/rooms/" + room.ID + "/messages"

	cases := map[string]struct {
		body string
		want int
	}{
		"missing user":     {`{"body":"x"}`, http.StatusBadRequest},
		"blank body":       {fmt.Sprintf(`{"user_id":%q,"body":" "}`, user.ID), http.StatusBadRequest},
		"bad mention kind": {fmt.Sprintf(`{"user_id":%q,"body":"x","mentions":[{"kind":"robot","id":"1"}]}`, user.ID), http.StatusBadRequest},
		"both thread refs": {fmt.Sprintf(`{"user_id":%q,"body":"x","thread_id":"t1","reply_to":"m1"}`, user.ID), http.StatusBadRequest},
		"unknown user":     {`{"user_id":"u404","body":"x"}`, http.StatusNotFound},
		"unknown reply_to": {fmt.Sprintf(`{"user_id":%q,"body":"x","reply_to":"m404"}`, user.ID), http.StatusNotFound},
		"unknown thread":   {fmt.Sprintf(`{"user_id":%q,"body":"x","thread_id":"t404"}`, user.ID), http.StatusNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if rec := do(t, handler, http.MethodPost, path, tc.body, nil); rec.Code != tc.want {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, tc.want, rec.Body)
			}
		})
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/rooms/r404/messages", fmt.Sprintf(`{"user_id":%q,"body":"x"}`, user.ID), nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown room: status = %d, want 404", rec.Code)
	}
}

func TestListRoomMessages_Cursors(t *testing.T) {
	handler, room, user, _ := chatHandler(t)
	path := "/api/v1/rooms/" + room.ID + "/messages"
	var seqs []int64
	for i := 1; i <= 4; i++ {
		var resp MessageResponse
		do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"user_id":%q,"body":"m%d"}`, user.ID, i), &resp)
		seqs = append(seqs, resp.Message.Seq)
	}
	// A reply must not appear in the timeline.
	do(t, handler, http.MethodPost, path, fmt.Sprintf(`{"user_id":%q,"body":"reply","reply_to":"m1"}`, user.ID), nil)

	var all MessagesResponse
	if rec := do(t, handler, http.MethodGet, path, "", &all); rec.Code != http.StatusOK || len(all.Messages) != 4 {
		t.Errorf("default page: status = %d, count = %d", rec.Code, len(all.Messages))
	}
	var after MessagesResponse
	if rec := do(t, handler, http.MethodGet, fmt.Sprintf("%s?after=%d", path, seqs[1]), "", &after); rec.Code != http.StatusOK || len(after.Messages) != 2 || after.Messages[0].Body != "m3" {
		t.Errorf("after: status = %d, messages = %+v", rec.Code, after.Messages)
	}
	var before MessagesResponse
	if rec := do(t, handler, http.MethodGet, fmt.Sprintf("%s?before=%d&limit=1", path, seqs[3]), "", &before); rec.Code != http.StatusOK || len(before.Messages) != 1 || before.Messages[0].Body != "m3" {
		t.Errorf("before: status = %d, messages = %+v", rec.Code, before.Messages)
	}

	for _, q := range []string{"?after=1&before=2", "?after=-1", "?limit=x"} {
		if rec := do(t, handler, http.MethodGet, path+q, "", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", q, rec.Code)
		}
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/r404/messages", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown room: status = %d, want 404", rec.Code)
	}
}

func TestGetMessageAndThread_NotFound(t *testing.T) {
	handler, _, _, _ := chatHandler(t)

	if rec := do(t, handler, http.MethodGet, "/api/v1/messages/m404", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("message: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/threads/t404", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("thread: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/threads/t404/messages", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("thread messages: status = %d, want 404", rec.Code)
	}
}
