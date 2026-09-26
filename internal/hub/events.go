package hub

import (
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// EventKind classifies what happened in a room.
type EventKind string

const (
	// EventMessage carries a message just posted, by anyone.
	EventMessage EventKind = "message"
	// EventTurnStarted carries a turn that was just dispatched.
	EventTurnStarted EventKind = "turn_started"
	// EventTurnEvent carries one runtime event of a running turn.
	EventTurnEvent EventKind = "turn_event"
	// EventTurnFinished carries a turn that reached a final status.
	EventTurnFinished EventKind = "turn_finished"
	// EventApprovalRequested carries a new pending approval.
	EventApprovalRequested EventKind = "approval_requested"
	// EventApprovalDecided carries an approval that left pending.
	EventApprovalDecided EventKind = "approval_decided"
	// EventTurnTrust carries a running turn a person let the rest of its
	// requests through, or took that back (docs/design.md 4.6).
	EventTurnTrust EventKind = "turn_trust"
	// EventWikiChanged says the project wiki changed: pages written,
	// confirmed or undone, by an agent, a person or an editor. It names
	// the project, not the pages; whoever shows the wiki reads it again.
	EventWikiChanged EventKind = "wiki_changed"
	// EventInboxRead says a person read some of their inbox, here or in
	// another tab: whoever shows it counts again. It names the person, and
	// goes to their inbox streams only.
	EventInboxRead EventKind = "inbox_read"
)

// Event is one thing that happened in a room, as pushed to live
// subscribers. The payload field matching Kind is set; the rest are empty.
type Event struct {
	Kind   EventKind `json:"kind"`
	RoomID string    `json:"room_id"`
	At     time.Time `json:"at"`

	Message *store.Message `json:"message,omitempty"`
	// Thread accompanies the message event of a topic root: the thread
	// the root heads, so replies and turns can be filed under it.
	Thread *store.ThreadSummary `json:"thread,omitempty"`
	Turn   *store.Turn          `json:"turn,omitempty"`
	// Work accompanies turn_started and turn_finished: the piece of work
	// the turn is part of, as it stands now.
	Work      *store.WorkSummary `json:"work,omitempty"`
	TurnID    string             `json:"turn_id,omitempty"`
	TurnEvent *runtime.Event     `json:"turn_event,omitempty"`
	Approval  *store.Approval    `json:"approval,omitempty"`
	// ProjectID accompanies wiki_changed, naming the project whose wiki
	// changed; Scope is library instead when it was the skill library.
	ProjectID string `json:"project_id,omitempty"`
	Scope     string `json:"scope,omitempty"`
	// UserID accompanies inbox_read, naming the person.
	UserID string `json:"user_id,omitempty"`
}

// topicSummary is what a message event says of the topic its message
// heads, as it opens: which one, and what it is called, #12, before
// anything was said in it.
func topicSummary(thread store.Thread) *store.ThreadSummary {
	return &store.ThreadSummary{ID: thread.ID, Number: thread.Number}
}

// wikiChangedEvent is the live event for a change to a project's wiki, or
// with projectID empty to the skill library.
func wikiChangedEvent(projectID, roomID string) Event {
	ev := Event{Kind: EventWikiChanged, RoomID: roomID, At: time.Now(), ProjectID: projectID, Scope: string(store.WikiProject)}
	if projectID == "" {
		ev.Scope = string(store.WikiLibrary)
	}
	return ev
}

// Subscription delivers a room's events to one consumer.
//
// Events is closed when the consumer calls Close, or when the consumer fell
// so far behind that the buffer overflowed; Lagged tells the two apart. A
// lagged consumer has missed events and should resync over the REST API
// before subscribing again.
type Subscription interface {
	Events() <-chan Event
	Close()
	Lagged() bool
}

// subscriptionBuffer is how many events a subscriber may fall behind
// before it is dropped. Publishing never blocks: the hub's connection
// loops call it, and a slow WebSocket client must not stall a machine.
const subscriptionBuffer = 256

// broker fans events out to per-room subscribers, and to those that watch
// every room for the events they keep.
type broker struct {
	buffer int

	mu         sync.Mutex
	rooms      map[string]map[*subscription]struct{}
	everywhere map[*subscription]struct{}
}

func newBroker(buffer int) *broker {
	return &broker{buffer: buffer, rooms: make(map[string]map[*subscription]struct{}), everywhere: make(map[*subscription]struct{})}
}

type subscription struct {
	broker *broker
	roomID string
	// keep picks the events of every room a subscription watching them all
	// gets; nil for a room's own subscribers. It runs under broker.mu, so
	// it must be quick.
	keep func(Event) bool
	ch   chan Event
	// closed and lagged are guarded by broker.mu.
	closed bool
	lagged bool
}

// subscribe registers a consumer for roomID.
func (b *broker) subscribe(roomID string) *subscription {
	s := &subscription{broker: b, roomID: roomID, ch: make(chan Event, b.buffer)}
	b.mu.Lock()
	defer b.mu.Unlock()
	subs, ok := b.rooms[roomID]
	if !ok {
		subs = make(map[*subscription]struct{})
		b.rooms[roomID] = subs
	}
	subs[s] = struct{}{}
	return s
}

// subscribeWhere registers a consumer for the events of every room that
// keep picks.
func (b *broker) subscribeWhere(keep func(Event) bool) *subscription {
	s := &subscription{broker: b, keep: keep, ch: make(chan Event, b.buffer)}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.everywhere[s] = struct{}{}
	return s
}

// publish delivers ev to every subscriber of its room, and to those
// watching every room that keep it, without blocking. A subscriber whose
// buffer is full is dropped and marked lagged.
func (b *broker) publish(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.rooms[ev.RoomID] {
		b.deliverLocked(s, ev)
	}
	for s := range b.everywhere {
		if s.keep(ev) {
			b.deliverLocked(s, ev)
		}
	}
}

// deliverLocked hands ev to s, or drops s if it fell too far behind.
// Callers hold b.mu.
func (b *broker) deliverLocked(s *subscription, ev Event) {
	select {
	case s.ch <- ev:
	default:
		s.lagged = true
		b.removeLocked(s)
	}
}

// removeLocked unregisters s and closes its channel. Callers hold b.mu.
func (b *broker) removeLocked(s *subscription) {
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
	if s.keep != nil {
		delete(b.everywhere, s)
		return
	}
	subs := b.rooms[s.roomID]
	delete(subs, s)
	if len(subs) == 0 {
		delete(b.rooms, s.roomID)
	}
}

// subscribers reports how many consumers a room has, for tests.
func (b *broker) subscribers(roomID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.rooms[roomID])
}

// Events implements Subscription.
func (s *subscription) Events() <-chan Event { return s.ch }

// Close implements Subscription. It is safe to call more than once and
// after the broker dropped the subscription.
func (s *subscription) Close() {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()
	s.broker.removeLocked(s)
}

// Lagged implements Subscription.
func (s *subscription) Lagged() bool {
	s.broker.mu.Lock()
	defer s.broker.mu.Unlock()
	return s.lagged
}
