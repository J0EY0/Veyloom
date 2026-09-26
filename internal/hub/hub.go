// Package hub is the central coordinator. It holds the authoritative view of
// everything shared between agents; at this stage that is the set of
// connected machines and the runtimes each of them offers.
package hub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Config holds the hub's tunables. Zero fields are filled from
// DefaultConfig, so callers may set only what they care about.
type Config struct {
	// HeartbeatInterval is how often machines are asked to report in.
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
	// HandshakeTimeout bounds how long a new connection may take to send
	// Hello.
	HandshakeTimeout time.Duration `mapstructure:"handshake_timeout"`
	// StoreTimeout bounds each database operation the hub performs outside
	// a request context: recording disconnects, finishing turns, posting
	// replies.
	StoreTimeout time.Duration `mapstructure:"store_timeout"`
	// TranscriptDir is where turn transcripts are written, one JSONL file
	// per turn. Empty means a "turns" directory under the state dir.
	TranscriptDir string `mapstructure:"transcript_dir"`
	// AttachmentDir is where uploaded attachments are stored; the brief
	// names files by their path under it. Empty means an "attachments"
	// directory under the state dir.
	AttachmentDir string `mapstructure:"attachment_dir"`
	// AvatarDir is where the pictures uploaded for agents are stored. Empty
	// means an "avatars" directory under the state dir.
	AvatarDir string `mapstructure:"avatar_dir"`
	// WikiDir is where the wikis are kept: each project's under projects/
	// (docs/design.md 5.9). Empty means a "wiki" directory under the state
	// dir; a hub given none keeps no wikis.
	WikiDir string `mapstructure:"wiki_dir"`
	// BriefMessages caps how many messages of the topic a turn is in its
	// brief includes. A brief tells a session only what it has not read, so
	// the caps below matter for a new session and after a long absence;
	// what a cap leaves out is counted in the brief.
	BriefMessages int `mapstructure:"brief_messages"`
	// BriefRoomMessages caps the new top-level messages of the room in a
	// brief.
	BriefRoomMessages int `mapstructure:"brief_room_messages"`
	// BriefTopics caps the other topics a brief lists as having news.
	BriefTopics int `mapstructure:"brief_topics"`
	// BriefWikiPages caps the pages of the project wiki's catalog a brief
	// lists: all of them to a new session, the ones changed since to one
	// that has seen them.
	BriefWikiPages int `mapstructure:"brief_wiki_pages"`
	// BriefResidentChars caps, in characters, the resident pages of the
	// project wiki a brief carries in full; the ones that do not fit are
	// named instead.
	BriefResidentChars int `mapstructure:"brief_resident_chars"`
	// MemoryPersonalChars and MemoryProjectChars cap, in characters, the
	// personal memory and each project's (design.md 5.16): every turn
	// carries both whole, so a change that would take one past its cap is
	// refused.
	MemoryPersonalChars int `mapstructure:"memory_personal_chars"`
	MemoryProjectChars  int `mapstructure:"memory_project_chars"`
	// ApprovalTimeout is how long an approval request waits for a decision
	// before it is denied. Zero takes the default; a negative value waits
	// forever.
	ApprovalTimeout time.Duration `mapstructure:"approval_timeout"`
	// UpkeepIdle is how long a topic stays quiet before the wiki maintainer
	// of a project that runs it on quiet topics goes over it (docs/design.md
	// 5.12); also the least time between two such upkeeps.
	UpkeepIdle time.Duration `mapstructure:"upkeep_idle"`
	// UpkeepCheck is how often the hub looks for wikis due for upkeep.
	UpkeepCheck time.Duration `mapstructure:"upkeep_check"`
	// UpkeepTurns caps the turns of its own project one upkeep goes over;
	// the rest wait for the next. Half as many of other projects' turns
	// that used the team's skills go along.
	UpkeepTurns int `mapstructure:"upkeep_turns"`
	// UpkeepRunsPerDay caps the upkeeps of one wiki in a day. An upkeep that
	// went over as many turns as it may leaves the rest to the next, which
	// then starts at once rather than a day or a week later; this is how
	// many may run so, one after another (docs/design.md 5.16).
	UpkeepRunsPerDay int `mapstructure:"upkeep_runs_per_day"`
	// UpkeepOfferTopics is how many quiet topics wait to be gone over before
	// a project without a wiki maintainer is offered one in its chat
	// (docs/design.md 5.16). Zero takes the default; a negative value turns
	// the offer off.
	UpkeepOfferTopics int `mapstructure:"upkeep_offer_topics"`
	// SkillTrialUses is how many turns must use a skill an agent changed,
	// and end well, before the change is kept (design.md 5.15).
	SkillTrialUses int `mapstructure:"skill_trial_uses"`
}

