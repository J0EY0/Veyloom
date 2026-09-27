package hub

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// fakeRoomStore is a small room for the read tools' answers.
type fakeRoomStore struct {
	users    map[string]string
	members  map[string]string
	messages map[string]store.Message
	threads  map[int]store.Thread
	topics   []store.TopicListing
	room     []store.Message // top level, oldest first
	replies  []store.Message // of whatever thread is asked for, oldest first
	turns    []store.Turn
	hits     []store.RoomNewsItem

	askedBefore int64
	askedLimit  int
	askedPhrase string
}

func (f *fakeRoomStore) GetUser(_ context.Context, id string) (store.User, error) {
	return store.User{ID: id, Name: f.users[id]}, nil
}

func (f *fakeRoomStore) GetMember(_ context.Context, id string) (store.Member, error) {
	return store.Member{ID: id, DisplayName: f.members[id]}, nil
}

func (f *fakeRoomStore) GetMessage(_ context.Context, id string) (store.Message, error) {
	m, ok := f.messages[id]
	if !ok {
		return store.Message{}, fmt.Errorf("message %s: %w", id, store.ErrNotFound)
	}
	return m, nil
}

func (f *fakeRoomStore) ThreadByNumber(_ context.Context, _ string, n int) (store.Thread, error) {
	t, ok := f.threads[n]
	if !ok {
		return store.Thread{}, fmt.Errorf("topic #%d: %w", n, store.ErrNotFound)
	}
	return t, nil
}

func (f *fakeRoomStore) ListRoomTopics(_ context.Context, _ string, before int64, limit int) ([]store.TopicListing, error) {
	f.askedBefore, f.askedLimit = before, limit
	return f.topics, nil
}

// tail is the newest limit of msgs older than before, oldest first.
func tail(msgs []store.Message, before int64, limit int) []store.Message {
	var out []store.Message
	for _, m := range msgs {
		if before <= 0 || m.Seq < before {
			out = append(out, m)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func (f *fakeRoomStore) ListRoomMessagesBefore(_ context.Context, _ string, before int64, limit int) ([]store.Message, error) {
	f.askedBefore, f.askedLimit = before, limit
	return tail(f.room, before, limit), nil
}

func (f *fakeRoomStore) ListThreadMessagesBefore(_ context.Context, _ string, before int64, limit int) ([]store.Message, error) {
	f.askedBefore, f.askedLimit = before, limit
	return tail(f.replies, before, limit), nil
}

func (f *fakeRoomStore) ListThreadTurns(context.Context, string) ([]store.Turn, error) {
	return f.turns, nil
}

func (f *fakeRoomStore) SearchRoomMessages(_ context.Context, _, phrase string, before int64, limit int) ([]store.RoomNewsItem, error) {
	f.askedPhrase, f.askedBefore, f.askedLimit = phrase, before, limit
	return f.hits, nil
}

var noon = time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

func stamped(m store.Message, seq int64, minutes int) store.Message {
	m.Seq, m.CreatedAt = seq, noon.Add(time.Duration(minutes)*time.Minute)
	return m
}

func newFakeRoom() *fakeRoomStore {
	root := stamped(user("m1", "plan the auth refactor"), 10, 0)
	return &fakeRoomStore{
		users:    map[string]string{"u1": "alice"},
		members:  map[string]string{"a1": "Claude", "a2": "Codex"},
		messages: map[string]store.Message{"m1": root},
		threads:  map[int]store.Thread{7: {ID: "t7", Number: 7, RootMessageID: "m1"}},
	}
}

func ask(t *testing.T, st roomStore, q runtime.RoomQuery) string {
	t.Helper()
	text, err := answerRoomQuery(context.Background(), st, "r1", "/var/veyloom/attachments", q)
	if err != nil {
		t.Fatalf("%+v: %v", q, err)
	}
	return text
}

func TestRoomRead_ListTopics(t *testing.T) {
	st := newFakeRoom()
	if got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolListTopics}); got != "This chat has no topics yet." {
		t.Errorf("an empty chat: %q", got)
	}
	if st.askedLimit != roomPageDefault {
		t.Errorf("limit = %d, want the default %d", st.askedLimit, roomPageDefault)
	}

	st.topics = []store.TopicListing{
		{Number: 7, ReplyCount: 3, LastSeq: 40, Root: st.messages["m1"], Last: stamped(agent("m9", "a1", "Done, see\nthe diff."), 40, 30)},
		{Number: 3, ReplyCount: 1, LastSeq: 22, Root: stamped(user("m5", "flaky test"), 20, 5), Last: stamped(user("m6", "still red"), 22, 9)},
	}
	got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolListTopics, Limit: 2})
	wantInOrder(t, got,
		"Topics, most recently active first:\n",
		`#7 "plan the auth refactor": 3 replies, last 2026-09-18 12:30 from Claude: "Done, see the diff."`,
		`#3 "flaky test": 1 reply, last 2026-09-18 12:09 from alice: "still red"`,
		// A full page may have more behind it, and says how to get it.
		"(older topics: list_topics with before=22)",
	)
	// A page that came up short is the last one.
	if got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolListTopics, Limit: 5}); strings.Contains(got, "older topics") {
		t.Errorf("a short page has nothing behind it:\n%s", got)
	}
	st.topics = nil
	if got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolListTopics, Before: 22}); got != "No older topics." {
		t.Errorf("past the last page: %q", got)
	}
	// Whatever is asked for, an answer stays a screenful.
	ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolListTopics, Limit: 10000})
	if st.askedLimit != roomPageMax {
		t.Errorf("limit = %d, want it capped at %d", st.askedLimit, roomPageMax)
	}
}

