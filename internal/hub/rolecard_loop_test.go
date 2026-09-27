package hub

import (
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// A changed role card reaches a runtime that fixes it when a session starts
// in a new session (docs/design.md 5.6).

// fixedRuntime has the loop's machine run the fake runtime under name as
// well: a runtime the hub does not know, which it takes to fix its system
// prompt when a session starts, as Codex does (runtime.TraitsOf).
func (l *loop) fixedRuntime(name string) {
	l.t.Helper()
	runners := runtime.BuiltinRunners()
	runners[name] = runners["fake"]
	l.runners = runners
	l.dropMachine()
	l.connectMachine()
}

// setRoleCard gives a member's agent a new role card, the rest as it was.
func (l *loop) setRoleCard(member store.Member, roleCard string) {
	l.t.Helper()
	a, err := l.s.GetAgent(l.ctx, member.AgentID)
	if err != nil {
		l.t.Fatal(err)
	}
	if _, err := l.s.UpdateAgent(l.ctx, a.ID, store.NewAgent{
		Name: a.Name, Avatar: a.Avatar, MachineID: a.MachineID, Runtime: a.Runtime, Model: a.Model,
		RoleCard: roleCard, PermissionPreset: a.PermissionPreset, RuntimeOptions: a.RuntimeOptions, Skills: a.Skills,
	}); err != nil {
		l.t.Fatal(err)
	}
}

// A member on a runtime that fixed its role card when the session started
// goes on in it while the role card stays, and starts a new one, told the
// new role card, once it changed; the room and the brief say why.
func TestLoop_AChangedRoleCardStartsANewSessionWhereTheRuntimeFixedIt(t *testing.T) {
	l := newLoop(t)
	l.fixedRuntime("fixed")
	agent, err := l.s.CreateAgent(l.ctx, store.NewAgent{Name: "Rev agent", MachineID: l.machineID, Runtime: "fixed", PermissionPreset: store.PermissionReadOnly, RoleCard: "You review."})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := l.s.CreateMember(l.ctx, store.NewMember{RoomID: l.room.ID, AgentID: agent.ID, DisplayName: "Rev"})
	if err != nil {
		t.Fatal(err)
	}

	l.say("@Rev one", "", rev)
	first := l.waitTurns(1, store.TurnDone, "the first turn")[0]
	if s, err := l.s.GetSession(l.ctx, first.SessionID); err != nil || s.RoleCardDigest != roleCardDigest("fixed", "You review.") {
		t.Fatalf("the session keeps the role card it started with: %+v %v", s, err)
	}
	l.say("@Rev two", "", rev)
	second := l.waitTurns(2, store.TurnDone, "the second turn")[0]
	if second.SessionID != first.SessionID || !specOf(t, second).Session.Resume {
		t.Fatalf("the same role card, the same session: %s, then %s", first.SessionID, second.SessionID)
	}

	l.setRoleCard(rev, "You test.")
	l.say("@Rev three", "", rev)
	third := l.waitTurns(3, store.TurnDone, "the turn after the change")[0]
	spec := specOf(t, third)
	if third.SessionID == first.SessionID || spec.Session.Resume || spec.SystemPrompt != "You test." {
		t.Errorf("a new session, told the new role card: session %s, %+v, %q", third.SessionID, spec.Session, spec.SystemPrompt)
	}
	if !strings.Contains(spec.Prompt, "This is a new session") || !strings.Contains(spec.Prompt, "(its role card changed)") {
		t.Errorf("the brief says why:\n%s", spec.Prompt)
	}
	if old, err := l.s.GetSession(l.ctx, first.SessionID); err != nil || old.EndReason != store.SessionRoleCardChanged {
		t.Errorf("the old session ended for it: %+v %v", old, err)
	}
	if notes := l.notes(third.ThreadID); countContaining(notes, "Rev started a new session (its role card changed)") != 1 {
		t.Errorf("the room is told: %q", notes)
	}

	l.say("@Rev four", "", rev)
	fourth := l.waitTurns(4, store.TurnDone, "the turn after that")[0]
	if fourth.SessionID != third.SessionID || !specOf(t, fourth).Session.Resume || len(l.notes(fourth.ThreadID)) != 0 {
		t.Errorf("the new session goes on: %s, then %s", third.SessionID, fourth.SessionID)
	}
}

// A runtime that takes its system prompt with every run is told a new role
// card in the session it has.
func TestLoop_AChangedRoleCardKeepsTheSessionOfARuntimeToldItEveryRun(t *testing.T) {
	l := newLoop(t)
	echo := l.member("Echo", nil)
	l.say("@Echo one", "", echo)
	first := l.waitTurns(1, store.TurnDone, "the first turn")[0]

	l.setRoleCard(echo, "Be thorough.")
	l.say("@Echo two", "", echo)
	second := l.waitTurns(2, store.TurnDone, "the turn after the change")[0]
	spec := specOf(t, second)
	if second.SessionID != first.SessionID || !spec.Session.Resume || !strings.HasPrefix(spec.SystemPrompt, "Be thorough.") {
		t.Errorf("the same session, told the new role card: %s, then %s, %q", first.SessionID, second.SessionID, spec.SystemPrompt)
	}
	if s, err := l.s.GetSession(l.ctx, first.SessionID); err != nil || s.RoleCardDigest != "" {
		t.Errorf("it fixes nothing: %+v %v", s, err)
	}
}

// A session a turn ran again in, its own not resuming, keeps the role card
// it started with too.
func TestLoop_ASessionStartedOverKeepsTheRoleCard(t *testing.T) {
	l := newLoop(t)
	l.fixedRuntime("fixed")
	agent, err := l.s.CreateAgent(l.ctx, store.NewAgent{Name: "Rev agent", MachineID: l.machineID, Runtime: "fixed", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "You review.", RuntimeOptions: map[string]any{"fail_on_resume": true}})
	if err != nil {
		t.Fatal(err)
	}
	rev, err := l.s.CreateMember(l.ctx, store.NewMember{RoomID: l.room.ID, AgentID: agent.ID, DisplayName: "Rev"})
	if err != nil {
		t.Fatal(err)
	}
	l.say("@Rev one", "", rev)
	first := l.waitTurns(1, store.TurnDone, "the first turn")[0]
	l.say("@Rev two", "", rev)
	second := l.waitTurns(2, store.TurnDone, "the second turn, run again")[0]
	if second.SessionID == first.SessionID {
		t.Fatalf("the second turn should have moved to a new session: %s", second.SessionID)
	}
	s, err := l.s.GetSession(l.ctx, second.SessionID)
	if err != nil || s.Ref == "" || s.RoleCardDigest != roleCardDigest("fixed", "You review.") {
		t.Errorf("the new session: %+v %v", s, err)
	}
}
