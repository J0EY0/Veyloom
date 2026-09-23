package hub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// TurnManager runs turns for members: it serialises turns per
// member, composes briefs, dispatches to the member's machine, records
// transcripts and posts what the agent says as it says it.
//
// A top-level @-mention opens a topic: before the turn starts the agent
// gets an empty top-level message, the thread is rooted at it, and the
// agent's first stretch of text becomes that message's body. Later text,
// split at tool calls, lands in the thread; when the turn ends the agent
// posts a closing top-level message that mentions whoever asked. A trigger
// inside an existing thread simply answers in that thread.
//
// Concurrency model: the connection loops call OnEvent and OnDone; both
// only touch memory and the transcript buffer so the loops never wait on
// the database. Everything that persists runs on the turn's own executor
// goroutine, in order, with a bounded context per operation.
type TurnManager struct {
	store           Store
	brief           *briefBuilder
	connFor         func(machineID string) (protocol.Conn, bool)
	publish         func(Event)
	transcripts     string
	storeTimeout    time.Duration
	approvalTimeout time.Duration
	// relayBudget caps agent-to-agent turns per topic between two human
	// messages; negative turns the relay off (see relay).
	relayBudget int
	logger      *slog.Logger

	mu        sync.Mutex
	active    map[string]*activeTurn  // by turn ID
	members   map[string]*memberState // by member ID
	approvals map[string]*activeTurn  // by approval ID, while pending
	// relays counts, per thread, the agent-to-agent turns since a person
	// last spoke there.
	relays map[string]int
	// wikis are the project wikis the wiki tools reach.
	wikis *wikiShelf
	// upkeepWaiting are the projects whose maintainer has an upkeep queued
	// that has not started yet; once started, the turn itself says so.
	upkeepWaiting map[string]bool
	// upkeepTurns caps the turns one upkeep goes over.
	upkeepTurns int
	// attachmentDir is where the files people send are kept: the room tools
	// and an upkeep's brief say where to open them, and a page of the wiki
	// may keep one (docs/design.md 5.16).
	attachmentDir string
	// residentBudget is what a brief carries of resident pages, which the
	// health check measures them against.
	residentBudget int
	// trialUses is how many turns keep a skill's change (design.md 5.15);
	// trialMu keeps a trial from being ended two ways at once.
	trialUses int
	trialMu   sync.Mutex
}

// memberState serialises a member's turns: one runs at a time and the
// rest wait. Pending triggers of the same thread are merged into one turn.
type memberState struct {
	starting bool
	running  *activeTurn
	pending  []trigger
}

// trigger is a message waiting to be answered. A zero thread means the
// message is top-level and its turn opens a new topic. With upkeep set the
// message announces a wiki upkeep, which runs as a turn of its own.
type trigger struct {
	msg    store.Message
	thread store.Thread
	upkeep *upkeep
}

// activeTurn is a turn between dispatch and completion.
type activeTurn struct {
	turn   store.Turn
	member store.Member
	agent  store.Agent
	thread store.Thread
	// triggers and spec are kept for running the turn again in a new
	// session: the brief is built anew, the rest of the spec reused.
	triggers []store.Message
	spec     runtime.TurnSpec
	// root is the agent's top-level message heading the topic this turn
	// opened; nil for a turn triggered inside an existing thread.
	root *store.Message
	// initiator is the user whose message opened the topic; the closing
	// message mentions them.
	initiator string
	// askedBy is set when the topic is rooted at a person's own message
	// (several agents asked at once): replies stay in the thread and the
	// last one is addressed to that person instead of a closing message.
	askedBy bool

	mu         sync.Mutex
	transcript *transcript
	// sessionRef is the runtime's reference to the turn's session as last
	// stored; a report that matches it is not written again.
	sessionRef string
	// resumed is set while the run in flight continues an earlier session,
	// which is what makes a failure before the agent said or did anything
	// worth a second run in a new one; retried is set once that was done.
	resumed bool
	retried bool
	// noRetry marks a failure of the hub's own making, which no new
	// session would cure.
	noRetry bool
	// fresh is the session the second run is trying out; nil otherwise.
	fresh *freshSession
	// spent is what runs before the last one cost.
	spent runtime.Usage
	// files are the files the runtime reported written, each once, in the
	// order first touched; fileSeen tells which are in.
	files    []string
	fileSeen map[string]bool
	// skillsUsed are the library's skills its tool calls used, each once.
	skillsUsed []string
	// position is where the room stood when the brief of the run in flight
	// was put together, and wikiPosition the project wiki: how far the
	// session will have read once it has taken the brief in. compactions
	// counts those the runtime reported.
	position     int64
	wikiPosition time.Time
	compactions  int
	// output is everything streamed, kept for the no-segments fallback.
	output strings.Builder
	// segment is the text streamed since the last tool call.
	segment strings.Builder
	// segments counts the text segments already persisted.
	segments     int
	toolActivity bool
	// lastReply is the last persisted segment and the message holding it.
	lastReply   string
	lastReplyID string
	// relayTo are the room's other members this turn's replies named, with
	// the reply that named them; relay wakes them when the turn is done.
	relayTo []relayTarget
	// pending holds the approvals waiting for a decision, by approval ID.
	pending map[string]*pendingApproval
	// withdrawn are the runtime's request ids it took back before the hub
	// had recorded them; each is closed as soon as it is.
	withdrawn map[string]bool
	// finished is set once completion has begun; approvals raised after
	// that are settled at once instead of registered.
	finished bool

	// work runs this turn's database writes one after another, so the
	// agent's replies land in the order they were said. closed guards
	// against enqueueing after completion.
	work   chan func()
	closed bool

	// wikis are the turn's holds on its project's wiki and on the skill
	// library, each taken on its first wiki tool call for that scope; what
	// the turn wrote to each is committed when it ends.
	wikiMu sync.Mutex
	wikis  map[store.WikiScope]*turnWiki

	// upkeep is set on a wiki maintainer's upkeep turn: what it goes over.
	upkeep *upkeep
}