// DefaultConfig returns the defaults every Config is completed with.
func DefaultConfig() Config {
	return Config{
		HeartbeatInterval:   15 * time.Second,
		HandshakeTimeout:    10 * time.Second,
		StoreTimeout:        10 * time.Second,
		TranscriptDir:       "",
		BriefMessages:       40,
		BriefRoomMessages:   30,
		BriefTopics:         10,
		BriefWikiPages:      30,
		BriefResidentChars:  4000,
		MemoryPersonalChars: 2000,
		MemoryProjectChars:  3000,
		ApprovalTimeout:     15 * time.Minute,
		UpkeepIdle:          30 * time.Minute,
		UpkeepCheck:         time.Minute,
		UpkeepTurns:         20,
		UpkeepRunsPerDay:    6,
		UpkeepOfferTopics:   3,
		SkillTrialUses:      3,
	}
}

// withDefaults returns c with zero fields replaced by DefaultConfig values.
func (c Config) withDefaults() Config {
	def := DefaultConfig()
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = def.HeartbeatInterval
	}
	if c.HandshakeTimeout <= 0 {
		c.HandshakeTimeout = def.HandshakeTimeout
	}
	if c.StoreTimeout <= 0 {
		c.StoreTimeout = def.StoreTimeout
	}
	if c.BriefMessages <= 0 {
		c.BriefMessages = def.BriefMessages
	}
	if c.BriefRoomMessages <= 0 {
		c.BriefRoomMessages = def.BriefRoomMessages
	}
	if c.BriefTopics <= 0 {
		c.BriefTopics = def.BriefTopics
	}
	if c.BriefWikiPages <= 0 {
		c.BriefWikiPages = def.BriefWikiPages
	}
	if c.BriefResidentChars <= 0 {
		c.BriefResidentChars = def.BriefResidentChars
	}
	if c.MemoryPersonalChars <= 0 {
		c.MemoryPersonalChars = def.MemoryPersonalChars
	}
	if c.MemoryProjectChars <= 0 {
		c.MemoryProjectChars = def.MemoryProjectChars
	}
	if c.ApprovalTimeout == 0 {
		c.ApprovalTimeout = def.ApprovalTimeout
	}
	if c.UpkeepIdle <= 0 {
		c.UpkeepIdle = def.UpkeepIdle
	}
	if c.UpkeepCheck <= 0 {
		c.UpkeepCheck = def.UpkeepCheck
	}
	if c.UpkeepTurns <= 0 {
		c.UpkeepTurns = def.UpkeepTurns
	}
	if c.UpkeepRunsPerDay <= 0 {
		c.UpkeepRunsPerDay = def.UpkeepRunsPerDay
	}
	if c.UpkeepOfferTopics == 0 {
		c.UpkeepOfferTopics = def.UpkeepOfferTopics
	}
	if c.SkillTrialUses <= 0 {
		c.SkillTrialUses = def.SkillTrialUses
	}
	return c
}

// ErrUnknownMachine is returned when a machine ID is not connected.
var ErrUnknownMachine = errors.New("hub: unknown machine")

// ErrAlreadyConnected is returned when a second connection claims the
// identity of a machine that is already connected.
var ErrAlreadyConnected = errors.New("hub: machine already connected")

