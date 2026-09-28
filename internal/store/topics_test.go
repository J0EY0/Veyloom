package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// topic builds what the hub builds for a top-level mention: an empty agent
// message heading a thread, with a turn running in it.
func (f turnFixture) topic(t *testing.T) (store.Message, store.Thread, store.Turn) {
	t.Helper()
	ctx := context.Background()
	root, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID})
	if err != nil {
		t.Fatalf("an agent root may be empty: %v", err)
	}
	thread, err := f.s.ThreadForMessage(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := f.s.CreateTurn(ctx, store.NewTurn{MemberID: f.member.ID, RoomID: f.room.ID, ThreadID: thread.ID, TriggerMessageID: f.root.ID, MachineID: f.machineID})
	if err != nil {
		t.Fatal(err)
	}
	return root, thread, turn
}

func TestTopic_RootFilledInLater(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	root, _, turn := f.topic(t)

	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: " "}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("people still have to say something: got %v", err)
	}

	filled, err := f.s.UpdateMessageBody(ctx, root.ID, "on it", turn.ID, []store.Mention{{Kind: store.MentionUser, ID: f.user.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if filled.ID != root.ID || filled.Seq != root.Seq || filled.Body != "on it" || filled.TurnID != turn.ID || len(filled.Mentions) != 1 || filled.Mentions[0].ID != f.user.ID {
		t.Errorf("filled root = %+v", filled)
	}
	got, _ := f.s.GetMessage(ctx, root.ID)
	if got.Body != "on it" || got.TurnID != turn.ID {
		t.Errorf("stored root = %+v", got)
	}
	if _, err := f.s.UpdateMessageBody(ctx, "00000000-0000-0000-0000-000000000000", "x", "", nil); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown message: got %v, want ErrNotFound", err)
	}
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderSystem, Body: "note", TurnID: "00000000-0000-0000-0000-000000000000"}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown turn: got %v, want ErrNotFound", err)
	}
}

func TestTopic_LastAgentMessageCountsTheRoot(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	root, thread, turn := f.topic(t)

	last, err := f.s.LastAgentMessageInThread(ctx, thread.ID)
	if err != nil || last.ID != root.ID {
		t.Fatalf("with no replies the root is the last agent message: %+v, %v", last, err)
	}
	reply, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "more", TurnID: turn.ID})
	if err != nil {
		t.Fatal(err)
	}
	if last, _ = f.s.LastAgentMessageInThread(ctx, thread.ID); last.ID != reply.ID {
		t.Errorf("a reply supersedes the root, got %+v", last)
	}
	// The user's own thread (rooted at a user message) has no agent voice.
	if _, err := f.s.LastAgentMessageInThread(ctx, f.thread.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("thread without agents: got %v, want ErrNotFound", err)
	}
}

