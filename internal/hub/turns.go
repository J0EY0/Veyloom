package hub

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/store"
)

// TurnManager runs turns for agent instances: it serialises turns per
// instance, composes briefs, dispatches to the instance's worker, records
// transcripts and posts the reply when the worker reports the turn done.
//
// Concurrency model: the connection loops call OnEvent and OnDone; both
// only touch memory and the transcript buffer so the loops never wait on
// the database. Everything that persists runs on completion goroutines
// with their own bounded context.
type TurnManager struct {
	store           Store
	brief           *briefBuilder
	connFor         func(workerID string) (protocol.Conn, bool)
	publish         func(Event)
	transcripts     string
	storeTimeout    time.Duration
	approvalTimeout time.Duration
	logger          *slog.Logger

	mu        sync.Mutex
	active    map[string]*activeTurn    // by turn ID
	instances map[string]*instanceState // by agent instance ID
	approvals map[string]*activeTurn    // by approval ID, while pending
}

// instanceState serialises an instance's turns: one runs at a time and the
// rest wait. Pending triggers of the same thread are merged into one turn.
type instanceState struct {
	starting bool
	running  *activeTurn
	pending  []trigger
}

type trigger struct {
	msg    store.Message
	thread store.Thread
}

// activeTurn is a turn between dispatch and completion.
type activeTurn struct {
	turn     store.Turn
	instance store.AgentInstance
	thread   store.Thread

	mu         sync.Mutex
	transcript *transcript
	output     strings.Builder
	// pending holds the approvals waiting for a decision, by approval ID.
	pending map[string]*pendingApproval
	// finished is set once completion has begun; approvals raised after
	// that are settled at once instead of registered.
	finished bool
}

func newTurnManager(st Store, brief *briefBuilder, connFor func(string) (protocol.Conn, bool), publish func(Event), transcripts string, storeTimeout, approvalTimeout time.Duration, logger *slog.Logger) *TurnManager {
	return &TurnManager{
		store:           st,
		brief:           brief,
		connFor:         connFor,
		publish:         publish,
		transcripts:     transcripts,
		storeTimeout:    storeTimeout,
		approvalTimeout: approvalTimeout,
		logger:          logger,
		active:          make(map[string]*activeTurn),
		instances:       make(map[string]*instanceState),
		approvals:       make(map[string]*activeTurn),
	}
}

// Trigger asks instance to answer msg. The reply goes to msg's thread,
// created if msg is top-level. If the instance is busy the trigger waits
// and is merged with others for the same thread into the next turn.
func (m *TurnManager) Trigger(ctx context.Context, instance store.AgentInstance, msg store.Message) error {
	thread, err := m.store.ThreadForMessage(ctx, msg.ID)
	if err != nil {
		return fmt.Errorf("trigger %s: %w", instance.DisplayName, err)
	}

	m.mu.Lock()
	st := m.state(instance.ID)
	if st.starting || st.running != nil {
		st.pending = append(st.pending, trigger{msg: msg, thread: thread})
		m.mu.Unlock()
		return nil
	}
	st.starting = true
	m.mu.Unlock()

	m.start(ctx, instance.ID, thread, []store.Message{msg})
	return nil
}

// state returns the instance's queue, creating it. Callers hold m.mu.
func (m *TurnManager) state(instanceID string) *instanceState {
	st, ok := m.instances[instanceID]
	if !ok {
		st = &instanceState{}
		m.instances[instanceID] = st
	}
	return st
}

// start runs one turn for instanceID answering triggers in thread. The
// caller has marked the instance as starting; start always ends by either
// registering the running turn or releasing the instance via advance.
//
// The instance is re-read here rather than passed in so that the session
// reference saved by the previous turn is the one resumed.
func (m *TurnManager) start(ctx context.Context, instanceID string, thread store.Thread, triggers []store.Message) {
	instance, err := m.store.GetAgentInstance(ctx, instanceID)
	if err != nil {
		m.abort(ctx, instanceID, thread, "", err)
		return
	}
	template, err := m.store.GetAgentTemplate(ctx, instance.TemplateID)
	if err != nil {
		m.abort(ctx, instanceID, thread, instance.DisplayName, err)
		return
	}

	turn, err := m.store.CreateTurn(ctx, store.NewTurn{
		AgentInstanceID:  instance.ID,
		RoomID:           thread.RoomID,
		ThreadID:         thread.ID,
		TriggerMessageID: triggers[len(triggers)-1].ID,
		WorkerID:         instance.WorkerID,
	})
	if err != nil {
		m.abort(ctx, instanceID, thread, instance.DisplayName, err)
		return
	}
	at := &activeTurn{turn: turn, instance: instance, thread: thread, pending: make(map[string]*pendingApproval)}

	// From here on every failure is recorded against the turn.
	conn, ok := m.connFor(instance.WorkerID)
	if !ok {
		m.register(at)
		m.OnDone(turn.ID, protocol.TurnDone{TurnID: turn.ID, Error: "worker is offline"})
		return
	}
	prompt, err := m.brief.Build(ctx, instance, thread, triggers)
	if err != nil {
		m.register(at)
		m.OnDone(turn.ID, protocol.TurnDone{TurnID: turn.ID, Error: err.Error()})
		return
	}
	spec := engine.TurnSpec{
		SystemPrompt: template.RoleCard,
		Prompt:       prompt,
		WorkDir:      instance.RepoPath,
		Model:        firstNonEmpty(instance.Model, template.Model),
		Permission:   firstNonEmpty(string(instance.PermissionPreset), string(template.PermissionPreset)),
		SessionRef:   instance.EngineSessionRef,
		Options:      template.EngineOptions,
	}

	path := filepath.Join(m.transcripts, turn.ID+".jsonl")
	tx, err := openTranscript(path)
	if err != nil {
		m.register(at)
		m.OnDone(turn.ID, protocol.TurnDone{TurnID: turn.ID, Error: err.Error()})
		return
	}
	at.transcript = tx
	if err := tx.write(transcriptLine{Kind: "start", TurnID: turn.ID, Engine: template.Engine, Spec: &spec}); err != nil {
		m.logger.Warn("transcript", "turn", turn.ID, "err", err)
	}
	m.register(at)

	if err := conn.Send(ctx, protocol.StartTurn{TurnID: turn.ID, Engine: template.Engine, Spec: spec}); err != nil {
		m.OnDone(turn.ID, protocol.TurnDone{TurnID: turn.ID, Error: "dispatch to worker: " + err.Error()})
	}
}

