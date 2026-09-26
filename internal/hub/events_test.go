package hub

import (
	"testing"
	"time"
)

func TestBroker_FansOutPerRoom(t *testing.T) {
	b := newBroker(4)
	a1 := b.subscribe("a")
	a2 := b.subscribe("a")
	other := b.subscribe("b")

	b.publish(Event{Kind: EventMessage, RoomID: "a"})

	for _, s := range []*subscription{a1, a2} {
		select {
		case ev := <-s.Events():
			if ev.Kind != EventMessage || ev.At.IsZero() {
				t.Errorf("unexpected event %+v", ev)
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber did not receive the event")
		}
	}
	select {
	case ev := <-other.Events():
		t.Errorf("room b received room a's event: %+v", ev)
	default:
	}
}

func TestBroker_CloseRemovesAndIsIdempotent(t *testing.T) {
	b := newBroker(4)
	s := b.subscribe("a")
	s.Close()
	s.Close()
	if _, ok := <-s.Events(); ok {
		t.Error("channel should be closed")
	}
	if b.subscribers("a") != 0 || s.Lagged() {
		t.Errorf("closed subscription should be gone and not lagged: subscribers=%d lagged=%v", b.subscribers("a"), s.Lagged())
	}
	// Publishing to a room with no subscribers is fine.
	b.publish(Event{Kind: EventMessage, RoomID: "a"})
}

func TestBroker_SlowSubscriberIsDroppedNotBlocked(t *testing.T) {
	b := newBroker(2)
	slow := b.subscribe("a")
	fast := b.subscribe("a")

	done := make(chan struct{})
	go func() {
		for i := 0; i < 5; i++ {
			b.publish(Event{Kind: EventMessage, RoomID: "a"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on a full subscriber")
	}

	// Both were unread, so both kept their first two events, were then
	// dropped and marked lagged. The point is that nothing blocked.
	for name, s := range map[string]*subscription{"slow": slow, "fast": fast} {
		got := 0
		for range s.Events() {
			got++
		}
		if got != 2 || !s.Lagged() {
			t.Errorf("%s: received %d, lagged %v", name, got, s.Lagged())
		}
	}
	if b.subscribers("a") != 0 {
		t.Errorf("dropped subscribers should be gone, %d left", b.subscribers("a"))
	}
	slow.Close() // harmless after being dropped
}

func TestBroker_WatchesEveryRoomForWhatItKeeps(t *testing.T) {
	b := newBroker(2)
	asks := b.subscribeWhere(func(ev Event) bool { return ev.Kind == EventApprovalRequested })
	b.publish(Event{Kind: EventMessage, RoomID: "a"})
	b.publish(Event{Kind: EventApprovalRequested, RoomID: "a"})
	b.publish(Event{Kind: EventApprovalRequested, RoomID: "b"})
	for _, room := range []string{"a", "b"} {
		select {
		case ev := <-asks.Events():
			if ev.Kind != EventApprovalRequested || ev.RoomID != room {
				t.Errorf("want room %s's request, got %+v", room, ev)
			}
		case <-time.After(time.Second):
			t.Fatalf("room %s's request did not arrive", room)
		}
	}
	select {
	case ev := <-asks.Events():
		t.Errorf("an event it does not keep arrived: %+v", ev)
	default:
	}
	if b.subscribers("a") != 0 {
		t.Error("watching every room is not subscribing to one")
	}

	// Falling behind drops it, as it does a room's subscriber.
	for range 3 {
		b.publish(Event{Kind: EventApprovalRequested, RoomID: "c"})
	}
	for range asks.Events() {
	}
	b.mu.Lock()
	left := len(b.everywhere)
	b.mu.Unlock()
	if !asks.Lagged() || left != 0 {
		t.Errorf("lagged %v, %d watching every room", asks.Lagged(), left)
	}
	asks.Close()
}
