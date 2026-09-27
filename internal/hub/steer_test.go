package hub

import (
	"slices"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// A person's message in the topic a chat turn runs in, on a runtime that
// takes input mid-turn, is passed to the turn; nothing else is.
func TestSteerable(t *testing.T) {
	topic := store.Thread{ID: "t1"}
	running := func(runtime string) *memberState {
		return &memberState{running: &activeTurn{thread: topic, agent: store.Agent{Runtime: runtime}}}
	}
	person := trigger{msg: store.Message{ID: "m1", SenderKind: store.SenderUser}, thread: topic}
	if !steerable(running("claude"), person) {
		t.Error("a person's message in the turn's topic is passed")
	}
	for name, c := range map[string]struct {
		st *memberState
		t  trigger
	}{
		"no turn running":    {&memberState{starting: true}, person},
		"another topic":      {running("claude"), trigger{msg: person.msg, thread: store.Thread{ID: "t2"}}},
		"the room":           {running("claude"), trigger{msg: person.msg}},
		"an agent's wake":    {running("claude"), trigger{msg: store.Message{ID: "m2", SenderKind: store.SenderAgent}, thread: topic}},
		"a wake let go on":   {running("claude"), trigger{msg: person.msg, thread: topic, anchor: "m0"}},
		"an upkeep":          {running("claude"), trigger{msg: person.msg, thread: topic, upkeep: &upkeep{}}},
		"a runtime without":  {running("gemini"), person},
		"an upkeep turn":     {&memberState{running: &activeTurn{thread: topic, agent: store.Agent{Runtime: "claude"}, upkeep: &upkeep{}}}, person},
		"the leader's setup": {&memberState{running: &activeTurn{thread: topic, agent: store.Agent{Runtime: "claude"}, setup: &setupRun{}}}, person},
	} {
		if steerable(c.st, c.t) {
			t.Errorf("%s: passed to the turn", name)
		}
	}
}

// What waits of the person's in the turn's topic goes along with the
// message passed, first; the rest waits on.
func TestSteerAlong(t *testing.T) {
	topic, other := store.Thread{ID: "t1"}, store.Thread{ID: "t2"}
	st := &memberState{running: &activeTurn{thread: topic, agent: store.Agent{Runtime: "codex"}}}
	early := trigger{msg: store.Message{ID: "early", SenderKind: store.SenderUser, Seq: 5}, thread: topic}
	elsewhere := trigger{msg: store.Message{ID: "elsewhere", SenderKind: store.SenderUser, Seq: 6}, thread: other}
	agent := trigger{msg: store.Message{ID: "agent", SenderKind: store.SenderAgent, Seq: 7}, thread: topic}
	st.pending = []trigger{early, elsewhere, agent}
	now := trigger{msg: store.Message{ID: "now", SenderKind: store.SenderUser, Seq: 9}, thread: topic}

	carried := steerAlong(st, now)
	ids := func(ts []trigger) []string {
		var out []string
		for _, t := range ts {
			out = append(out, t.msg.ID)
		}
		return out
	}
	if got := ids(carried); !slices.Equal(got, []string{"early", "now"}) {
		t.Errorf("carried %v", got)
	}
	if got := ids(st.pending); !slices.Equal(got, []string{"elsewhere", "agent"}) {
		t.Errorf("left waiting %v", got)
	}
}

func TestAskersOf(t *testing.T) {
	steered := []store.Message{
		{SenderKind: store.SenderUser, UserID: "bob"},
		{SenderKind: store.SenderUser, UserID: "alice"},
		{SenderKind: store.SenderUser, UserID: "bob"},
		{SenderKind: store.SenderSystem},
	}
	if got := askersOf("alice", steered); !slices.Equal(got, []string{"alice", "bob"}) {
		t.Errorf("askersOf = %v", got)
	}
	if got := askersOf("", steered); !slices.Equal(got, []string{"bob", "alice"}) {
		t.Errorf("an agent's turn a person spoke into answers them: %v", got)
	}
}

func TestAddressAll(t *testing.T) {
	users := []store.User{{ID: "1", Name: "alice"}, {ID: "2", Name: "bob"}}
	for text, want := range map[string]string{
		"done":               "@alice @bob done",
		"@alice done":        "@bob @alice done",
		"@bob @alice done":   "@bob @alice done",
		"@alice，@bob 好了":     "@alice，@bob 好了",
		"@bobby done":        "@alice @bob @bobby done",
		"# Plan\n\n- one":    "@alice @bob\n\n# Plan\n\n- one",
		"@alice\n\n# Plan\n": "@bob @alice\n\n# Plan\n",
	} {
		if got := addressAll(users, text); got != want {
			t.Errorf("addressAll(%q) = %q, want %q", text, got, want)
		}
	}
	if got := addressAll(users[:1], "done"); got != addressTo("alice", "done") {
		t.Errorf("one person is addressed as addressTo does: %q", got)
	}
}
