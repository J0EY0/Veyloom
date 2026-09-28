package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/machine"
	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

// loop is a complete single-machine setup on a real database: hub, an
// in-process machine with the fake runtime, one project with its main room,
// one user. It is what `veyloom serve` runs, minus HTTP.
type loop struct {
	t         *testing.T
	ctx       context.Context
	s         *store.Store
	h         *Hub
	room      store.Room
	user      store.User
	machineID string
	// worktrees is where the machine makes the members' worktrees.
	worktrees string
	// cfg and opts made the hub, for restart; identity is the machine's,
	// kept by it across its connections.
	cfg      Config
	opts     []Option
	identity *machine.MemoryIdentity
	// runners are what the machine runs turns with: the built-in ones
	// unless a test gives it more (fixedRuntime).
	runners map[string]runtime.Runner
	// hubRun is the hub's life; stopHub and stopMachine end the hub and
	// its machine's connection.
	hubRun               context.Context
	stopHub, stopMachine context.CancelFunc
}

func newLoop(t *testing.T) *loop {
	t.Helper()
	return newLoopWith(t, Config{})
}

// newLoopWith is newLoop with hub settings a test wants to pin, and hub
// options such as a clock.
func newLoopWith(t *testing.T, cfg Config, opts ...Option) *loop {
	t.Helper()
	s := storetest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg.TranscriptDir = t.TempDir()
	cfg.HeartbeatInterval = time.Hour
	worktrees, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	l := &loop{t: t, ctx: ctx, s: s, worktrees: worktrees, cfg: cfg, opts: opts, identity: &machine.MemoryIdentity{}}
	l.startHub()

	_, room, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	l.room, l.user = room, user
	return l
}

// startHub starts a hub over the loop's store and connects the machine.
func (l *loop) startHub() {
	l.t.Helper()
	run, stop := context.WithCancel(l.ctx)
	l.t.Cleanup(stop)
	l.h, l.hubRun, l.stopHub = New(l.s, l.cfg, l.opts...), run, stop
	l.connectMachine()
}

// connectMachine connects the loop's machine to its hub, as itself.
func (l *loop) connectMachine() {
	l.t.Helper()
	run, stop := context.WithCancel(l.ctx)
	l.t.Cleanup(stop)
	l.stopMachine = stop
	runners := l.runners
	if runners == nil {
		runners = runtime.BuiltinRunners()
	}
	w := machine.New(machine.Config{Name: "laptop", ToolDir: l.t.TempDir(), WorktreeDir: l.worktrees}, machine.NewDiscovery(nil, time.Second), l.identity, runners)
	hubEnd, machineEnd := protocol.Pipe()
	h := l.h
	go h.Serve(l.hubRun, hubEnd)
	go w.Run(run, machineEnd)
	eventually(l.t, func() bool { return len(h.Machines()) == 1 }, "machine to connect")
	l.machineID = h.Machines()[0].ID
}

// dropMachine takes the machine off its hub, as when it loses the network.
func (l *loop) dropMachine() {
	l.t.Helper()
	l.stopMachine()
	eventually(l.t, func() bool { return len(l.h.Machines()) == 0 }, "machine to go")
}

// restart stops the hub and its machine and starts them again over the
// same store, as `veyloom serve` does after a stop: the turns the old hub
// left running are failed first.
func (l *loop) restart() {
	l.t.Helper()
	l.stopHub()
	l.dropMachine()
	if _, err := l.s.FailRunningTurns(l.ctx, "the hub stopped while the turn was running"); err != nil {
		l.t.Fatal(err)
	}
	if _, err := l.s.ReleaseRunningDrafts(l.ctx); err != nil {
		l.t.Fatal(err)
	}
	l.startHub()
}

// member sets up a fake-runtime agent with the given options and adds it
// to the room.
func (l *loop) member(name string, options map[string]any) store.Member {
	l.t.Helper()
	agent, err := l.s.CreateAgent(l.ctx, store.NewAgent{
		Name: name + " agent", MachineID: l.machineID, Runtime: "fake", PermissionPreset: store.PermissionReadOnly, RoleCard: "Be brief.", RuntimeOptions: options,
	})
	if err != nil {
		l.t.Fatal(err)
	}
	member, err := l.s.CreateMember(l.ctx, store.NewMember{RoomID: l.room.ID, AgentID: agent.ID, DisplayName: name})
	if err != nil {
		l.t.Fatal(err)
	}
	return member
}

// say posts a user message, optionally in a thread, mentioning agents.
func (l *loop) say(body, threadID string, mentions ...store.Member) store.Message {
	l.t.Helper()
	msg := store.NewMessage{RoomID: l.room.ID, ThreadID: threadID, UserID: l.user.ID, Body: body}
	for _, a := range mentions {
		msg.Mentions = append(msg.Mentions, store.Mention{Kind: store.MentionAgent, ID: a.ID})
	}
	posted, err := l.h.PostUserMessage(l.ctx, msg)
	if err != nil {
		l.t.Fatal(err)
	}
	return posted
}

// replies returns the messages of a thread by sender kind.
func (l *loop) replies(threadID string, kind store.SenderKind) []store.Message {
	l.t.Helper()
	all, err := l.s.ListThreadMessages(l.ctx, threadID, 0, 200)
	if err != nil {
		l.t.Fatal(err)
	}
	var out []store.Message
	for _, m := range all {
		if m.SenderKind == kind {
			out = append(out, m)
		}
	}
	return out
}

// topic waits for the turn msg triggered and returns the thread it runs in:
// a top-level mention opens a topic rooted at the agent's own message.
func (l *loop) topic(msg store.Message) store.Thread {
	l.t.Helper()
	var turn store.Turn
	eventually(l.t, func() bool {
		for _, t := range l.turns() {
			if t.TriggerMessageID == msg.ID {
				turn = t
				return true
			}
		}
		return false
	}, "the turn for "+msg.Body)
	thread, err := l.s.GetThread(l.ctx, turn.ThreadID)
	if err != nil {
		l.t.Fatal(err)
	}
	return thread
}

// root returns the message a thread is rooted at.
func (l *loop) root(thread store.Thread) store.Message {
	l.t.Helper()
	msg, err := l.s.GetMessage(l.ctx, thread.RootMessageID)
	if err != nil {
		l.t.Fatal(err)
	}
	return msg
}

// topLevel returns the room's top-level messages, oldest first.
func (l *loop) topLevel() []store.Message {
	l.t.Helper()
	msgs, err := l.s.ListRoomMessages(l.ctx, l.room.ID, 0, 200)
	if err != nil {
		l.t.Fatal(err)
	}
	return msgs
}

func (l *loop) turns() []store.Turn {
	l.t.Helper()
	turns, err := l.s.ListRoomTurns(l.ctx, l.room.ID, 50)
	if err != nil {
		l.t.Fatal(err)
	}
	return turns
}

func (l *loop) waitTurns(n int, status store.TurnStatus, what string) []store.Turn {
	l.t.Helper()
	var turns []store.Turn
	eventually(l.t, func() bool {
		turns = l.turns()
		if len(turns) != n {
			return false
		}
		for _, turn := range turns {
			if turn.Status != status {
				return false
			}
		}
		return true
	}, what)
	return turns
}

