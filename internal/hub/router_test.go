package hub

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

type fakeRouterStore struct {
	members map[string][]store.Member // by room
	last    map[string]store.Message  // by thread
}

func (f fakeRouterStore) ListRoomMembers(_ context.Context, roomID string) ([]store.Member, error) {
	return f.members[roomID], nil
}

func (f fakeRouterStore) LastAgentMessageInThread(_ context.Context, threadID string) (store.Message, error) {
	m, ok := f.last[threadID]
	if !ok {
		return store.Message{}, fmt.Errorf("none: %w", store.ErrNotFound)
	}
	return m, nil
}

func TestRouter(t *testing.T) {
	claude := store.Member{ID: "a1", RoomID: "r1", DisplayName: "Claude", Enabled: true}
	codex := store.Member{ID: "a2", RoomID: "r1", DisplayName: "Codex", Enabled: true}
	retired := store.Member{ID: "a3", RoomID: "r1", DisplayName: "Old", Enabled: false}
	outAt := time.Now()
	gone := store.Member{ID: "a4", RoomID: "r1", DisplayName: "Gone", Enabled: true, RemovedAt: &outAt}
	st := fakeRouterStore{
		members: map[string][]store.Member{"r1": {claude, codex, retired, gone}},
		last: map[string]store.Message{
			"t-claude":  {SenderKind: store.SenderAgent, MemberID: "a1"},
			"t-retired": {SenderKind: store.SenderAgent, MemberID: "a3"},
			"t-gone":    {SenderKind: store.SenderAgent, MemberID: "a4"},
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
		{"agent taken out of the project is skipped", store.Message{Room: "r1", SenderKind: store.SenderUser, Mentions: mention("a4")}, nil},
		{"agent from another room is skipped", store.Message{Room: "r1", SenderKind: store.SenderUser, Mentions: mention("elsewhere")}, nil},
		{"user mention wakes nobody", store.Message{Room: "r1", SenderKind: store.SenderUser, Mentions: []store.Mention{{Kind: store.MentionUser, ID: "u1"}}}, nil},
		{"top-level without mention wakes nobody among several", store.Message{Room: "r1", SenderKind: store.SenderUser}, nil},
		{"thread continues with last agent", store.Message{Room: "r1", ThreadID: "t-claude", SenderKind: store.SenderUser}, []string{"a1"}},
		{"thread with mention prefers the mention", store.Message{Room: "r1", ThreadID: "t-claude", SenderKind: store.SenderUser, Mentions: mention("a2")}, []string{"a2"}},
		{"thread where no agent spoke", store.Message{Room: "r1", ThreadID: "t-quiet", SenderKind: store.SenderUser}, nil},
		{"thread whose last agent is disabled", store.Message{Room: "r1", ThreadID: "t-retired", SenderKind: store.SenderUser}, nil},
		{"thread whose last agent was taken out", store.Message{Room: "r1", ThreadID: "t-gone", SenderKind: store.SenderUser}, nil},
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

func TestRouter_OnlyMember(t *testing.T) {
	outAt := time.Now()
	solo := store.Member{ID: "s1", RoomID: "solo", DisplayName: "Pi", Enabled: true}
	resting := store.Member{ID: "s2", RoomID: "resting", DisplayName: "Pi", Enabled: false}
	st := fakeRouterStore{
		members: map[string][]store.Member{
			// A member taken out does not make the room a group.
			"solo":    {solo, {ID: "x1", RoomID: "solo", DisplayName: "Gone", Enabled: true, RemovedAt: &outAt}},
			"resting": {resting},
			"pair":    {solo, {ID: "s3", RoomID: "pair", DisplayName: "Off", Enabled: false}},
			"empty":   {{ID: "x2", RoomID: "empty", DisplayName: "Gone", Enabled: true, RemovedAt: &outAt}},
		},
	}
	r := NewRouter(st)

	tests := []struct {
		name string
		msg  store.Message
		want []string
	}{
		{"an unaddressed message goes to the only member", store.Message{Room: "solo", SenderKind: store.SenderUser}, []string{"s1"}},
		{"a message to a person stays with them", store.Message{Room: "solo", SenderKind: store.SenderUser, Mentions: []store.Mention{{Kind: store.MentionUser, ID: "u1"}}}, nil},
		{"a mention that finds no one is not redirected", store.Message{Room: "solo", SenderKind: store.SenderUser, Mentions: []store.Mention{{Kind: store.MentionAgent, ID: "elsewhere"}}}, nil},
		{"a switched-off only member takes nothing", store.Message{Room: "resting", SenderKind: store.SenderUser}, nil},
		{"a switched-off second member still makes it a group", store.Message{Room: "pair", SenderKind: store.SenderUser}, nil},
		{"a room with no current member", store.Message{Room: "empty", SenderKind: store.SenderUser}, nil},
		{"a topic where no agent spoke keeps its own rule", store.Message{Room: "solo", ThreadID: "t-quiet", SenderKind: store.SenderUser}, nil},
		{"agents never wake the only member", store.Message{Room: "solo", SenderKind: store.SenderAgent}, nil},
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

func (failingRouterStore) ListRoomMembers(context.Context, string) ([]store.Member, error) {
	return nil, errBoom
}

func (failingRouterStore) LastAgentMessageInThread(context.Context, string) (store.Message, error) {
	return store.Message{}, errBoom
}
