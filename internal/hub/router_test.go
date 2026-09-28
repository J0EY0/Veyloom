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
	// toUser is the latest to answer the person, by thread.
	toUser  map[string]store.Message
	running map[string][]string // by thread
	leaders map[string]string   // by room
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

func (f fakeRouterStore) TalkingMemberInThread(_ context.Context, threadID, _ string) (string, error) {
	m, ok := f.toUser[threadID]
	if !ok {
		return "", fmt.Errorf("none: %w", store.ErrNotFound)
	}
	return m.MemberID, nil
}

func (f fakeRouterStore) RunningMembersInThread(_ context.Context, threadID string) ([]string, error) {
	return f.running[threadID], nil
}

func (f fakeRouterStore) RoomProject(_ context.Context, roomID string) (store.Project, error) {
	return store.Project{LeaderID: f.leaders[roomID]}, nil
}

func routedIDs(t *testing.T, r *Router, msg store.Message) []string {
	t.Helper()
	got, err := r.Route(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range got {
		ids = append(ids, a.ID)
	}
	return ids
}

func TestRouter(t *testing.T) {
	claude := store.Member{ID: "a1", RoomID: "r1", DisplayName: "Claude", Enabled: true}
	codex := store.Member{ID: "a2", RoomID: "r1", DisplayName: "Codex", Enabled: true}
	retired := store.Member{ID: "a3", RoomID: "r1", DisplayName: "Old", Enabled: false}
	outAt := time.Now()
	gone := store.Member{ID: "a4", RoomID: "r1", DisplayName: "Gone", Enabled: true, RemovedAt: &outAt}
	said := func(memberID string) store.Message {
		return store.Message{SenderKind: store.SenderAgent, MemberID: memberID}
	}
	st := fakeRouterStore{
		members: map[string][]store.Member{
			"r1": {claude, codex, retired, gone},
			// The leader chosen, and turned off.
			"r2": {claude, codex, {ID: "a5", RoomID: "r2", DisplayName: "Lead", Enabled: false}},
		},
		last: map[string]store.Message{
			"t-claude":  said("a1"),
			"t-retired": said("a3"),
			"t-gone":    said("a4"),
			"t-talk":    said("a1"),
			"t-run":     said("a1"),
			"t-two":     said("a1"),
		},
		toUser: map[string]store.Message{
			"t-talk":        said("a2"),
			"t-two":         said("a2"),
			"t-talk-gone":   said("a4"),
			"t-talk-retire": said("a3"),
		},
		running: map[string][]string{
			"t-run":     {"a2"},
			"t-two":     {"a1", "a2"},
			"t-run-off": {"a3"},
		},
		leaders: map[string]string{"r1": "a2", "r2": "a5"},
	}
	r := NewRouter(st)
	mention := func(ids ...string) []store.Mention {
		var out []store.Mention
		for _, id := range ids {
			out = append(out, store.Mention{Kind: store.MentionAgent, ID: id})
		}
		return out
	}
	person := func(room, thread string, mentions ...store.Mention) store.Message {
		return store.Message{Room: room, ThreadID: thread, SenderKind: store.SenderUser, UserID: "u1", Mentions: mentions}
	}

	tests := []struct {
		name string
		msg  store.Message
		want []string
	}{
		{"mention wakes the agent", person("r1", "", mention("a1")...), []string{"a1"}},
		{"several mentions, deduplicated, in order", person("r1", "", mention("a2", "a1", "a2")...), []string{"a2", "a1"}},
		{"disabled agent is skipped", person("r1", "", mention("a3")...), nil},
		{"agent taken out of the project is skipped", person("r1", "", mention("a4")...), nil},
		{"agent from another room is skipped", person("r1", "", mention("elsewhere")...), nil},
		{"user mention wakes nobody", person("r1", "", store.Mention{Kind: store.MentionUser, ID: "u1"}), nil},
		{"top-level without mention among several goes to the leader", person("r1", ""), []string{"a2"}},
		{"the leader turned off takes nothing", person("r2", ""), nil},
		{"thread continues with last agent", person("r1", "t-claude"), []string{"a1"}},
		{"thread with mention prefers the mention", person("r1", "t-claude", mention("a2")...), []string{"a2"}},
		{"thread mention of a member turned off wakes nobody", person("r1", "t-claude", mention("a3")...), nil},
		{"thread mention of a person wakes nobody", person("r1", "t-claude", store.Mention{Kind: store.MentionUser, ID: "u2"}), nil},
		{"thread goes to the member running there", person("r1", "t-run"), []string{"a2"}},
		{"thread goes to the member talking with the person, not the last to speak", person("r1", "t-talk"), []string{"a2"}},
		{"two running: the one talking with the person", person("r1", "t-two"), []string{"a2"}},
		{"the member running there turned off: the leader", person("r1", "t-run-off"), []string{"a2"}},
		{"the member talking with the person taken out: the leader", person("r1", "t-talk-gone"), []string{"a2"}},
		{"the member talking with the person turned off: the leader", person("r1", "t-talk-retire"), []string{"a2"}},
		{"thread no member took part in, people talking, goes to nobody", person("r1", "t-quiet"), nil},
		{"nor does a person's word to a person there", person("r1", "t-quiet", store.Mention{Kind: store.MentionUser, ID: "u2"}), nil},
		{"thread whose last agent is disabled goes to the leader", person("r1", "t-retired"), []string{"a2"}},
		{"thread whose last agent was taken out goes to the leader", person("r1", "t-gone"), []string{"a2"}},
		{"agent messages never wake agents", store.Message{Room: "r1", SenderKind: store.SenderAgent, Mentions: mention("a1")}, nil},
		{"system messages never wake agents", store.Message{Room: "r1", ThreadID: "t-claude", SenderKind: store.SenderSystem}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routedIDs(t, r, tt.msg); fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("routed to %v, want %v", got, tt.want)
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
		leaders: map[string]string{"solo": "s1", "resting": "s2", "pair": "s1"},
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
		{"a switched-off second member still makes it a group, the leader's", store.Message{Room: "pair", SenderKind: store.SenderUser}, []string{"s1"}},
		{"a room with no current member", store.Message{Room: "empty", SenderKind: store.SenderUser}, nil},
		{"a topic no member took part in goes to nobody", store.Message{Room: "solo", ThreadID: "t-quiet", SenderKind: store.SenderUser}, nil},
		{"agents never wake the only member", store.Message{Room: "solo", SenderKind: store.SenderAgent}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := routedIDs(t, r, tt.msg); fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("routed to %v, want %v", got, tt.want)
			}
		})
	}
}