func TestLoop_MentionOpensATopicHeadedByTheReply(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", nil)

	msg := l.say("@Echo hello there", "", echo)

	turns := l.waitTurns(1, store.TurnDone, "the turn to finish")
	// The runtime it ran on and the tokens it reported are kept with the turn.
	if turns[0].Runtime != "fake" {
		t.Errorf("turn runtime = %q, want fake", turns[0].Runtime)
	}
	if u := turns[0].Usage; u.InputTokens == 0 || u.OutputTokens == 0 {
		t.Errorf("the turn should keep what it spent, got %+v", u)
	}
	thread := l.topic(msg)
	root := l.root(thread)
	if root.SenderKind != store.SenderAgent || root.MemberID != echo.ID || root.ThreadID != "" {
		t.Fatalf("the topic root should be the agent's own top-level message, got %+v", root)
	}
	// The fake runtime echoes the last brief line: the trigger, marked, comes
	// first and the empty root is skipped, so that is what it echoes.
	if !strings.Contains(root.Body, ">> [alice] @Echo hello there") {
		t.Errorf("root body = %q", root.Body)
	}
	if len(l.replies(thread.ID, store.SenderAgent)) != 0 {
		t.Error("a one-breath reply lives in the root, not in the thread")
	}
	// One segment and no tools: the root is the whole answer, no closing.
	if top := l.topLevel(); len(top) != 2 || top[0].ID != msg.ID || top[1].ID != root.ID {
		t.Errorf("room should hold the question and the root only, got %+v", top)
	}

	turn := turns[0]
	if turn.ReplyMessageID != root.ID || turn.ThreadID != thread.ID || turn.TriggerMessageID != msg.ID || turn.EndedAt == nil || root.TurnID != turn.ID {
		t.Errorf("unexpected turn record: %+v (root %+v)", turn, root)
	}
	data, err := os.ReadFile(turn.TranscriptPath)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 3 || !strings.Contains(lines[0], `"kind":"start"`) || !strings.Contains(lines[len(lines)-1], `"kind":"done"`) {
		t.Errorf("transcript should hold start, events and done:\n%s", data)
	}
	if !strings.Contains(lines[0], "Be brief.") {
		t.Error("the start record should carry the spec with the role card")
	}

	session, err := l.s.GetOpenSession(l.ctx, echo.ID)
	if err != nil {
		t.Fatalf("the turn should leave the member a session: %v", err)
	}
	if turn.SessionID != session.ID || !session.Started() || session.Runtime != "fake" || session.MachineID != l.machineID {
		t.Errorf("session = %+v, want the turn's (%s), started, on the fake runtime of this machine", session, turn.SessionID)
	}
}

func TestLoop_ToolTurnSplitsTextAndAddressesTheLastWord(t *testing.T) {
	l := newLoop(t)
	machine := l.member("Machine", map[string]any{"tool": true, "preamble": "let me look", "reply": "all done"})

	msg := l.say("@Machine go", "", machine)

	turns := l.waitTurns(1, store.TurnDone, "the turn to finish")
	thread := l.topic(msg)
	root := l.root(thread)
	if root.Body != "let me look" {
		t.Errorf("the text before the tool call fills the root, got %q", root.Body)
	}
	// The text after the tool call is a reply in the topic, the turn's last
	// word, addressed to whoever asked so it reaches their inbox.
	replies := l.replies(thread.ID, store.SenderAgent)
	if len(replies) != 1 || replies[0].Body != "@alice all done" || replies[0].TurnID != turns[0].ID {
		t.Errorf("the text after the tool call is a reply in the topic, got %+v", replies)
	}
	if len(replies) == 1 && (len(replies[0].Mentions) != 1 || replies[0].Mentions[0] != (store.Mention{Kind: store.MentionUser, ID: l.user.ID})) {
		t.Errorf("the last word mentions whoever asked, got %+v", replies[0].Mentions)
	}
	if turns[0].ReplyMessageID != replies[0].ID {
		t.Errorf("the turn's reply is its last message, got %q", turns[0].ReplyMessageID)
	}
	if top := l.topLevel(); len(top) != 2 {
		t.Fatalf("room should hold the question and the root, nothing more, got %+v", top)
	}

	summaries, err := l.s.ThreadSummaries(l.ctx, []string{root.ID, msg.ID})
	if err != nil {
		t.Fatal(err)
	}
	summary, ok := summaries[root.ID]
	if !ok || summary.ID != thread.ID || summary.ReplyCount != 1 || summary.Turns != 1 || summary.LastTurn == nil || summary.LastTurn.Status != store.TurnDone || summary.LastReplyAt == nil {
		t.Errorf("summary of the root = %+v", summary)
	}
	if _, ok := summaries[msg.ID]; ok {
		t.Error("the question heads no topic")
	}
}

// A person asking again in a topic gets the answer addressed to them, in
// the topic: they would not see it in the room otherwise. The answer at the
// head of the topic, given in one breath right under the question, is not.
func TestLoop_AnAnswerInATopicIsAddressed(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", map[string]any{"reply": "looked"})
	msg := l.say("@Echo look", "", echo)
	l.waitTurns(1, store.TurnDone, "the first answer")
	thread := l.topic(msg)
	if root := l.root(thread); root.Body != "looked" || len(root.Mentions) != 0 {
		t.Errorf("an answer in one breath under the question: %+v", root)
	}
	l.say("and again?", thread.ID, echo)
	l.waitTurns(2, store.TurnDone, "the answer in the topic")
	replies := l.replies(thread.ID, store.SenderAgent)
	if len(replies) != 1 || replies[0].Body != "@alice looked" || !slices.Equal(replies[0].Mentions, []store.Mention{{Kind: store.MentionUser, ID: l.user.ID}}) {
		t.Errorf("the answer in the topic: %+v", replies)
	}
	if top := l.topLevel(); len(top) != 2 {
		t.Errorf("the room should hold the question and the root only, got %+v", top)
	}
}

// Reaching for a tool, as Claude Code does to load Veyloom's, is not using
// one: the answer is still given in one breath, and no work.
func TestLoop_ReachingForAToolIsStillOneBreath(t *testing.T) {
	l := newLoop(t)
	reach := map[string]any{"tool": "ToolSearch", "args": map[string]any{"query": "select:mcp__veyloom__read_topic"}}
	reacher := l.member("Reacher", map[string]any{"tool_calls": []any{reach}, "reply": "ok"})
	msg := l.say("@Reacher hi", "", reacher)
	turns := l.waitTurns(1, store.TurnDone, "the turn")
	if root := l.root(l.topic(msg)); root.Body != "ok" || len(root.Mentions) != 0 {
		t.Errorf("the answer after reaching for a tool: %+v", root)
	}
	if turns[0].Worked {
		t.Error("reaching for a tool is no work")
	}
}

// A turn that says nothing after its tools has its last word, said before
// them, addressed to whoever asked, in place.
func TestLoop_TheLastWordBeforeTheToolsIsAddressed(t *testing.T) {
	l := newLoop(t)
	quiet := l.member("Quiet", map[string]any{"tool": true, "preamble": "on it", "quiet": true})
	msg := l.say("@Quiet go", "", quiet)
	turns := l.waitTurns(1, store.TurnDone, "the turn")
	root := l.root(l.topic(msg))
	if root.Body != "@alice on it" || !slices.Equal(root.Mentions, []store.Mention{{Kind: store.MentionUser, ID: l.user.ID}}) || turns[0].ReplyMessageID != root.ID {
		t.Errorf("the root, the turn's last word: %+v (turn's reply %s)", root, turns[0].ReplyMessageID)
	}
	if len(l.replies(l.topic(msg).ID, store.SenderAgent)) != 0 || len(l.topLevel()) != 2 {
		t.Error("nothing more is said")
	}
}