// relayTarget is a member named in a reply, and that reply.
type relayTarget struct {
	member store.Member
	msg    store.Message
}

// workQueue bounds how many writes may wait per turn before the event loop
// blocks; a turn does not produce anywhere near this many segments.
const workQueue = 256

func newTurnManager(st Store, brief *briefBuilder, connFor func(string) (protocol.Conn, bool), publish func(Event), transcripts string, storeTimeout, approvalTimeout time.Duration, relayBudget int, logger *slog.Logger) *TurnManager {
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
		members:         make(map[string]*memberState),
		approvals:       make(map[string]*activeTurn),
		relays:          make(map[string]int),
		relayBudget:     relayBudget,
		upkeepWaiting:   make(map[string]bool),
	}
}

// Trigger asks member to answer msg. A message in a thread is answered
// there; a top-level message gets a topic of its own. If the member is
// busy the trigger waits; waiting triggers of one thread are merged into
// the next turn.
func (m *TurnManager) Trigger(ctx context.Context, member store.Member, msg store.Message) error {
	var thread store.Thread
	if msg.ThreadID != "" {
		var err error
		if thread, err = m.store.GetThread(ctx, msg.ThreadID); err != nil {
			return fmt.Errorf("trigger %s: %w", member.DisplayName, err)
		}
	}
	return m.TriggerIn(ctx, member, msg, thread)
}

// TriggerIn is Trigger with the thread decided by the caller: a top-level
// message that several agents answer together is answered in the topic
// rooted at that message rather than in one topic per agent.
func (m *TurnManager) TriggerIn(ctx context.Context, member store.Member, msg store.Message, thread store.Thread) error {
	m.mu.Lock()
	st := m.state(member.ID)
	if st.starting || st.running != nil {
		st.pending = append(st.pending, trigger{msg: msg, thread: thread})
		m.mu.Unlock()
		return nil
	}
	st.starting = true
	m.mu.Unlock()

	m.start(ctx, member.ID, thread, []store.Message{msg}, nil)
	return nil
}

// state returns the member's queue, creating it. Callers hold m.mu.
func (m *TurnManager) state(memberID string) *memberState {
	st, ok := m.members[memberID]
	if !ok {
		st = &memberState{}
		m.members[memberID] = st
	}
	return st
}

