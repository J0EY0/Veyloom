package hub

import (
	"cmp"
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
	"unicode"
	"unicode/utf8"

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
// split at tool calls, lands in the thread; the last of it is addressed to
// whoever asked (finishReply). A trigger inside an existing thread simply
// answers in that thread.
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
	logger          *slog.Logger

	mu        sync.Mutex
	active    map[string]*activeTurn  // by turn ID
	members   map[string]*memberState // by member ID
	approvals map[string]*activeTurn  // by approval ID, while pending
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
	// workspace asks a machine to work on a checkout or a worktree; nil
	// leaves every member in its checkout (design.md 5.21).
	workspace workspaceCall
	// setups are the projects whose setup members' turns wait for, by id.
	setups map[string]*setupWait
	// errands are the work turns people asked for handed on, by the asked
	// turn; wakes which errand each wake is part of; sumUps the errand each
	// note asking for a summing up is about (handedon.go).
	errands map[string]*errand
	wakes   map[wakeKey]string
	sumUps  map[string]*errand
	// overlapLocks are the locks each project's overlaps are worked out
	// under, by main room; overlapChecks the checks under way.
	overlapMu     sync.Mutex
	overlapLocks  map[string]*sync.Mutex
	overlapChecks sync.WaitGroup
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
// message announces a wiki upkeep, with setup the leader's setup of the
// project (design.md 5.21); each runs as a turn of its own.
type trigger struct {
	msg    store.Message
	thread store.Thread
	upkeep *upkeep
	setup  *setupRun
	// anchor starts a piece of work of its own at this message: a person
	// let a held wake go on (design.md 5.22).
	anchor string
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
	// initiator is the user whose message asked for the turn; its last
	// word is addressed to them.
	initiator string
	// askedBy is set when the topic is rooted at a person's own message
	// (several agents asked at once): replies stay in the thread, the last
	// one addressed to that person.
	askedBy bool
	// origin is the turn a person asked for whose work this one carries
	// on, when an agent's wake started it; asked is the member it asked,
	// and waker the member whose turn woke this one. Naming either reports
	// back, and wakes it not. handedOn, on the turn summing that work up,
	// is what came of it (handedon.go).
	origin   string
	asked    store.Member
	waker    store.Member
	handedOn []handedResult

	mu         sync.Mutex
	transcript *transcript
	// trustedBy is the person who let the rest of the turn's requests
	// through, empty while nobody did (docs/design.md 4.6).
	trustedBy string
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
	segments int
	// toolActivity says the turn reached a tool boundary at all; usedTools
	// that it used a tool or asked permission, more than answering in one
	// breath (finishReply). Reaching for a tool is the first, not the
	// second.
	toolActivity bool
	usedTools    bool
	// worked says the turn did work: called a tool other than those that
	// follow and talk in the chat, or changed a file (design.md 5.22).
	worked bool
	// sent counts the messages it posted with send_message, and named the
	// members they named: its reply naming them again only tells of it,
	// and wakes them no more. relays is how its piece of work stood against
	// the relay limit as it began, for the brief, nil for a turn not in the
	// chat.
	sent   int
	named  map[string]bool
	relays *relaysLeft
	// handedBy says, for the brief, whose work a woken turn is part of.
	handedBy *handedBy
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
	// setup is set on the leader's turn setting the project up.
	setup *setupRun
	// dir is where the member works this turn (design.md 5.21).
	dir string
	// early are the notices of the turn from before its transcript was
	// opened, while the member's worktree was got ready; they go in first.
	early []runtime.Event
	// prepCancel stops getting the worktree ready; cancelled says a person
	// cancelled the turn before it reached the machine, dispatched that it
	// did.
	prepCancel context.CancelFunc
	cancelled  bool
	dispatched bool
}

// relayTarget is a member named in a reply, and that reply.
type relayTarget struct {
	member store.Member
	msg    store.Message
}

// workQueue bounds how many writes may wait per turn before the event loop
// blocks; a turn does not produce anywhere near this many segments.
const workQueue = 256

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
		members:         make(map[string]*memberState),
		approvals:       make(map[string]*activeTurn),
		upkeepWaiting:   make(map[string]bool),
		setups:          make(map[string]*setupWait),
		errands:         make(map[string]*errand),
		wakes:           make(map[wakeKey]string),
		sumUps:          make(map[string]*errand),
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
	return m.triggerFrom(ctx, member, msg, thread, "")
}

