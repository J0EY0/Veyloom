// Package hub is the central coordinator. It holds the authoritative view of
// everything shared between agents; at this stage that is the set of
// connected workers and the engines each of them offers.
package hub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/store"
)

// Config holds the hub's tunables. Zero fields are filled from
// DefaultConfig, so callers may set only what they care about.
type Config struct {
	// HeartbeatInterval is how often workers are asked to report in.
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
	// BriefMessages caps how many thread messages a turn's brief includes.
	BriefMessages int `mapstructure:"brief_messages"`
	// ApprovalTimeout is how long an approval request waits for a decision
	// before it is denied. Zero takes the default; a negative value waits
	// forever.
	ApprovalTimeout time.Duration `mapstructure:"approval_timeout"`
}

// DefaultConfig returns the defaults every Config is completed with.
func DefaultConfig() Config {
	return Config{
		HeartbeatInterval: 15 * time.Second,
		HandshakeTimeout:  10 * time.Second,
		StoreTimeout:      10 * time.Second,
		TranscriptDir:     "",
		BriefMessages:     40,
		ApprovalTimeout:   15 * time.Minute,
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
	if c.ApprovalTimeout == 0 {
		c.ApprovalTimeout = def.ApprovalTimeout
	}
	return c
}

// ErrUnknownWorker is returned when a worker ID is not connected.
var ErrUnknownWorker = errors.New("hub: unknown worker")

// ErrAlreadyConnected is returned when a second connection claims the
// identity of a worker that is already connected.
var ErrAlreadyConnected = errors.New("hub: worker already connected")

// ErrUnknownTurn is returned when a turn ID is not running.
var ErrUnknownTurn = errors.New("hub: unknown turn")

// WorkerStore persists worker registrations. The hub keeps live connections
// in memory; the store is the durable record that survives restarts and
// hands out worker IDs. *store.Store satisfies it.
type WorkerStore interface {
	// RegisterWorker records a connecting worker and returns its ID.
	// presentedID is what the worker sent in Hello: a known id reconnects
	// that worker, while an empty or unknown id registers a new one.
	RegisterWorker(ctx context.Context, presentedID, name string, engines []engine.Info) (id string, err error)
	// TouchWorker records that the worker was heard from.
	TouchWorker(ctx context.Context, id string) error
	// UpdateWorkerEngines replaces the worker's discovered engines.
	UpdateWorkerEngines(ctx context.Context, id string, engines []engine.Info) error
	// MarkWorkerDisconnected records that the worker's connection ended.
	MarkWorkerDisconnected(ctx context.Context, id string) error
}

// WorkerInfo is the hub's view of one connected worker.
type WorkerInfo struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Engines     []engine.Info `json:"engines"`
	ConnectedAt time.Time     `json:"connected_at"`
	LastSeen    time.Time     `json:"last_seen"`
}

// connectedWorker pairs a worker's info with the connection used to reach it.
type connectedWorker struct {
	info WorkerInfo
	conn protocol.Conn
}

// Hub is the central coordinator: it serves worker connections, accepts
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

	mu      sync.Mutex
	workers map[string]*connectedWorker
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

// New creates a Hub over st.
func New(st Store, cfg Config, opts ...Option) *Hub {
	h := &Hub{
		store:   st,
		cfg:     cfg.withDefaults(),
		now:     time.Now,
		logger:  slog.Default(),
		workers: make(map[string]*connectedWorker),
	}
	for _, opt := range opts {
		opt(h)
	}
	h.router = NewRouter(st)
	h.events = newBroker(subscriptionBuffer)
	h.turns = newTurnManager(st, newBriefBuilder(st, h.cfg.BriefMessages), h.connFor, h.events.publish, h.cfg.TranscriptDir, h.cfg.StoreTimeout, h.cfg.ApprovalTimeout, h.logger)
	return h
}

// Subscribe returns a live feed of everything that happens in a room:
// messages, turns and their events, approvals. The room is not checked to
// exist; an unknown room simply never produces events.
func (h *Hub) Subscribe(roomID string) Subscription {
	return h.events.subscribe(roomID)
}