func TestAddressTo(t *testing.T) {
	for text, want := range map[string]string{
		"done":            "@alice done",
		"@alice done":     "@alice done",
		"@alice，好了":       "@alice，好了",
		"@alice":          "@alice",
		"@alicex done":    "@alice @alicex done",
		"# Plan\n\n- one": "@alice\n\n# Plan\n\n- one",
	} {
		if got := addressTo("alice", text); got != want {
			t.Errorf("addressTo(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestLoop_AnAgentMentioningAnotherRelaysWithinTheTopic(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", map[string]any{"reply": "looked, fine"})
	hander := l.member("Hander", map[string]any{"tool": true, "reply": "交给 @Echo 看看，@alice 你也看下", "summary_reply": "Echo 看过了"})

	msg := l.say("@Hander go", "", hander)
	// Hander, Echo, and Hander summing up what it handed on.
	turns := l.settle(3, "Hander's turn, Echo's relayed turn and the summing up")
	thread := l.topic(msg)

	// With no text before the tool call, the reply is the root itself; it
	// records the agent it names. It hands the work on, so it is not
	// addressed to whoever asked: the summing up is.
	root := l.root(thread)
	if root.Body != "交给 @Echo 看看，@alice 你也看下" || !slices.Equal(root.Mentions, []store.Mention{{Kind: store.MentionAgent, ID: echo.ID}}) {
		t.Errorf("the reply should record the agent it names, and no more, got %+v", root)
	}
	if top := l.topLevel(); len(top) != 2 {
		t.Errorf("the room should hold the question and the root only, got %+v", top)
	}
	// Naming Echo woke it, inside the same topic, answering the reply that
	// named it.
	var relayed *store.Turn
	for i := range turns {
		if turns[i].MemberID == echo.ID {
			relayed = &turns[i]
		}
	}
	if relayed == nil || relayed.ThreadID != thread.ID || relayed.TriggerMessageID != root.ID {
		t.Fatalf("Echo should have run in the topic, triggered by the reply naming it, got %+v", relayed)
	}
	// Echo's answer names nobody, so the work handed on ends there, and
	// Hander sums it up for the person.
	replies := l.replies(thread.ID, store.SenderAgent)
	if len(replies) != 2 || replies[0].MemberID != echo.ID || replies[1].MemberID != hander.ID || replies[1].Body != "@alice Echo 看过了" {
		t.Errorf("Echo's answer, then Hander summing up, got %+v", replies)
	}
}

// A name in an agent's code, as when it quotes how to ask for something,
// wakes no one and is recorded as no mention.
func TestLoop_ANameInCodeWakesNoOne(t *testing.T) {
	l := newLoop(t)
	l.member("Echo", map[string]any{"reply": "looked, fine"})
	teller := l.member("Teller", map[string]any{"reply": "写 `@Echo` 就能叫它，或者：\n```\n@Echo 看看\n```"})
	msg := l.say("@Teller how do I ask Echo?", "", teller)
	l.waitTurns(1, store.TurnDone, "Teller's turn")
	if root := l.root(l.topic(msg)); len(root.Mentions) != 0 {
		t.Errorf("mentions recorded from code: %+v", root.Mentions)
	}
	time.Sleep(200 * time.Millisecond)
	if turns := l.turns(); len(turns) != 1 {
		t.Errorf("woken from code: %+v", turns)
	}
}

func TestLoop_AnAgentTakenOutOfTheProjectIsNeitherWokenNorNamed(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", map[string]any{"reply": "looked, fine"})
	hander := l.member("Hander", map[string]any{"reply": "交给 @Echo 看看"})
	if _, err := l.s.RemoveMember(l.ctx, echo.ID); err != nil {
		t.Fatal(err)
	}

	// Asked directly it stays quiet; named by another agent it is neither
	// recorded as a mention nor woken.
	l.say("@Echo still there?", "", echo)
	msg := l.say("@Hander go", "", hander)
	l.waitTurns(1, store.TurnDone, "Hander's turn")
	time.Sleep(300 * time.Millisecond)
	if turns := l.turns(); len(turns) != 1 || turns[0].MemberID != hander.ID {
		t.Fatalf("only Hander should have run, got %+v", turns)
	}
	thread := l.topic(msg)
	if root := l.root(thread); len(root.Mentions) != 0 {
		t.Errorf("a removed agent is not a mention, got %+v", root.Mentions)
	}

	// Asked just before it was taken out, it says so in the topic instead
	// of starting.
	if err := l.h.turns.TriggerIn(l.ctx, echo, msg, thread); err != nil {
		t.Fatal(err)
	}
	var notes []store.Message
	eventually(t, func() bool {
		notes = l.replies(thread.ID, store.SenderSystem)
		return len(notes) > 0
	}, "the note that Echo was taken out")
	time.Sleep(100 * time.Millisecond)
	notes = l.replies(thread.ID, store.SenderSystem)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "taken out of the project") {
		t.Errorf("want one note that Echo was taken out, got %+v", notes)
	}
	if n := len(l.turns()); n != 1 {
		t.Errorf("want still 1 turn, got %d", n)
	}
}

// An @ means the longest name after it: with Coder and Coder2 in the room,
// "@Coder2" wakes Coder2 and not Coder, and once Coder2 is taken out it
// wakes no one, Coder2 still owning its name.
func TestLoop_AnAtMeansTheLongestNameAfterIt(t *testing.T) {
	l := newLoop(t)
	coder := l.member("Coder", map[string]any{"reply": "coded"})
	coder2 := l.member("Coder2", map[string]any{"reply": "coded too"})
	hander := l.member("Hander", map[string]any{"tool": true, "reply": "交给 @Coder2 看看", "summary_reply": "Coder2 看过了"})

	msg := l.say("@Hander go", "", hander)
	// Hander, Coder2, and Hander taking back what it handed on.
	turns := l.settle(3, "Hander's turn, Coder2's and the hand back")
	if slices.ContainsFunc(turns, func(turn store.Turn) bool { return turn.MemberID == coder.ID }) {
		t.Fatalf("Coder was woken by @Coder2: %+v", turns)
	}
	if root := l.root(l.topic(msg)); !slices.Equal(root.Mentions, []store.Mention{{Kind: store.MentionAgent, ID: coder2.ID}}) {
		t.Errorf("the reply names Coder2 alone, got %+v", root.Mentions)
	}

	if _, err := l.s.RemoveMember(l.ctx, coder2.ID); err != nil {
		t.Fatal(err)
	}
	again := l.say("@Hander again", "", hander)
	turns = l.settle(4, "Hander's second turn")
	if slices.ContainsFunc(turns, func(turn store.Turn) bool { return turn.MemberID == coder.ID }) {
		t.Fatalf("Coder was woken in place of Coder2: %+v", turns)
	}
	// Handing nothing on, Hander's word is addressed to the person instead.
	if root := l.root(l.topic(again)); slices.ContainsFunc(root.Mentions, func(m store.Mention) bool { return m.Kind == store.MentionAgent }) {
		t.Errorf("a member taken out is no mention, nor is the name it begins with: %+v", root.Mentions)
	}
}

// The person's name competes for an @ too: a member whose name begins it
// is not woken when an agent names the person.
func TestLoop_NamingThePersonWakesNoMemberWhoseNameBeginsTheirs(t *testing.T) {
	l := newLoop(t)
	ali := l.member("ali", map[string]any{"reply": "here"})
	lead := l.member("Lead", map[string]any{"tool_calls": []any{sendCall("@alice have a look", "")}, "reply": "Done."})
	msg := l.say("@Lead go", "", lead)
	l.settle(1, "Lead's turn")
	said := l.saidIn(l.topic(msg))
	sent := slices.IndexFunc(said, func(m store.Message) bool {
		return m.Body == "@alice have a look" && slices.Equal(m.Mentions, []store.Mention{{Kind: store.MentionUser, ID: l.user.ID}})
	})
	if sent < 0 {
		t.Errorf("the message names the person alone: %+v", said)
	}
	if turns := l.turns(); slices.ContainsFunc(turns, func(turn store.Turn) bool { return turn.MemberID == ali.ID }) {
		t.Errorf("ali was woken by @alice: %+v", turns)
	}
}

func TestLoop_ThreadFollowUpResumesSession(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", nil)
	msg := l.say("@Echo start", "", echo)
	l.waitTurns(1, store.TurnDone, "first turn")
	thread := l.topic(msg)
	first, err := l.s.GetOpenSession(l.ctx, echo.ID)
	if err != nil {
		t.Fatal(err)
	}

	// No mention: in a topic that goes to the agent heading it.
	l.say("and now?", thread.ID)

	turns := l.waitTurns(2, store.TurnDone, "second turn")
	if got := l.replies(thread.ID, store.SenderAgent); len(got) != 1 || !strings.Contains(got[0].Body, ">> [alice] and now?") {
		t.Fatalf("expected the follow-up answered in the topic, got %+v", got)
	}
	if turns[0].SessionID != first.ID || turns[1].SessionID != first.ID {
		t.Errorf("both turns should run in session %s, got %s and %s", first.ID, turns[1].SessionID, turns[0].SessionID)
	}
	data, _ := os.ReadFile(turns[0].TranscriptPath)
	if want := `"session":{"key":"` + first.ID + `","ref":"` + first.Ref + `","resume":true}`; !strings.Contains(string(data), want) {
		t.Errorf("the second turn should resume the session the first started: want %s in\n%s", want, data)
	}
	first1, _ := os.ReadFile(turns[1].TranscriptPath)
	if want := `"session":{"key":"` + first.ID + `"}`; !strings.Contains(string(first1), want) {
		t.Errorf("the first turn should start the session, not resume it: want %s in\n%s", want, first1)
	}
}

func TestLoop_MovedMemberStartsANewSession(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", nil)
	msg := l.say("@Echo start", "", echo)
	l.waitTurns(1, store.TurnDone, "first turn")
	thread := l.topic(msg)
	first, err := l.s.GetOpenSession(l.ctx, echo.ID)
	if err != nil {
		t.Fatal(err)
	}

	moved := "/somewhere/else"
	if _, err := l.s.UpdateMember(l.ctx, echo.ID, store.MemberPatch{RepoPath: &moved}); err != nil {
		t.Fatal(err)
	}
	l.say("and now?", thread.ID)
	turns := l.waitTurns(2, store.TurnDone, "turn after the move")

	second, err := l.s.GetOpenSession(l.ctx, echo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.WorkDir != moved || turns[0].SessionID != second.ID {
		t.Errorf("open session = %+v, want a new one in %s carrying the turn %s", second, moved, turns[0].SessionID)
	}
	old, err := l.s.GetSession(l.ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.EndedAt == nil || old.EndReason != store.SessionDirChanged {
		t.Errorf("the session left behind = %+v, want ended as dir_changed", old)
	}
	data, _ := os.ReadFile(turns[0].TranscriptPath)
	if strings.Contains(string(data), `"resume":true`) {
		t.Errorf("a turn in a new session must not resume:\n%s", data)
	}
}

func TestLoop_FailedFirstTurnStillLeavesASession(t *testing.T) {
	l := newLoop(t)
	flaky := l.member("Flaky", map[string]any{"fail": true})
	l.say("@Flaky go", "", flaky)
	turns := l.waitTurns(1, store.TurnFailed, "the failing turn")

	// The runtime named its session before it failed, and that was stored
	// at once: the next turn continues instead of starting over.
	var session store.MemberSession
	eventually(t, func() bool {
		var err error
		session, err = l.s.GetOpenSession(l.ctx, flaky.ID)
		return err == nil && session.Started()
	}, "the session reference of the failed turn")
	if turns[0].SessionID != session.ID {
		t.Errorf("turn ran in %s, open session is %s", turns[0].SessionID, session.ID)
	}
}

func TestLoop_TurnsOfOneAgentNeverOverlap(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"delay_ms": float64(150)})

	for i := 0; i < 3; i++ {
		l.say("@Slow go", "", slow)
	}

	eventually(t, func() bool {
		turns := l.turns()
		done := 0
		for _, turn := range turns {
			if turn.Status == store.TurnDone {
				done++
			}
		}
		// Three triggers in three threads: three turns, run one after another.
		return done == 3
	}, "all three turns to finish")
	turns := l.turns() // newest first
	for i := 0; i+1 < len(turns); i++ {
		later, earlier := turns[i], turns[i+1]
		if earlier.EndedAt == nil || later.StartedAt.Before(*earlier.EndedAt) {
			t.Errorf("turn %s started at %v before %s ended at %v", later.ID, later.StartedAt, earlier.ID, earlier.EndedAt)
		}
	}
}

func TestLoop_QueuedTriggersInOneThreadMergeIntoOneTurn(t *testing.T) {
	l := newLoop(t)
	// A runtime that takes nothing while a turn runs: what is said in the
	// topic meanwhile waits for the next turn (steer.go).
	slow := l.member("Slow", map[string]any{"delay_ms": float64(200), "steer": "refuse"})
	msg := l.say("@Slow first", "", slow)
	thread := l.topic(msg)

	// They mention it explicitly and queue behind the first turn.
	l.say("second", thread.ID, slow)
	l.say("third", thread.ID, slow)

	turns := l.waitTurns(2, store.TurnDone, "the merged second turn")
	if turns[0].TriggerMessageID == "" {
		t.Error("the merged turn should record its last trigger")
	}
	// The second turn's brief carries both queued messages as triggers.
	data, _ := os.ReadFile(turns[0].TranscriptPath)
	if !strings.Contains(string(data), ">> [alice] second") || !strings.Contains(string(data), ">> [alice] third") {
		t.Errorf("merged brief should mark both queued messages:\n%s", data)
	}
}

// queued is what the store keeps waiting on the loop's machine.
func (l *loop) queued() []store.QueuedWake {
	l.t.Helper()
	q, err := l.s.ListQueuedWakes(l.ctx, l.machineID)
	if err != nil {
		l.t.Fatal(err)
	}
	return q
}

// What a busy member was asked outlives the hub: while the machine is away
// it is neither started nor failed, and the next hub answers it once the
// machine is back, the store having kept it (queue.go).
func TestLoop_WhatWaitsOutlivesTheHub(t *testing.T) {
	l := newLoop(t)
	busy := l.member("Busy", map[string]any{"reply": "done", "delay_ms": float64(5000)})
	l.say("@Busy one", "", busy)
	l.waitTurns(1, store.TurnRunning, "Busy's first turn")
	second := l.say("@Busy two", "", busy)
	eventually(t, func() bool { q := l.queued(); return len(q) == 1 && q[0].MessageID == second.ID }, "the second ask to be kept")

	// The hub stops, its machine with it: the running turn fails, the
	// second ask waits.
	l.setOptions(busy, map[string]any{"reply": "done"})
	l.restart()
	eventually(t, func() bool {
		turns := l.turns()
		return len(turns) == 2 && turns[0].TriggerMessageID == second.ID && turns[0].Status == store.TurnDone
	}, "the next hub to answer the second ask")
	if turns := l.turns(); turns[1].Status != store.TurnFailed {
		t.Errorf("the turn the stop cut off: %+v", turns[1])
	}
	if q := l.queued(); len(q) != 0 {
		t.Errorf("answered, it waits no longer: %+v", q)
	}
	// Nothing was started for it while the hub was down, so no note of a
	// failure to start.
	for _, note := range l.notes(l.topic(second).ID) {
		if strings.Contains(note, "could not start") {
			t.Errorf("a note of a failed start: %q", note)
		}
	}
}

// A machine that drops off and comes back finds what its members were
// asked meanwhile waiting in the same hub, and goes on with it.
func TestLoop_WhatWaitsGoesOnWhenTheMachineIsBack(t *testing.T) {
	l := newLoop(t)
	busy := l.member("Busy", map[string]any{"reply": "done", "delay_ms": float64(5000)})
	l.say("@Busy one", "", busy)
	l.waitTurns(1, store.TurnRunning, "Busy's first turn")
	second := l.say("@Busy two", "", busy)
	eventually(t, func() bool { return len(l.queued()) == 1 }, "the second ask to be kept")

	l.setOptions(busy, map[string]any{"reply": "done"})
	l.dropMachine()
	// Cancelled by the machine as it went, or failed by the hub: over.
	eventually(t, func() bool { turns := l.turns(); return len(turns) == 1 && turns[0].Status != store.TurnRunning }, "the first turn to end with its machine")
	time.Sleep(200 * time.Millisecond)
	if n := len(l.turns()); n != 1 || len(l.queued()) != 1 {
		t.Fatalf("with the machine away the second ask waits: %d turns, %+v", n, l.queued())
	}

	l.connectMachine()
	eventually(t, func() bool {
		turns := l.turns()
		return len(turns) == 2 && turns[0].TriggerMessageID == second.ID && turns[0].Status == store.TurnDone
	}, "the second ask to be answered once the machine is back")
	time.Sleep(200 * time.Millisecond)
	if n := len(l.turns()); n != 2 || len(l.queued()) != 0 {
		t.Errorf("answered once: %d turns, %+v queued", n, l.queued())
	}
}

func TestLoop_FailureIsReportedInThread(t *testing.T) {
	l := newLoop(t)
	broken := l.member("Broken", map[string]any{"fail": true})

	msg := l.say("@Broken go", "", broken)

	turns := l.waitTurns(1, store.TurnFailed, "the failed turn")
	if !strings.Contains(turns[0].Error, "scripted failure") {
		t.Errorf("turn error = %q", turns[0].Error)
	}
	thread := l.topic(msg)
	notes := l.replies(thread.ID, store.SenderSystem)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "Broken failed") || notes[0].TurnID != turns[0].ID {
		t.Errorf("expected a system note about the failure, filed under the turn, got %+v", notes)
	}
	if len(l.replies(thread.ID, store.SenderAgent)) != 0 || l.root(thread).Body != "" {
		t.Error("a failed turn must not post a reply; its root stays empty")
	}
	if top := l.topLevel(); len(top) != 2 {
		t.Errorf("a failed turn posts no closing message, got %+v", top)
	}
}