// start runs one turn for memberID answering triggers in thread, opening
// a topic first when thread is zero; with up set, the turn is that wiki
// upkeep instead, answering its note. The caller has marked the member as
// starting; start always ends by either registering the running turn or
// releasing the member via advance.
//
// The member is re-read here rather than passed in so that the session
// reference saved by the previous turn is the one resumed.
func (m *TurnManager) start(ctx context.Context, memberID string, thread store.Thread, triggers []store.Message, up *upkeep) {
	if up != nil {
		// Queued no longer: from here on the turn, or its failure to start,
		// is what says how the upkeep went.
		defer m.upkeepStarted(up.project.ID)
	}
	member, err := m.store.GetMember(ctx, memberID)
	if err != nil {
		m.abort(ctx, memberID, thread, "", err)
		return
	}
	if member.Removed() {
		// Taken out of its project between being asked and starting: what
		// else it was asked in the meantime goes with it, an upkeep too.
		m.mu.Lock()
		for _, p := range m.state(memberID).pending {
			if p.upkeep != nil {
				delete(m.upkeepWaiting, p.upkeep.project.ID)
			}
		}
		m.state(memberID).pending = nil
		m.mu.Unlock()
		m.abort(ctx, memberID, thread, member.DisplayName, errRemoved)
		return
	}
	agent, err := m.store.GetAgent(ctx, member.AgentID)
	if err != nil {
		m.abort(ctx, memberID, thread, member.DisplayName, err)
		return
	}

	last := triggers[len(triggers)-1]
	var root *store.Message
	askedBy, initiator := false, last.UserID
	if thread.ID != "" {
		// A topic rooted at a person's message: the last reply of every
		// turn in it is addressed to them.
		if head, err := m.store.GetMessage(ctx, thread.RootMessageID); err == nil && head.SenderKind == store.SenderUser {
			askedBy = true
			if initiator == "" {
				initiator = head.UserID
			}
		}
	}
	if thread.ID == "" {
		msg, err := m.openTopic(ctx, member, last)
		if err != nil {
			m.abort(ctx, memberID, thread, member.DisplayName, err)
			return
		}
		if thread, err = m.store.ThreadForMessage(ctx, msg.ID); err != nil {
			m.abort(ctx, memberID, thread, member.DisplayName, err)
			return
		}
		root = &msg
		// Announced only now: subscribers learn the root and its thread
		// together, so the turn events that follow can be filed under it.
		m.publish(Event{Kind: EventMessage, RoomID: msg.Room, At: msg.CreatedAt, Message: &msg, Thread: &store.ThreadSummary{ID: thread.ID}})
	}

	if last.SenderKind == store.SenderUser {
		// A person spoke: the topic's relay budget starts over.
		m.mu.Lock()
		m.relays[thread.ID] = 0
		m.mu.Unlock()
	}

	// The member's one conversation with its runtime: resumed when it still
	// fits where the member runs now, replaced when it does not. An upkeep
	// runs in a session of its own, new every time: what the member keeps
	// in mind for the chat is no business of the wiki's, nor the other way
	// round, and the wiki it keeps is its memory.
	var session store.MemberSession
	var follows newSession
	kind := store.TurnChat
	if up == nil {
		if session, follows, err = m.sessionFor(ctx, member, agent); err != nil {
			m.abort(ctx, memberID, thread, member.DisplayName, err)
			return
		}
	} else {
		kind = store.TurnUpkeep
	}

	turn, err := m.store.CreateTurn(ctx, store.NewTurn{
		MemberID:         member.ID,
		RoomID:           thread.RoomID,
		ThreadID:         thread.ID,
		TriggerMessageID: last.ID,
		MachineID:        member.MachineID,
		Runtime:          agent.Runtime,
		SessionID:        session.ID,
		Kind:             kind,
	})
	if err != nil {
		m.abort(ctx, memberID, thread, member.DisplayName, err)
		return
	}
	at := &activeTurn{
		turn:       turn,
		member:     member,
		agent:      agent,
		thread:     thread,
		triggers:   triggers,
		root:       root,
		initiator:  initiator,
		askedBy:    askedBy,
		sessionRef: session.Ref,
		resumed:    session.Started(),
		pending:    make(map[string]*pendingApproval),
		work:       make(chan func(), workQueue),
		upkeep:     up,
	}

	// From here on every failure is recorded against the turn.
	conn, ok := m.connFor(member.MachineID)
	if !ok {
		m.register(at)
		m.failHere(at, "machine is offline")
		return
	}
	skills := m.skillSet(ctx, agent)
	var b brief
	if up == nil {
		b, err = m.brief.Build(ctx, briefInput{
			Member: member, Thread: thread, Triggers: triggers, Session: session, NewSession: follows.Reason,
			Skills: skills.Names(), Busy: m.busyIn(thread.RoomID, member.ID),
		})
	} else {
		b.Prompt, err = m.upkeepBrief(ctx, up, member, thread)
	}
	if err != nil {
		m.register(at)
		m.failHere(at, store.Reason(err))
		return
	}
	at.position, at.wikiPosition = b.Position, b.Wiki
	spec := runtime.TurnSpec{
		SystemPrompt: agent.RoleCard,
		Prompt:       b.Prompt,
		WorkDir:      member.RepoPath,
		Model:        firstNonEmpty(member.Model, agent.Model),
		Permission:   firstNonEmpty(string(member.PermissionPreset), string(agent.PermissionPreset)),
		Session:      sessionSpec(session),
		Options:      agent.RuntimeOptions,
		Skills:       skills,
	}
	// The memory tools while the person uses a memory (design.md 5.19).
	if m.wikis != nil && m.wikis.root != "" && m.wikis.memoryPrefs().UsesAny() {
		spec.ExtraTools = append(spec.ExtraTools, runtime.MemoryToolNames...)
	}
	if up != nil {
		// A session of one turn, under a name of the hub's so the runtimes
		// that take one keep it where they keep the member's others.
		spec.Session = runtime.Session{Key: turn.ID}
		spec.ExtraTools = append(spec.ExtraTools, runtime.UpkeepToolNames...)
	}

	at.spec = spec

	path := filepath.Join(m.transcripts, turn.ID+".jsonl")
	tx, err := openTranscript(path)
	if err != nil {
		m.register(at)
		m.failHere(at, store.Reason(err))
		return
	}
	at.transcript = tx
	if err := tx.write(transcriptLine{Kind: "start", TurnID: turn.ID, Runtime: agent.Runtime, Spec: transcriptSpec(spec)}); err != nil {
		m.logger.Warn("transcript", "turn", turn.ID, "err", err)
	}
	m.register(at)
	if follows.Announce {
		// The member starts over, through no fault of the conversation:
		// say so where the people are, before it answers.
		at.enqueue(func() {
			ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
			defer cancel()
			m.postSystem(ctx, thread, turn.ID, newSessionNote(member.DisplayName, follows.Reason))
		})
	}

	if err := conn.Send(ctx, protocol.StartTurn{TurnID: turn.ID, Runtime: agent.Runtime, Spec: spec}); err != nil {
		m.failHere(at, "dispatch to machine: "+err.Error())
	}
}