// A file a person sent is named with the id that keeps it in the wiki and
// the path that opens it (design.md 5.16).
func TestRoomRead_NamesTheFilesPeopleSent(t *testing.T) {
	st := newFakeRoom()
	sent := stamped(user("m2", "the diagram"), 11, 1)
	sent.Attachments = []store.Attachment{{ID: "at1", Filename: "arch.png", MediaType: "image/png", Size: 1234, Path: "r1/at1.png"}}
	st.replies = []store.Message{sent}
	got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolReadTopic, Topic: 7})
	if !strings.Contains(got, "(attached arch.png, image/png, 1234 bytes; file at1 at /var/veyloom/attachments/r1/at1.png)") {
		t.Errorf("the file is not named so an agent can open or keep it:\n%s", got)
	}
}

// A topic an agent's reply opened starts from the question in the room the
// reply answers; a turn that changed nothing is named all the same.
func TestRoomRead_ReadTopicOpenedByAReply(t *testing.T) {
	st := newFakeRoom()
	root := stamped(agent("m1", "a1", "ok"), 10, 0)
	root.TurnID = "turn-0"
	st.messages["m1"] = root
	st.messages["m0"] = stamped(user("m0", "@Claude remember the word saffron"), 9, 0)
	st.turns = []store.Turn{{ID: "turn-0", TriggerMessageID: "m0"}}
	got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolReadTopic, Topic: 7})
	wantInOrder(t, got,
		`Topic #7 "ok", oldest first:`,
		"(asked in the room) 2026-09-18 12:00 [alice] @Claude remember the word saffron\n",
		"[Claude] ok\n",
		"     (turn turn-0)\n",
	)
}

func TestRoomRead_ReadTopic(t *testing.T) {
	st := newFakeRoom()
	first := stamped(agent("m2", "a1", "I will start with the tokens."), 11, 1)
	first.TurnID = "turn-1"
	second := stamped(agent("m3", "a1", "Tokens are done."), 12, 2)
	second.TurnID = "turn-1"
	st.replies = []store.Message{first, second, stamped(user("m4", "thanks"), 13, 3)}
	st.turns = []store.Turn{{ID: "turn-1", FilesChanged: []string{"auth/token.go", "auth/token_test.go"}}, {ID: "turn-2"}}

	got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolReadTopic, Topic: 7})
	wantInOrder(t, got,
		`Topic #7 "plan the auth refactor", oldest first:`,
		"2026-09-18 12:00 [alice] plan the auth refactor\n",
		"2026-09-18 12:01 [Claude] I will start with the tokens.\n",
		"2026-09-18 12:02 [Claude] Tokens are done.\n",
		// Once, after the last thing the turn said: its id, for read_turn,
		// and the files it changed.
		"     (turn turn-1; it changed: auth/token.go, auth/token_test.go)\n",
		"2026-09-18 12:03 [alice] thanks\n",
	)
	if strings.Count(got, "(turn turn-1") != 1 || strings.Contains(got, "asked in the room") {
		t.Errorf("a turn is named once, and a topic a person opened has no question before it:\n%s", got)
	}

	// The latest page of a longer topic says where the rest is and leaves
	// the root to the page that reaches it.
	got = ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolReadTopic, Topic: 7, Limit: 2})
	wantInOrder(t, got, "(earlier ones: read_topic with topic=7 before=12)", "[Claude] Tokens are done.", "[alice] thanks")
	if strings.Contains(got, "[alice] plan the auth refactor\n") || strings.Contains(got, "I will start") {
		t.Errorf("the latest page holds the latest only:\n%s", got)
	}
	got = ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolReadTopic, Topic: 7, Limit: 2, Before: 12})
	wantInOrder(t, got, "[alice] plan the auth refactor\n", "[Claude] I will start with the tokens.\n")
	if strings.Contains(got, "earlier ones") {
		t.Errorf("the page that reaches the root has nothing before it:\n%s", got)
	}

	for _, q := range []runtime.RoomQuery{{Tool: runtime.RoomToolReadTopic}, {Tool: runtime.RoomToolReadTopic, Topic: 99}} {
		if _, err := answerRoomQuery(context.Background(), st, "r1", "", q); err == nil || !strings.Contains(err.Error(), "topic") {
			t.Errorf("%+v: err = %v, want one that says what is wrong", q, err)
		}
	}
}

