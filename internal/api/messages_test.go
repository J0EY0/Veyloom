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

func (f *fakeUsers) RenameUser(_ context.Context, id, name string) (store.User, error) {
	u, ok := f.users[id]
	if !ok {
		return store.User{}, fmt.Errorf("user %s: %w", id, store.ErrNotFound)
	}
	u.Name = name
	f.users[id] = u
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
	// work is the pieces of work, by the message that began each.
	work map[string]store.WorkSummary
	// read are the messages read in their inbox, by user and message.
	read map[string]bool
}

func newFakeMessages(rooms *fakeProjects, users *fakeUsers) *fakeMessages {
	return &fakeMessages{rooms: rooms, users: users, threads: map[string]store.Thread{}, read: map[string]bool{}}
}

// fakeChat stands in for the hub: it stores the message and remembers what
// was cancelled.
type fakeChat struct {
	messages   *fakeMessages
	posted     []store.NewMessage
	running    map[string]bool
	cancelled  []string
	continued  []string
	approvals  fakeApprovals
	decided    []string
	untrusted  []string
	sub        *fakeSub
	subscribed []string
}

func (c *fakeChat) PostUserMessage(ctx context.Context, m store.NewMessage) (store.Message, error) {
	m.SenderKind = store.SenderUser
	c.posted = append(c.posted, m)
	return c.messages.CreateMessage(ctx, m)
}

func (c *fakeChat) ContinueRelay(_ context.Context, noteID string) error {
	if noteID == "gone" {
		return store.ErrNotFound
	}
	c.continued = append(c.continued, noteID)
	return nil
}

func (c *fakeChat) CancelTurn(_ context.Context, turnID string) error {
	if !c.running[turnID] {
		return fmt.Errorf("%w: %s", hub.ErrUnknownTurn, turnID)
	}
	c.cancelled = append(c.cancelled, turnID)
	return nil
}

