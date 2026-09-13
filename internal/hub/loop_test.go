package hub

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
	"github.com/J0EY0/veyloom/internal/worker"
)

// loop is a complete single-machine setup on a real database: hub, an
// in-process worker with the fake engine, one project with its main room,
// one user. It is what `veyloom serve` runs, minus HTTP.
type loop struct {
	t        *testing.T
	ctx      context.Context
	s        *store.Store
	h        *Hub
	room     store.Room
	user     store.User
	workerID string
}

func newLoop(t *testing.T) *loop {
	t.Helper()
	return newLoopWith(t, Config{})
}

// newLoopWith is newLoop with hub settings a test wants to pin.
func newLoopWith(t *testing.T, cfg Config) *loop {
	t.Helper()
	s := storetest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg.TranscriptDir = t.TempDir()
	cfg.HeartbeatInterval = time.Hour
	h := New(s, cfg)
	w := worker.New(worker.Config{Name: "laptop"}, worker.NewDiscovery(nil, time.Second), &worker.MemoryIdentity{}, engine.BuiltinRunners())
	hubEnd, workerEnd := protocol.Pipe()
	go h.Serve(ctx, hubEnd)
	go w.Run(ctx, workerEnd)
	eventually(t, func() bool { return len(h.Workers()) == 1 }, "worker to connect")

	_, room, err := s.CreateProject(ctx, store.NewProject{Name: "p"})
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	return &loop{t: t, ctx: ctx, s: s, h: h, room: room, user: user, workerID: h.Workers()[0].ID}
}

// agent adds a fake-engine agent to the room with the given options.
func (l *loop) agent(name string, options map[string]any) store.AgentInstance {
	l.t.Helper()
	tpl, err := l.s.CreateAgentTemplate(l.ctx, store.NewAgentTemplate{
		Name: name + " template", Engine: "fake", PermissionPreset: store.PermissionReadOnly, RoleCard: "Be brief.", EngineOptions: options,
	})
	if err != nil {
		l.t.Fatal(err)
	}
	inst, err := l.s.CreateAgentInstance(l.ctx, store.NewAgentInstance{RoomID: l.room.ID, TemplateID: tpl.ID, WorkerID: l.workerID, DisplayName: name})
	if err != nil {
		l.t.Fatal(err)
	}
	return inst
}