// ErrUnknownTurn is returned when a turn ID is not running.
var ErrUnknownTurn = errors.New("hub: unknown turn")

// MachineStore persists machine registrations. The hub keeps live connections
// in memory; the store is the durable record that survives restarts and
// hands out machine IDs. *store.Store satisfies it.
type MachineStore interface {
	// RegisterMachine records a connecting machine and returns its ID.
	// presentedID is what the machine sent in Hello: a known id reconnects
	// that machine, while an empty or unknown id registers a new one.
	RegisterMachine(ctx context.Context, presentedID, name string, runtimes []runtime.Info) (id string, err error)
	// TouchMachine records that the machine was heard from.
	TouchMachine(ctx context.Context, id string) error
	// UpdateMachineRuntimes replaces the machine's discovered runtimes.
	UpdateMachineRuntimes(ctx context.Context, id string, runtimes []runtime.Info) error
	// MarkMachineDisconnected records that the machine's connection ended.
	MarkMachineDisconnected(ctx context.Context, id string) error
}

// MachineInfo is the hub's view of one connected machine.
type MachineInfo struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Runtimes    []runtime.Info `json:"runtimes"`
	ConnectedAt time.Time      `json:"connected_at"`
	LastSeen    time.Time      `json:"last_seen"`
	// ProbedAt is when Runtimes was last discovered: at connect, since Hello
	// carries them, and at every runtimes report after that. Whoever asked
	// for a probe knows it is answered once this moves.
	ProbedAt time.Time `json:"probed_at"`
}

// connectedMachine pairs a machine's info with the connection used to reach it.
type connectedMachine struct {
	info MachineInfo
	conn protocol.Conn
}

// Hub is the central coordinator: it serves machine connections, accepts
// messages, decides which agents they wake and runs their turns. It is
// safe for concurrent use.
type Hub struct {
	store  Store
	cfg    Config
	now    func() time.Time
	logger *slog.Logger
	router *Router
	turns  *TurnManager
	events *broker
	wikis  *wikiShelf
	// person names the account, for the wikis' history.
	person func() string
	// memoryPrefs are the account's memory switches (design.md 5.19).
	memoryPrefs func() store.MemoryPrefs

	mu       sync.Mutex
	machines map[string]*connectedMachine
	// calls are the requests on checkouts and worktrees waiting for their
	// machine's answer (docs/design.md 5.21).
	calls workspaceCalls
}

// Option customises a Hub.
type Option func(*Hub)

// WithClock replaces the time source, which lets tests assert on timestamps.
func WithClock(now func() time.Time) Option {
	return func(h *Hub) { h.now = now }
}

// WithLogger sets where the hub reports problems; the default is
// slog.Default().
func WithLogger(logger *slog.Logger) Option {
	return func(h *Hub) { h.logger = logger }
}

// WithPerson tells the hub the name of the one account, which the wikis
// commit edits made outside Veyloom under.
func WithPerson(name func() string) Option {
	return func(h *Hub) { h.person = name }
}

// WithMemoryPrefs tells the hub where the account's memory switches are
// read (design.md 5.19); without it every memory is on.
func WithMemoryPrefs(prefs func() store.MemoryPrefs) Option {
	return func(h *Hub) { h.memoryPrefs = prefs }
}