func TestLoop_OfflineMachineFailsTheTurn(t *testing.T) {
	l := newLoop(t)
	ghostID, err := l.s.RegisterMachine(l.ctx, "", "ghost", nil)
	if err != nil {
		t.Fatal(err)
	}
	agent, _ := l.s.CreateAgent(l.ctx, store.NewAgent{Name: "Ghost", MachineID: ghostID, Runtime: "fake", PermissionPreset: store.PermissionReadOnly})
	ghost, err := l.s.CreateMember(l.ctx, store.NewMember{RoomID: l.room.ID, AgentID: agent.ID, DisplayName: "Ghost"})
	if err != nil {
		t.Fatal(err)
	}

	msg := l.say("@Ghost anyone home?", "", ghost)

	turns := l.waitTurns(1, store.TurnFailed, "the offline turn to fail")
	if !strings.Contains(turns[0].Error, "offline") {
		t.Errorf("turn error = %q", turns[0].Error)
	}
	thread := l.topic(msg)
	if notes := l.replies(thread.ID, store.SenderSystem); len(notes) != 1 || !strings.Contains(notes[0].Body, "offline") {
		t.Errorf("expected a system note about the offline machine, got %+v", notes)
	}
}

func TestLoop_Cancel(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"delay_ms": float64(30000)})
	l.say("@Slow take your time", "", slow)

	var turnID string
	eventually(t, func() bool {
		turns := l.turns()
		if len(turns) == 1 && l.h.TurnRunning(turns[0].ID) {
			turnID = turns[0].ID
			return true
		}
		return false
	}, "the turn to be running")

	if err := l.h.CancelTurn(l.ctx, turnID, false); err != nil {
		t.Fatal(err)
	}

	turns := l.waitTurns(1, store.TurnCancelled, "the turn to be cancelled")
	if turns[0].EndedAt == nil {
		t.Error("a cancelled turn is finished")
	}
	if err := l.h.CancelTurn(l.ctx, turnID, false); err == nil {
		t.Error("cancelling a finished turn should report ErrUnknownTurn")
	}
}