// say posts a user message, optionally in a thread, mentioning agents.
func (l *loop) say(body, threadID string, mentions ...store.AgentInstance) store.Message {
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

func TestLoop_MentionGetsAReplyInAThread(t *testing.T) {
	l := newLoop(t)
	echo := l.agent("Echo", nil)

	msg := l.say("@Echo hello there", "", echo)

	turns := l.waitTurns(1, store.TurnDone, "the turn to finish")
	thread, err := l.s.ThreadForMessage(l.ctx, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	replies := l.replies(thread.ID, store.SenderAgent)
	if len(replies) != 1 || replies[0].AgentInstanceID != echo.ID {
		t.Fatalf("expected one agent reply in the thread, got %+v", replies)
	}
	// The fake engine echoes the last brief line, which is the marked trigger.
	if !strings.Contains(replies[0].Body, ">> [alice] @Echo hello there") {
		t.Errorf("reply = %q", replies[0].Body)
	}

	turn := turns[0]
	if turn.ReplyMessageID != replies[0].ID || turn.ThreadID != thread.ID || turn.TriggerMessageID != msg.ID || turn.EndedAt == nil {
		t.Errorf("unexpected turn record: %+v", turn)
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

	inst, _ := l.s.GetAgentInstance(l.ctx, echo.ID)
	if !strings.HasPrefix(inst.EngineSessionRef, "fake-") {
		t.Errorf("session ref should be saved after the turn, got %q", inst.EngineSessionRef)
	}
}

func TestLoop_ThreadFollowUpResumesSession(t *testing.T) {
	l := newLoop(t)
	echo := l.agent("Echo", nil)
	msg := l.say("@Echo start", "", echo)
	l.waitTurns(1, store.TurnDone, "first turn")
	thread, _ := l.s.ThreadForMessage(l.ctx, msg.ID)
	first, _ := l.s.GetAgentInstance(l.ctx, echo.ID)

	// No mention: the thread rule routes to the agent that answered last.
	l.say("and now?", thread.ID)

	turns := l.waitTurns(2, store.TurnDone, "second turn")
	if got := l.replies(thread.ID, store.SenderAgent); len(got) != 2 {
		t.Fatalf("expected two agent replies, got %d", len(got))
	}
	data, _ := os.ReadFile(turns[0].TranscriptPath)
	if !strings.Contains(string(data), `"session_ref":"`+first.EngineSessionRef+`"`) {
		t.Error("the second turn should resume the session saved by the first")
	}
}

func TestLoop_TurnsOfOneAgentNeverOverlap(t *testing.T) {
	l := newLoop(t)
	slow := l.agent("Slow", map[string]any{"delay_ms": float64(150)})

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
	slow := l.agent("Slow", map[string]any{"delay_ms": float64(200)})
	msg := l.say("@Slow first", "", slow)
	thread, _ := l.s.ThreadForMessage(l.ctx, msg.ID)
	eventually(t, func() bool { return len(l.turns()) == 1 }, "first turn to start")

	// The agent has not spoken in the thread yet, so the thread rule cannot
	// route these; they mention it explicitly and queue behind the first
	// turn.
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

func TestLoop_FailureIsReportedInThread(t *testing.T) {
	l := newLoop(t)
	broken := l.agent("Broken", map[string]any{"fail": true})

	msg := l.say("@Broken go", "", broken)

	turns := l.waitTurns(1, store.TurnFailed, "the failed turn")
	if !strings.Contains(turns[0].Error, "scripted failure") {
		t.Errorf("turn error = %q", turns[0].Error)
	}
	thread, _ := l.s.ThreadForMessage(l.ctx, msg.ID)
	notes := l.replies(thread.ID, store.SenderSystem)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "Broken failed") {
		t.Errorf("expected a system note about the failure, got %+v", notes)
	}
	if len(l.replies(thread.ID, store.SenderAgent)) != 0 {
		t.Error("a failed turn must not post a reply")
	}
}

func TestLoop_OfflineWorkerFailsTheTurn(t *testing.T) {
	l := newLoop(t)
	ghostID, err := l.s.RegisterWorker(l.ctx, "", "ghost", nil)
	if err != nil {
		t.Fatal(err)
	}
	tpl, _ := l.s.CreateAgentTemplate(l.ctx, store.NewAgentTemplate{Name: "Ghost", Engine: "fake", PermissionPreset: store.PermissionReadOnly})
	ghost, err := l.s.CreateAgentInstance(l.ctx, store.NewAgentInstance{RoomID: l.room.ID, TemplateID: tpl.ID, WorkerID: ghostID, DisplayName: "Ghost"})
	if err != nil {
		t.Fatal(err)
	}

	msg := l.say("@Ghost anyone home?", "", ghost)

	turns := l.waitTurns(1, store.TurnFailed, "the offline turn to fail")
	if !strings.Contains(turns[0].Error, "offline") {
		t.Errorf("turn error = %q", turns[0].Error)
	}
	thread, _ := l.s.ThreadForMessage(l.ctx, msg.ID)
	if notes := l.replies(thread.ID, store.SenderSystem); len(notes) != 1 || !strings.Contains(notes[0].Body, "offline") {
		t.Errorf("expected a system note about the offline worker, got %+v", notes)
	}
}

func TestLoop_Cancel(t *testing.T) {
	l := newLoop(t)
	slow := l.agent("Slow", map[string]any{"delay_ms": float64(30000)})
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

	if err := l.h.CancelTurn(l.ctx, turnID); err != nil {
		t.Fatal(err)
	}

	turns := l.waitTurns(1, store.TurnCancelled, "the turn to be cancelled")
	if turns[0].EndedAt == nil {
		t.Error("a cancelled turn is finished")
	}
	if err := l.h.CancelTurn(l.ctx, turnID); err == nil {
		t.Error("cancelling a finished turn should report ErrUnknownTurn")
	}
}
