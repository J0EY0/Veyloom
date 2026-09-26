package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

// chatFixture is a project with its main room and one user.
type chatFixture struct {
	s    *store.Store
	room store.Room
	user store.User
}

func newChatFixture(t *testing.T) chatFixture {
	t.Helper()
	s := storetest.New(t)
	ctx := context.Background()
	_, room, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	return chatFixture{s: s, room: room, user: user}
}

// post creates a user message and fails the test on error.
func (f chatFixture) post(t *testing.T, body, threadID string) store.Message {
	t.Helper()
	m, err := f.s.CreateMessage(context.Background(), store.NewMessage{
		RoomID:     f.room.ID,
		ThreadID:   threadID,
		SenderKind: store.SenderUser,
		UserID:     f.user.ID,
		Body:       body,
	})
	if err != nil {
		t.Fatalf("post %q: %v", body, err)
	}
	return m
}

func TestCreateMessage_TopLevel(t *testing.T) {
	f := newChatFixture(t)

	m, err := f.s.CreateMessage(context.Background(), store.NewMessage{
		RoomID:     f.room.ID,
		SenderKind: store.SenderUser,
		UserID:     f.user.ID,
		Body:       "hello @codex",
		Mentions:   []store.Mention{{Kind: store.MentionAgent, ID: "agent-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.ThreadID != "" || m.Seq <= 0 || m.UserID != f.user.ID || m.SenderKind != store.SenderUser {
		t.Errorf("unexpected message: %+v", m)
	}
	if len(m.Mentions) != 1 || m.Mentions[0].Kind != store.MentionAgent {
		t.Errorf("mentions not round-tripped: %+v", m.Mentions)
	}

	got, err := f.s.GetMessage(context.Background(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "hello @codex" {
		t.Errorf("unexpected message: %+v", got)
	}
}

func TestCreateMessage_NilMentionsBecomeEmptyList(t *testing.T) {
	f := newChatFixture(t)

	m := f.post(t, "no mentions", "")

	if m.Mentions == nil || len(m.Mentions) != 0 {
		t.Errorf("Mentions = %#v, want empty non-nil slice", m.Mentions)
	}
}

func TestCreateMessage_Errors(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	unknown := "00000000-0000-0000-0000-000000000000"

	tests := []struct {
		name string
		msg  store.NewMessage
		want error
	}{
		{"unknown room", store.NewMessage{RoomID: unknown, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "x"}, store.ErrNotFound},
		{"unknown thread", store.NewMessage{RoomID: f.room.ID, ThreadID: unknown, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "x"}, store.ErrNotFound},
		{"blank body", store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "  "}, store.ErrInvalidInput},
		{"user kind without user", store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, Body: "x"}, store.ErrInvalidInput},
		{"system kind with user", store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderSystem, UserID: f.user.ID, Body: "x"}, store.ErrInvalidInput},
		{"malformed room id", store.NewMessage{RoomID: "nope", SenderKind: store.SenderUser, UserID: f.user.ID, Body: "x"}, store.ErrInvalidID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.s.CreateMessage(ctx, tt.msg)
			if !errors.Is(err, tt.want) {
				t.Errorf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestThreadForMessage_CreatesOnceAndFollowsReplies(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	root := f.post(t, "root", "")

	thread, err := f.s.ThreadForMessage(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if thread.RootMessageID != root.ID || thread.RoomID != f.room.ID {
		t.Errorf("unexpected thread: %+v", thread)
	}

	// Asking again returns the same thread rather than a second one.
	again, err := f.s.ThreadForMessage(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != thread.ID {
		t.Errorf("second call created a new thread %s, want %s", again.ID, thread.ID)
	}

	// A reply's thread is the root's thread, so replying to a reply stays
	// in the same conversation.
	reply := f.post(t, "reply", thread.ID)
	viaReply, err := f.s.ThreadForMessage(ctx, reply.ID)
	if err != nil {
		t.Fatal(err)
	}
	if viaReply.ID != thread.ID {
		t.Errorf("reply resolved to thread %s, want %s", viaReply.ID, thread.ID)
	}
}

func TestThreadOfMessage_FindsWithoutStarting(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	alone := f.post(t, "nothing came of this", "")
	root := f.post(t, "root", "")

	// A top-level message nothing came of has no thread, and asking does
	// not start one: the next topic still gets the next number.
	if _, err := f.s.ThreadOfMessage(ctx, alone.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
	thread, err := f.s.ThreadForMessage(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if thread.Number != 1 {
		t.Errorf("the first topic is #%d, want #1", thread.Number)
	}

	// The root finds the thread it started, a reply the thread it is in.
	reply := f.post(t, "reply", thread.ID)
	for _, id := range []string{root.ID, reply.ID} {
		got, err := f.s.ThreadOfMessage(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != thread.ID {
			t.Errorf("message %s is in thread %s, want %s", id, got.ID, thread.ID)
		}
	}
}

func TestCreateMessage_ThreadMustBelongToRoom(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	root := f.post(t, "root", "")
	thread, err := f.s.ThreadForMessage(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.s.CreateRoom(ctx, f.room.ProjectID, "other")
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.s.CreateMessage(ctx, store.NewMessage{
		RoomID: other.ID, ThreadID: thread.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "wrong room",
	})
	if !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("got %v, want ErrInvalidInput", err)
	}
}

func TestListRoomMessages_TopLevelOnlyWithCursor(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()

	var roots []store.Message
	for i := 1; i <= 5; i++ {
		roots = append(roots, f.post(t, fmt.Sprintf("root %d", i), ""))
	}
	// Replies must not show up in the room timeline.
	thread, err := f.s.ThreadForMessage(ctx, roots[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	f.post(t, "reply", thread.ID)

	all, err := f.s.ListRoomMessages(ctx, f.room.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 5 || all[0].Body != "root 1" || all[4].Body != "root 5" {
		t.Errorf("unexpected timeline: %+v", bodies(all))
	}

	// After a cursor, only newer messages.
	newer, err := f.s.ListRoomMessages(ctx, f.room.ID, roots[2].Seq, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(newer) != 2 || newer[0].Body != "root 4" {
		t.Errorf("after cursor: %v", bodies(newer))
	}

	// Limit is honoured.
	limited, err := f.s.ListRoomMessages(ctx, f.room.ID, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 2 {
		t.Errorf("limit: got %d messages", len(limited))
	}
}

func TestListRoomMessagesBefore_WalksBackChronologically(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	for i := 1; i <= 5; i++ {
		f.post(t, fmt.Sprintf("root %d", i), "")
	}

	// Latest two, still oldest first.
	latest, err := f.s.ListRoomMessagesBefore(ctx, f.room.ID, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(latest); len(got) != 2 || got[0] != "root 4" || got[1] != "root 5" {
		t.Errorf("latest page: %v", got)
	}

	// The page before that.
	earlier, err := f.s.ListRoomMessagesBefore(ctx, f.room.ID, latest[0].Seq, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(earlier); len(got) != 2 || got[0] != "root 2" || got[1] != "root 3" {
		t.Errorf("earlier page: %v", got)
	}
}

func TestListThreadMessages(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	root := f.post(t, "root", "")
	thread, err := f.s.ThreadForMessage(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	first := f.post(t, "reply 1", thread.ID)
	f.post(t, "reply 2", thread.ID)
	f.post(t, "unrelated top-level", "")

	replies, err := f.s.ListThreadMessages(ctx, thread.ID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(replies); len(got) != 2 || got[0] != "reply 1" || got[1] != "reply 2" {
		t.Errorf("replies: %v", got)
	}

	newer, err := f.s.ListThreadMessages(ctx, thread.ID, first.Seq, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(newer); len(got) != 1 || got[0] != "reply 2" {
		t.Errorf("after cursor: %v", got)
	}
}

func TestDeleteRootMessageRemovesThreadAndReplies(t *testing.T) {
	// Guards the cascade so replies never outlive their thread.
	f := newChatFixture(t)
	ctx := context.Background()
	root := f.post(t, "root", "")
	thread, _ := f.s.ThreadForMessage(ctx, root.ID)
	reply := f.post(t, "reply", thread.ID)

	if err := storetest.Exec(t, f.s, "DELETE FROM messages WHERE id = $1", root.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := f.s.GetThread(ctx, thread.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("thread should be gone, got %v", err)
	}
	if _, err := f.s.GetMessage(ctx, reply.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("reply should be gone, got %v", err)
	}
}

func bodies(ms []store.Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Body
	}
	return out
}

func TestListThreadMessagesBefore_LatestChronological(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	root := f.post(t, "root", "")
	thread, _ := f.s.ThreadForMessage(ctx, root.ID)
	for i := 1; i <= 5; i++ {
		f.post(t, fmt.Sprintf("reply %d", i), thread.ID)
	}

	latest, err := f.s.ListThreadMessagesBefore(ctx, thread.ID, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodies(latest); len(got) != 2 || got[0] != "reply 4" || got[1] != "reply 5" {
		t.Errorf("latest page: %v", got)
	}
}

func TestListUserMentions_AcrossRoomsNewestFirst(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	bob, err := f.s.CreateUser(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.s.CreateRoom(ctx, f.room.ProjectID, "websocket")
	if err != nil {
		t.Fatal(err)
	}
	mention := []store.Mention{{Kind: store.MentionUser, ID: f.user.ID}}
	first, _ := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: bob.ID, Body: "@alice one", Mentions: mention})
	f.post(t, "nothing for alice", "")
	second, _ := f.s.CreateMessage(ctx, store.NewMessage{RoomID: other.ID, SenderKind: store.SenderUser, UserID: bob.ID, Body: "@alice two", Mentions: mention})

	items, err := f.s.ListUserMentions(ctx, f.user.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != second.ID || items[1].ID != first.ID {
		t.Fatalf("inbox = %+v", items)
	}
	if items[0].RoomName != "websocket" || items[0].SenderName != "bob" || items[1].RoomName != "main" {
		t.Errorf("names on items: %+v", items)
	}
	if page, _ := f.s.ListUserMentions(ctx, f.user.ID, second.Seq, 10); len(page) != 1 || page[0].ID != first.ID {
		t.Errorf("before pages back: %+v", page)
	}
	if none, _ := f.s.ListUserMentions(ctx, bob.ID, 0, 10); len(none) != 0 {
		t.Errorf("bob was never mentioned: %+v", none)
	}
}

// What a person read of their inbox is kept: messages marked by id, by the
// topic they are in, its root and replies, or all up to a point; the rest
// are counted as unread.
func TestInboxReads(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	bob, err := f.s.CreateUser(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	say := func(body, threadID string) store.Message {
		t.Helper()
		m, err := f.s.CreateMessage(ctx, store.NewMessage{
			RoomID: f.room.ID, ThreadID: threadID, SenderKind: store.SenderUser, UserID: bob.ID, Body: body,
			Mentions: []store.Mention{{Kind: store.MentionUser, ID: f.user.ID}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	unread := func() int {
		t.Helper()
		n, err := f.s.CountUnreadMentions(ctx, f.user.ID)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	mark := func(read store.InboxRead) int {
		t.Helper()
		n, err := f.s.MarkMentionsRead(ctx, f.user.ID, read)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	root := say("@alice look", "")
	thread, err := f.s.ThreadForMessage(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	say("@alice and this", thread.ID)
	f.post(t, "not for alice", thread.ID)
	other := say("@alice elsewhere", "")

	if n := unread(); n != 3 {
		t.Fatalf("unread at first: %d", n)
	}
	if n := mark(store.InboxRead{MessageIDs: []string{other.ID}}); n != 1 || unread() != 2 {
		t.Errorf("one read: %d, unread %d", n, unread())
	}
	if n := mark(store.InboxRead{MessageIDs: []string{other.ID}}); n != 0 {
		t.Errorf("read again: %d", n)
	}
	if n := mark(store.InboxRead{ThreadID: thread.ID}); n != 2 || unread() != 0 {
		t.Errorf("the topic read: %d, unread %d", n, unread())
	}
	items, err := f.s.ListUserMentions(ctx, f.user.ID, 0, 10)
	if err != nil || len(items) != 3 || !items[0].Read || !items[1].Read || !items[2].Read {
		t.Errorf("the inbox: %+v %v", items, err)
	}

	later := say("@alice later", "")
	if n := mark(store.InboxRead{UpTo: later.Seq - 1}); n != 0 || unread() != 1 {
		t.Errorf("all up to before it: %d, unread %d", n, unread())
	}
	if n := mark(store.InboxRead{UpTo: later.Seq}); n != 1 || unread() != 0 {
		t.Errorf("all: %d, unread %d", n, unread())
	}
	if n, _ := f.s.CountUnreadMentions(ctx, bob.ID); n != 0 {
		t.Errorf("bob was never mentioned, yet has %d unread", n)
	}

	// The signed-in account is no users row (it lives in the state dir),
	// and reads its inbox all the same.
	account := "0b5a3f9e-7d1c-4c2a-9e8f-1a2b3c4d5e6f"
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{
		RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: bob.ID, Body: "@you look",
		Mentions: []store.Mention{{Kind: store.MentionUser, ID: account}},
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.MarkMentionsRead(ctx, account, store.InboxRead{UpTo: later.Seq + 10}); err != nil || n != 1 {
		t.Errorf("the account reads its inbox: %d %v", n, err)
	}
}

func TestAskLine(t *testing.T) {
	names := []string{"Coder", "Codex Implementer", "Code"}
	for _, c := range []struct{ body, want string }{
		{"@Coder 把标签加上：要能按标签过滤", "把标签加上"},
		{"@Codex Implementer @Coder  看一下 README。然后提交", "看一下 README"},
		{"\n\n  review the tags feature\nmore", "review the tags feature"},
		{"@Coder", ""},
		{"@Bob hello", "@Bob hello"},
	} {
		if got := store.AskLine(c.body, names); got != c.want {
			t.Errorf("AskLine(%q) = %q, want %q", c.body, got, c.want)
		}
	}
}
