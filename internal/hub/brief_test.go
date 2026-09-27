package hub

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// fakeBriefStore hands a brief what a test put in it and remembers what it
// was asked, which is how a test sees what a brief considered new.
type fakeBriefStore struct {
	project  store.Project
	position int64
	members  []store.Member
	agents   map[string]store.Agent
	users    map[string]string
	messages map[string]store.Message

	roomNews   []store.RoomNewsItem
	roomTotal  int
	topicNews  []store.TopicNewsItem
	topicTotal int
	replies    []store.Message
	replyTotal int
	turns      []store.Turn
	reminders  []store.Reminder
	threads    map[string]store.Thread

	asked struct {
		room, topics, thread store.NewsQuery
		exceptThread         string
	}
	lookups int
}

func (f *fakeBriefStore) GetMessage(_ context.Context, id string) (store.Message, error) {
	m, ok := f.messages[id]
	if !ok {
		return store.Message{}, fmt.Errorf("message %s: %w", id, store.ErrNotFound)
	}
	return m, nil
}

func (f *fakeBriefStore) GetUser(_ context.Context, id string) (store.User, error) {
	f.lookups++
	return store.User{ID: id, Name: f.users[id]}, nil
}

func (f *fakeBriefStore) GetMember(_ context.Context, id string) (store.Member, error) {
	f.lookups++
	for _, m := range f.members {
		if m.ID == id {
			return m, nil
		}
	}
	return store.Member{}, fmt.Errorf("member %s: %w", id, store.ErrNotFound)
}

func (f *fakeBriefStore) GetAgent(_ context.Context, id string) (store.Agent, error) {
	a, ok := f.agents[id]
	if !ok {
		return store.Agent{}, fmt.Errorf("agent %s: %w", id, store.ErrNotFound)
	}
	return a, nil
}

func (f *fakeBriefStore) ListRoomMembers(context.Context, string) ([]store.Member, error) {
	return f.members, nil
}

func (f *fakeBriefStore) RoomProject(context.Context, string) (store.Project, error) {
	return f.project, nil
}

func (f *fakeBriefStore) RoomPosition(context.Context, string) (int64, error) {
	return f.position, nil
}

func (f *fakeBriefStore) RoomNews(_ context.Context, q store.NewsQuery) ([]store.RoomNewsItem, int, error) {
	f.asked.room = q
	return f.roomNews, max(f.roomTotal, len(f.roomNews)), nil
}

func (f *fakeBriefStore) TopicNews(_ context.Context, q store.NewsQuery, except string) ([]store.TopicNewsItem, int, error) {
	f.asked.topics, f.asked.exceptThread = q, except
	return f.topicNews, max(f.topicTotal, len(f.topicNews)), nil
}

func (f *fakeBriefStore) ThreadNews(_ context.Context, _ string, q store.NewsQuery) ([]store.Message, int, error) {
	f.asked.thread = q
	return f.replies, max(f.replyTotal, len(f.replies)), nil
}

func (f *fakeBriefStore) ListThreadTurns(context.Context, string) ([]store.Turn, error) {
	return f.turns, nil
}