// Serve handles one worker connection: it performs the handshake, registers
// the worker, then processes messages until the peer disconnects or ctx is
// cancelled. It blocks, so callers run it in a goroutine per connection. The
// worker is unregistered when Serve returns.
func (h *Hub) Serve(ctx context.Context, conn protocol.Conn) error {
	defer conn.Close()

	hello, err := awaitHello(ctx, conn, h.cfg.HandshakeTimeout)
	if err != nil {
		return err
	}

	id, err := h.store.RegisterWorker(ctx, hello.WorkerID, hello.Name, hello.Engines)
	if err != nil {
		return fmt.Errorf("register worker %q: %w", hello.Name, err)
	}
	w, err := h.track(id, hello, conn)
	if err != nil {
		return err
	}
	defer h.forget(w)

	welcome := protocol.Welcome{
		WorkerID:          id,
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

// track adds the worker to the live set. A worker ID can only be connected
// once; a second connection presenting the same identity is refused rather
// than silently replacing the first.
func (h *Hub) track(id string, hello protocol.Hello, conn protocol.Conn) (*connectedWorker, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.workers[id]; exists {
		return nil, fmt.Errorf("%w: %s (%q)", ErrAlreadyConnected, id, hello.Name)
	}

	now := h.now()
	w := &connectedWorker{
		info: WorkerInfo{
			ID:          id,
			Name:        hello.Name,
			Engines:     hello.Engines,
			ConnectedAt: now,
			LastSeen:    now,
		},
		conn: conn,
	}
	h.workers[id] = w
	return w, nil
}

// forget removes the worker from the live set, fails the turns it was
// running and records the disconnect.
func (h *Hub) forget(w *connectedWorker) {
	h.mu.Lock()
	delete(h.workers, w.info.ID)
	h.mu.Unlock()
	h.turns.WorkerGone(w.info.ID)

	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.StoreTimeout)
	defer cancel()
	// Nothing useful can be done about a failure here: the connection is
	// already gone and the next reconnect resets the row anyway.
	_ = h.store.MarkWorkerDisconnected(ctx, w.info.ID)
}

// handle applies one inbound message. Any message proves the worker is
// alive, so LastSeen is refreshed regardless of kind, but only heartbeats
// and engine reports touch the database: turn traffic can be frequent and
// is recorded by the turn manager in memory and in transcripts instead.
// Unexpected kinds are ignored rather than fatal, so a newer worker cannot
// take down an older hub.
func (h *Hub) handle(ctx context.Context, w *connectedWorker, m protocol.Message) error {
	var engines []engine.Info
	switch m := m.(type) {
	case protocol.Heartbeat:
		if err := h.store.TouchWorker(ctx, w.info.ID); err != nil {
			return fmt.Errorf("persist heartbeat from worker %s: %w", w.info.ID, err)
		}
	case protocol.EnginesReport:
		if err := h.store.UpdateWorkerEngines(ctx, w.info.ID, m.Engines); err != nil {
			return fmt.Errorf("persist engines report from worker %s: %w", w.info.ID, err)
		}
		engines = m.Engines
	case protocol.TurnEvent:
		h.turns.OnEvent(m.TurnID, m.Event)
	case protocol.TurnDone:
		h.turns.OnDone(m.TurnID, m)
	case protocol.ApprovalRequest:
		h.turns.OnApproval(w.conn, m)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	w.info.LastSeen = h.now()
	if engines != nil {
		w.info.Engines = engines
	}
	return nil
}

// connFor returns the connection of a connected worker.
func (h *Hub) connFor(workerID string) (protocol.Conn, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w, ok := h.workers[workerID]
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
	for _, agent := range targets {
		if err := h.turns.Trigger(ctx, agent, msg); err != nil {
			h.logger.Error("trigger agent", "agent", agent.ID, "message", msg.ID, "err", err)
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
// forwards it to the waiting turn. An unknown approval is
// store.ErrNotFound; one already decided is store.ErrConflict.
func (h *Hub) DecideApproval(ctx context.Context, approvalID, userID string, d engine.Decision) (store.Approval, error) {
	return h.turns.Decide(ctx, approvalID, userID, d)
}

// Workers returns a snapshot of every connected worker, ordered by ID.
func (h *Hub) Workers() []WorkerInfo {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make([]WorkerInfo, 0, len(h.workers))
	for _, w := range h.workers {
		info := w.info
		info.Engines = append([]engine.Info(nil), w.info.Engines...)
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Probe asks a worker to re-run engine discovery. The refreshed engines show
// up in Workers once the worker answers.
func (h *Hub) Probe(ctx context.Context, workerID string) error {
	h.mu.Lock()
	w, ok := h.workers[workerID]
	h.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownWorker, workerID)
	}
	return w.conn.Send(ctx, protocol.Probe{})
}
