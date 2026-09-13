package hub

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

type fakeRouterStore struct {
	agents map[string][]store.AgentInstance // by room
	last   map[string]store.Message         // by thread
}

func (f fakeRouterStore) ListRoomAgentInstances(_ context.Context, roomID string) ([]store.AgentInstance, error) {
	return f.agents[roomID], nil
}

func (f fakeRouterStore) LastAgentMessageInThread(_ context.Context, threadID string) (store.Message, error) {
	m, ok := f.last[threadID]
	if !ok {
		return store.Message{}, fmt.Errorf("none: %w", store.ErrNotFound)
	}
	return m, nil
}

func TestRouter(t *testing.T) {
	claude := store.AgentInstance{ID: "a1", RoomID: "r1", DisplayName: "Claude", Enabled: true}
	codex := store.AgentInstance{ID: "a2", RoomID: "r1", DisplayName: "Codex", Enabled: true}
	retired := store.AgentInstance{ID: "a3", RoomID: "r1", DisplayName: "Old", Enabled: false}
	st := fakeRouterStore{
		agents: map[string][]store.AgentInstance{"r1": {claude, codex, retired}},
		last: map[string]store.Message{
			"t-claude":  {SenderKind: store.SenderAgent, AgentInstanceID: "a1"},
			"t-retired": {SenderKind: store.SenderAgent, AgentInstanceID: "a3"},
		},
	}
	r := NewRouter(st)
	mention := func(ids ...string) []store.Mention {
		var out []store.Mention
		for _, id := range ids {
			out = append(out, store.Mention{Kind: store.MentionAgent, ID: id})
		}
		return out
	}

	tests := []struct {
		name string
		msg  store.Message
		want []string
	}{
		{"mention wakes the agent", store.Message{Room: "r1", SenderKind: store.SenderUser, Mentions: mention("a1")}, []string{"a1"}},
		{"several mentions, deduplicated, in order", store.Message{Room: "r1", SenderKind: store.SenderUser, Mentions: mention("a2", "a1", "a2")}, []string{"a2", "a1"}},
		{"disabled agent is skipped", store.Message{Room: "r1", SenderKind: store.SenderUser, Mentions: mention("a3")}, nil},
		{"agent from another room is skipped", store.Message{Room: "r1", SenderKind: store.SenderUser, Mentions: mention("elsewhere")}, nil},
		{"user mention wakes nobody", store.Message{Room: "r1", SenderKind: store.SenderUser, Mentions: []store.Mention{{Kind: store.MentionUser, ID: "u1"}}}, nil},
		{"top-level without mention wakes nobody", store.Message{Room: "r1", SenderKind: store.SenderUser}, nil},
		{"thread continues with last agent", store.Message{Room: "r1", ThreadID: "t-claude", SenderKind: store.SenderUser}, []string{"a1"}},
		{"thread with mention prefers the mention", store.Message{Room: "r1", ThreadID: "t-claude", SenderKind: store.SenderUser, Mentions: mention("a2")}, []string{"a2"}},
		{"thread where no agent spoke", store.Message{Room: "r1", ThreadID: "t-quiet", SenderKind: store.SenderUser}, nil},
		{"thread whose last agent is disabled", store.Message{Room: "r1", ThreadID: "t-retired", SenderKind: store.SenderUser}, nil},
		{"agent messages never wake agents", store.Message{Room: "r1", SenderKind: store.SenderAgent, Mentions: mention("a1")}, nil},
		{"system messages never wake agents", store.Message{Room: "r1", ThreadID: "t-claude", SenderKind: store.SenderSystem}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Route(context.Background(), tt.msg)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, a := range got {
				ids = append(ids, a.ID)
			}
			if fmt.Sprint(ids) != fmt.Sprint(tt.want) {
				t.Errorf("routed to %v, want %v", ids, tt.want)
			}
		})
	}
}

func TestRouter_StoreErrorIsReported(t *testing.T) {
	r := NewRouter(failingRouterStore{})
	_, err := r.Route(context.Background(), store.Message{Room: "r1", SenderKind: store.SenderUser})
	if err == nil || !errors.Is(err, errBoom) {
		t.Errorf("got %v, want the store error", err)
	}
}

var errBoom = errors.New("boom")

type failingRouterStore struct{}

func (failingRouterStore) ListRoomAgentInstances(context.Context, string) ([]store.AgentInstance, error) {
	return nil, errBoom
}

func (failingRouterStore) LastAgentMessageInThread(context.Context, string) (store.Message, error) {
	return store.Message{}, errBoom
}