func (c *fakeChat) UntrustTurn(_ context.Context, turnID string) (store.Turn, error) {
	if !c.running[turnID] {
		return store.Turn{}, fmt.Errorf("%w: %s", hub.ErrUnknownTurn, turnID)
	}
	c.untrusted = append(c.untrusted, turnID)
	return store.Turn{ID: turnID, Status: store.TurnRunning}, nil
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

// ListUserMentions filters by the structured mentions, newest first.
func (f *fakeMessages) CountUnreadMentions(ctx context.Context, userID string) (int, error) {
	items, err := f.ListUserMentions(ctx, userID, 0, 0)
	n := 0
	for _, it := range items {
		if !it.Read {
			n++
		}
	}
	return n, err
}

// MarkInboxRead marks the messages named read; the fake knows no topics.
func (c *fakeChat) MarkInboxRead(_ context.Context, userID string, read store.InboxRead) (int, error) {
	n := 0
	for _, id := range read.MessageIDs {
		if key := userID + "/" + id; !c.messages.read[key] {
			c.messages.read[key] = true
			n++
		}
	}
	return n, nil
}

func (f *fakeMessages) ListUserMentions(_ context.Context, userID string, before int64, limit int) ([]store.InboxItem, error) {
	if _, ok := f.users.users[userID]; !ok {
		return nil, fmt.Errorf("user %s: %w", userID, store.ErrNotFound)
	}
	var out []store.InboxItem
	for i := len(f.messages) - 1; i >= 0; i-- {
		m := f.messages[i]
		if before > 0 && m.Seq >= before {
			continue
		}
		for _, mention := range m.Mentions {
			if mention.Kind == store.MentionUser && mention.ID == userID {
				out = append(out, store.InboxItem{Message: m, RoomName: "main", SenderName: "someone", Read: f.read[userID+"/"+m.ID]})
				break
			}
		}
		if limit > 0 && len(out) == limit {
			break
		}
	}
	if out == nil {
		out = []store.InboxItem{}
	}
	return out, nil
}

// ThreadSummaries counts replies per thread; turns are not modelled here.
func (f *fakeMessages) ThreadSummaries(_ context.Context, rootMessageIDs []string) (map[string]store.ThreadSummary, error) {
	out := map[string]store.ThreadSummary{}
	for _, id := range rootMessageIDs {
		for _, thread := range f.threads {
			if thread.RootMessageID != id {
				continue
			}
			summary := store.ThreadSummary{ID: thread.ID}
			for _, m := range f.messages {
				if m.ThreadID == thread.ID {
					summary.ReplyCount++
					at := m.CreatedAt
					summary.LastReplyAt = &at
				}
			}
			out[id] = summary
		}
	}
	return out, nil
}

func (f *fakeMessages) ChainWork(_ context.Context, chain string) (store.WorkSummary, error) {
	work, ok := f.work[chain]
	if !ok {
		return store.WorkSummary{}, store.ErrNotFound
	}
	return work, nil
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
	handler := NewHandler(Deps{Projects: projects, Users: users, Messages: messages, Turns: fakeTurns{}, Chat: chat})
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

func TestUserInbox(t *testing.T) {
	handler, room, alice, messages := chatHandler(t)
	bob, err := messages.users.CreateUser(context.Background(), "bob")
	if err != nil {
		t.Fatal(err)
	}
	mention := fmt.Sprintf(`,"mentions":[{"kind":"user","id":%q}]`, alice.ID)
	do(t, handler, http.MethodPost, "/api/v1/rooms/"+room.ID+"/messages", fmt.Sprintf(`{"user_id":%q,"body":"@alice one"%s}`, bob.ID, mention), nil)
	do(t, handler, http.MethodPost, "/api/v1/rooms/"+room.ID+"/messages", fmt.Sprintf(`{"user_id":%q,"body":"plain"}`, bob.ID), nil)
	do(t, handler, http.MethodPost, "/api/v1/rooms/"+room.ID+"/messages", fmt.Sprintf(`{"user_id":%q,"body":"@alice two"%s}`, bob.ID, mention), nil)

	var inbox InboxResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/users/"+alice.ID+"/inbox", "", &inbox); rec.Code != http.StatusOK || len(inbox.Items) != 2 || inbox.Items[0].Body != "@alice two" || inbox.Items[0].RoomName == "" {
		t.Errorf("inbox: status = %d, items = %+v", rec.Code, inbox.Items)
	}
	if rec := do(t, handler, http.MethodGet, fmt.Sprintf("/api/v1/users/%s/inbox?before=%d", alice.ID, inbox.Items[0].Seq), "", &inbox); rec.Code != http.StatusOK || len(inbox.Items) != 1 || inbox.Items[0].Body != "@alice one" {
		t.Errorf("before: status = %d, items = %+v", rec.Code, inbox.Items)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/users/"+alice.ID+"/inbox?before=x", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad before: status = %d, want 400", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/users/u404/inbox", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown user: status = %d, want 404", rec.Code)
	}

	// Read, one of the two leaves one unread.
	do(t, handler, http.MethodGet, "/api/v1/users/"+alice.ID+"/inbox", "", &inbox)
	if inbox.Unread != 2 {
		t.Errorf("unread at first: %d", inbox.Unread)
	}
	var read InboxReadResponse
	body := fmt.Sprintf(`{"message_ids":[%q]}`, inbox.Items[0].ID)
	if rec := do(t, handler, http.MethodPost, "/api/v1/users/"+alice.ID+"/inbox/read", body, &read); rec.Code != http.StatusOK || read.Marked != 1 || read.Unread != 1 {
		t.Errorf("read: status = %d, %+v", rec.Code, read)
	}
	do(t, handler, http.MethodGet, "/api/v1/users/"+alice.ID+"/inbox", "", &inbox)
	if inbox.Unread != 1 || !inbox.Items[0].Read || inbox.Items[1].Read {
		t.Errorf("after reading one: %d unread, %+v", inbox.Unread, inbox.Items)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/users/"+alice.ID+"/inbox/read", `{`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad body: status = %d", rec.Code)
	}
}

// A topic comes with the piece of work its latest turn is part of, begun
// in it or in another topic; a topic whose turns are part of none has none.
func TestGetThread_TheWorkItIsPartOf(t *testing.T) {
	projects := newFakeProjects()
	users := newFakeUsers()
	messages := newFakeMessages(projects, users)
	messages.messages = []store.Message{{ID: "m1", Room: "r1", SenderKind: store.SenderAgent}, {ID: "m2", Room: "r1", SenderKind: store.SenderAgent}}
	messages.threads["th1"] = store.Thread{ID: "th1", RoomID: "r1", RootMessageID: "m1", Number: 4}
	messages.threads["th2"] = store.Thread{ID: "th2", RoomID: "r1", RootMessageID: "m2", Number: 5}
	messages.work = map[string]store.WorkSummary{"ask": {ThreadID: "th0", ThreadNumber: 1, Chain: "ask", Turns: 5, Members: []string{"lead", "tester"}}}
	turns := fakeTurns{"t1": {ID: "t1", ThreadID: "th1", ChainMessageID: "ask"}, "t2": {ID: "t2", ThreadID: "th2"}}
	handler := NewHandler(Deps{Projects: projects, Users: users, Messages: messages, Turns: turns})

	var thread ThreadResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/threads/th1", "", &thread); rec.Code != http.StatusOK || thread.Work == nil || thread.Work.ThreadNumber != 1 || thread.Work.Turns != 5 || len(thread.Work.Members) != 2 {
		t.Errorf("a member's topic: %d %+v", rec.Code, thread.Work)
	}
	thread = ThreadResponse{}
	if rec := do(t, handler, http.MethodGet, "/api/v1/threads/th2", "", &thread); rec.Code != http.StatusOK || thread.Work != nil {
		t.Errorf("a topic with no piece of work: %d %+v", rec.Code, thread.Work)
	}
}
