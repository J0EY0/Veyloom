package hub

import (
	"context"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/machine"
	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
)

// smokeTurnTimeout is how long a turn on a real CLI may take.
const smokeTurnTimeout = 3 * time.Minute

// smokeRoom is what the tests against real CLIs share: a hub on a test
// database, one machine with the runners given, and a project whose room
// has one member on the runtime under test.
type smokeRoom struct {
	t      *testing.T
	ctx    context.Context
	s      *store.Store
	h      *Hub
	room   store.Room
	user   store.User
	agent  store.Agent
	member store.Member
}

// newSmokeRoom connects a machine with runners to a new hub, creates agent
// on that machine and makes it the one member of a project in dir.
func newSmokeRoom(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent, dir string) *smokeRoom {
	t.Helper()
	s := storetest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := New(s, Config{TranscriptDir: t.TempDir(), HeartbeatInterval: time.Hour})
	w := machine.New(machine.Config{Name: "laptop"}, machine.NewDiscovery(nil, time.Second), &machine.MemoryIdentity{}, runners)
	hubEnd, machineEnd := protocol.Pipe()
	go h.Serve(ctx, hubEnd)
	go w.Run(ctx, machineEnd)
	eventually(t, func() bool { return len(h.Machines()) == 1 }, "machine to connect")

	_, room, err := s.CreateProject(ctx, store.NewProject{Name: "smoke", RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	agent.MachineID = h.Machines()[0].ID
	created, err := s.CreateAgent(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: created.ID, DisplayName: agent.Name, RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	return &smokeRoom{t: t, ctx: ctx, s: s, h: h, room: room, user: user, agent: created, member: member}
}

// say posts body from the person, mentioning the member, in the thread or,
// with threadID empty, in the room itself.
func (r *smokeRoom) say(body, threadID string) store.Message {
	r.t.Helper()
	msg, err := r.h.PostUserMessage(r.ctx, store.NewMessage{
		RoomID: r.room.ID, ThreadID: threadID, UserID: r.user.ID, Body: body,
		Mentions: []store.Mention{{Kind: store.MentionAgent, ID: r.member.ID}},
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return msg
}

// waitDone waits for the room's n-th turn to end and fails unless it is done.
func (r *smokeRoom) waitDone(n int) store.Turn {
	r.t.Helper()
	return r.waitTurn(n, nil)
}

// decideAll is waitDone for a turn that asks for approval: every approval
// it raises is decided as allow says. It fails if the turn asked for none.
func (r *smokeRoom) decideAll(n int, allow bool) (store.Turn, []store.Approval) {
	r.t.Helper()
	return r.decideAllWith(n, func(store.Approval) runtime.Decision { return runtime.Decision{Allow: allow} })
}

// decideAllWith is decideAll deciding each request as decide says, such as
// answering a question.
func (r *smokeRoom) decideAllWith(n int, decide func(store.Approval) runtime.Decision) (store.Turn, []store.Approval) {
	r.t.Helper()
	var decided []store.Approval
	turn := r.waitTurn(n, func() {
		pending, err := r.s.ListPendingRoomApprovals(r.ctx, r.room.ID)
		if err != nil {
			r.t.Fatal(err)
		}
		for _, a := range pending {
			d, err := r.h.DecideApproval(r.ctx, a.ID, r.user.ID, decide(a))
			if err != nil {
				r.t.Fatal(err)
			}
			decided = append(decided, d)
		}
	})
	if len(decided) == 0 {
		r.t.Fatalf("turn %d asked for no approval", n)
	}
	return turn, decided
}

// waitTurn polls until the room's n-th turn ends, calling poll (if any)
// each time while it runs, and fails unless the turn is done.
func (r *smokeRoom) waitTurn(n int, poll func()) store.Turn {
	r.t.Helper()
	deadline := time.Now().Add(smokeTurnTimeout)
	for time.Now().Before(deadline) {
		turns, err := r.s.ListRoomTurns(r.ctx, r.room.ID, 50)
		if err != nil {
			r.t.Fatal(err)
		}
		if len(turns) == n && turns[0].Status != store.TurnRunning {
			if turns[0].Status != store.TurnDone {
				r.t.Fatalf("turn %d ended %s: %s", n, turns[0].Status, turns[0].Error)
			}
			return turns[0]
		}
		if poll != nil {
			poll()
		}
		time.Sleep(200 * time.Millisecond)
	}
	r.t.Fatalf("turn %d did not finish in time", n)
	return store.Turn{}
}

// lastReply is the body of the latest agent message in the thread.
func (r *smokeRoom) lastReply(threadID string) string {
	r.t.Helper()
	msg, err := r.s.LastAgentMessageInThread(r.ctx, threadID)
	if err != nil {
		r.t.Fatal(err)
	}
	return msg.Body
}
