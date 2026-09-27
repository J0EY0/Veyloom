package hub

import (
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// A member someone holds, starting a turn or running one, is theirs to let
// go: taking up what waits once a pause is lifted or the machine is back
// leaves it to them, lest the member run two turns at once.
func TestResume_LeavesAHeldMemberToItsHolder(t *testing.T) {
	waiting := func(id string) []trigger { return []trigger{{msg: store.Message{ID: id}}} }
	m := &TurnManager{members: map[string]*memberState{
		"starting":  {starting: true, pending: waiting("m1")},
		"running":   {running: &activeTurn{}, pending: waiting("m2")},
		"elsewhere": {pending: waiting("m3"), machine: "other"},
	}}
	m.resume(func(_ string, st *memberState) bool { return st.machine != "other" })
	for id, st := range m.members {
		if len(st.pending) != 1 {
			t.Errorf("%s: what waits was taken: %+v", id, st.pending)
		}
	}
	if st := m.members["starting"]; !st.starting {
		t.Errorf("the holder lost its member: %+v", st)
	}
	if st := m.members["elsewhere"]; st.starting {
		t.Errorf("a member left out was taken: %+v", st)
	}
}

// What a turn is getting going for is held, as what waits and what the
// running turn answers are: asked again meanwhile, it is not queued twice.
func TestMemberState_Holds(t *testing.T) {
	msg := func(id string) store.Message { return store.Message{ID: id} }
	st := &memberState{
		pending: []trigger{{msg: msg("waiting")}},
		next:    []store.Message{msg("starting")},
		running: &activeTurn{triggers: []store.Message{msg("answering")}},
	}
	for _, id := range []string{"waiting", "starting", "answering"} {
		if !st.holds(msg(id)) {
			t.Errorf("%s is not held", id)
		}
	}
	if st.holds(msg("new")) {
		t.Error("a new message is held")
	}
}
