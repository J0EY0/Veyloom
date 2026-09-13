package hub

import (
	"context"
	"errors"
	"fmt"

	"github.com/J0EY0/veyloom/internal/store"
)

// routerStore is the slice of Store the router reads.
type routerStore interface {
	ListRoomAgentInstances(ctx context.Context, roomID string) ([]store.AgentInstance, error)
	LastAgentMessageInThread(ctx context.Context, threadID string) (store.Message, error)
}

// Router decides which agent instances a message wakes.
//
// Phase-one rules, chosen so that a human is always the one who starts a
// turn: only user messages wake agents; a message wakes the enabled agents
// of its room that it @-mentions; a threaded message that mentions no agent
// goes to the agent that last spoke in that thread; anything else wakes
// nobody. Agent-to-agent mentions are shown but never acted on.
type Router struct {
	store routerStore
}

// NewRouter creates a Router over store.
func NewRouter(store routerStore) *Router {
	return &Router{store: store}
}

// Route returns the agents msg wakes, in a stable order with no duplicates.
func (r *Router) Route(ctx context.Context, msg store.Message) ([]store.AgentInstance, error) {
	if msg.SenderKind != store.SenderUser {
		return nil, nil
	}

	agents, err := r.store.ListRoomAgentInstances(ctx, msg.Room)
	if err != nil {
		return nil, fmt.Errorf("route message %s: %w", msg.ID, err)
	}
	enabled := make(map[string]store.AgentInstance, len(agents))
	for _, a := range agents {
		if a.Enabled {
			enabled[a.ID] = a
		}
	}

	var targets []store.AgentInstance
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
	if len(targets) > 0 || msg.ThreadID == "" {
		return targets, nil
	}

	// In a thread, an unaddressed message continues the conversation with
	// whichever agent answered last.
	last, err := r.store.LastAgentMessageInThread(ctx, msg.ThreadID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("route message %s: %w", msg.ID, err)
	}
	if a, ok := enabled[last.AgentInstanceID]; ok {
		return []store.AgentInstance{a}, nil
	}
	return nil, nil
}