// failHere ends a turn over something the hub itself ran into. No second
// run in a new session follows: the session had nothing to do with it.
func (m *TurnManager) failHere(at *activeTurn, reason string) {
	at.mu.Lock()
	at.noRetry = true
	at.mu.Unlock()
	m.OnDone(at.turn.ID, protocol.TurnDone{TurnID: at.turn.ID, Error: reason})
}

// openTopic writes the empty top-level message that will hold the agent's
// first reply to trigger and head the topic.
func (m *TurnManager) openTopic(ctx context.Context, member store.Member, trigger store.Message) (store.Message, error) {
	msg, err := m.store.CreateMessage(ctx, store.NewMessage{
		RoomID:     trigger.Room,
		SenderKind: store.SenderAgent,
		MemberID:   member.ID,
	})
	if err != nil {
		return store.Message{}, fmt.Errorf("open topic: %w", err)
	}
	return msg, nil
}

// register makes at the member's running turn, starts its executor and
// announces it. It runs before dispatch, so subscribers always see a turn
// start before they see it finish, even when it fails at once.
func (m *TurnManager) register(at *activeTurn) {
	m.mu.Lock()
	st := m.state(at.member.ID)
	st.starting = false
	st.running = at
	m.active[at.turn.ID] = at
	m.mu.Unlock()
	go func() {
		for job := range at.work {
			job()
		}
	}()
	turn := at.turn
	m.publish(Event{Kind: EventTurnStarted, RoomID: turn.RoomID, At: turn.StartedAt, Turn: &turn})
}

// enqueue schedules a database write on the turn's executor and reports
// whether it was accepted. Writes after completion are dropped: the turn
// they belong to is already over.
func (at *activeTurn) enqueue(job func()) bool {
	at.mu.Lock()
	defer at.mu.Unlock()
	if at.closed {
		return false
	}
	at.work <- job
	return true
}

// closeSegment ends the current stretch of text at a tool boundary and
// stores it in order with everything said before. wait blocks until the
// text is stored, for callers about to post something that must follow it.
func (m *TurnManager) closeSegment(ctx context.Context, at *activeTurn, wait bool) {
	at.mu.Lock()
	at.toolActivity = true
	closed := strings.TrimSpace(at.segment.String())
	at.segment.Reset()
	at.mu.Unlock()
	if closed == "" {
		return
	}
	done := make(chan struct{})
	if !at.enqueue(func() {
		m.persistSegment(at, closed, nil)
		close(done)
	}) {
		return
	}
	if wait {
		select {
		case <-done:
		case <-ctx.Done():
		}
	}
}