// Who a person talks with in a topic: the member in their latest exchange
// there, a message of its that mentions them, one of theirs that names it,
// or a turn one of theirs set going; neither of two they asked at once. And
// whose turns run there.
func TestTopic_WhoAPersonTalksWithAndWhoRuns(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	root, thread, turn := f.topic(t)
	talking := func() (string, error) { return f.s.TalkingMemberInThread(ctx, thread.ID, f.user.ID) }

	// The turn alice's message set going answers her, in one go at the head
	// of the topic, mentioning no one.
	if _, err := f.s.UpdateMessageBody(ctx, root.ID, "on it", turn.ID, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := talking(); err != nil || got != f.member.ID {
		t.Errorf("the member her message set going: %q %v", got, err)
	}
	// Another member answers her since, mentioning her.
	agent, err := f.s.CreateAgent(ctx, store.NewAgent{Name: "Other", MachineID: f.machineID, Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.s.CreateMember(ctx, store.NewMember{RoomID: f.room.ID, AgentID: agent.ID, DisplayName: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: thread.ID, SenderKind: store.SenderAgent, MemberID: other.ID, Body: "@alice done",
		Mentions: []store.Mention{{Kind: store.MentionUser, ID: f.user.ID}}}); err != nil {
		t.Fatal(err)
	}
	if got, err := talking(); err != nil || got != other.ID {
		t.Errorf("the latest to answer her, by a mention: %q %v", got, err)
	}
	// A reply that mentions nobody changes nothing.
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "working", TurnID: turn.ID}); err != nil {
		t.Fatal(err)
	}
	if got, _ := talking(); got != other.ID {
		t.Errorf("a word to nobody: %q", got)
	}
	// Her word there sets the first member going again: she talks with it.
	asked, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: thread.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "and a test"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.s.CreateTurn(ctx, store.NewTurn{MemberID: f.member.ID, RoomID: f.room.ID, ThreadID: thread.ID, TriggerMessageID: asked.ID, MachineID: f.machineID})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := talking(); err != nil || got != f.member.ID {
		t.Errorf("the turn her word set going: %q %v", got, err)
	}
	// She names the other member there, whose wake still waits in a queue:
	// she talks with it, though no turn of its has begun.
	say := func(body string, mentions ...store.Mention) {
		t.Helper()
		if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: thread.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: body, Mentions: mentions}); err != nil {
			t.Fatal(err)
		}
	}
	say("@Other take it over", store.Mention{Kind: store.MentionAgent, ID: other.ID})
	if got, err := talking(); err != nil || got != other.ID {
		t.Errorf("the member she named last: %q %v", got, err)
	}
	// Naming both at once, she talks with neither in particular.
	say("@agent @Other both of you", store.Mention{Kind: store.MentionAgent, ID: f.member.ID}, store.Mention{Kind: store.MentionAgent, ID: other.ID})
	if got, err := talking(); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("two members asked at once: %q %v, want neither", got, err)
	}
	// A topic whose head is a member's answer to her, mentioning her.
	head, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderAgent, MemberID: other.ID, Body: "@alice here is the plan",
		Mentions: []store.Mention{{Kind: store.MentionUser, ID: f.user.ID}}})
	if err != nil {
		t.Fatal(err)
	}
	headed, err := f.s.ThreadForMessage(ctx, head.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.s.TalkingMemberInThread(ctx, headed.ID, f.user.ID); err != nil || got != other.ID {
		t.Errorf("the member whose answer heads the topic: %q %v", got, err)
	}
	bob, err := f.s.CreateUser(ctx, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TalkingMemberInThread(ctx, thread.ID, bob.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("nobody talks with bob there: %v", err)
	}

	running, err := f.s.RunningMembersInThread(ctx, thread.ID)
	if err != nil || len(running) != 1 || running[0] != f.member.ID {
		t.Errorf("running there, once for both turns: %v %v", running, err)
	}
	for _, id := range []string{turn.ID, again.ID} {
		if _, err := f.s.FinishTurn(ctx, id, store.TurnOutcome{Status: store.TurnDone}); err != nil {
			t.Fatal(err)
		}
	}
	if running, err := f.s.RunningMembersInThread(ctx, thread.ID); err != nil || len(running) != 0 {
		t.Errorf("over, none runs: %v %v", running, err)
	}
}

// A piece of work stands as its latest turn does: running while one runs,
// else as the turn that ended last ended. A person stopping the last turn
// leaves it cancelled, whatever came before; a member answering after one
// failed leaves it done.
func TestTopic_HowAPieceOfWorkEnded(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	chain := f.root.ID
	stands := func() (store.TurnStatus, store.TurnStatus) {
		t.Helper()
		summaries, err := f.s.ThreadSummaries(ctx, []string{f.root.ID})
		if err != nil || summaries[f.root.ID].Work == nil {
			t.Fatalf("the topic's piece of work: %+v %v", summaries, err)
		}
		work, err := f.s.ChainWork(ctx, chain)
		if err != nil {
			t.Fatal(err)
		}
		return summaries[f.root.ID].Work.LastStatus, work.LastStatus
	}
	for _, step := range []struct {
		name string
		end  store.TurnStatus
	}{
		{"one that fails", store.TurnFailed},
		{"one that answers after it", store.TurnDone},
		{"one a person stops", store.TurnCancelled},
	} {
		turn, err := f.s.CreateTurn(ctx, store.NewTurn{MemberID: f.member.ID, RoomID: f.room.ID, ThreadID: f.thread.ID, TriggerMessageID: f.root.ID, MachineID: f.machineID, ChainMessageID: chain})
		if err != nil {
			t.Fatal(err)
		}
		if timeline, work := stands(); timeline != store.TurnRunning || work != store.TurnRunning {
			t.Errorf("%s, running: the timeline says %q, the topic %q", step.name, timeline, work)
		}
		if _, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: step.end, Error: "x"}); err != nil {
			t.Fatal(err)
		}
		if timeline, work := stands(); timeline != step.end || work != step.end {
			t.Errorf("%s, over: the timeline says %q, the topic %q, want %q", step.name, timeline, work, step.end)
		}
	}
	// Two at once: the one begun first ends last, and it failed. How the
	// piece of work ended is how the turn that ended last did.
	begun := func() store.Turn {
		t.Helper()
		turn, err := f.s.CreateTurn(ctx, store.NewTurn{MemberID: f.member.ID, RoomID: f.room.ID, ThreadID: f.thread.ID, TriggerMessageID: f.root.ID, MachineID: f.machineID, ChainMessageID: chain})
		if err != nil {
			t.Fatal(err)
		}
		return turn
	}
	first, second := begun(), begun()
	if _, err := f.s.FinishTurn(ctx, second.ID, store.TurnOutcome{Status: store.TurnDone}); err != nil {
		t.Fatal(err)
	}
	if timeline, work := stands(); timeline != store.TurnRunning || work != store.TurnRunning {
		t.Errorf("one of two still running: the timeline says %q, the topic %q", timeline, work)
	}
	if _, err := f.s.FinishTurn(ctx, first.ID, store.TurnOutcome{Status: store.TurnFailed, Error: "x"}); err != nil {
		t.Fatal(err)
	}
	if timeline, work := stands(); timeline != store.TurnFailed || work != store.TurnFailed {
		t.Errorf("the one that ended last failed: the timeline says %q, the topic %q", timeline, work)
	}
}