func TestLoop_AskingTwoAgentsOpensOneTopicRootedAtTheAsk(t *testing.T) {
	l := newLoop(t)
	alpha := l.member("Alpha", map[string]any{"reply": "Alpha here"})
	beta := l.member("Beta", map[string]any{"reply": "Beta here"})

	ask := l.say("@Alpha @Beta look at this together", "", alpha, beta)
	turns := l.waitTurns(2, store.TurnDone, "both turns to finish")

	// Each is told the other was asked too, and to do its own part now.
	for _, turn := range turns {
		other := "Beta"
		if turn.MemberID == beta.ID {
			other = "Alpha"
		}
		if prompt := specOf(t, turn).Prompt; !strings.Contains(prompt, "The person asked "+other+" in the same message as you") {
			t.Errorf("the brief does not say %s was asked too:\n%s", other, prompt)
		}
	}
	// One topic for both, rooted at the ask itself, not one per agent.
	if turns[0].ThreadID != turns[1].ThreadID {
		t.Fatalf("the two turns should share a topic, got %s and %s", turns[0].ThreadID, turns[1].ThreadID)
	}
	thread := l.topic(ask)
	if root := l.root(thread); root.ID != ask.ID || root.SenderKind != store.SenderUser {
		t.Fatalf("the topic should be rooted at the ask, got %+v", root)
	}
	if top := l.topLevel(); len(top) != 1 || top[0].ID != ask.ID {
		t.Errorf("the room should hold the ask alone, no agent roots and no closing, got %d messages", len(top))
	}

	// Each agent answered in the thread, and addressed the person who asked.
	replies := l.replies(thread.ID, store.SenderAgent)
	if len(replies) != 2 {
		t.Fatalf("want a reply from each agent, got %d", len(replies))
	}
	seen := map[string]bool{}
	for _, r := range replies {
		seen[r.MemberID] = true
		if !strings.HasPrefix(r.Body, "@alice ") {
			t.Errorf("reply should address the asker, got %q", r.Body)
		}
		var mentionsAsker bool
		for _, m := range r.Mentions {
			if m.Kind == store.MentionUser && m.ID == l.user.ID {
				mentionsAsker = true
			}
		}
		if !mentionsAsker {
			t.Errorf("reply should mention the asker, got %+v", r.Mentions)
		}
	}
	if !seen[alpha.ID] || !seen[beta.ID] {
		t.Errorf("both agents should have replied, got %v", seen)
	}
	// And it reaches the inbox.
	items, err := l.s.ListUserMentions(l.ctx, l.user.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Errorf("the asker's inbox should hold both replies, got %d", len(items))
	}
}

