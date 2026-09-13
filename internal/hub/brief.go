package hub

import (
	"context"
	"fmt"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
)

// briefStore is the slice of Store the brief builder reads.
type briefStore interface {
	GetMessage(ctx context.Context, id string) (store.Message, error)
	ListThreadMessagesBefore(ctx context.Context, threadID string, before int64, limit int) ([]store.Message, error)
	GetUser(ctx context.Context, id string) (store.User, error)
	GetAgentInstance(ctx context.Context, id string) (store.AgentInstance, error)
}

// briefBuilder composes the prompt an agent receives for a turn.
//
// Phase one is deliberately plain: the conversation of the thread the agent
// replies in, oldest first, with the messages that triggered the turn
// marked. Memory, rolling summaries and "since your last turn" belong to
// phase two and slot in here without touching the turn manager.
type briefBuilder struct {
	store       briefStore
	maxMessages int
}

func newBriefBuilder(store briefStore, maxMessages int) *briefBuilder {
	return &briefBuilder{store: store, maxMessages: maxMessages}
}

// Build renders the brief for agent answering triggers in thread.
func (b *briefBuilder) Build(ctx context.Context, agent store.AgentInstance, thread store.Thread, triggers []store.Message) (string, error) {
	root, err := b.store.GetMessage(ctx, thread.RootMessageID)
	if err != nil {
		return "", fmt.Errorf("brief: %w", err)
	}
	replies, err := b.store.ListThreadMessagesBefore(ctx, thread.ID, 0, b.maxMessages)
	if err != nil {
		return "", fmt.Errorf("brief: %w", err)
	}

	addressed := make(map[string]bool, len(triggers))
	for _, t := range triggers {
		addressed[t.ID] = true
	}
	names := newNameResolver(b.store)

	var sb strings.Builder
	fmt.Fprintf(&sb, "You are %q, an agent in a team chat room. Lines marked with >> are addressed to you; reply to them.\n\n", agent.DisplayName)
	sb.WriteString("Conversation (oldest first):\n")
	writeLine := func(m store.Message) {
		marker := "   "
		if addressed[m.ID] {
			marker = ">> "
		}
		fmt.Fprintf(&sb, "%s[%s] %s\n", marker, names.of(ctx, m), m.Body)
	}
	writeLine(root)
	for _, m := range replies {
		writeLine(m)
	}
	return sb.String(), nil
}

// nameResolver turns message senders into display names, caching lookups
// for the duration of one brief.
type nameResolver struct {
	store briefStore
	cache map[string]string
}

func newNameResolver(store briefStore) *nameResolver {
	return &nameResolver{store: store, cache: make(map[string]string)}
}

func (n *nameResolver) of(ctx context.Context, m store.Message) string {
	switch m.SenderKind {
	case store.SenderUser:
		return n.lookup(m.UserID, func() (string, error) {
			u, err := n.store.GetUser(ctx, m.UserID)
			return u.Name, err
		})
	case store.SenderAgent:
		return n.lookup(m.AgentInstanceID, func() (string, error) {
			a, err := n.store.GetAgentInstance(ctx, m.AgentInstanceID)
			return a.DisplayName, err
		})
	default:
		return "system"
	}
}

func (n *nameResolver) lookup(id string, fetch func() (string, error)) string {
	if name, ok := n.cache[id]; ok {
		return name
	}
	name, err := fetch()
	if err != nil || name == "" {
		name = "unknown"
	}
	n.cache[id] = name
	return name
}