// register makes at the instance's running turn and announces it. It
// runs before dispatch, so subscribers always see a turn start before
// they see it finish, even when it fails at once.
func (m *TurnManager) register(at *activeTurn) {
	m.mu.Lock()
	st := m.state(at.instance.ID)
	st.starting = false
	st.running = at
	m.active[at.turn.ID] = at
	m.mu.Unlock()
	turn := at.turn
	m.publish(Event{Kind: EventTurnStarted, RoomID: turn.RoomID, At: turn.StartedAt, Turn: &turn})
}

// abort handles a failure before a turn row exists: it tells the room and
// releases the instance so queued triggers still get their turn.
func (m *TurnManager) abort(ctx context.Context, instanceID string, thread store.Thread, name string, err error) {
	if name == "" {
		name = "agent"
	}
	m.logger.Error("turn could not start", "instance", instanceID, "err", err)
	m.postSystem(ctx, thread, fmt.Sprintf("%s could not start a turn: %v", name, err))
	m.mu.Lock()
	m.state(instanceID).starting = false
	m.mu.Unlock()
	m.advance(ctx, instanceID)
}

// OnEvent records one event of a running turn. Events for unknown turns
// are dropped: they belong to a turn that already completed, for example
// after its worker disconnected.
func (m *TurnManager) OnEvent(turnID string, ev engine.Event) {
	m.mu.Lock()
	at := m.active[turnID]
	m.mu.Unlock()
	if at == nil {
		return
	}

	at.mu.Lock()
	if at.transcript != nil {
		if err := at.transcript.write(transcriptLine{Kind: "event", At: ev.At, Event: &ev}); err != nil {
			m.logger.Warn("transcript", "turn", turnID, "err", err)
		}
	}
	if ev.Kind == engine.EventText {
		at.output.WriteString(ev.Text)
	}
	at.mu.Unlock()
	m.publish(Event{Kind: EventTurnEvent, RoomID: at.thread.RoomID, At: ev.At, TurnID: turnID, TurnEvent: &ev})
}

// OnDone completes a turn. It returns immediately; persistence and the
// reply happen on a goroutine so the caller's connection loop is never
// blocked on the database.
func (m *TurnManager) OnDone(turnID string, done protocol.TurnDone) {
	m.mu.Lock()
	at := m.active[turnID]
	delete(m.active, turnID)
	m.mu.Unlock()
	if at == nil {
		return
	}
	go m.complete(at, done)
}