// abort handles a failure before a turn row exists: it tells the room when
// there is a thread to tell, and releases the member so queued triggers
// still get their turn.
func (m *TurnManager) abort(ctx context.Context, memberID string, thread store.Thread, name string, err error) {
	if name == "" {
		name = "agent"
	}
	m.logger.Error("turn could not start", "member", memberID, "err", err)
	if thread.ID != "" {
		m.postSystem(ctx, thread, "", fmt.Sprintf("%s could not start a turn: %s", name, store.Reason(err)))
	}
	m.mu.Lock()
	m.state(memberID).starting = false
	m.mu.Unlock()
	m.advance(ctx, memberID)
}

// OnEvent records one event of a running turn. Text accumulates into the
// current segment; a tool call closes the segment and sends it to be
// stored. Events for unknown turns are dropped: they belong to a turn that
// already completed, for example after its machine disconnected.
func (m *TurnManager) OnEvent(turnID string, ev runtime.Event) {
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
	if ev.Kind == runtime.EventText {
		at.output.WriteString(ev.Text)
		at.segment.WriteString(ev.Text)
	}
	if ev.Kind == runtime.EventCompaction && ev.Phase == runtime.CompactionEnd {
		at.compactions++
	}
	if ev.Kind == runtime.EventFileChanged && ev.Path != "" && !at.fileSeen[ev.Path] {
		if at.fileSeen == nil {
			at.fileSeen = make(map[string]bool)
		}
		at.fileSeen[ev.Path] = true
		at.files = append(at.files, ev.Path)
	}
	for _, name := range skillsIn(ev, at.spec.Skills) {
		if !slices.Contains(at.skillsUsed, name) {
			at.skillsUsed = append(at.skillsUsed, name)
		}
	}
	at.mu.Unlock()

	if ev.Kind == runtime.EventSession {
		// Bookkeeping between the runtime and the hub: it is in the
		// transcript, and the room has no use for it.
		m.noteSessionRef(at, ev.SessionRef)
		return
	}
	if ev.Kind == runtime.EventApprovalWithdrawn {
		// The room hears of it as the approval's decision.
		m.withdraw(at, ev.ApprovalID)
		return
	}
	if ev.Kind == runtime.EventToolCall {
		m.closeSegment(context.Background(), at, false)
	}
	m.publish(Event{Kind: EventTurnEvent, RoomID: at.thread.RoomID, At: ev.At, TurnID: turnID, TurnEvent: &ev})
}

// persistSegment stores one stretch of the agent's text: the first segment
// of a topic fills its root, every other one is a reply in the thread.
// Runs on the turn's executor.
func (m *TurnManager) persistSegment(at *activeTurn, text string, extra []store.Mention) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()

	named := m.mentionedMembers(ctx, at, text)
	mentions := make([]store.Mention, 0, len(named)+len(extra))
	for _, a := range named {
		mentions = append(mentions, store.Mention{Kind: store.MentionAgent, ID: a.ID})
	}
	mentions = append(mentions, extra...)

	var msg store.Message
	var err error
	if at.root != nil && at.segments == 0 {
		msg, err = m.store.UpdateMessageBody(ctx, at.root.ID, text, at.turn.ID, mentions)
		if err == nil {
			m.publish(Event{Kind: EventMessage, RoomID: msg.Room, At: msg.CreatedAt, Message: &msg, Thread: &store.ThreadSummary{ID: at.thread.ID}})
		}
	} else {
		msg, err = m.post(ctx, store.NewMessage{
			RoomID:     at.thread.RoomID,
			ThreadID:   at.thread.ID,
			SenderKind: store.SenderAgent,
			MemberID:   at.member.ID,
			Body:       text,
			Mentions:   mentions,
			TurnID:     at.turn.ID,
		})
	}
	if err != nil {
		m.logger.Error("post reply", "turn", at.turn.ID, "err", err)
		return
	}
	at.mu.Lock()
	if at.root != nil && at.segments == 0 {
		at.root = &msg
	}
	at.segments++
	at.lastReply, at.lastReplyID = text, msg.ID
	for _, a := range named {
		if !at.namedAlready(a.ID) {
			at.relayTo = append(at.relayTo, relayTarget{member: a, msg: msg})
		}
	}
	at.mu.Unlock()
}

// namedAlready reports whether a turn's earlier reply named the member.
// Callers hold at.mu.
func (at *activeTurn) namedAlready(memberID string) bool {
	for _, target := range at.relayTo {
		if target.member.ID == memberID {
			return true
		}
	}
	return false
}