// triggerFrom is TriggerIn, the turn starting a piece of work of its own at
// anchor when that is set.
func (m *TurnManager) triggerFrom(ctx context.Context, member store.Member, msg store.Message, thread store.Thread, anchor string) error {
	m.mu.Lock()
	st := m.state(member.ID)
	if st.starting || st.running != nil {
		st.pending = append(st.pending, trigger{msg: msg, thread: thread, anchor: anchor})
		m.mu.Unlock()
		return nil
	}
	st.starting = true
	m.mu.Unlock()

	m.start(ctx, member.ID, thread, []store.Message{msg}, anchor, nil, nil)
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
// upkeep instead, answering its note, with setup the leader's setup of the
// project. The caller has marked the member as starting; start always ends
// by either registering the running turn or releasing the member via
// advance.
//
// The member is re-read here rather than passed in so that the session
// reference saved by the previous turn is the one resumed.
func (m *TurnManager) start(ctx context.Context, memberID string, thread store.Thread, triggers []store.Message, anchor string, up *upkeep, setup *setupRun) {
	if up != nil {
		// Queued no longer: from here on the turn, or its failure to start,
		// is what says how the upkeep went.
		defer m.upkeepStarted(up.project.ID)
	}
	recorded := false
	defer func() {
		if !recorded {
			// The wakes it answered end with it.
			m.dropWakes(memberID, triggers)
		}
	}()
	if setup != nil {
		// Once on record the turn's end settles the setup; before, nothing
		// else would.
		defer func() {
			if !recorded {
				m.settleSetup(setup.project.ID, errors.New("the leader's setup could not start"))
			}
		}()
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
		var setups []string
		var waiting []store.Message
		for _, p := range m.state(memberID).pending {
			if p.upkeep != nil {
				delete(m.upkeepWaiting, p.upkeep.project.ID)
			}
			if p.setup != nil {
				setups = append(setups, p.setup.project.ID)
			}
			waiting = append(waiting, p.msg)
		}
		m.state(memberID).pending = nil
		m.mu.Unlock()
		for _, id := range setups {
			m.settleSetup(id, errRemoved)
		}
		m.dropWakes(memberID, waiting)
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
		// A topic rooted at a person's message, where several agents were
		// asked at once: the last reply of every turn asked for in it is
		// addressed to them. A turn another member's message woke answers
		// that member instead; what came of it reaches the person once, as
		// the member that handed it on sums up (handedon.go).
		if head, err := m.store.GetMessage(ctx, thread.RootMessageID); err == nil && head.SenderKind == store.SenderUser {
			askedBy = true
			if initiator == "" && last.SenderKind != store.SenderAgent {
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
		m.publish(Event{Kind: EventMessage, RoomID: msg.Room, At: msg.CreatedAt, Message: &msg, Thread: topicSummary(thread)})
	}

	kind := store.TurnChat
	switch {
	case up != nil:
		kind = store.TurnUpkeep
	case setup != nil:
		kind = store.TurnSetup
	}

	// The turn is on record before its session: which session carries a
	// chat turn depends on where the member works, and getting its worktree
	// ready may take a while, which the turn spends under way (design.md
	// 5.21).
	chain, wokenBy := m.pieceOfWork(ctx, triggers, anchor)
	turn, err := m.store.CreateTurn(ctx, store.NewTurn{
		MemberID:         member.ID,
		RoomID:           thread.RoomID,
		ThreadID:         thread.ID,
		TriggerMessageID: last.ID,
		MachineID:        member.MachineID,
		Runtime:          agent.Runtime,
		Kind:             kind,
		ChainMessageID:   chain,
		WokenByTurnID:    wokenBy,
	})
	if err != nil {
		m.abort(ctx, memberID, thread, member.DisplayName, err)
		return
	}
	recorded = true
	// Work handed on: the work this turn carries on, and what came of the
	// work it sums up, whose last word goes to the person.
	var handedOn []handedResult
	if summing := m.takeSumUp(triggers); summing != nil {
		handedOn = summing.results
		if initiator == "" {
			initiator = m.chainPerson(ctx, summing.chain)
		}
	}
	origin := m.originOf(member.ID, triggers)
	asked := m.askedOf(origin)
	var waker store.Member
	if origin != "" && wokenBy != "" {
		if woke, err := m.store.GetTurn(ctx, wokenBy); err == nil {
			waker, _ = m.store.GetMember(ctx, woke.MemberID)
		}
	}
	at := &activeTurn{
		turn:      turn,
		member:    member,
		agent:     agent,
		thread:    thread,
		triggers:  triggers,
		root:      root,
		initiator: initiator,
		askedBy:   askedBy,
		origin:    origin,
		asked:     asked,
		waker:     waker,
		handedOn:  handedOn,
		pending:   make(map[string]*pendingApproval),
		work:      make(chan func(), workQueue),
		upkeep:    up,
		setup:     setup,
	}

	// From here on every failure is recorded against the turn.
	m.register(at)
	conn, ok := m.connFor(member.MachineID)
	if !ok {
		m.failHere(at, "machine is offline")
		return
	}
	dir, err := m.workDir(ctx, at)
	if err != nil {
		m.failPreparing(at, err)
		return
	}
	// Getting the worktree ready may have taken its time: what follows gets
	// time of its own.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.storeTimeout)
	defer cancel()
	at.mu.Lock()
	over := at.closed || at.cancelled
	member = at.member
	at.dir = dir
	at.mu.Unlock()
	if over {
		m.endUndispatched(at)
		return
	}

	// The member's one conversation with its runtime: resumed when it still
	// fits where the member runs now, replaced when it does not. An upkeep
	// and a setup run in sessions of their own, new every time: what the
	// member keeps in mind for the chat is no business of theirs, nor the
	// other way round, and the wiki a maintainer keeps is its memory.
	var session store.MemberSession
	var follows newSession
	if kind == store.TurnChat {
		if session, follows, err = m.sessionFor(ctx, member, agent, dir); err != nil {
			m.failHere(at, store.Reason(err))
			return
		}
		if err := m.store.SetTurnSession(ctx, turn.ID, session.ID); err != nil {
			m.failHere(at, store.Reason(err))
			return
		}
		at.mu.Lock()
		at.turn.SessionID, at.sessionRef, at.resumed = session.ID, session.Ref, session.Started()
		at.mu.Unlock()
	}

	skills := m.skillSet(ctx, agent)
	var b brief
	leads := false
	switch kind {
	case store.TurnUpkeep:
		b.Prompt, err = m.upkeepBrief(ctx, up, member, thread)
	case store.TurnSetup:
		var project store.Project
		if project, err = m.store.GetProject(ctx, setup.project.ID); err == nil {
			b.Prompt, leads = setupBrief(setup, member, project), true
		}
	default:
		at.relays = m.relaysLeftOf(ctx, member, turn)
		at.handedBy = m.handedByOf(ctx, at)
		b, err = m.brief.Build(ctx, briefInput{
			Member: member, Thread: thread, Triggers: triggers, Session: session, NewSession: follows.Reason,
			Skills: skills.Names(), Busy: m.busyIn(thread.RoomID, member.ID), Dir: dir, Relays: at.relays, HandedOn: at.handedOn,
			HandedBy: at.handedBy,
		})
		leads = b.Leads
	}
	if err != nil {
		m.failHere(at, store.Reason(err))
		return
	}
	at.position, at.wikiPosition = b.Position, b.Wiki
	spec := runtime.TurnSpec{
		SystemPrompt: agent.RoleCard,
		Prompt:       b.Prompt,
		WorkDir:      dir,
		Model:        firstNonEmpty(member.Model, agent.Model),
		Permission:   firstNonEmpty(string(member.PermissionPreset), string(agent.PermissionPreset)),
		Session:      sessionSpec(session),
		Options:      agent.RuntimeOptions,
		Skills:       skills,
	}
	// What people allowed the member always, in the presets that ask
	// people (design.md 4.6). Without them people are asked, as before.
	if spec.Permission == runtime.PermissionEditWithApproval || spec.Permission == runtime.PermissionAutoReview {
		rules, err := m.store.ListMemberRules(ctx, member.ID, agent.Runtime)
		if err != nil {
			m.logger.Warn("read the member's rules", "member", member.ID, "err", err)
		}
		for _, r := range rules {
			spec.AllowedRules = append(spec.AllowedRules, r.Rule)
		}
	}
	// The memory tools while the person uses a memory (design.md 5.19).
	if m.wikis != nil && m.wikis.root != "" && m.wikis.memoryPrefs().UsesAny() {
		spec.ExtraTools = append(spec.ExtraTools, runtime.MemoryToolNames...)
	}
	if up != nil {
		spec.ExtraTools = append(spec.ExtraTools, runtime.UpkeepToolNames...)
	}
	if leads {
		// The leader writes down how worktrees are got ready, in its setup
		// and whenever a person asks it to (design.md 5.21).
		spec.ExtraTools = append(spec.ExtraTools, runtime.SetupToolNames...)
	}
	if kind == store.TurnChat {
		// Members talk and hand work on as they go (design.md 5.22).
		spec.ExtraTools = append(spec.ExtraTools, runtime.MessageToolNames...)
	}
	if kind != store.TurnChat {
		// A session of one turn, under a name of the hub's so the runtimes
		// that take one keep it where they keep the member's others.
		spec.Session = runtime.Session{Key: turn.ID}
	}

	path := filepath.Join(m.transcripts, turn.ID+".jsonl")
	tx, err := openTranscript(path)
	if err != nil {
		m.failHere(at, store.Reason(err))
		return
	}
	if err := tx.write(transcriptLine{Kind: "start", TurnID: turn.ID, Runtime: agent.Runtime, Spec: transcriptSpec(spec)}); err != nil {
		m.logger.Warn("transcript", "turn", turn.ID, "err", err)
	}
	at.mu.Lock()
	at.spec = spec
	for _, ev := range at.early {
		if err := tx.write(transcriptLine{Kind: "event", At: ev.At, Event: &ev}); err != nil {
			m.logger.Warn("transcript", "turn", turn.ID, "err", err)
		}
	}
	at.early = nil
	at.transcript = tx
	at.mu.Unlock()
	if follows.Announce {
		// The member starts over, through no fault of the conversation:
		// say so where the people are, before it answers.
		at.enqueue(func() {
			ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
			defer cancel()
			m.postSystem(ctx, thread, turn.ID, newSessionNote(member.DisplayName, follows.Reason))
		})
	}

	at.mu.Lock()
	over = at.closed || at.cancelled
	at.dispatched = !over
	at.mu.Unlock()
	if over {
		m.endUndispatched(at)
		return
	}
	if err := conn.Send(ctx, protocol.StartTurn{TurnID: turn.ID, Runtime: agent.Runtime, Spec: spec}); err != nil {
		m.failHere(at, "dispatch to machine: "+err.Error())
	}
}

// workDir is where the member of at works this turn: for a chat turn, its
// worktree got ready first when it needs one; for an upkeep or a setup,
// where it works already. A person's cancelling stops the waiting.
func (m *TurnManager) workDir(ctx context.Context, at *activeTurn) (string, error) {
	if at.upkeep != nil || at.setup != nil {
		project, err := m.store.RoomProject(ctx, at.member.RoomID)
		if err != nil {
			return "", err
		}
		return works(at.member, project), nil
	}
	prep, cancel := context.WithTimeout(context.WithoutCancel(ctx), prepareLimit)
	defer cancel()
	at.mu.Lock()
	at.prepCancel = cancel
	stop := at.cancelled
	at.mu.Unlock()
	if stop {
		return "", context.Canceled
	}
	return m.dirFor(prep, at)
}

// failPreparing ends a turn whose worktree could not be got ready, or which
// a person cancelled meanwhile.
func (m *TurnManager) failPreparing(at *activeTurn, err error) {
	at.mu.Lock()
	cancelled := at.cancelled
	at.mu.Unlock()
	if cancelled {
		m.endUndispatched(at)
		return
	}
	m.failHere(at, store.Reason(err))
}

// endUndispatched ends a turn that never reached its machine: cancelled by
// a person, or already over for another reason.
func (m *TurnManager) endUndispatched(at *activeTurn) {
	at.mu.Lock()
	closed := at.closed
	at.noRetry = true
	at.mu.Unlock()
	if closed {
		return
	}
	m.OnDone(at.turn.ID, protocol.TurnDone{TurnID: at.turn.ID, Error: "cancelled before it began", Cancelled: true})
}

// failHere ends a turn over something the hub itself ran into. No second
// run in a new session follows: the session had nothing to do with it.
func (m *TurnManager) failHere(at *activeTurn, reason string) {
	at.mu.Lock()
	at.noRetry = true
	if stop := at.prepCancel; stop != nil && !at.dispatched {
		// Whatever was being got ready is not needed any more.
		defer stop()
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()
	m.publish(Event{Kind: EventTurnStarted, RoomID: turn.RoomID, At: turn.StartedAt, Turn: &turn, Work: m.workOf(ctx, turn)})
}

// workOf is the piece of work a turn is part of, as it stands now, for the
// timeline to show under the topic it began in; nil when that is not known.
func (m *TurnManager) workOf(ctx context.Context, turn store.Turn) *store.WorkSummary {
	if turn.ChainMessageID == "" {
		return nil
	}
	work, err := m.store.ChainWork(ctx, turn.ChainMessageID)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			m.logger.Warn("read a piece of work", "chain", turn.ChainMessageID, "err", err)
		}
		return nil
	}
	return &work
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
// text is stored, for callers about to post something that must follow it;
// used says the turn used a tool there, which reaching for one is not.
func (m *TurnManager) closeSegment(ctx context.Context, at *activeTurn, wait, used bool) {
	at.mu.Lock()
	at.toolActivity = true
	at.usedTools = at.usedTools || used
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
	if (ev.Kind == runtime.EventToolCall && workTool(ev.Tool)) || ev.Kind == runtime.EventFileChanged {
		at.worked = true
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
		m.closeSegment(context.Background(), at, false, !reachTools[ev.Tool])
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
			m.publish(Event{Kind: EventMessage, RoomID: msg.Room, At: msg.CreatedAt, Message: &msg, Thread: topicSummary(at.thread)})
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
// last word happen on the turn's executor, after any segment still
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
// addressed to whoever asked, or the failure, and lets the member's next
// turn start. Approvals still pending are closed first: nothing waits for
// them any more.
func (m *TurnManager) complete(at *activeTurn, done protocol.TurnDone) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()

	m.abandonApprovals(ctx, at)
	wikiPages := m.commitWiki(ctx, at)

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
	fresh, spent, files, skills, worked := at.fresh, at.spent, at.files, at.skillsUsed, at.worked
	at.fresh = nil
	at.mu.Unlock()

	outcome := store.TurnOutcome{
		TranscriptPath: filepath.Join(m.transcripts, at.turn.ID+".jsonl"), Usage: spent.Plus(done.Result.Usage),
		FilesChanged: files, SkillsUsed: skills, WikiPages: wikiPages, Worked: worked,
	}
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
	// Seen to before the turn is over: whoever sees it over sees the merge
	// it left settled, or the files it left unsettled named.
	m.concludeMerge(ctx, at)
	m.checkOverlaps(at)
	finished, err := m.store.FinishTurn(ctx, at.turn.ID, outcome)
	if err != nil {
		m.logger.Error("finish turn", "turn", at.turn.ID, "err", err)
		// Subscribers still need to hear that the turn is over. A turn
		// that ended before it was dispatched may still be starting: its
		// session is set under the lock.
		at.mu.Lock()
		finished = at.turn
		at.mu.Unlock()
		finished.Status, finished.Error, finished.ReplyMessageID, finished.Usage = outcome.Status, outcome.Error, outcome.ReplyMessageID, outcome.Usage
		now := time.Now()
		finished.EndedAt = &now
	}
	m.publish(Event{Kind: EventTurnFinished, RoomID: finished.RoomID, Turn: &finished, Work: m.workOf(ctx, finished)})
	m.noteTrialUses(ctx, finished)
	if at.setup != nil {
		m.setupDone(ctx, at, outcome.Status, outcome.Error)
	}
	if done.Error == "" && at.upkeep == nil && at.setup == nil && len(at.handedOn) == 0 {
		// An upkeep or a setup hands nothing over, nor does summing up the
		// work handed on: the members it names are named in passing, not
		// asked. Summing up hands more work on with send_message only.
		m.relay(ctx, at)
	}
	// Once its own wakes are set going, the work it took part in is
	// settled: were it the last, it is summed up.
	m.turnEnded(at, outcome.Status, outcome.Error)
	m.advance(ctx, at.member.ID)
}

// finishReply stores the final text of a successful turn. A turn a person
// asked for addresses its last word to them, in its topic, so it reaches
// their inbox: always when the answer lies in the topic, and at the head
// of a topic the turn opened once it did more than answer in one breath
// right under their question. A turn that hands its work on does not: the
// summing up does, once the work is done (handedon.go). The mention is
// also written into the text, as a person would, so every reader sees it.
// When the last word came before the turn's last tool call, that message
// is addressed in place.
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
	at.mu.Lock()
	address := at.initiator != "" && (at.root == nil || at.segments > 0 || at.usedTools)
	at.mu.Unlock()
	if address && m.handsOn(ctx, at, text) {
		// Its work goes on with others: summing it up, or the note of a
		// wake held back, is what reaches the person, once.
		address = false
	}
	var user store.User
	if address {
		var err error
		if user, err = m.store.GetUser(ctx, at.initiator); err != nil {
			m.logger.Warn("the person a turn answers", "turn", at.turn.ID, "err", err)
			address = false
		}
	}
	switch {
	case text != "" && address:
		m.persistSegment(at, addressTo(user.Name, text), []store.Mention{{Kind: store.MentionUser, ID: user.ID}})
	case text != "":
		m.persistSegment(at, text, nil)
	case address:
		m.addressLast(ctx, at, user)
	}
}

// addressLast addresses the turn's last message to user in place: the
// turn said nothing after its last tool call.
func (m *TurnManager) addressLast(ctx context.Context, at *activeTurn, user store.User) {
	at.mu.Lock()
	id := at.lastReplyID
	at.mu.Unlock()
	if id == "" {
		return
	}
	msg, err := m.store.GetMessage(ctx, id)
	if err != nil {
		m.logger.Error("address the last word", "turn", at.turn.ID, "err", err)
		return
	}
	mentions := msg.Mentions
	if mention := (store.Mention{Kind: store.MentionUser, ID: user.ID}); !slices.Contains(mentions, mention) {
		mentions = append(slices.Clone(mentions), mention)
	}
	msg, err = m.store.UpdateMessageBody(ctx, id, addressTo(user.Name, msg.Body), at.turn.ID, mentions)
	if err != nil {
		m.logger.Error("address the last word", "turn", at.turn.ID, "err", err)
		return
	}
	ev := messageEvent(msg)
	if msg.ThreadID == "" {
		// The head of the topic the turn opened.
		ev.Thread = topicSummary(at.thread)
	}
	m.publish(ev)
	at.mu.Lock()
	at.lastReply = msg.Body
	at.mu.Unlock()
}

// errRemoved is why a member taken out of its project does not start the
// turn it was asked for just before.
var errRemoved = errors.New("it was taken out of the project")

// mentionedMembers finds the room's other current members named with an @
// in what an agent said, its code left out.
func (m *TurnManager) mentionedMembers(ctx context.Context, at *activeTurn, text string) []store.Member {
	if text = prose(text); !strings.Contains(text, "@") {
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

// relay wakes the members this turn's replies named, in the same topic, as
// long as the limits on agents waking one another in the piece of work
// allow (design.md 5.22); a wake they hold back is told to the person, the
// mention staying a hand-off button. Runs on the turn's executor, after
// the turn is recorded, so the woken agent's brief holds everything this
// one said.
func (m *TurnManager) relay(ctx context.Context, at *activeTurn) {
	at.mu.Lock()
	targets := at.relayTo
	at.mu.Unlock()
	for _, target := range targets {
		at.mu.Lock()
		sentTo := at.named[target.member.ID]
		at.mu.Unlock()
		if sentTo {
			// Woken, or held back, by what the turn sent already.
			continue
		}
		if at.reportsTo(target.member.ID) {
			// Reports back: the member asked hears it with the rest, once
			// the work it handed on is done.
			continue
		}
		member, err := m.store.GetMember(ctx, target.member.ID)
		if err != nil || !member.Enabled || member.Removed() {
			continue
		}
		if !m.mayWake(ctx, at, member, target.msg, at.thread) {
			continue
		}
		m.handOn(at, member, target.msg)
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
	if next.upkeep != nil || next.setup != nil {
		// An upkeep or a setup runs on its own.
		st.pending = st.pending[1:]
		st.starting = true
		m.mu.Unlock()
		m.start(ctx, memberID, next.thread, []store.Message{next.msg}, "", next.upkeep, next.setup)
		return
	}
	thread := next.thread
	var msgs []store.Message
	var rest []trigger
	anchor := ""
	for i, p := range st.pending {
		if p.upkeep == nil && p.setup == nil && ((thread.ID == "" && i == 0) || (thread.ID != "" && p.thread.ID == thread.ID)) {
			msgs = append(msgs, p.msg)
			anchor = cmp.Or(p.anchor, anchor)
		} else {
			rest = append(rest, p)
		}
	}
	st.pending = rest
	st.starting = true
	m.mu.Unlock()

	m.start(ctx, memberID, thread, msgs, anchor, nil, nil)
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
	at.mu.Lock()
	before := !at.dispatched
	if before {
		// Not on the machine yet, perhaps still getting a worktree ready:
		// the hub ends it itself.
		at.cancelled = true
	}
	stop := at.prepCancel
	at.mu.Unlock()
	if before {
		if stop != nil {
			stop()
		}
		return nil
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

// addressTo addresses text to the person named, the way joinMention puts
// it, unless the text already starts by addressing them.
func addressTo(name, text string) string {
	if rest, ok := strings.CutPrefix(text, "@"+name); ok {
		if r, _ := utf8.DecodeRuneInString(rest); rest == "" || !(unicode.IsLetter(r) || unicode.IsDigit(r)) {
			return text
		}
	}
	return joinMention(name, text)
}

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
