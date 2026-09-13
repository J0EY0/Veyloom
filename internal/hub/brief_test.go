package hub

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

type fakeBriefStore struct {
	messages map[string]store.Message
	replies  []store.Message
	users    map[string]string
	agents   map[string]string
	lookups  int
}

func (f *fakeBriefStore) GetMessage(_ context.Context, id string) (store.Message, error) {
	m, ok := f.messages[id]
	if !ok {
		return store.Message{}, fmt.Errorf("message %s: %w", id, store.ErrNotFound)
	}
	return m, nil
}

func (f *fakeBriefStore) ListThreadMessagesBefore(_ context.Context, _ string, _ int64, limit int) ([]store.Message, error) {
	if len(f.replies) > limit {
		return f.replies[len(f.replies)-limit:], nil
	}
	return f.replies, nil
}

func (f *fakeBriefStore) GetUser(_ context.Context, id string) (store.User, error) {
	f.lookups++
	return store.User{ID: id, Name: f.users[id]}, nil
}

func (f *fakeBriefStore) GetAgentInstance(_ context.Context, id string) (store.AgentInstance, error) {
	f.lookups++
	return store.AgentInstance{ID: id, DisplayName: f.agents[id]}, nil
}

func TestBrief_RendersThreadWithMarkers(t *testing.T) {
	root := store.Message{ID: "m1", SenderKind: store.SenderUser, UserID: "u1", Body: "@Claude plan the auth refactor"}
	st := &fakeBriefStore{
		messages: map[string]store.Message{"m1": root},
		replies: []store.Message{
			{ID: "m2", SenderKind: store.SenderAgent, AgentInstanceID: "a1", Body: "Here is a plan."},
			{ID: "m3", SenderKind: store.SenderSystem, Body: "Codex joined"},
			{ID: "m4", SenderKind: store.SenderUser, UserID: "u1", Body: "looks good, do step 1"},
			{ID: "m5", SenderKind: store.SenderUser, UserID: "u2", Body: "+1"},
		},
		users:  map[string]string{"u1": "alice", "u2": "bob"},
		agents: map[string]string{"a1": "Claude"},
	}
	b := newBriefBuilder(st, 40)
	agent := store.AgentInstance{ID: "a1", DisplayName: "Claude"}
	thread := store.Thread{ID: "t1", RootMessageID: "m1"}

	brief, err := b.Build(context.Background(), agent, thread, []store.Message{st.replies[2], st.replies[3]})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		`You are "Claude"`,
		"   [alice] @Claude plan the auth refactor",
		"   [Claude] Here is a plan.",
		"   [system] Codex joined",
		">> [alice] looks good, do step 1",
		">> [bob] +1",
	}
	for _, line := range want {
		if !strings.Contains(brief, line) {
			t.Errorf("brief missing %q:\n%s", line, brief)
		}
	}
	// Lines must appear in conversation order.
	if strings.Index(brief, "Here is a plan.") > strings.Index(brief, "do step 1") {
		t.Error("replies are out of order")
	}
	// alice appears twice and is looked up once; Claude, bob once each.
	if st.lookups != 3 {
		t.Errorf("sender names looked up %d times, want 3 (cached)", st.lookups)
	}
}

func TestBrief_CapsMessagesToTheLatest(t *testing.T) {
	root := store.Message{ID: "m1", SenderKind: store.SenderUser, UserID: "u1", Body: "root"}
	st := &fakeBriefStore{messages: map[string]store.Message{"m1": root}, users: map[string]string{"u1": "alice"}}
	for i := 1; i <= 10; i++ {
		st.replies = append(st.replies, store.Message{ID: fmt.Sprintf("r%d", i), SenderKind: store.SenderUser, UserID: "u1", Body: fmt.Sprintf("reply %d", i)})
	}

	brief, err := newBriefBuilder(st, 3).Build(context.Background(), store.AgentInstance{DisplayName: "A"}, store.Thread{RootMessageID: "m1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(brief, "reply 7") || !strings.Contains(brief, "reply 8") || !strings.Contains(brief, "reply 10") {
		t.Errorf("brief should hold only the latest 3 replies:\n%s", brief)
	}
	if !strings.Contains(brief, "[alice] root") {
		t.Error("the root message is always included")
	}
}