// OnDone completes a turn. It returns immediately; persistence and the
// closing message happen on the turn's executor, after any segment still
// waiting to be stored, so the caller's connection loop never blocks on
// the database.
func (m *TurnManager) OnDone(turnID string, done protocol.TurnDone) {
	m.mu.Lock()
	at := m.active[turnID]
	m.mu.Unlock()
	if at == nil {
		return
	}
	// A session that would not resume: the turn goes on, in a new one.
	if m.retryFresh(at, done) {
		return
	}
	m.mu.Lock()
	delete(m.active, turnID)
	m.mu.Unlock()
	at.mu.Lock()
	if at.closed {
		at.mu.Unlock()
		return
	}
	at.closed = true
	at.work <- func() { m.complete(at, done) }
	close(at.work)
	at.mu.Unlock()
}

// complete records the outcome, stores the last of what the agent said,
// posts the closing message or the failure, and lets the member's next
// turn start. Approvals still pending are closed first: nothing waits for
// them any more.
func (m *TurnManager) complete(at *activeTurn, done protocol.TurnDone) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()

	m.abandonApprovals(ctx, at)
	m.commitWiki(ctx, at)

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
	tail := strings.TrimSpace(at.segment.String())
	at.segment.Reset()
	at.mu.Unlock()

	at.mu.Lock()
	fresh, spent, files, skills := at.fresh, at.spent, at.files, at.skillsUsed
	at.fresh = nil
	at.mu.Unlock()

	outcome := store.TurnOutcome{TranscriptPath: filepath.Join(m.transcripts, at.turn.ID+".jsonl"), Usage: spent.Plus(done.Result.Usage), FilesChanged: files, SkillsUsed: skills}
	name := at.member.DisplayName
	switch {
	case done.Error == "":
		if fresh != nil {
			// The second run succeeded where resuming had failed: the new
			// session has earned its place and the old one ends. Had it
			// failed as well, the old one would simply have stayed.
			if ref := done.Result.SessionRef; ref != "" {
				fresh.ref = ref
			}
			if err := m.adopt(ctx, at, fresh); err != nil {
				m.logger.Error("keep the new session", "member", at.member.ID, "err", err)
			} else {
				m.postSystem(ctx, at.thread, at.turn.ID, newSessionNote(name, fresh.reason))
			}
		}
		m.finishReply(ctx, at, done.Result, streamed, tail)
		at.mu.Lock()
		outcome.Status, outcome.ReplyMessageID = store.TurnDone, at.lastReplyID
		at.mu.Unlock()
		// Runtimes report their session as soon as they know it; one that
		// only names it in the result is caught here.
		if ref := done.Result.SessionRef; ref != "" {
			at.mu.Lock()
			known := ref == at.sessionRef
			at.sessionRef = ref
			at.mu.Unlock()
			if !known {
				m.saveSessionRef(at, ref)
			}
		}
	case done.Cancelled:
		outcome.Status, outcome.Error = store.TurnCancelled, done.Error
		m.postSystem(ctx, at.thread, at.turn.ID, fmt.Sprintf("%s's turn was cancelled", name))
	default:
		outcome.Status, outcome.Error = store.TurnFailed, done.Error
		m.postSystem(ctx, at.thread, at.turn.ID, fmt.Sprintf("%s failed: %s", name, done.Error))
	}

	// A session tried and dropped has read nothing worth recording, and
	// the one it was tried in place of never saw this brief.
	if dropped := fresh != nil && done.Error != ""; !dropped {
		m.settleReading(ctx, at)
	}

	if at.upkeep != nil && outcome.Status == store.TurnDone {
		// Gone over: the next upkeep starts after them. One that failed or
		// was cancelled leaves them for the next.
		if err := m.store.RecordWikiReviews(ctx, at.upkeep.project.ID, at.turn.ID, at.upkeep.turnIDs()); err != nil {
			m.logger.Error("record the upkeep", "turn", at.turn.ID, "err", err)
		}
		// What people said up to where the chat stood as it was planned.
		if err := m.store.SetProjectWikiSeen(ctx, at.upkeep.project.ID, at.upkeep.position); err != nil {
			m.logger.Error("record how far the upkeep went", "turn", at.turn.ID, "err", err)
		}
	}
	finished, err := m.store.FinishTurn(ctx, at.turn.ID, outcome)
	if err != nil {
		m.logger.Error("finish turn", "turn", at.turn.ID, "err", err)
		// Subscribers still need to hear that the turn is over.
		finished = at.turn
		finished.Status, finished.Error, finished.ReplyMessageID, finished.Usage = outcome.Status, outcome.Error, outcome.ReplyMessageID, outcome.Usage
		now := time.Now()
		finished.EndedAt = &now
	}
	m.publish(Event{Kind: EventTurnFinished, RoomID: finished.RoomID, Turn: &finished})
	m.noteTrialUses(ctx, finished)
	if done.Error == "" && at.upkeep == nil {
		// An upkeep hands nothing over: the members it names are named in
		// passing, not asked.
		m.relay(ctx, at)
	}
	m.advance(ctx, at.member.ID)
}