func (f *fakeBriefStore) ListMemberPendingReminders(_ context.Context, memberID string) ([]store.Reminder, error) {
	var out []store.Reminder
	for _, r := range f.reminders {
		if r.MemberID == memberID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeBriefStore) GetThread(_ context.Context, id string) (store.Thread, error) {
	t, ok := f.threads[id]
	if !ok {
		return store.Thread{}, fmt.Errorf("thread %s: %w", id, store.ErrNotFound)
	}
	return t, nil
}

func user(id, body string) store.Message {
	return store.Message{ID: id, SenderKind: store.SenderUser, UserID: "u1", Body: body}
}

func agent(id, memberID, body string) store.Message {
	return store.Message{ID: id, SenderKind: store.SenderAgent, MemberID: memberID, Body: body}
}

// newBriefRoom is a project with two agents and alice, and a topic #7
// rooted at alice's message m1.
func newBriefRoom() (*fakeBriefStore, briefInput) {
	st := &fakeBriefStore{
		project:  store.Project{Name: "Veyloom", Description: "A group chat for coding agents.\nGo and Postgres."},
		position: 90,
		members: []store.Member{
			{ID: "a1", AgentID: "g1", DisplayName: "Claude", Enabled: true},
			{ID: "a2", AgentID: "g2", DisplayName: "Codex", Enabled: true},
		},
		agents: map[string]store.Agent{
			"g1": {RoleCard: "# Architect\nYou design before you build.\nMore lines follow."},
			"g2": {RoleCard: "You implement what was designed."},
		},
		users:    map[string]string{"u1": "alice"},
		messages: map[string]store.Message{"m1": user("m1", "@Claude plan the auth refactor")},
	}
	in := briefInput{
		Member:  st.members[0],
		Runtime: "claude",
		Thread:  store.Thread{ID: "t7", RoomID: "r1", Number: 7, RootMessageID: "m1"},
	}
	return st, in
}

func build(t *testing.T, st *fakeBriefStore, in briefInput) brief {
	t.Helper()
	b, err := newBriefBuilder(st, briefLimits{Thread: 40, Room: 30, Topics: 10}, "/var/veyloom/attachments").Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// wantInOrder fails unless every part occurs in text, each after the last.
func wantInOrder(t *testing.T, text string, parts ...string) {
	t.Helper()
	at := 0
	for _, part := range parts {
		i := strings.Index(text[at:], part)
		if i < 0 {
			t.Fatalf("brief lacks %q (after the parts before it):\n%s", part, text)
		}
		at += i + len(part)
	}
}

// What a brief opens with: what the project is and who is in the chat,
// the ones taken out or disabled left out, each member with the first line
// of its role card. Who the agent is and how the chat works are its
// standing instructions, the system prompt of a runtime that takes one
// with every run (design.md 5.23.1).
func TestBrief_OpensWithTheProjectAndWhoIsInTheChat(t *testing.T) {
	st, in := newBriefRoom()
	removed := time.Now()
	st.members = append(st.members,
		store.Member{ID: "a3", AgentID: "g2", DisplayName: "Gone", Enabled: true, RemovedAt: &removed},
		store.Member{ID: "a4", AgentID: "g2", DisplayName: "Off", Enabled: false},
		store.Member{ID: "a5", AgentID: "g9", DisplayName: "Plain", Enabled: true}, // an agent with no card
	)
	b := build(t, st, in)

	wantInOrder(t, b.Prompt,
		"About the project:\nA group chat for coding agents.\nGo and Postgres.\n",
		"In this chat",
		"- Claude (you): Architect\n",
		"- Codex: You implement what was designed.\n",
		"- Plain\n",
	)
	for _, absent := range []string{"Gone", "- Off", "You design before you build", "new session", "You are \"Claude\""} {
		if strings.Contains(b.Prompt, absent) {
			t.Errorf("brief should not hold %q:\n%s", absent, b.Prompt)
		}
	}
	if !strings.HasPrefix(b.Standing, `You are "Claude", an agent in the team chat of the project "Veyloom".`) {
		t.Errorf("who the agent is opens its standing instructions:\n%s", b.Standing)
	}

	// The leader is marked, the member being briefed too.
	st.project.LeaderID = "a2"
	wantInOrder(t, build(t, st, in).Prompt, "- Claude (you): Architect\n", "- Codex (the leader): You implement what was designed.\n")
	st.project.LeaderID = "a1"
	wantInOrder(t, build(t, st, in).Prompt, "- Claude (you, the leader): Architect\n", "- Codex: You implement")

	// No description: no empty part for it.
	st.project.Description = "  "
	if text := build(t, st, in).Prompt; strings.Contains(text, "About the project") {
		t.Errorf("an empty description needs no heading:\n%s", text)
	}
}

// A session is told a part that changes now and then again only when it
// changed, or the session compacted and its record of what it saw is gone;
// the parts it saw as they are are named as not repeated.
func TestBrief_TellsAgainOnlyWhatChanged(t *testing.T) {
	st, in := newBriefRoom()
	first := build(t, st, in)
	if first.Parts["about"] == "" || first.Parts["members"] == "" {
		t.Fatalf("the parts shown are recorded: %v", first.Parts)
	}

	in.Session = store.MemberSession{ID: "s1", RoomSeen: 90, ThreadSeen: map[string]int64{"t7": 90}, BriefSeen: first.Parts}
	b := build(t, st, in)
	if strings.Contains(b.Prompt, "About the project") || strings.Contains(b.Prompt, "- Codex") ||
		!strings.Contains(b.Prompt, "As you were told earlier in this session, unchanged and not repeated here: about the project, who is in the chat.\n") {
		t.Errorf("seen and unchanged, they are only named:\n%s", b.Prompt)
	}

	st.members = append(st.members, store.Member{ID: "a5", AgentID: "g2", DisplayName: "Tester", Enabled: true})
	b = build(t, st, in)
	wantInOrder(t, b.Prompt, "In this chat", "- Tester: You implement", "unchanged and not repeated here: about the project.")

	// A compaction empties the record: the session is told them all.
	in.Session.BriefSeen = nil
	if b = build(t, st, in); !strings.Contains(b.Prompt, "About the project") || strings.Contains(b.Prompt, "not repeated") {
		t.Errorf("after a compaction every part is told:\n%s", b.Prompt)
	}
}

// A runtime that fixes its system prompt when a session starts, as Codex
// does, gets the standing instructions in a brief: the first of a session,
// after a compaction, and when they changed, marked as replacing the ones
// told before.
func TestBrief_CodexIsToldTheStandingInstructionsInTheBrief(t *testing.T) {
	st, in := newBriefRoom()
	in.Runtime = "codex"
	first := build(t, st, in)
	if !strings.HasPrefix(first.Prompt, `You are "Claude", an agent in the team chat of the project "Veyloom".`) || !strings.Contains(first.Prompt, "use your veyloom tools") {
		t.Errorf("the first brief opens with the standing instructions:\n%s", first.Prompt)
	}

	in.Session = store.MemberSession{ID: "s1", RoomSeen: 90, ThreadSeen: map[string]int64{"t7": 90}, BriefSeen: first.Parts}
	if b := build(t, st, in); strings.Contains(b.Prompt, "use your veyloom tools") {
		t.Errorf("seen, they are not told again:\n%s", b.Prompt)
	}

	st.project.LeaderID = "a1"
	b := build(t, st, in)
	wantInOrder(t, b.Prompt, "What follows replaces how this chat works, as you were told it earlier in this session.\n", `You are "Claude"`, "You are the project's leader.")

	// Claude takes them as its system prompt, and its briefs never hold them.
	in.Runtime = "claude"
	if b := build(t, st, in); strings.Contains(b.Prompt, "use your veyloom tools") || !strings.Contains(b.Standing, "use your veyloom tools") {
		t.Errorf("a runtime that takes its system prompt with every run gets them there:\n%s", b.Prompt)
	}
}

func TestBrief_ASessionThatHasReadNothingGetsTheWholeStory(t *testing.T) {
	st, in := newBriefRoom()
	st.roomNews = []store.RoomNewsItem{
		{Message: user("m0", "welcome everyone")},
		{Message: user("m1", "@Claude plan the auth refactor"), TopicNumber: 7},
	}
	st.topicNews = []store.TopicNewsItem{
		{ThreadID: "t3", Number: 3, NewCount: 4, Root: user("m30", "rate limits for the public API\nsecond line"), Last: agent("m34", "a2", "Shipped behind a flag.")},
		{ThreadID: "t5", Number: 5, NewCount: 1, Root: user("m50", "flaky test"), Last: user("m51", "still red")},
	}
	st.replies = []store.Message{
		agent("m2", "a2", "I can take the token part."),
		{ID: "m3", SenderKind: store.SenderSystem, Body: "Codex joined"},
		user("m4", "Claude, go ahead"),
	}
	in.Triggers = []store.Message{st.replies[2]}
	b := build(t, st, in)

	wantInOrder(t, b.Prompt,
		"New in the room since you last looked:\n",
		"   [alice] welcome everyone\n",
		"   #7 [alice] @Claude plan the auth refactor\n",
		"Other topics with news:\n",
		`   #3 "rate limits for the public API second line": 4 new replies, the last from Codex: "Shipped behind a flag."`,
		`   #5 "flaky test": 1 new reply, the last from alice: "still red"`,
		`This topic, #7 "@Claude plan the auth refactor", in full (oldest first):`,
		"   [alice] @Claude plan the auth refactor\n",
		"   [Codex] I can take the token part.\n",
		"   [system] Codex joined\n",
		">> [alice] Claude, go ahead\n",
	)
	if !strings.HasSuffix(b.Prompt, ">> [alice] Claude, go ahead\n") {
		t.Errorf("what the agent is to answer should come last:\n%s", b.Prompt)
	}
	if b.Position != 90 {
		t.Errorf("Position = %d, want the room's when the brief was taken", b.Position)
	}
	// Nothing was read, so nothing is held back, and nothing is the
	// session's own yet.
	if q := st.asked.room; q.After != 0 || q.UpTo != 90 || q.SessionID != "" || q.Limit != 30 {
		t.Errorf("room news asked as %+v", q)
	}
	if q := st.asked.thread; q.After != 0 || q.UpTo != 90 || q.SessionID != "" || q.Limit != 40 {
		t.Errorf("thread asked as %+v", q)
	}
	if st.asked.exceptThread != "t7" || st.asked.topics.Limit != 10 {
		t.Errorf("topics asked as %+v except %q", st.asked.topics, st.asked.exceptThread)
	}
}

func TestBrief_AContinuedSessionGetsOnlyWhatIsNew(t *testing.T) {
	st, in := newBriefRoom()
	in.Session = store.MemberSession{ID: "s1", RoomSeen: 60, ThreadSeen: map[string]int64{"t7": 55}}
	st.replies = []store.Message{user("m9", "one more thing")}
	in.Triggers = st.replies
	text := build(t, st, in).Prompt

	wantInOrder(t, text,
		`This topic, #7 "@Claude plan the auth refactor", new since you last looked:`,
		">> [alice] one more thing\n",
	)
	// The root was read long ago and is not said again; with no news in
	// the room or in other topics, those parts are not there at all.
	for _, absent := range []string{"   [alice] @Claude plan", "in full", "New in the room", "Other topics"} {
		if strings.Contains(text, absent) {
			t.Errorf("brief should not hold %q:\n%s", absent, text)
		}
	}
	// From the session's positions on, leaving out what it said itself.
	if q := st.asked.room; q.After != 60 || q.SessionID != "s1" {
		t.Errorf("room news asked as %+v, want after 60 for session s1", q)
	}
	if q := st.asked.thread; q.After != 55 || q.SessionID != "s1" {
		t.Errorf("thread asked as %+v, want after 55 for session s1", q)
	}

	// A topic the session has not been in is shown in full, whatever it
	// has read of the room. That is also what follows a compaction, which
	// empties the topics read.
	in.Session.ThreadSeen = nil
	text = build(t, st, in).Prompt
	wantInOrder(t, text, "in full (oldest first):", "   [alice] @Claude plan the auth refactor\n", ">> [alice] one more thing\n")
	if q := st.asked.thread; q.After != 0 || q.SessionID != "" {
		t.Errorf("a topic shown in full holds the session's own words too: asked as %+v", q)
	}
	if q := st.asked.room; q.After != 60 {
		t.Errorf("the room is still read from where the session left it: asked as %+v", q)
	}
}

// A topic an agent's reply opened starts, when told in full, from the
// question in the room the reply answered: the topic says little without
// it, and after a compaction the room is not told again.
func TestBrief_ATopicToldInFullStartsFromItsQuestion(t *testing.T) {
	st, in := newBriefRoom()
	// Claude's reply m1 opened #7, answering alice's m0 in the room.
	question := user("m0", "@Claude remember the word saffron")
	root := agent("m1", "a1", "ok")
	root.TurnID = "turn1"
	st.messages["m0"], st.messages["m1"] = question, root
	st.turns = []store.Turn{{ID: "turn1", TriggerMessageID: "m0"}}
	ask := user("m2", "@Claude which word?")
	st.replies = []store.Message{ask}
	in.Triggers = []store.Message{ask}
	text := build(t, st, in).Prompt
	wantInOrder(t, text, "in full (oldest first):\n", "   (asked in the room) [alice] @Claude remember the word saffron\n", "   [Claude] ok\n", ">> [alice] @Claude which word?\n")

	// The room part of the same brief shows it: once is enough.
	st.roomNews = []store.RoomNewsItem{{Message: question}}
	text = build(t, st, in).Prompt
	if n := strings.Count(text, "remember the word saffron"); n != 1 {
		t.Errorf("the question should be shown once, not %d times:\n%s", n, text)
	}

	// A session that has been in the topic is told what is new only.
	st.roomNews = nil
	in.Session = store.MemberSession{ID: "s1", RoomSeen: 60, ThreadSeen: map[string]int64{"t7": 55}}
	if text := build(t, st, in).Prompt; strings.Contains(text, "saffron") {
		t.Errorf("a continued topic should not tell the question again:\n%s", text)
	}
}

func TestBrief_SaysWhoIsAtWork(t *testing.T) {
	st, in := newBriefRoom()
	now := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	files := []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go", "i.go", "j.go"}
	in.Busy = []busyMember{
		{Name: "Codex", Topic: 12, Since: now.Add(-90 * time.Minute), Files: files},
		{Name: "Keeper", Topic: 3, Since: now.Add(-4 * time.Minute), Upkeep: true, Waiting: []string{"allow running `make test`"}},
		{Name: "Pi", Topic: 9, Since: now.Add(-20 * time.Second)},
	}
	b := newBriefBuilder(st, briefLimits{Thread: 40, Room: 30, Topics: 10}, "")
	b.now = func() time.Time { return now }
	got, err := b.Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	wantInOrder(t, got.Prompt,
		"- Codex: You implement what was designed.\n",
		"\nAt work right now:\n",
		"- Codex, in topic #12, for 1 h 30 min; has changed a.go, b.go, c.go, d.go, e.go, f.go, g.go, h.go and 2 more\n",
		"- Keeper is tidying the wiki, for 4 min; waits for a person to allow running `make test`\n",
		"- Pi, in topic #9, started just now\n",
		"This topic, #7",
	)
}

func TestBrief_NothingNewInTheTopic(t *testing.T) {
	st, in := newBriefRoom()
	in.Session = store.MemberSession{ID: "s1", RoomSeen: 60, ThreadSeen: map[string]int64{"t7": 80}}
	trigger := user("m8", "@Claude are you there?")
	st.roomNews = []store.RoomNewsItem{{Message: trigger}}
	in.Triggers = []store.Message{trigger}
	text := build(t, st, in).Prompt

	wantInOrder(t, text, ">> [alice] @Claude are you there?\n", `You are in topic #7 "@Claude plan the auth refactor". Nothing new in it since you last looked.`)
}

func TestBrief_AskedInTheRoomTheQuestionComesLast(t *testing.T) {
	st, in := newBriefRoom()
	// The topic is the one the agent's reply will open: an empty root.
	st.messages["m1"] = agent("m1", "a1", "")
	ask := user("m8", "@Claude what is next?")
	st.roomNews = []store.RoomNewsItem{{Message: user("m6", "morning")}, {Message: agent("m1", "a1", ""), TopicNumber: 7}, {Message: ask}}
	st.topicNews = []store.TopicNewsItem{{ThreadID: "t3", Number: 3, NewCount: 2, Root: user("m30", "rate limits"), Last: user("m33", "ok")}}
	in.Triggers = []store.Message{ask}
	text := build(t, st, in).Prompt

	wantInOrder(t, text,
		"Other topics with news:\n",
		"Your reply will open topic #7.\n",
		"New in the room since you last looked:\n",
		"   [alice] morning\n",
		">> [alice] @Claude what is next?\n",
	)
	if !strings.HasSuffix(text, ">> [alice] @Claude what is next?\n") {
		t.Errorf("what the agent is to answer should come last:\n%s", text)
	}
	if strings.Contains(text, "[Claude] \n") {
		t.Errorf("the empty root says nothing and is not shown:\n%s", text)
	}
}

func TestBrief_CapsAreCounted(t *testing.T) {
	st, in := newBriefRoom()
	st.roomNews, st.roomTotal = []store.RoomNewsItem{{Message: user("m8", "latest")}}, 13
	st.topicNews, st.topicTotal = []store.TopicNewsItem{{ThreadID: "t3", Number: 3, NewCount: 2, Root: user("m30", "x"), Last: user("m33", "y")}}, 5
	st.replies, st.replyTotal = []store.Message{user("m4", "tail")}, 2
	text := build(t, st, in).Prompt

	wantInOrder(t, text,
		"New in the room since you last looked (12 earlier messages left out):",
		"   (and 4 more)\n",
		"in full (1 earlier reply left out) (oldest first):",
	)
}

func TestBrief_SaysWhenTheSessionIsNew(t *testing.T) {
	st, in := newBriefRoom()
	if text := build(t, st, in).Prompt; strings.Contains(text, "new session") {
		t.Errorf("a continued session needs no note:\n%s", text)
	}
	in.NewSession = store.SessionNotFound
	text := build(t, st, in).Prompt
	// Before anything else.
	if !strings.HasPrefix(text, "This is a new session") {
		t.Errorf("the note comes first:\n%s", text)
	}
	wantInOrder(t, text, "This is a new session", "the runtime no longer has it", "About the project:")
}

func TestBrief_ATriggerNoPartShowedIsNotLost(t *testing.T) {
	st, in := newBriefRoom()
	in.Session = store.MemberSession{ID: "s1", RoomSeen: 90, ThreadSeen: map[string]int64{"t7": 90}}
	in.Triggers = []store.Message{user("m99", "@Claude hello?")}
	text := build(t, st, in).Prompt
	wantInOrder(t, text, "Addressed to you:\n", ">> [alice] @Claude hello?\n")
}

func TestBrief_NamesAttachedFiles(t *testing.T) {
	st, in := newBriefRoom()
	st.messages["m1"] = store.Message{ID: "m1", SenderKind: store.SenderUser, UserID: "u1", Attachments: []store.Attachment{
		{ID: "at1", Filename: "shot.png", MediaType: "image/png", Size: 1234, Path: "r1/at1.png"},
	}}
	in.Triggers = []store.Message{st.messages["m1"]}
	text := build(t, st, in).Prompt

	// A root that is only a file still heads the topic, under the file's name.
	wantInOrder(t, text, `This topic, #7 "shot.png", in full`, ">> [alice] \n", "     (attached shot.png, image/png, 1234 bytes: /var/veyloom/attachments/r1/at1.png)\n")
}

func TestBrief_LooksEachSenderUpOnce(t *testing.T) {
	st, in := newBriefRoom()
	for i := range 6 {
		st.replies = append(st.replies, user(fmt.Sprintf("r%d", i), "again"))
	}
	build(t, st, in)
	if st.lookups != 1 {
		t.Errorf("alice was looked up %d times, want once", st.lookups)
	}
}

func TestExcerpt(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"two\n  lines\tand tabs", 40, "two lines and tabs"},
		{"cut right here please", 9, "cut right…"},
		{"中文也不能切在字中间", 10, "中文也…"},
		{"", 10, ""},
	}
	for _, c := range cases {
		if got := excerpt(c.in, c.max); got != c.want {
			t.Errorf("excerpt(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	cases := map[string]string{
		"# Architect\nYou design.":   "Architect",
		"\n\n  You implement.\nmore": "You implement.",
		"":                           "",
		"##\n":                       "",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// How a member talks and hands work on is standing, told as the project's
// relay limit has it: counted, unlimited, or with agents waking no one.
func TestBrief_RelayRulesFollowTheProjectsLimit(t *testing.T) {
	for limit, want := range map[int]string{
		30: "Agents waking one another stop after so many turns of one piece of work, which the brief tells",
		0:  "3 turns in a row that agents woke and that only talked, doing no work, stop agents waking one another in that piece of work",
		-1: "in this project a member you name is not woken by it",
	} {
		if got := relayRules(limit); !strings.Contains(got, want) {
			t.Errorf("limit %d: %s", limit, got)
		}
	}
}

// How members work together is Veyloom's skill team-practices, which the
// standing instructions point to when the turn has it; the practices
// themselves are the skill's, not the instructions' (design.md 5.23.6).
func TestBrief_PracticesAreASkill(t *testing.T) {
	if got := practicesLine([]string{"team-practices"}); !strings.Contains(got, "Veyloom's skill team-practices, which every agent has") {
		t.Errorf("the pointer: %q", got)
	}
	if got := practicesLine(nil); got != "" {
		t.Errorf("no skill, no pointer: %q", got)
	}
	for _, limit := range []int{30, 0, -1} {
		for _, practice := range []string{"hand on once that result is in", "When you are stuck"} {
			if strings.Contains(relayRules(limit), practice) {
				t.Errorf("limit %d: the practice %q is the skill's", limit, practice)
			}
		}
	}
}
