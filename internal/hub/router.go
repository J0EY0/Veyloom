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
}

// Router decides which members a message wakes.
//
// Phase-one rules, chosen so that a human is always the one who starts a
// turn: only user messages wake agents; a message wakes the enabled members
// of its room that it @-mentions; a threaded message that mentions no one
// goes to the member that last spoke in that thread; a message to the room
// that mentions no one goes to the room's member when it has only one;
// anything else wakes nobody. An agent's mention of another agent is not
// routed here: the turn manager relays it inside the topic, within a budget
// (see relay).
type Router struct {
	store routerStore
}

// NewRouter creates a Router over store.
func NewRouter(store routerStore) *Router {
	return &Router{store: store}
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
	// Disabled members and those taken out of the project take no turns.
	enabled := make(map[string]store.Member, len(members))
	for _, a := range members {
		if a.Enabled && !a.Removed() {
			enabled[a.ID] = a
		}
	}

	var targets []store.Member
	seen := make(map[string]bool)
	for _, m := range msg.Mentions {
		if m.Kind != store.MentionAgent || seen[m.ID] {
			continue
		}
		if a, ok := enabled[m.ID]; ok {
			targets = append(targets, a)
			seen[m.ID] = true
		}
	}
	if len(targets) > 0 {
		return targets, nil
	}
	if msg.ThreadID == "" {
		// With one member the room is a conversation with it, and an @ on
		// every line would say nothing. A message that @s anyone at all is
		// addressed to them instead.
		if len(msg.Mentions) == 0 {
			return onlyMember(members), nil
		}
		return nil, nil
	}

	// In a thread, an unaddressed message continues the conversation with
	// whichever member answered last.
	last, err := r.store.LastAgentMessageInThread(ctx, msg.ThreadID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("route message %s: %w", msg.ID, err)
	}
	if a, ok := enabled[last.MemberID]; ok {
		return []store.Member{a}, nil
	}
	return nil, nil
}

// onlyMember is the room's one current member, when the room has exactly one
// and it takes turns. Members taken out of the project do not count; a
// switched-off one still does, so switching it off quiets the room rather
// than handing its messages to nobody in particular.
func onlyMember(members []store.Member) []store.Member {
	var current []store.Member
	for _, m := range members {
		if !m.Removed() {
			current = append(current, m)
		}
	}
	if len(current) != 1 || !current[0].Enabled {
		return nil
	}
	return current
}