// finishReply stores the final text of a successful turn and, for a turn
// that opened a topic and did more than answer in one breath, posts the
// closing top-level message that mentions the initiator. The mention is
// also written into the text, as a person would, so every reader sees it.
//
// tail is the text since the last tool call. When nothing was streamed at
// all the runtime's result output stands in, so runtimes that only report
// at the end still get their reply posted.
func (m *TurnManager) finishReply(ctx context.Context, at *activeTurn, result runtime.Result, streamed, tail string) {
	text := tail
	if text == "" && at.segments == 0 {
		text = strings.TrimSpace(result.Output)
		if text == "" {
			text = strings.TrimSpace(streamed)
		}
		if text == "" {
			text = "(no reply)"
		}
	}
	if text != "" {
		var extra []store.Mention
		if at.askedBy && at.initiator != "" {
			// In a topic a person opened, the last word is addressed to them,
			// so it reaches their inbox; there is no closing message to do it.
			if user, err := m.store.GetUser(ctx, at.initiator); err == nil {
				text = joinMention(user.Name, text)
			}
			extra = []store.Mention{{Kind: store.MentionUser, ID: at.initiator}}
		}
		m.persistSegment(at, text, extra)
	}

	at.mu.Lock()
	closing := at.root != nil && at.initiator != "" && (at.segments > 1 || at.toolActivity)
	body := at.lastReply
	at.mu.Unlock()
	if !closing || body == "" {
		return
	}
	if user, err := m.store.GetUser(ctx, at.initiator); err == nil {
		body = joinMention(user.Name, body)
	}
	mentions := append([]store.Mention{{Kind: store.MentionUser, ID: at.initiator}}, m.agentMentions(ctx, at, body)...)
	_, err := m.post(ctx, store.NewMessage{
		RoomID:     at.thread.RoomID,
		SenderKind: store.SenderAgent,
		MemberID:   at.member.ID,
		Body:       body,
		Mentions:   mentions,
		TurnID:     at.turn.ID,
	})
	if err != nil {
		m.logger.Error("post closing message", "turn", at.turn.ID, "err", err)
	}
}

// errRemoved is why a member taken out of its project does not start the
// turn it was asked for just before.
var errRemoved = errors.New("it was taken out of the project")

// mentionedMembers finds the room's other current members named with an @
// in what an agent said.
func (m *TurnManager) mentionedMembers(ctx context.Context, at *activeTurn, text string) []store.Member {
	if !strings.Contains(text, "@") {
		return nil
	}
	members, err := m.store.ListRoomMembers(ctx, at.thread.RoomID)
	if err != nil {
		m.logger.Warn("list members for mentions", "room", at.thread.RoomID, "err", err)
		return nil
	}
	var out []store.Member
	for _, a := range members {
		if a.ID != at.member.ID && !a.Removed() && strings.Contains(text, "@"+a.DisplayName) {
			out = append(out, a)
		}
	}
	return out
}

// agentMentions is mentionedMembers as mentions, recorded on a message so
// the UI can draw them and offer hand-offs.
func (m *TurnManager) agentMentions(ctx context.Context, at *activeTurn, text string) []store.Mention {
	var out []store.Mention
	for _, a := range m.mentionedMembers(ctx, at, text) {
		out = append(out, store.Mention{Kind: store.MentionAgent, ID: a.ID})
	}
	return out
}