// New creates a Hub over st.
func New(st Store, cfg Config, opts ...Option) *Hub {
	h := &Hub{
		store:    st,
		cfg:      cfg.withDefaults(),
		now:      time.Now,
		logger:   slog.Default(),
		machines: make(map[string]*connectedMachine),
	}
	for _, opt := range opts {
		opt(h)
	}
	h.router = NewRouter(st)
	h.events = newBroker(subscriptionBuffer)
	h.wikis = newWikiShelf(h.cfg.WikiDir, h.person, h.logger)
	h.wikis.now = h.now
	if h.memoryPrefs != nil {
		h.wikis.prefs = h.memoryPrefs
	}
	h.wikis.budgets = memoryBudgets{Personal: h.cfg.MemoryPersonalChars, Project: h.cfg.MemoryProjectChars}
	h.wikis.changed = func(projectID, roomID string) { h.events.publish(wikiChangedEvent(projectID, roomID)) }
	limits := briefLimits{
		Thread: h.cfg.BriefMessages, Room: h.cfg.BriefRoomMessages, Topics: h.cfg.BriefTopics,
		WikiPages: h.cfg.BriefWikiPages, Resident: h.cfg.BriefResidentChars,
	}
	brief := newBriefBuilder(st, limits, h.cfg.AttachmentDir)
	brief.trialUses = h.cfg.SkillTrialUses
	if h.cfg.WikiDir != "" {
		brief.wikis = h.wikis
	}
	h.turns = newTurnManager(st, brief, h.connFor, h.events.publish, h.cfg.TranscriptDir, h.cfg.StoreTimeout, h.cfg.ApprovalTimeout, h.logger)
	h.turns.workspace = h.workspace
	h.turns.wikis = h.wikis
	h.turns.upkeepTurns, h.turns.residentBudget = h.cfg.UpkeepTurns, h.cfg.BriefResidentChars
	h.turns.attachmentDir = h.cfg.AttachmentDir
	h.turns.trialUses = h.cfg.SkillTrialUses
	// A person changing a skill on trial by hand keeps their version.
	h.wikis.edited = func(ctx context.Context, projectID string, pages []string) {
		if person := h.wikis.human(); projectID == "" && person != "" {
			h.turns.skillsEditedOutside(ctx, pages, person)
		}
	}
	return h
}

// Subscribe returns a live feed of everything that happens in a room:
// messages, turns and their events, approvals. The room is not checked to
// exist; an unknown room simply never produces events.
func (h *Hub) Subscribe(roomID string) Subscription {
	return h.events.subscribe(roomID)
}

// SubscribeInbox delivers what reaches a person's inbox as it happens, in
// every project: the messages that mention them, the requests waiting for
// a person as they are asked and decided (docs/webui.md 5.2), and what
// they read of it.
func (h *Hub) SubscribeInbox(userID string) Subscription {
	mention := store.Mention{Kind: store.MentionUser, ID: userID}
	return h.events.subscribeWhere(func(ev Event) bool {
		switch ev.Kind {
		case EventMessage:
			return ev.Message != nil && slices.Contains(ev.Message.Mentions, mention)
		case EventApprovalRequested, EventApprovalDecided:
			return true
		case EventInboxRead:
			return ev.UserID == userID
		}
		return false
	})
}

// MarkInboxRead marks read what read picks of the messages that mention a
// person (docs/webui.md 4.19), and tells their inbox streams when that
// changed anything, so every tab counts again. It says how many were not
// read before.
func (h *Hub) MarkInboxRead(ctx context.Context, userID string, read store.InboxRead) (int, error) {
	n, err := h.store.MarkMentionsRead(ctx, userID, read)
	if err != nil || n == 0 {
		return n, err
	}
	h.events.publish(Event{Kind: EventInboxRead, At: time.Now(), UserID: userID})
	return n, nil
}

