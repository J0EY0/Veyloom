package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// briefFixture is a room with two topics and one agent turn, enough to
// tell apart what a session has read, what it said itself and what is new.
//
//	root   (alice, top level, heads #1)
//	  own    (agent, in #1, said in the session's turn)
//	  reply  (alice, in #1)
//	second (alice, top level, heads #2)
//	  more   (alice, in #2)
//	closing (agent, top level, said in the session's turn)
type briefFixture struct {
	turnFixture
	session                           store.MemberSession
	own, reply, second, more, closing store.Message
	topic2                            store.Thread
	position                          int64
}

func newBriefFixture(t *testing.T) briefFixture {
	t.Helper()
	f := briefFixture{turnFixture: newTurnFixture(t)}
	ctx := context.Background()
	must := func(m store.Message, err error) store.Message {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	var err error
	if f.session, err = f.s.StartSession(ctx, f.newSession()); err != nil {
		t.Fatal(err)
	}
	n := f.newTurn()
	n.SessionID = f.session.ID
	turn, err := f.s.CreateTurn(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	user := func(body, threadID string) store.NewMessage {
		return store.NewMessage{RoomID: f.room.ID, ThreadID: threadID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: body}
	}
	agent := func(body, threadID string) store.NewMessage {
		return store.NewMessage{RoomID: f.room.ID, ThreadID: threadID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: body, TurnID: turn.ID}
	}
	f.own = must(f.s.CreateMessage(ctx, agent("on it", f.thread.ID)))
	f.reply = must(f.s.CreateMessage(ctx, user("thanks", f.thread.ID)))
	f.second = must(f.s.CreateMessage(ctx, user("another thing", "")))
	if f.topic2, err = f.s.ThreadForMessage(ctx, f.second.ID); err != nil {
		t.Fatal(err)
	}
	f.more = must(f.s.CreateMessage(ctx, user("details", f.topic2.ID)))
	f.closing = must(f.s.CreateMessage(ctx, agent("all done", "")))
	if f.position, err = f.s.RoomPosition(ctx, f.room.ID); err != nil {
		t.Fatal(err)
	}
	return f
}

func bodiesOf[T any](items []T, body func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, body(it))
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBrief_RoomPosition(t *testing.T) {
	f := newBriefFixture(t)
	if f.position != f.closing.Seq {
		t.Errorf("position = %d, want the newest message's seq %d", f.position, f.closing.Seq)
	}
	_, empty, err := f.s.CreateProject(context.Background(), store.NewProject{Name: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	if pos, err := f.s.RoomPosition(context.Background(), empty.ID); err != nil || pos != 0 {
		t.Errorf("position of an empty room = %d, %v; want 0", pos, err)
	}
}

func TestBrief_RoomNews(t *testing.T) {
	f := newBriefFixture(t)
	ctx := context.Background()
	body := func(it store.RoomNewsItem) string { return it.Body }

	// To the session: the top level, oldest first, without what it said.
	q := store.NewsQuery{RoomID: f.room.ID, UpTo: f.position, SessionID: f.session.ID, Limit: 10}
	news, total, err := f.s.RoomNews(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if got := bodiesOf(news, body); !sameStrings(got, []string{"@agent go", "another thing"}) || total != 2 {
		t.Fatalf("news = %q (total %d), want the two top-level messages of alice", got, total)
	}
	if news[0].TopicNumber != 1 || news[1].TopicNumber != 2 {
		t.Errorf("topic numbers = #%d, #%d; want #1, #2", news[0].TopicNumber, news[1].TopicNumber)
	}

	// To anyone else the agent's closing message is news too, and heads no topic.
	q.SessionID = ""
	news, total, _ = f.s.RoomNews(ctx, q)
	if got := bodiesOf(news, body); !sameStrings(got, []string{"@agent go", "another thing", "all done"}) || total != 3 || news[2].TopicNumber != 0 {
		t.Errorf("news without a session = %q (total %d, last #%d)", got, total, news[2].TopicNumber)
	}

	// The cap keeps the newest and still says how many there were.
	q.Limit = 1
	news, total, _ = f.s.RoomNews(ctx, q)
	if got := bodiesOf(news, body); !sameStrings(got, []string{"all done"}) || total != 3 {
		t.Errorf("capped news = %q (total %d), want the newest of 3", got, total)
	}

	// Bounded on both sides.
	q = store.NewsQuery{RoomID: f.room.ID, After: f.root.Seq, UpTo: f.second.Seq, Limit: 10}
	news, total, _ = f.s.RoomNews(ctx, q)
	if got := bodiesOf(news, body); !sameStrings(got, []string{"another thing"}) || total != 1 {
		t.Errorf("news in (root, second] = %q (total %d)", got, total)
	}
}

func TestBrief_TopicNews(t *testing.T) {
	f := newBriefFixture(t)
	ctx := context.Background()
	q := store.NewsQuery{RoomID: f.room.ID, UpTo: f.position, SessionID: f.session.ID, Limit: 10}

	// Seen from topic #1: only #2 has news.
	topics, total, err := f.s.TopicNews(ctx, q, f.thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) != 1 || total != 1 {
		t.Fatalf("topics = %+v (total %d), want #2 alone", topics, total)
	}
	if got := topics[0]; got.ThreadID != f.topic2.ID || got.Number != 2 || got.NewCount != 1 || got.Root.ID != f.second.ID || got.Last.ID != f.more.ID {
		t.Errorf("topic = %+v, want #2 with one new reply, its root and that reply", got)
	}

	// From nowhere both show, most recently active first; in #1 the
	// session's own reply does not count.
	topics, total, _ = f.s.TopicNews(ctx, q, "")
	if len(topics) != 2 || total != 2 || topics[0].Number != 2 || topics[1].Number != 1 || topics[1].NewCount != 1 || topics[1].Last.ID != f.reply.ID {
		t.Errorf("topics = %+v (total %d), want #2 then #1 with one new reply each", topics, total)
	}
	q.SessionID = ""
	topics, _, _ = f.s.TopicNews(ctx, q, "")
	if topics[1].NewCount != 2 {
		t.Errorf("to another reader #1 has %d new replies, want 2", topics[1].NewCount)
	}

	// Nothing after the position the session has read to.
	q.After = f.position
	if topics, total, _ = f.s.TopicNews(ctx, q, ""); len(topics) != 0 || total != 0 {
		t.Errorf("topics after the position = %+v (total %d), want none", topics, total)
	}
	// The cap counts the topics it left out.
	q.After, q.Limit = 0, 1
	if topics, total, _ = f.s.TopicNews(ctx, q, ""); len(topics) != 1 || total != 2 || topics[0].Number != 2 {
		t.Errorf("capped topics = %+v (total %d), want the most recent of 2", topics, total)
	}
}

func TestBrief_ThreadNews(t *testing.T) {
	f := newBriefFixture(t)
	ctx := context.Background()
	body := func(m store.Message) string { return m.Body }

	// The whole thread: for a session that has not been in it.
	all, total, err := f.s.ThreadNews(ctx, f.thread.ID, store.NewsQuery{UpTo: f.position, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if got := bodiesOf(all, body); !sameStrings(got, []string{"on it", "thanks"}) || total != 2 {
		t.Fatalf("thread = %q (total %d), want both replies, oldest first", got, total)
	}
	// What is new to the session that wrote one of them.
	news, total, _ := f.s.ThreadNews(ctx, f.thread.ID, store.NewsQuery{UpTo: f.position, SessionID: f.session.ID, Limit: 10})
	if got := bodiesOf(news, body); !sameStrings(got, []string{"thanks"}) || total != 1 {
		t.Errorf("news = %q (total %d), want alice's reply alone", got, total)
	}
	// After a position, and up to one.
	news, _, _ = f.s.ThreadNews(ctx, f.thread.ID, store.NewsQuery{After: f.own.Seq, UpTo: f.position, Limit: 10})
	if got := bodiesOf(news, body); !sameStrings(got, []string{"thanks"}) {
		t.Errorf("news after the agent's reply = %q", got)
	}
	news, _, _ = f.s.ThreadNews(ctx, f.thread.ID, store.NewsQuery{UpTo: f.own.Seq, Limit: 10})
	if got := bodiesOf(news, body); !sameStrings(got, []string{"on it"}) {
		t.Errorf("news up to the agent's reply = %q", got)
	}
	// The cap keeps the tail.
	news, total, _ = f.s.ThreadNews(ctx, f.thread.ID, store.NewsQuery{UpTo: f.position, Limit: 1})
	if got := bodiesOf(news, body); !sameStrings(got, []string{"thanks"}) || total != 2 {
		t.Errorf("capped thread = %q (total %d), want the newest of 2", got, total)
	}
}

func TestSessions_AdvanceAndCompact(t *testing.T) {
	f := newBriefFixture(t)
	ctx := context.Background()

	if f.session.RoomSeen != 0 || len(f.session.ThreadSeen) != 0 || f.session.Compactions != 0 || f.session.WikiSeen != nil {
		t.Fatalf("a new session has read nothing: %+v", f.session)
	}
	wiki := time.Date(2026, 9, 21, 9, 0, 0, 123456000, time.UTC)
	if err := f.s.AdvanceSession(ctx, f.session.ID, store.Reading{Position: 40, ThreadID: f.thread.ID, Wiki: wiki}); err != nil {
		t.Fatal(err)
	}
	// A brief that showed no wiki leaves the session's place in it.
	if err := f.s.AdvanceSession(ctx, f.session.ID, store.Reading{Position: 55, ThreadID: f.topic2.ID}); err != nil {
		t.Fatal(err)
	}
	// Positions never move back.
	if err := f.s.AdvanceSession(ctx, f.session.ID, store.Reading{Position: 12, ThreadID: f.thread.ID, Wiki: wiki.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.GetSession(ctx, f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.RoomSeen != 55 || got.ThreadSeen[f.thread.ID] != 40 || got.ThreadSeen[f.topic2.ID] != 55 || len(got.ThreadSeen) != 2 {
		t.Errorf("after advancing: room %d, threads %v; want 55 and {#1: 40, #2: 55}", got.RoomSeen, got.ThreadSeen)
	}
	if got.WikiSeen == nil || !got.WikiSeen.Equal(wiki) {
		t.Errorf("the wiki position, to the microsecond: %v, want %v", got.WikiSeen, wiki)
	}

	// A compaction forgets what was read of each topic and of the wiki,
	// not of the room.
	if err := f.s.NoteSessionCompactions(ctx, f.session.ID, 2); err != nil {
		t.Fatal(err)
	}
	if err := f.s.NoteSessionCompactions(ctx, f.session.ID, 0); err != nil {
		t.Fatal(err)
	}
	got, _ = f.s.GetSession(ctx, f.session.ID)
	if got.Compactions != 2 || len(got.ThreadSeen) != 0 || got.RoomSeen != 55 || got.WikiSeen != nil {
		t.Errorf("after compacting: %d compactions, threads %v, room %d, wiki %v; want 2, none, 55, none", got.Compactions, got.ThreadSeen, got.RoomSeen, got.WikiSeen)
	}

	if err := f.s.AdvanceSession(ctx, store.NewID(), store.Reading{Position: 1, ThreadID: f.thread.ID}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("advance an unknown session: got %v, want ErrNotFound", err)
	}
	if err := f.s.NoteSessionCompactions(ctx, store.NewID(), 1); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("compactions of an unknown session: got %v, want ErrNotFound", err)
	}
}

func TestProjects_Description(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	project, room, err := f.s.CreateProject(ctx, store.NewProject{Name: "described", Description: "A chat for coding agents. Go and Postgres."})
	if err != nil {
		t.Fatal(err)
	}
	if project.Description != "A chat for coding agents. Go and Postgres." {
		t.Errorf("Description = %q", project.Description)
	}
	of, err := f.s.RoomProject(ctx, room.ID)
	if err != nil || of.ID != project.ID || of.Description != project.Description {
		t.Errorf("RoomProject = %+v, %v", of, err)
	}

	rewritten := "Now with memory."
	updated, err := f.s.UpdateProject(ctx, project.ID, store.ProjectPatch{Description: &rewritten})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Description != rewritten || updated.Name != "described" {
		t.Errorf("after the patch = %+v", updated)
	}
	// Left out of a patch, it stays.
	name := "renamed"
	if updated, _ = f.s.UpdateProject(ctx, project.ID, store.ProjectPatch{Name: &name}); updated.Description != rewritten {
		t.Errorf("a rename lost the description: %+v", updated)
	}
	if _, err := f.s.RoomProject(ctx, store.NewID()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("project of an unknown room: got %v, want ErrNotFound", err)
	}
}