func TestRoomRead_ReadRoom(t *testing.T) {
	st := newFakeRoom()
	if got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolReadRoom}); got != "Nobody has said anything in this chat yet." {
		t.Errorf("an empty chat: %q", got)
	}
	long := strings.Repeat("x", roomBodyMax+500)
	st.room = []store.Message{st.messages["m1"], stamped(user("m20", "fyi the deploy is at noon"), 20, 10), stamped(agent("m21", "a2", long), 21, 11)}
	st.topics = []store.TopicListing{{Number: 7, Root: st.messages["m1"]}}

	got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolReadRoom})
	wantInOrder(t, got,
		"The chat's top-level messages, oldest first:\n",
		"#7 2026-09-18 12:00 [alice] plan the auth refactor\n",
		"2026-09-18 12:10 [alice] fyi the deploy is at noon\n",
		"2026-09-18 12:11 [Codex] xxx",
		"x … (500 more characters: read_message with message m21 reads it whole)\n",
	)
	if len(got) > roomBodyMax+600 {
		t.Errorf("a long message should be cut, the answer is %d bytes", len(got))
	}
	got = ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolReadRoom, Limit: 2})
	wantInOrder(t, got, "(earlier ones: read_room with before=20)", "fyi the deploy")
	if got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolReadRoom, Before: 10}); got != "No older messages." {
		t.Errorf("past the first message: %q", got)
	}
}

func TestRoomRead_Search(t *testing.T) {
	st := newFakeRoom()
	if got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolSearch, Text: " rate limit "}); got != `No message in this chat holds "rate limit".` || st.askedPhrase != "rate limit" {
		t.Errorf("no hits: %q (asked for %q)", got, st.askedPhrase)
	}
	st.hits = []store.RoomNewsItem{
		{Message: stamped(agent("m31", "a2", "The rate limit is 100 per minute,\nper token."), 31, 20), TopicNumber: 3},
		{Message: stamped(user("m15", "what about a rate limit?"), 15, 4)},
	}
	got := ask(t, st, runtime.RoomQuery{Tool: runtime.RoomToolSearch, Text: "rate limit", Limit: 2})
	wantInOrder(t, got,
		`Messages holding "rate limit", newest first:`,
		"#3 2026-09-18 12:20 [Codex] The rate limit is 100 per minute, per token.\n",
		"room 2026-09-18 12:04 [alice] what about a rate limit?\n",
		`(older hits: search_messages with text="rate limit" before=15)`,
	)
	if _, err := answerRoomQuery(context.Background(), st, "r1", "", runtime.RoomQuery{Tool: runtime.RoomToolSearch}); err == nil {
		t.Error("a search for nothing should say so")
	}
}

func TestRoomRead_UnknownTool(t *testing.T) {
	if _, err := answerRoomQuery(context.Background(), newFakeRoom(), "r1", "", runtime.RoomQuery{Tool: "delete_everything"}); err == nil {
		t.Error("an unknown tool should be turned down")
	}
}

// A long message is cut by characters, Chinese as English, its lines kept.
func TestCutBody_CountsCharacters(t *testing.T) {
	chinese := strings.Repeat("审批超时", 375)
	if got, more := cutBody(chinese, 4000); got != chinese || more != 0 {
		t.Errorf("1500 characters of Chinese fit in 4000: cut to %d, %d more", len([]rune(got)), more)
	}
	got, more := cutBody(strings.Repeat("超", 4000)+"时间", 4000)
	if len([]rune(got)) != 4000 || more != 2 {
		t.Errorf("4002 characters: kept %d, %d more", len([]rune(got)), more)
	}
	got, more = cutBody("line one\nline two   \nline three", 18)
	if got != "line one\nline two" || more != len("  \nline three") {
		t.Errorf("cut at a line's end: %q, %d more", got, more)
	}
	if got, more := cutBody("short", 4000); got != "short" || more != 0 {
		t.Errorf("short: %q %d", got, more)
	}
}