// relay wakes the members this turn's replies named, in the same topic, as
// long as the topic's budget of agent-to-agent turns since a person last
// spoke allows; past it the mentions stay as hand-off buttons and a note
// in the topic says so. Runs on the turn's executor, after the turn is
// recorded, so the woken agent's brief holds everything this one said.
func (m *TurnManager) relay(ctx context.Context, at *activeTurn) {
	at.mu.Lock()
	targets := at.relayTo
	at.mu.Unlock()
	if len(targets) == 0 || m.relayBudget < 0 {
		return
	}
	for _, target := range targets {
		member, err := m.store.GetMember(ctx, target.member.ID)
		if err != nil || !member.Enabled || member.Removed() {
			continue
		}
		m.mu.Lock()
		hops := m.relays[at.thread.ID]
		if hops < m.relayBudget {
			m.relays[at.thread.ID] = hops + 1
		}
		m.mu.Unlock()
		if hops >= m.relayBudget {
			m.postSystem(ctx, at.thread, at.turn.ID, fmt.Sprintf("%s mentioned %s, but this topic has had %d agent-to-agent turns since a person last spoke; it waits for a person now", at.member.DisplayName, member.DisplayName, m.relayBudget))
			return
		}
		if err := m.TriggerIn(ctx, member, target.msg, at.thread); err != nil {
			m.logger.Error("relay to member", "member", member.ID, "turn", at.turn.ID, "err", err)
		}
	}
}

// advance releases a member and, if triggers are waiting, starts the
// next turn: every pending trigger of the oldest waiting thread together,
// or a single top-level trigger on its own, since each of those opens its
// own topic.
func (m *TurnManager) advance(ctx context.Context, memberID string) {
	m.mu.Lock()
	st := m.state(memberID)
	st.running = nil
	if len(st.pending) == 0 {
		delete(m.members, memberID)
		m.mu.Unlock()
		return
	}
	next := st.pending[0]
	if next.upkeep != nil {
		// An upkeep runs on its own.
		st.pending = st.pending[1:]
		st.starting = true
		m.mu.Unlock()
		m.start(ctx, memberID, next.thread, []store.Message{next.msg}, next.upkeep)
		return
	}
	thread := next.thread
	var msgs []store.Message
	var rest []trigger
	for i, p := range st.pending {
		if p.upkeep == nil && ((thread.ID == "" && i == 0) || (thread.ID != "" && p.thread.ID == thread.ID)) {
			msgs = append(msgs, p.msg)
		} else {
			rest = append(rest, p)
		}
	}
	st.pending = rest
	st.starting = true
	m.mu.Unlock()

	m.start(ctx, memberID, thread, msgs, nil)
}

// Cancel asks the machine running turnID to stop it. The turn completes
// through the normal OnDone path once the machine confirms.
func (m *TurnManager) Cancel(ctx context.Context, turnID string) error {
	m.mu.Lock()
	at := m.active[turnID]
	m.mu.Unlock()
	if at == nil {
		return fmt.Errorf("%w: turn %s is not running", ErrUnknownTurn, turnID)
	}
	conn, ok := m.connFor(at.member.MachineID)
	if !ok {
		return fmt.Errorf("cancel turn %s: %w", turnID, store.Conflicting("machineOffline", nil, "the machine running it is offline"))
	}
	return conn.Send(ctx, protocol.CancelTurn{TurnID: turnID})
}

// MachineGone fails every turn running on a machine that disconnected; no
// TurnDone will ever arrive for them.
func (m *TurnManager) MachineGone(machineID string) {
	m.mu.Lock()
	var orphaned []*activeTurn
	for _, at := range m.active {
		if at.member.MachineID == machineID {
			orphaned = append(orphaned, at)
		}
	}
	m.mu.Unlock()
	for _, at := range orphaned {
		m.failHere(at, "machine disconnected")
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

// postSystem puts a note from the system into a thread, filed under
// turnID when there is one. Failures are logged: a lost note must not
// stop the turn pipeline.
func (m *TurnManager) postSystem(ctx context.Context, thread store.Thread, turnID, text string) {
	_, err := m.post(ctx, store.NewMessage{
		RoomID:     thread.RoomID,
		ThreadID:   thread.ID,
		SenderKind: store.SenderSystem,
		Body:       text,
		TurnID:     turnID,
	})
	if err != nil {
		m.logger.Error("post system message", "thread", thread.ID, "err", err)
	}
}

// messageEvent is the live event for a stored message.
func messageEvent(msg store.Message) Event {
	return Event{Kind: EventMessage, RoomID: msg.Room, At: msg.CreatedAt, Message: &msg}
}

// blockStart matches a first line that markdown reads as a block: a
// heading, a list item, a quote, a fence, a table. Prefixing such text on
// the same line would turn the block into plain words.
var blockStart = regexp.MustCompile("^(#{1,6} |[-*+] |\\d+\\. |> |```|\\|)")

// joinMention puts "@name" in front of body the way a person would: on the
// same line for a sentence, on its own line when the body starts with a
// markdown block.
func joinMention(name, body string) string {
	if blockStart.MatchString(body) {
		return "@" + name + "\n\n" + body
	}
	return "@" + name + " " + body
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