func TestTopic_SummariesAndTurns(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	root, thread, first := f.topic(t)

	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "done", TurnID: first.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FinishTurn(ctx, first.ID, store.TurnOutcome{Status: store.TurnDone}); err != nil {
		t.Fatal(err)
	}
	second, err := f.s.CreateTurn(ctx, store.NewTurn{MemberID: f.member.ID, RoomID: f.room.ID, ThreadID: thread.ID, MachineID: f.machineID})
	if err != nil {
		t.Fatal(err)
	}

	summaries, err := f.s.ThreadSummaries(ctx, []string{root.ID, f.root.ID})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := summaries[root.ID]
	if !ok || got.ID != thread.ID || got.ReplyCount != 1 || got.LastReplyAt == nil || got.Turns != 2 {
		t.Errorf("summary = %+v", got)
	}
	if got.LastTurn == nil || got.LastTurn.ID != second.ID || got.LastTurn.Status != store.TurnRunning || got.LastTurn.EndedAt != nil {
		t.Errorf("last turn = %+v", got.LastTurn)
	}
	// The user's thread exists but has no replies and no turns.
	if plain, ok := summaries[f.root.ID]; !ok || plain.ReplyCount != 0 || plain.Turns != 0 || plain.LastTurn != nil || plain.LastReplyAt != nil {
		t.Errorf("summary of an untouched thread = %+v (present %v)", plain, ok)
	}
	if empty, err := f.s.ThreadSummaries(ctx, nil); err != nil || len(empty) != 0 {
		t.Errorf("no ids: %v, %v", empty, err)
	}

	turns, err := f.s.ListThreadTurns(ctx, thread.ID)
	if err != nil || len(turns) != 2 || turns[0].ID != first.ID || turns[1].ID != second.ID {
		t.Errorf("thread turns = %+v, %v", turns, err)
	}
}

func TestTopic_NumbersRisePerRoom(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	// The fixture's thread is the room's first topic.
	if f.thread.Number != 1 {
		t.Fatalf("first topic is #%d, want #1", f.thread.Number)
	}
	// Asking again for a thread that exists returns it and takes no number.
	for range 3 {
		again, err := f.s.ThreadForMessage(ctx, f.root.ID)
		if err != nil {
			t.Fatal(err)
		}
		if again.ID != f.thread.ID || again.Number != 1 {
			t.Fatalf("same root gave thread %s #%d, want %s #1", again.ID, again.Number, f.thread.ID)
		}
	}
	// A reply lands in the root's thread, not in one of its own.
	reply, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "more"})
	if err != nil {
		t.Fatal(err)
	}
	if of, err := f.s.ThreadForMessage(ctx, reply.ID); err != nil || of.ID != f.thread.ID {
		t.Fatalf("thread of a reply = %+v, %v", of, err)
	}

	second, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "another thing"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := f.s.ThreadForMessage(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.Number != 2 {
		t.Errorf("second topic is #%d, want #2: looking the first up again must not burn numbers", next.Number)
	}
	if got, err := f.s.GetThread(ctx, next.ID); err != nil || got.Number != 2 {
		t.Errorf("GetThread = %+v, %v", got, err)
	}

	// Numbers are per room: another project's first topic is #1 again.
	_, otherRoom, err := f.s.CreateProject(ctx, store.NewProject{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: otherRoom.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.s.ThreadForMessage(ctx, elsewhere.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Number != 1 {
		t.Errorf("another room's first topic is #%d, want #1", first.Number)
	}
}
