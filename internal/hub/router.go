package hub

import (
	"context"
	"errors"
	"fmt"

	"github.com/J0EY0/veyloom/internal/store"
)

// routerStore is the slice of Store the router reads.
type routerStore interface {
	ListRoomMembers(ctx context.Context, roomID string) ([]store.Member, error)
	LastAgentMessageInThread(ctx context.Context, threadID string) (store.Message, error)
	TalkingMemberInThread(ctx context.Context, threadID, userID string) (string, error)
	RunningMembersInThread(ctx context.Context, threadID string) ([]string, error)
	RoomProject(ctx context.Context, roomID string) (store.Project, error)
}

// Router decides which members a person's message wakes (design.md 4.2).
// An agent's messages are not routed here: the turn manager wakes the
// members an agent names, within the limits of a piece of work (5.22).
//
// A message that names members wakes those of them that are enabled and in
// the project. One that names nobody goes to a single member, its
// Addressee, picked by the rules of addressee rather than by a model: in
// the room, where no one else plainly takes it, the leader does, to take on
// or hand on. A message that names someone but no member that takes turns
// wakes nobody, and so does one in a topic no member ever took part in, as
// people talking among themselves.
type Router struct {
	store routerStore
}

// NewRouter creates a Router over store.
func NewRouter(store routerStore) *Router {
	return &Router{store: store}
}

// AddresseeReason says why a person's message that names nobody goes where
// it goes.
type AddresseeReason string

const (
	// AddresseeNone: to no member.
	AddresseeNone AddresseeReason = "none"
	// AddresseeOnly: to the room's one member, the room being a
	// conversation with it.
	AddresseeOnly AddresseeReason = "only"
	// AddresseeLeader: to the leader, to take on or hand on.
	AddresseeLeader AddresseeReason = "leader"
	// AddresseeRunning: to the member whose turn runs in the topic.
	AddresseeRunning AddresseeReason = "running"
	// AddresseeTalking: to the member the person is talking with in the
	// topic, the one in their latest exchange there.
	AddresseeTalking AddresseeReason = "talking"
	// AddresseeLast: to the member that spoke last in the topic.
	AddresseeLast AddresseeReason = "last"
	// AddresseeLeaderFallback: to the leader, the topic's member being
	// gone or turned off.
	AddresseeLeaderFallback AddresseeReason = "leader_fallback"
)

// Addressee is the member a person's message that names nobody goes to,
// and why; Member is zero when Reason is AddresseeNone.
type Addressee struct {
	Member store.Member
	Reason AddresseeReason
}

// Route returns the members msg wakes, in a stable order with no duplicates.
func (r *Router) Route(ctx context.Context, msg store.Message) ([]store.Member, error) {
	if msg.SenderKind != store.SenderUser {
		return nil, nil
	}
	members, err := r.store.ListRoomMembers(ctx, msg.Room)
	if err != nil {
		return nil, fmt.Errorf("route message %s: %w", msg.ID, err)
	}

	var targets []store.Member
	seen := make(map[string]bool)
	for _, m := range msg.Mentions {
		if m.Kind != store.MentionAgent || seen[m.ID] {
			continue
		}
		if a, ok := available(members, m.ID); ok {
			targets = append(targets, a)
			seen[m.ID] = true
		}
	}
	if len(targets) > 0 {
		return targets, nil
	}
	if len(msg.Mentions) > 0 {
		// Addressed to someone, a person say, or a member turned off or no
		// longer there: theirs, and nobody else's, in the room or a topic.
		return nil, nil
	}
	to, err := r.addressee(ctx, msg.Room, msg.ThreadID, msg.UserID, members)
	if err != nil {
		return nil, fmt.Errorf("route message %s: %w", msg.ID, err)
	}
	if to.Reason == AddresseeNone {
		return nil, nil
	}
	return []store.Member{to.Member}, nil
}

// Addressee says where a message of userID's that names nobody would go,
// in the room or in its topic threadID: what the composer tells the person
// before they send it.
func (r *Router) Addressee(ctx context.Context, roomID, threadID, userID string) (Addressee, error) {
	members, err := r.store.ListRoomMembers(ctx, roomID)
	if err != nil {
		return Addressee{}, fmt.Errorf("addressee in room %s: %w", roomID, err)
	}
	return r.addressee(ctx, roomID, threadID, userID, members)
}

// addressee picks the member for a person's message that names nobody. In
// the room: its only member, else the leader. In a topic: the member whose
// turn runs there when there is just one, else the member the person is
// talking with, else the last to speak there; the leader when the member
// picked is gone or turned off. A topic no member took part in, people
// talking among themselves, goes to nobody: it is theirs.
func (r *Router) addressee(ctx context.Context, roomID, threadID, userID string, members []store.Member) (Addressee, error) {
	if threadID == "" {
		switch current := present(members); {
		case len(current) == 0:
			return Addressee{Reason: AddresseeNone}, nil
		case len(current) == 1:
			// Switched off, the only member quiets the room rather than
			// hand its messages to nobody in particular.
			if !current[0].Enabled {
				return Addressee{Reason: AddresseeNone}, nil
			}
			return Addressee{Member: current[0], Reason: AddresseeOnly}, nil
		}
		return r.leader(ctx, roomID, members, AddresseeLeader)
	}
	pick := func(memberID string, reason AddresseeReason) (Addressee, error) {
		if m, ok := available(members, memberID); ok {
			return Addressee{Member: m, Reason: reason}, nil
		}
		return r.leader(ctx, roomID, members, AddresseeLeaderFallback)
	}
	running, err := r.store.RunningMembersInThread(ctx, threadID)
	if err != nil {
		return Addressee{}, err
	}
	if len(running) == 1 {
		return pick(running[0], AddresseeRunning)
	}
	if userID != "" {
		talking, err := r.store.TalkingMemberInThread(ctx, threadID, userID)
		switch {
		case err == nil:
			return pick(talking, AddresseeTalking)
		case !errors.Is(err, store.ErrNotFound):
			return Addressee{}, err
		}
	}
	last, err := r.store.LastAgentMessageInThread(ctx, threadID)
	switch {
	case err == nil:
		return pick(last.MemberID, AddresseeLast)
	case !errors.Is(err, store.ErrNotFound):
		return Addressee{}, err
	}
	return Addressee{Reason: AddresseeNone}, nil
}

// leader is the project's leader as the addressee, for reason; no member
// when the leader is turned off, or there is none.
func (r *Router) leader(ctx context.Context, roomID string, members []store.Member, reason AddresseeReason) (Addressee, error) {
	project, err := r.store.RoomProject(ctx, roomID)
	if err != nil {
		return Addressee{}, fmt.Errorf("the project of room %s: %w", roomID, err)
	}
	if m, ok := available(members, project.LeaderID); ok {
		return Addressee{Member: m, Reason: reason}, nil
	}
	return Addressee{Reason: AddresseeNone}, nil
}

// available is the member id among members when it takes turns: enabled
// and still in the project.
func available(members []store.Member, id string) (store.Member, bool) {
	for _, m := range members {
		if m.ID == id && id != "" {
			return m, m.Enabled && !m.Removed()
		}
	}
	return store.Member{}, false
}

// present are the members still in the project, switched off or not.
func present(members []store.Member) []store.Member {
	var current []store.Member
	for _, m := range members {
		if !m.Removed() {
			current = append(current, m)
		}
	}
	return current
}