// Where a message that names nobody would go, and why: what the composer
// tells the person before they send it.
func TestRouter_Addressee(t *testing.T) {
	lead := store.Member{ID: "l1", RoomID: "r1", DisplayName: "Lead", Enabled: true}
	coder := store.Member{ID: "c1", RoomID: "r1", DisplayName: "Coder", Enabled: true}
	st := fakeRouterStore{
		members: map[string][]store.Member{"r1": {lead, coder}, "solo": {{ID: "s1", RoomID: "solo", DisplayName: "Pi", Enabled: true}}, "none": nil},
		last:    map[string]store.Message{"t-last": {MemberID: "c1"}, "t-gone": {MemberID: "x"}},
		toUser:  map[string]store.Message{"t-talk": {MemberID: "c1"}},
		running: map[string][]string{"t-run": {"c1"}},
		leaders: map[string]string{"r1": "l1"},
	}
	r := NewRouter(st)
	for _, c := range []struct {
		room, thread string
		member       string
		reason       AddresseeReason
	}{
		{"solo", "", "s1", AddresseeOnly},
		{"r1", "", "l1", AddresseeLeader},
		{"r1", "t-run", "c1", AddresseeRunning},
		{"r1", "t-talk", "c1", AddresseeTalking},
		{"r1", "t-last", "c1", AddresseeLast},
		{"r1", "t-gone", "l1", AddresseeLeaderFallback},
		{"r1", "t-quiet", "", AddresseeNone},
		{"none", "", "", AddresseeNone},
	} {
		got, err := r.Addressee(context.Background(), c.room, c.thread, "u1")
		if err != nil || got.Member.ID != c.member || got.Reason != c.reason {
			t.Errorf("%s/%s: %s (%s), want %s (%s), %v", c.room, c.thread, got.Member.ID, got.Reason, c.member, c.reason, err)
		}
	}
}

func TestRouter_StoreErrorIsReported(t *testing.T) {
	r := NewRouter(failingRouterStore{})
	_, err := r.Route(context.Background(), store.Message{Room: "r1", SenderKind: store.SenderUser})
	if err == nil || !errors.Is(err, errBoom) {
		t.Errorf("got %v, want the store error", err)
	}
	// A topic's reads fail as loudly.
	r = NewRouter(failingThreadStore{fakeRouterStore{members: map[string][]store.Member{"r1": {{ID: "a1", RoomID: "r1", Enabled: true}}}}})
	if _, err := r.Route(context.Background(), store.Message{Room: "r1", ThreadID: "t1", SenderKind: store.SenderUser, UserID: "u1"}); !errors.Is(err, errBoom) {
		t.Errorf("a topic's read failing: got %v, want the store error", err)
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

func (failingRouterStore) TalkingMemberInThread(context.Context, string, string) (string, error) {
	return "", errBoom
}

func (failingRouterStore) RunningMembersInThread(context.Context, string) ([]string, error) {
	return nil, errBoom
}

func (failingRouterStore) RoomProject(context.Context, string) (store.Project, error) {
	return store.Project{}, errBoom
}

// failingThreadStore reads the room but not its topics.
type failingThreadStore struct{ fakeRouterStore }

func (failingThreadStore) RunningMembersInThread(context.Context, string) ([]string, error) {
	return nil, errBoom
}