// complete records the outcome, posts the reply or the failure, and lets
// the instance's next turn start. Approvals still pending are closed
// first: nothing waits for them any more.
func (m *TurnManager) complete(at *activeTurn, done protocol.TurnDone) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()

	m.abandonApprovals(ctx, at)

	at.mu.Lock()
	if at.transcript != nil {
		result := done.Result
		if err := at.transcript.write(transcriptLine{Kind: "done", Result: &result, Error: done.Error}); err != nil {
			m.logger.Warn("transcript", "turn", at.turn.ID, "err", err)
		}
		if err := at.transcript.close(); err != nil {
			m.logger.Warn("transcript", "turn", at.turn.ID, "err", err)
		}
	}
	streamed := at.output.String()
	at.mu.Unlock()

	outcome := store.TurnOutcome{TranscriptPath: filepath.Join(m.transcripts, at.turn.ID+".jsonl")}
	name := at.instance.DisplayName
	switch {
	case done.Error == "":
		reply := strings.TrimSpace(done.Result.Output)
		if reply == "" {
			reply = strings.TrimSpace(streamed)
		}
		if reply == "" {
			reply = "(no reply)"
		}
		msg, err := m.post(ctx, store.NewMessage{
			RoomID:          at.thread.RoomID,
			ThreadID:        at.thread.ID,
			SenderKind:      store.SenderAgent,
			AgentInstanceID: at.instance.ID,
			Body:            reply,
		})
		if err != nil {
			m.logger.Error("post reply", "turn", at.turn.ID, "err", err)
			outcome.Status, outcome.Error = store.TurnFailed, "post reply: "+err.Error()
		} else {
			outcome.Status, outcome.ReplyMessageID = store.TurnDone, msg.ID
		}
		if ref := done.Result.SessionRef; ref != "" && ref != at.instance.EngineSessionRef {
			if err := m.store.UpdateAgentInstanceSession(ctx, at.instance.ID, ref); err != nil {
				m.logger.Error("save session", "instance", at.instance.ID, "err", err)
			}
		}
	case done.Cancelled:
		outcome.Status, outcome.Error = store.TurnCancelled, done.Error
		m.postSystem(ctx, at.thread, fmt.Sprintf("%s's turn was cancelled", name))
	default:
		outcome.Status, outcome.Error = store.TurnFailed, done.Error
		m.postSystem(ctx, at.thread, fmt.Sprintf("%s failed: %s", name, done.Error))
	}

	finished, err := m.store.FinishTurn(ctx, at.turn.ID, outcome)
	if err != nil {
		m.logger.Error("finish turn", "turn", at.turn.ID, "err", err)
		// Subscribers still need to hear that the turn is over.
		finished = at.turn
		finished.Status, finished.Error, finished.ReplyMessageID = outcome.Status, outcome.Error, outcome.ReplyMessageID
		now := time.Now()
		finished.EndedAt = &now
	}
	m.publish(Event{Kind: EventTurnFinished, RoomID: finished.RoomID, Turn: &finished})
	m.advance(ctx, at.instance.ID)
}

// advance releases an instance and, if triggers are waiting, starts the
// next turn with every pending trigger of the oldest waiting thread.
func (m *TurnManager) advance(ctx context.Context, instanceID string) {
	m.mu.Lock()
	st := m.state(instanceID)
	st.running = nil
	if len(st.pending) == 0 {
		delete(m.instances, instanceID)
		m.mu.Unlock()
		return
	}
	thread := st.pending[0].thread
	var msgs []store.Message
	var rest []trigger
	for _, p := range st.pending {
		if p.thread.ID == thread.ID {
			msgs = append(msgs, p.msg)
		} else {
			rest = append(rest, p)
		}
	}
	st.pending = rest
	st.starting = true
	m.mu.Unlock()

	m.start(ctx, instanceID, thread, msgs)
}

// Cancel asks the worker running turnID to stop it. The turn completes
// through the normal OnDone path once the worker confirms.
func (m *TurnManager) Cancel(ctx context.Context, turnID string) error {
	m.mu.Lock()
	at := m.active[turnID]
	m.mu.Unlock()
	if at == nil {
		return fmt.Errorf("%w: turn %s is not running", ErrUnknownTurn, turnID)
	}
	conn, ok := m.connFor(at.instance.WorkerID)
	if !ok {
		return fmt.Errorf("cancel turn %s: worker is offline", turnID)
	}
	return conn.Send(ctx, protocol.CancelTurn{TurnID: turnID})
}

// WorkerGone fails every turn running on a worker that disconnected; no
// TurnDone will ever arrive for them.
func (m *TurnManager) WorkerGone(workerID string) {
	m.mu.Lock()
	var orphaned []string
	for id, at := range m.active {
		if at.instance.WorkerID == workerID {
			orphaned = append(orphaned, id)
		}
	}
	m.mu.Unlock()
	for _, id := range orphaned {
		m.OnDone(id, protocol.TurnDone{TurnID: id, Error: "worker disconnected"})
	}
}

// Running reports whether a turn is in flight.
func (m *TurnManager) Running(turnID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.active[turnID]
	return ok
}

// post stores a message and announces it to subscribers. Every message
// the hub itself writes goes through here so none is missed live.
func (m *TurnManager) post(ctx context.Context, in store.NewMessage) (store.Message, error) {
	msg, err := m.store.CreateMessage(ctx, in)
	if err != nil {
		return store.Message{}, err
	}
	m.publish(messageEvent(msg))
	return msg, nil
}

// postSystem puts a note from the system into a thread. Failures are
// logged: a lost note must not stop the turn pipeline.
func (m *TurnManager) postSystem(ctx context.Context, thread store.Thread, text string) {
	_, err := m.post(ctx, store.NewMessage{
		RoomID:     thread.RoomID,
		ThreadID:   thread.ID,
		SenderKind: store.SenderSystem,
		Body:       text,
	})
	if err != nil {
		m.logger.Error("post system message", "thread", thread.ID, "err", err)
	}
}

// messageEvent is the live event for a stored message.
func messageEvent(msg store.Message) Event {
	return Event{Kind: EventMessage, RoomID: msg.Room, At: msg.CreatedAt, Message: &msg}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