// Serve handles one machine connection: it performs the handshake, registers
// the machine, then processes messages until the peer disconnects or ctx is
// cancelled. It blocks, so callers run it in a goroutine per connection. The
// machine is unregistered when Serve returns.
func (h *Hub) Serve(ctx context.Context, conn protocol.Conn) error {
	defer conn.Close()

	hello, err := awaitHello(ctx, conn, h.cfg.HandshakeTimeout)
	if err != nil {
		return err
	}

	id, err := h.store.RegisterMachine(ctx, hello.MachineID, hello.Name, hello.Runtimes)
	if err != nil {
		return fmt.Errorf("register machine %q: %w", hello.Name, err)
	}
	w, err := h.track(id, hello, conn)
	if err != nil {
		return err
	}
	defer h.forget(w)

	welcome := protocol.Welcome{
		MachineID:         id,
		HeartbeatInterval: protocol.Duration(h.cfg.HeartbeatInterval),
	}
	if err := conn.Send(ctx, welcome); err != nil {
		return fmt.Errorf("send welcome: %w", err)
	}

	for {
		m, err := conn.Recv(ctx)
		if err != nil {
			// A closed connection or a cancelled context is a normal end
			// of service, not a failure.
			if errors.Is(err, protocol.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := h.handle(ctx, w, m); err != nil {
			return err
		}
	}
}

// awaitHello reads the first message and insists that it is a Hello.
func awaitHello(ctx context.Context, conn protocol.Conn, timeout time.Duration) (protocol.Hello, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	m, err := conn.Recv(ctx)
	if err != nil {
		return protocol.Hello{}, fmt.Errorf("await hello: %w", err)
	}
	hello, ok := m.(protocol.Hello)
	if !ok {
		return protocol.Hello{}, fmt.Errorf("await hello: got %s instead", m.Kind())
	}
	return hello, nil
}

// track adds the machine to the live set. A machine ID can only be connected
// once; a second connection presenting the same identity is refused rather
// than silently replacing the first.
func (h *Hub) track(id string, hello protocol.Hello, conn protocol.Conn) (*connectedMachine, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.machines[id]; exists {
		return nil, fmt.Errorf("%w: %s (%q)", ErrAlreadyConnected, id, hello.Name)
	}

	now := h.now()
	w := &connectedMachine{
		info: MachineInfo{
			ID:          id,
			Name:        hello.Name,
			Runtimes:    hello.Runtimes,
			ConnectedAt: now,
			LastSeen:    now,
			ProbedAt:    now,
		},
		conn: conn,
	}
	h.machines[id] = w
	return w, nil
}

// forget removes the machine from the live set, fails the turns it was
// running and records the disconnect.
func (h *Hub) forget(w *connectedMachine) {
	h.mu.Lock()
	delete(h.machines, w.info.ID)
	h.mu.Unlock()
	h.turns.MachineGone(w.info.ID)
	h.workspaceMachineGone(w.info.ID)

	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.StoreTimeout)
	defer cancel()
	// Nothing useful can be done about a failure here: the connection is
	// already gone and the next reconnect resets the row anyway.
	_ = h.store.MarkMachineDisconnected(ctx, w.info.ID)
}

// handle applies one inbound message. Any message proves the machine is
// alive, so LastSeen is refreshed regardless of kind, but only heartbeats
// and runtime reports touch the database: turn traffic can be frequent and
// is recorded by the turn manager in memory and in transcripts instead.
// Unexpected kinds are ignored rather than fatal, so a newer machine cannot
// take down an older hub.
func (h *Hub) handle(ctx context.Context, w *connectedMachine, m protocol.Message) error {
	var runtimes []runtime.Info
	switch m := m.(type) {
	case protocol.Heartbeat:
		if err := h.store.TouchMachine(ctx, w.info.ID); err != nil {
			return fmt.Errorf("persist heartbeat from machine %s: %w", w.info.ID, err)
		}
	case protocol.RuntimesReport:
		if err := h.store.UpdateMachineRuntimes(ctx, w.info.ID, m.Runtimes); err != nil {
			return fmt.Errorf("persist runtimes report from machine %s: %w", w.info.ID, err)
		}
		runtimes = m.Runtimes
	case protocol.TurnEvent:
		h.turns.OnEvent(m.TurnID, m.Event)
	case protocol.TurnDone:
		h.turns.OnDone(m.TurnID, m)
	case protocol.ApprovalRequest:
		h.turns.OnApproval(w.conn, m)
	case protocol.RoomQuery:
		h.turns.OnRoomQuery(w.conn, m)
	case protocol.WorkspaceResult:
		h.onWorkspaceResult(m)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	w.info.LastSeen = h.now()
	if runtimes != nil {
		w.info.Runtimes = runtimes
		w.info.ProbedAt = w.info.LastSeen
	}
	return nil
}

// connFor returns the connection of a connected machine.
func (h *Hub) connFor(machineID string) (protocol.Conn, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w, ok := h.machines[machineID]
	if !ok {
		return nil, false
	}
	return w.conn, true
}

// PostUserMessage stores a message from a human and wakes the agents it is
// addressed to. It is the single entry point for user messages so that
// routing can never be bypassed. The message is returned as soon as it is
// stored; agent turns proceed in the background.
func (h *Hub) PostUserMessage(ctx context.Context, in store.NewMessage) (store.Message, error) {
	in.SenderKind = store.SenderUser
	msg, err := h.store.CreateMessage(ctx, in)
	if err != nil {
		return store.Message{}, err
	}
	h.events.publish(messageEvent(msg))
	// Dispatch outlives the request: a client that disconnects right after
	// posting must not cancel the turn it asked for.
	go h.dispatch(context.WithoutCancel(ctx), msg)
	return msg, nil
}

// dispatch routes a stored message to the agents it wakes.
func (h *Hub) dispatch(ctx context.Context, msg store.Message) {
	ctx, cancel := context.WithTimeout(ctx, h.cfg.StoreTimeout)
	defer cancel()

	targets, err := h.router.Route(ctx, msg)
	if err != nil {
		h.logger.Error("route message", "message", msg.ID, "err", err)
		return
	}
	// Several agents asked at once share one topic, rooted at the ask
	// itself, so each sees what the others say (docs/webui.md §4.1).
	if len(targets) > 1 && msg.ThreadID == "" {
		thread, err := h.store.ThreadForMessage(ctx, msg.ID)
		if err != nil {
			h.logger.Error("open topic for message", "message", msg.ID, "err", err)
			return
		}
		h.events.publish(Event{Kind: EventMessage, RoomID: msg.Room, At: msg.CreatedAt, Message: &msg, Thread: topicSummary(thread)})
		for _, member := range targets {
			if err := h.turns.TriggerIn(ctx, member, msg, thread); err != nil {
				h.logger.Error("trigger member", "member", member.ID, "message", msg.ID, "err", err)
			}
		}
		return
	}
	for _, member := range targets {
		if err := h.turns.Trigger(ctx, member, msg); err != nil {
			h.logger.Error("trigger member", "member", member.ID, "message", msg.ID, "err", err)
		}
	}
}

// CancelTurn stops a running turn. ErrUnknownTurn means it is not running.
func (h *Hub) CancelTurn(ctx context.Context, turnID string) error {
	return h.turns.Cancel(ctx, turnID)
}

// TurnRunning reports whether a turn is in flight.
func (h *Hub) TurnRunning(turnID string) bool {
	return h.turns.Running(turnID)
}

// DecideApproval applies userID's decision to a pending approval and
// forwards it to the waiting turn; scope is how far an allow goes. An
// unknown approval is store.ErrNotFound; one already decided is
// store.ErrConflict; a scope the request does not offer is
// store.ErrInvalidInput.
func (h *Hub) DecideApproval(ctx context.Context, approvalID, userID string, d runtime.Decision, scope store.AllowScope) (store.Approval, error) {
	return h.turns.Decide(ctx, approvalID, userID, d, scope)
}

// UntrustTurn takes back a person's letting the rest of a running turn's
// requests through. A turn not running is ErrUnknownTurn.
func (h *Hub) UntrustTurn(ctx context.Context, turnID string) (store.Turn, error) {
	return h.turns.UntrustTurn(ctx, turnID)
}

// Machines returns a snapshot of every connected machine, ordered by ID.
func (h *Hub) Machines() []MachineInfo {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]MachineInfo, 0, len(h.machines))
	for _, w := range h.machines {
		info := w.info
		info.Runtimes = append([]runtime.Info(nil), w.info.Runtimes...)
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Probe asks a machine to re-run runtime discovery. The refreshed runtimes show
// up in Machines once the machine answers.
func (h *Hub) Probe(ctx context.Context, machineID string) error {
	h.mu.Lock()
	w, ok := h.machines[machineID]
	h.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownMachine, machineID)
	}
	return w.conn.Send(ctx, protocol.Probe{})
}