// transcriptOf is the text of a turn's transcript.
func transcriptOf(t *testing.T, turn store.Turn) string {
	t.Helper()
	data, err := os.ReadFile(turn.TranscriptPath)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// notes returns the system messages of a thread.
func (l *loop) notes(threadID string) []string {
	l.t.Helper()
	var out []string
	for _, m := range l.replies(threadID, store.SenderSystem) {
		out = append(out, m.Body)
	}
	return out
}

func countContaining(lines []string, part string) int {
	n := 0
	for _, line := range lines {
		if strings.Contains(line, part) {
			n++
		}
	}
	return n
}

func TestLoop_SessionThatWillNotResumeIsReplacedOnceANewOneWorks(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", map[string]any{"fail_on_resume": true})
	msg := l.say("@Echo start", "", echo)
	l.waitTurns(1, store.TurnDone, "first turn")
	thread := l.topic(msg)
	first, err := l.s.GetOpenSession(l.ctx, echo.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Resuming fails before the agent says a word; nobody has to notice.
	l.say("and now?", thread.ID)
	turns := l.waitTurns(2, store.TurnDone, "the turn that had to start over")

	second, err := l.s.GetOpenSession(l.ctx, echo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || !second.Started() || turns[0].SessionID != second.ID {
		t.Errorf("open session = %+v, want a new, started one carrying the turn (%s)", second, turns[0].SessionID)
	}
	if old, _ := l.s.GetSession(l.ctx, first.ID); old.EndedAt == nil || old.EndReason != store.SessionResumeFailed {
		t.Errorf("the session that would not resume = %+v, want ended as resume_failed", old)
	}
	if got := l.replies(thread.ID, store.SenderAgent); len(got) != 1 || !strings.Contains(got[0].Body, "and now?") {
		t.Errorf("the follow-up should be answered exactly once, got %+v", got)
	}
	if notes := l.notes(thread.ID); countContaining(notes, "started a new session (resuming it failed)") != 1 || countContaining(notes, "failed:") != 0 {
		t.Errorf("the room should hear of the new session and of no failure, got %q", notes)
	}
	tx := transcriptOf(t, turns[0])
	if !strings.Contains(tx, `"kind":"restart"`) || !strings.Contains(tx, `"session":{"key":"`+second.ID+`"}`) {
		t.Errorf("the transcript should record the second run in session %s:\n%s", second.ID, tx)
	}
	if !strings.Contains(tx, "This is a new session") || !strings.Contains(tx, "cannot resume the session") {
		t.Errorf("the second run's brief should say it is a new session, and the restart why:\n%s", tx)
	}
}

func TestLoop_SessionTheRuntimeDeclaresGoneEndsAtOnce(t *testing.T) {
	l := newLoop(t)
	// Every run fails, and the runtime says the session is gone.
	flaky := l.member("Flaky", map[string]any{"fail": true, "failure": "session_not_found"})
	msg := l.say("@Flaky go", "", flaky)
	l.waitTurns(1, store.TurnFailed, "first turn")
	thread := l.topic(msg)
	first, err := l.s.GetOpenSession(l.ctx, flaky.ID)
	if err != nil {
		t.Fatal(err)
	}

	l.say("again", thread.ID, flaky)
	turns := l.waitTurns(2, store.TurnFailed, "second turn")

	// The second run failed too, but the old session is gone all the same:
	// the runtime said so, and keeping it would fail every turn to come.
	second, err := l.s.GetOpenSession(l.ctx, flaky.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || turns[0].SessionID != second.ID {
		t.Errorf("open session = %+v, want a new one carrying the turn (%s)", second, turns[0].SessionID)
	}
	if old, _ := l.s.GetSession(l.ctx, first.ID); old.EndReason != store.SessionNotFound {
		t.Errorf("the lost session ended as %q, want not_found", old.EndReason)
	}
	notes := l.notes(thread.ID)
	if countContaining(notes, "the runtime no longer has it") != 1 {
		t.Errorf("the room should hear why Flaky starts over, got %q", notes)
	}
	if !strings.Contains(transcriptOf(t, turns[0]), `"kind":"restart"`) {
		t.Error("the transcript should record the second run")
	}
}

func TestLoop_SessionIsKeptWhenANewOneFailsToo(t *testing.T) {
	l := newLoop(t)
	// Fails every time and cannot say why: credentials, the network.
	flaky := l.member("Flaky", map[string]any{"fail": true})
	msg := l.say("@Flaky go", "", flaky)
	l.waitTurns(1, store.TurnFailed, "first turn")
	thread := l.topic(msg)
	first, err := l.s.GetOpenSession(l.ctx, flaky.ID)
	if err != nil {
		t.Fatal(err)
	}

	l.say("again", thread.ID, flaky)
	turns := l.waitTurns(2, store.TurnFailed, "second turn")

	open, err := l.s.GetOpenSession(l.ctx, flaky.ID)
	if err != nil {
		t.Fatal(err)
	}
	if open.ID != first.ID || turns[0].SessionID != first.ID {
		t.Errorf("open session = %s, turn in %s; want both still %s: the trouble was never the session", open.ID, turns[0].SessionID, first.ID)
	}
	all, _ := l.s.ListMemberSessions(l.ctx, flaky.ID)
	if len(all) != 1 {
		t.Errorf("the member has %d sessions, want the one: a session on trial leaves no row", len(all))
	}
	notes := l.notes(thread.ID)
	if countContaining(notes, "started a new session") != 0 || countContaining(notes, "Flaky failed:") != 2 {
		t.Errorf("want one failure note per turn and no word of a new session, got %q", notes)
	}
	if !strings.Contains(transcriptOf(t, turns[0]), `"kind":"restart"`) {
		t.Error("the second run was still tried, and the transcript should show it")
	}
}

func TestLoop_NoSecondRunOnceTheAgentHasActed(t *testing.T) {
	l := newLoop(t)
	// Uses a tool, then fails: running that again would do the work twice.
	worker := l.member("Worker", map[string]any{"tool": true, "fail": true})
	msg := l.say("@Worker go", "", worker)
	l.waitTurns(1, store.TurnFailed, "first turn")
	thread := l.topic(msg)
	first, _ := l.s.GetOpenSession(l.ctx, worker.ID)

	l.say("again", thread.ID, worker)
	turns := l.waitTurns(2, store.TurnFailed, "second turn")

	if strings.Contains(transcriptOf(t, turns[0]), `"kind":"restart"`) {
		t.Error("a turn that used a tool must not be run again")
	}
	if open, _ := l.s.GetOpenSession(l.ctx, worker.ID); open.ID != first.ID {
		t.Errorf("open session = %s, want it untouched (%s)", open.ID, first.ID)
	}
}

func TestLoop_MovedMemberIsToldItStartsOver(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", nil)
	msg := l.say("@Echo start", "", echo)
	l.waitTurns(1, store.TurnDone, "first turn")
	thread := l.topic(msg)

	moved := "/somewhere/else"
	if _, err := l.s.UpdateMember(l.ctx, echo.ID, store.MemberPatch{RepoPath: &moved}); err != nil {
		t.Fatal(err)
	}
	l.say("and now?", thread.ID)
	turns := l.waitTurns(2, store.TurnDone, "turn after the move")

	if tx := transcriptOf(t, turns[0]); !strings.Contains(tx, "This is a new session") || !strings.Contains(tx, "its working directory changed") {
		t.Errorf("the brief should tell the agent it starts over and why:\n%s", tx)
	}
	if tx := transcriptOf(t, turns[1]); strings.Contains(tx, "This is a new session") {
		t.Errorf("a member's very first session replaces nothing and needs no such note:\n%s", tx)
	}
	eventually(t, func() bool {
		return countContaining(l.notes(thread.ID), "Echo started a new session (its working directory changed)") == 1
	}, "the note about the new session")
}

// promptOf is the brief a turn's run started with, as its transcript has it.
// systemPromptOf is the system prompt a turn's run was given: the role
// card, and for a runtime that takes one with every run the standing
// instructions (design.md 5.23.1).
func systemPromptOf(t *testing.T, turn store.Turn) string {
	t.Helper()
	return specOf(t, turn).SystemPrompt
}

func promptOf(t *testing.T, turn store.Turn) string {
	t.Helper()
	for _, line := range strings.Split(transcriptOf(t, turn), "\n") {
		var rec struct {
			Kind string `json:"kind"`
			Spec struct {
				Prompt string `json:"prompt"`
			} `json:"spec"`
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Kind == "start" {
			return rec.Spec.Prompt
		}
	}
	t.Fatalf("turn %s has no start record", turn.ID)
	return ""
}

func TestLoop_LaterTurnsAreBriefedOnlyOnWhatIsNew(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", nil)
	other := l.member("Other", nil)

	// Echo is asked in the room; its reply opens topic #1.
	first := l.say("@Echo start on the parser", "", echo)
	l.waitTurns(1, store.TurnDone, "Echo's first turn")
	topicA := l.topic(first)

	// Meanwhile: a word to everybody, stored as it is lest it go to the
	// leader, and Other gets work of its own, which becomes topic #2 and
	// is talked about there.
	if _, err := l.s.CreateMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, SenderKind: store.SenderUser, UserID: l.user.ID, Body: "fyi the deploy is at noon"}); err != nil {
		t.Fatal(err)
	}
	asked := l.say("@Other look at the logs", "", other)
	l.waitTurns(2, store.TurnDone, "Other's turn")
	topicB := l.topic(asked)
	l.say("thanks, keep going", topicB.ID)
	l.waitTurns(3, store.TurnDone, "Other's second turn")

	// Back to Echo, in its topic.
	l.say("and now?", topicA.ID)
	turns := l.waitTurns(4, store.TurnDone, "Echo's second turn")
	opening, later := promptOf(t, turns[3]), promptOf(t, turns[0])

	// The first brief: who is in the chat; asked in the room, the question
	// comes last.
	if !strings.Contains(opening, "- Echo (you, the leader)") || !strings.Contains(opening, "Your reply will open topic #1.") || !strings.HasSuffix(opening, ">> [alice] @Echo start on the parser\n") {
		t.Errorf("first brief:\n%s", opening)
	}
	// The second: what happened since, and no more; who is in the chat is
	// as the first said.
	for _, want := range []string{
		"As you were told earlier in this session, unchanged and not repeated here: who is in the chat.",
		"New in the room since you last looked:",
		"[alice] fyi the deploy is at noon",
		"[alice] @Other look at the logs",
		"#2 [Other] ",
		"Other topics with news:",
		`#2 "Echo: >> [alice] @Other look at the logs": 2 new replies, the last from Other`,
		"This topic, #1 ", "new since you last looked:",
	} {
		if !strings.Contains(later, want) {
			t.Errorf("second brief lacks %q:\n%s", want, later)
		}
	}
	if !strings.HasSuffix(later, ">> [alice] and now?\n") {
		t.Errorf("the question should come last:\n%s", later)
	}
	if strings.Contains(later, "- Echo (you, the leader)") {
		t.Errorf("who is in the chat is not told again:\n%s", later)
	}
	// As lines of their own: the topic's title quotes Echo's first reply,
	// which the fake runtime makes an echo of the question.
	for _, read := range []string{"\n   [alice] @Echo start on the parser\n", "\n   #1 [Echo] ", "\n   [Echo] ", "in full"} {
		if strings.Contains(later, read) {
			t.Errorf("second brief repeats %q, which the session has read (or said itself):\n%s", read, later)
		}
	}

	session, err := l.s.GetOpenSession(l.ctx, echo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.RoomSeen == 0 || session.ThreadSeen[topicA.ID] != session.RoomSeen || len(session.ThreadSeen) != 1 {
		t.Errorf("Echo's session has read the room to %d and topics %v; want both at the last brief's position, and topic #1 only", session.RoomSeen, session.ThreadSeen)
	}
}

func TestLoop_CompactionShowsTheTopicInFullAgain(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", map[string]any{"compact": true})
	msg := l.say("@Echo start", "", echo)
	l.waitTurns(1, store.TurnDone, "first turn")
	thread := l.topic(msg)

	var session store.MemberSession
	eventually(t, func() bool {
		session, _ = l.s.GetOpenSession(l.ctx, echo.ID)
		return session.Compactions == 1
	}, "the compaction to be counted")
	if len(session.ThreadSeen) != 0 || session.RoomSeen == 0 || len(session.BriefSeen) != 0 {
		t.Errorf("after compacting: topics read %v, room read to %d, parts seen %v; want none, and the room kept", session.ThreadSeen, session.RoomSeen, session.BriefSeen)
	}

	l.say("and now?", thread.ID)
	turns := l.waitTurns(2, store.TurnDone, "second turn")
	later := promptOf(t, turns[0])
	if !strings.Contains(later, "in full") || !strings.Contains(later, "[Echo] Echo: ") {
		t.Errorf("after a compaction the topic is shown in full, the agent's own words included:\n%s", later)
	}
	if !strings.Contains(later, "In this chat (mention one as @Name") {
		t.Errorf("after a compaction the parts that change now and then are told again:\n%s", later)
	}
	if strings.Contains(later, "New in the room") || strings.Contains(later, "\n   [alice] @Echo start\n") {
		t.Errorf("the room is still read from where the session left it:\n%s", later)
	}
}

func TestLoop_ATurnThatNeverGotGoingLeavesThePositions(t *testing.T) {
	l := newLoop(t)
	flaky := l.member("Flaky", map[string]any{"fail": true})
	l.say("@Flaky go", "", flaky)
	l.waitTurns(1, store.TurnFailed, "the failing turn")

	session, err := l.s.GetOpenSession(l.ctx, flaky.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.RoomSeen != 0 || len(session.ThreadSeen) != 0 || len(session.BriefSeen) != 0 {
		t.Errorf("a turn that failed before saying or doing anything read nothing: room %d, topics %v, parts %v", session.RoomSeen, session.ThreadSeen, session.BriefSeen)
	}
}

// A long message is cut short in a brief, its id given, and read whole
// with read_message, lines and all; what the turn answers is told whole.
// Another project's messages are not the turn's to read.
func TestLoop_ALongMessageIsReadWholeWithReadMessage(t *testing.T) {
	l := newLoop(t)
	var long strings.Builder
	for i := range 300 {
		fmt.Fprintf(&long, "line %03d of the build log\n", i)
	}
	logged, err := l.h.PostUserMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, UserID: l.user.ID, Body: long.String()})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := l.s.CreateProject(l.ctx, store.NewProject{Name: "elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := l.s.CreateMessage(l.ctx, store.NewMessage{RoomID: other.MainRoomID, SenderKind: store.SenderUser, UserID: l.user.ID, Body: "not yours"})
	if err != nil {
		t.Fatal(err)
	}
	reader := l.member("Reader", map[string]any{"tool_calls": []any{
		call(runtime.RoomToolReadMessage, map[string]any{"message": logged.ID}),
		call(runtime.RoomToolReadMessage, map[string]any{"message": elsewhere.ID}),
	}})
	asked := l.say("@Reader what does the log say?\n"+long.String(), "", reader)
	turns := l.waitTurns(1, store.TurnDone, "Reader's turn")

	brief := promptOf(t, turns[0])
	if !strings.Contains(brief, "line 100 of the build log") || !strings.Contains(brief, "more characters: read_message with message "+logged.ID+" reads it whole)") {
		t.Errorf("the long message is cut in the brief, its id given:\n%s", brief)
	}
	if !strings.Contains(brief, ">> [alice] @Reader what does the log say?\nline 000 of the build log") || !strings.Contains(brief, "line 299 of the build log\n") {
		t.Errorf("what the turn answers is told whole:\n%s", brief)
	}
	answer := l.root(l.topic(asked)).Body
	if !strings.Contains(answer, "Message "+logged.ID+", from alice") || !strings.Contains(answer, "line 298 of the build log\nline 299 of the build log\n") {
		t.Errorf("read_message reads it whole, lines kept:\n%s", answer)
	}
	if !strings.Contains(answer, "this chat has no message "+elsewhere.ID) || strings.Contains(answer, "not yours") {
		t.Errorf("another project's message is not read:\n%s", answer)
	}
}

func TestLoop_AgentReadsAnotherTopicWithItsRoomTools(t *testing.T) {
	l := newLoop(t)
	worker := l.member("Worker", map[string]any{"changes": []any{"auth/token.go", "auth/token_test.go", "auth/token.go"}, "reply": "Tokens are done."})
	reader := l.member("Reader", map[string]any{"room_tool": runtime.RoomToolReadTopic, "room_topic": 1})

	// Worker does something in topic #1 ...
	l.say("@Worker do the tokens", "", worker)
	turns := l.waitTurns(1, store.TurnDone, "Worker's turn")
	if got := strings.Join(turns[0].FilesChanged, ","); got != "auth/token.go,auth/token_test.go" {
		t.Errorf("the turn changed %q, want each file once, in the order first touched", got)
	}

	// ... and Reader, asked elsewhere, looks it up: the tool call goes from
	// the runtime to the machine, over the protocol to the hub, and the
	// answer all the way back.
	asked := l.say("@Reader what did Worker do?", "", reader)
	// Reader's, the newest: its answer quotes "@Worker", which may wake
	// Worker again before the brief is looked at.
	read := l.waitTurns(2, store.TurnDone, "Reader's turn")[0]
	answer := l.root(l.topic(asked)).Body
	for _, want := range []string{
		`Topic #1 "@alice Tokens are done.", oldest first:`,
		"(asked in the room) ", "[alice] @Worker do the tokens",
		"[Worker] @alice Tokens are done.",
		"(turn " + turns[0].ID + "; it changed: auth/token.go, auth/token_test.go)",
	} {
		if !strings.Contains(answer, want) {
			t.Errorf("Reader's answer lacks %q:\n%s", want, answer)
		}
	}
	// Every run is told the tools are there, in its standing instructions.
	if standing := systemPromptOf(t, read); !strings.Contains(standing, "use your veyloom tools: list_topics, read_topic, read_turn, read_message, read_room, search_messages") {
		t.Errorf("the standing instructions should say how to read more:\n%s", standing)
	}
}

// A member reads what another member did in a turn, which its reply leaves
// out, with read_turn: every chat turn has it, for its own chat's turns.
func TestLoop_MemberReadsATurnOfItsChat(t *testing.T) {
	l := newLoop(t)
	worker := l.member("Worker", map[string]any{"changes": []any{"auth/token.go"}, "reply": "Tokens are done."})
	l.say("@Worker do the tokens", "", worker)
	turns := l.waitTurns(1, store.TurnDone, "Worker's turn")

	reader := l.member("Reader", map[string]any{"tool_calls": []any{
		map[string]any{"tool": runtime.RoomToolReadTurn, "args": map[string]any{"turn": turns[0].ID}},
	}})
	asked := l.say("@Reader what did Worker do?", "", reader)
	l.waitTurns(2, store.TurnDone, "Reader's turn")
	answer := l.root(l.topic(asked)).Body
	for _, want := range []string{
		"Turn " + turns[0].ID + " of Worker (fake) in topic #1",
		"Files it changed: auth/token.go",
		"What it was asked, by alice", "@Worker do the tokens",
		"What happened:\nCalled write_file auth/token.go", "Said: Tokens are done.",
	} {
		if !strings.Contains(answer, want) {
			t.Errorf("Reader's answer lacks %q:\n%s", want, answer)
		}
	}
}

// A member asked while another is at work is told who is busy where, what
// it has changed so far and what it waits on a person for: read off the
// turn under way, not the database.
func TestLoop_BriefSaysWhoIsAtWork(t *testing.T) {
	l := newLoop(t)
	worker := l.member("Worker", map[string]any{"changes": []any{"auth/token.go"}, "approval": true})
	helper := l.member("Helper", nil)
	l.say("@Worker do the tokens", "", worker)
	asked := l.waitApproval()

	l.say("@Helper how is it going?", "", helper)
	var helped store.Turn
	eventually(t, func() bool {
		for _, turn := range l.turns() {
			if turn.MemberID == helper.ID && turn.Status == store.TurnDone {
				helped = turn
				return true
			}
		}
		return false
	}, "Helper's turn")
	wantInOrder(t, promptOf(t, helped),
		"In this chat (mention one as @Name to hand something over):\n",
		"At work right now:\n",
		"- Worker, in topic #1, started just now; has changed auth/token.go; waits for a person to allow running `make test`\n",
		">> [alice] @Helper how is it going?",
	)

	if _, err := l.h.DecideApproval(l.ctx, asked.ID, l.user.ID, runtime.Decision{Allow: true}, ""); err != nil {
		t.Fatal(err)
	}
	l.waitTurns(2, store.TurnDone, "both turns")
	// Nobody is at work now: the section is left out.
	l.say("@Helper and now?", "", helper)
	turns := l.waitTurns(3, store.TurnDone, "Helper's second turn")
	if prompt := promptOf(t, turns[len(turns)-1]); strings.Contains(prompt, "At work right now") {
		t.Errorf("nobody is at work:\n%s", prompt)
	}
}

func TestLoop_RoomToolsReadTheTurnsOwnRoomOnly(t *testing.T) {
	l := newLoop(t)
	// Another project, with a topic #1 of its own that this room must not see.
	_, elsewhere, err := l.s.CreateProject(l.ctx, store.NewProject{Name: "elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := l.s.CreateMessage(l.ctx, store.NewMessage{RoomID: elsewhere.ID, SenderKind: store.SenderUser, UserID: l.user.ID, Body: "the launch code is 0000"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.s.ThreadForMessage(l.ctx, secret.ID); err != nil {
		t.Fatal(err)
	}

	searcher := l.member("Searcher", map[string]any{"room_tool": runtime.RoomToolSearch, "room_text": "launch code"})
	asked := l.say("@Searcher find the code", "", searcher)
	l.waitTurns(1, store.TurnDone, "Searcher's turn")
	if answer := l.root(l.topic(asked)).Body; strings.Contains(answer, "0000") || !strings.Contains(answer, "No message in this chat holds") {
		t.Errorf("a turn reads its own room and no other:\n%s", answer)
	}
}

func TestRoomQuery_ForATurnThatIsOver(t *testing.T) {
	l := newLoop(t)
	hubEnd, machineEnd := protocol.Pipe()
	l.h.turns.OnRoomQuery(hubEnd, protocol.RoomQuery{TurnID: store.NewID(), QueryID: "q1", Query: runtime.RoomQuery{Tool: runtime.RoomToolReadRoom}})

	ctx, cancel := context.WithTimeout(l.ctx, 2*time.Second)
	defer cancel()
	m, err := machineEnd.Recv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res, ok := m.(protocol.RoomResult); !ok || res.QueryID != "q1" || res.Error == "" || res.Text != "" {
		t.Errorf("answer = %+v, want an error and nothing else", m)
	}
}

func TestLoop_SessionResetByAPersonIsToldToTheAgentNotTheRoom(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", nil)
	msg := l.say("@Echo start", "", echo)
	l.waitTurns(1, store.TurnDone, "first turn")
	thread := l.topic(msg)
	first, _ := l.s.GetOpenSession(l.ctx, echo.ID)

	if err := l.s.ResetSession(l.ctx, echo.ID); err != nil {
		t.Fatal(err)
	}
	l.say("and now?", thread.ID)
	turns := l.waitTurns(2, store.TurnDone, "turn after the reset")

	second, err := l.s.GetOpenSession(l.ctx, echo.ID)
	if err != nil || second.ID == first.ID || turns[0].SessionID != second.ID {
		t.Fatalf("open session = %+v, %v; want a new one carrying the turn", second, err)
	}
	prompt := promptOf(t, turns[0])
	if !strings.Contains(prompt, "This is a new session") || !strings.Contains(prompt, "a person asked for a new one") {
		t.Errorf("the agent should be told it starts over, and why:\n%s", prompt)
	}
	// It has read nothing, so it gets the topic whole.
	if !strings.Contains(prompt, "in full") {
		t.Errorf("a new session is shown the topic in full:\n%s", prompt)
	}
	// The person who asked needs no telling.
	if notes := l.notes(thread.ID); countContaining(notes, "started a new session") != 0 {
		t.Errorf("no note in the room for a reset a person asked for, got %q", notes)
	}
}
