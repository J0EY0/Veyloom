package hub

import (
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// latestDone waits until the room has n turns, the newest done.
func (l *loop) latestDone(n int, what string) {
	l.t.Helper()
	eventually(l.t, func() bool {
		turns := l.turns()
		return len(turns) == n && turns[0].Status == store.TurnDone
	}, what)
}

// pausesNow is what the hub holds in effect.
func (l *loop) pausesNow() []store.Pause {
	l.t.Helper()
	return l.h.Pauses(l.ctx)
}

// roomNotes are the system messages of the room itself, not of a topic.
func (l *loop) roomNotes() []string {
	l.t.Helper()
	var out []string
	for _, m := range l.topLevel() {
		if m.SenderKind == store.SenderSystem {
			out = append(out, m.Body)
		}
	}
	return out
}

// quickPauses has the loop's pauses run out without the grace and the
// minute's cooldown a real account gets.
func (l *loop) quickPauses(cooldown time.Duration) {
	l.h.turns.pauses.grace, l.h.turns.pauses.cooldown = 0, cooldown
}

// An account whose usage limit is reached rests until it resets: what the
// member is asked meanwhile waits, the places it was asked in are told
// why, and it goes on by itself once the limit resets.
func TestLoop_AnAccountOutOfQuotaWaitsUntilItResets(t *testing.T) {
	l := newLoop(t)
	l.quickPauses(time.Minute)
	slow := l.member("Slow", map[string]any{"fail": true, "failure": "quota", "retry_in_ms": 1500})
	first := l.say("@Slow build the parser", "", slow)
	l.waitTurns(1, store.TurnFailed, "the turn the limit stops")

	pauses := l.pausesNow()
	if len(pauses) != 1 || pauses[0].Reason != store.PauseQuota || pauses[0].Runtime != "fake" || pauses[0].MachineID != l.machineID || pauses[0].EndsAt == nil {
		t.Fatalf("pauses = %+v", pauses)
	}
	notes := l.notes(l.topic(first).ID)
	if countContaining(notes, "Slow waits for fake's usage limit on laptop to reset at ") != 1 {
		t.Errorf("the topic is told why it stops: %q", notes)
	}

	l.setOptions(slow, map[string]any{"reply": "done"})
	second := l.say("@Slow and the lexer", "", slow)
	time.Sleep(300 * time.Millisecond)
	if n := len(l.turns()); n != 1 {
		t.Fatalf("nothing starts while the account rests: %d turns", n)
	}
	if q := l.queued(); len(q) != 1 || q[0].MessageID != second.ID {
		t.Errorf("what was asked is kept: %+v", q)
	}
	if notes := l.roomNotes(); countContaining(notes, "Slow waits for fake's usage limit") != 1 {
		t.Errorf("the room is told why, once: %q", notes)
	}
	l.say("@Slow and the docs", "", slow)
	if notes := l.roomNotes(); countContaining(notes, "Slow waits for fake's usage limit") != 1 {
		t.Errorf("told once per place: %q", notes)
	}

	eventuallyWithin(t, 10*time.Second, func() bool {
		turns := l.turns()
		return len(turns) == 3 && turns[0].Status == store.TurnDone && turns[1].Status == store.TurnDone
	}, "what waited to be answered once the limit resets")
	if pauses := l.pausesNow(); len(pauses) != 0 {
		t.Errorf("the pause ran out: %+v", pauses)
	}
	if q := l.queued(); len(q) != 0 {
		t.Errorf("answered, nothing waits: %+v", q)
	}
}

// A signed-out account waits for a person, who lifts the pause once it is
// signed in again.
func TestLoop_ASignedOutAccountWaitsForAPerson(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"fail": true, "failure": "auth"})
	l.say("@Slow build the parser", "", slow)
	l.waitTurns(1, store.TurnFailed, "the turn with no one signed in")
	pauses := l.pausesNow()
	if len(pauses) != 1 || pauses[0].Reason != store.PauseAuth || pauses[0].EndsAt != nil {
		t.Fatalf("pauses = %+v", pauses)
	}

	l.setOptions(slow, map[string]any{"reply": "done"})
	l.say("@Slow and the lexer", "", slow)
	time.Sleep(300 * time.Millisecond)
	if n := len(l.turns()); n != 1 {
		t.Fatalf("nothing starts while it is signed out: %d turns", n)
	}
	if notes := l.roomNotes(); countContaining(notes, "Slow waits: fake is signed out on laptop; sign it in again there, then resume it.") != 1 {
		t.Errorf("the room is told what to do: %q", notes)
	}
	if err := l.h.LiftPause(l.ctx, pauses[0].ID, l.user.ID); err != nil {
		t.Fatal(err)
	}
	l.latestDone(2, "what waited, once a person resumed it")
	if err := l.h.LiftPause(l.ctx, pauses[0].ID, l.user.ID); err == nil {
		t.Error("a pause lifted is lifted once")
	}
}

// A member whose turns keep failing is paused after the third; its
// account is not, and the member's next turn goes on once a person
// resumes it.
func TestLoop_AMemberWhoseTurnsKeepFailingIsPaused(t *testing.T) {
	l := newLoop(t)
	broken := l.member("Broken", map[string]any{"fail": true})
	other := l.member("Other", map[string]any{"reply": "fine"})
	for i, body := range []string{"@Broken one", "@Broken two", "@Broken three"} {
		l.say(body, "", broken)
		l.waitTurns(i+1, store.TurnFailed, "a failed turn")
		if i < 2 && len(l.pausesNow()) != 0 {
			t.Fatalf("paused after %d failures: %+v", i+1, l.pausesNow())
		}
	}
	pauses := l.pausesNow()
	if len(pauses) != 1 || pauses[0].Reason != store.PauseFailing || pauses[0].MemberID != broken.ID || !strings.Contains(pauses[0].Detail, "scripted failure") {
		t.Fatalf("pauses = %+v", pauses)
	}

	l.setOptions(broken, map[string]any{"reply": "fixed"})
	l.say("@Broken four", "", broken)
	l.say("@Other hello", "", other)
	l.latestDone(4, "the other member's turn, its account at work")
	if notes := l.roomNotes(); countContaining(notes, "Broken waits: its last 3 turns failed; resume it once what failed is seen to.") != 1 {
		t.Errorf("the room is told: %q", notes)
	}
	if err := l.h.LiftPause(l.ctx, pauses[0].ID, l.user.ID); err != nil {
		t.Fatal(err)
	}
	l.latestDone(5, "the fourth ask once resumed")
}

// A turn on the account that goes well, begun before the pause, lifts it:
// the account can take turns after all.
func TestLoop_ATurnThatGoesWellLiftsTheAccountsPause(t *testing.T) {
	l := newLoop(t)
	l.quickPauses(time.Minute)
	busy := l.member("Busy", map[string]any{"reply": "done", "delay_ms": float64(800)})
	limited := l.member("Limited", map[string]any{"fail": true, "failure": "rate_limit"})
	l.say("@Busy go", "", busy)
	l.waitTurns(1, store.TurnRunning, "Busy's turn")
	l.say("@Limited go", "", limited)
	eventually(t, func() bool { return len(l.pausesNow()) == 1 }, "the account paused for too many requests")
	if p := l.pausesNow()[0]; p.Reason != store.PauseRateLimit || p.EndsAt == nil || time.Until(*p.EndsAt) < 50*time.Second {
		t.Errorf("a minute's rest: %+v", p)
	}
	eventually(t, func() bool { return len(l.pausesNow()) == 0 }, "Busy's turn going well to lift it")
}

// An account the runtime reports its limit reached for rests until it
// resets, even though the turn that reported it went well; the machine
// shows how the account stands.
func TestLoop_ALimitReportedReachedPausesTheAccount(t *testing.T) {
	l := newLoop(t)
	l.quickPauses(time.Minute)
	slow := l.member("Slow", map[string]any{"reply": "done", "quota": map[string]any{"limited": true, "window": "5h", "used_percent": 100, "resets_in_ms": 60000}})
	l.say("@Slow go", "", slow)
	l.waitTurns(1, store.TurnDone, "the turn that reached the limit")
	eventually(t, func() bool { return len(l.pausesNow()) == 1 }, "the account paused")
	if p := l.pausesNow()[0]; p.Reason != store.PauseQuota || p.EndsAt == nil || !strings.Contains(p.Detail, "5h") {
		t.Errorf("pause = %+v", p)
	}
	machines := l.h.Machines()
	if q, ok := machines[0].Quotas["fake"]; !ok || !q.Limited || q.Window != "5h" || q.UsedPercent == nil || *q.UsedPercent != 100 {
		t.Errorf("quotas = %+v", machines[0].Quotas)
	}
}

// Pauses outlive the hub; one that ran out while it was away is lifted.
func TestLoop_PausesOutliveTheHub(t *testing.T) {
	l := newLoop(t)
	slow := l.member("Slow", map[string]any{"fail": true, "failure": "auth"})
	l.say("@Slow go", "", slow)
	l.waitTurns(1, store.TurnFailed, "the turn with no one signed in")
	gone := time.Now().Add(-time.Minute)
	if _, err := l.s.PauseMember(l.ctx, slow.ID, store.PauseFailing, "old", &gone); err != nil {
		t.Fatal(err)
	}
	l.restart()
	pauses := l.pausesNow()
	if len(pauses) != 1 || pauses[0].Reason != store.PauseAuth {
		t.Errorf("the account's pause is still in effect, the one run out lifted: %+v", pauses)
	}
	eventually(t, func() bool {
		left, err := l.s.ListPauses(l.ctx)
		return err == nil && len(left) == 1
	}, "the pause that ran out to be lifted in the store")
}

// Turns that fail through no fault of the member, its machine offline,
// do not pause it.
func TestLoop_AnOfflineMachinesFailuresDoNotPauseTheMember(t *testing.T) {
	l := newLoop(t)
	ghostID, err := l.s.RegisterMachine(l.ctx, "", "ghost", nil)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := l.s.CreateAgent(l.ctx, store.NewAgent{Name: "Ghost", MachineID: ghostID, Runtime: "fake", PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	ghost, err := l.s.CreateMember(l.ctx, store.NewMember{RoomID: l.room.ID, AgentID: agent.ID, DisplayName: "Ghost"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range failingTurns + 1 {
		l.say("@Ghost anyone home?", "", ghost)
		l.waitTurns(i+1, store.TurnFailed, "the offline turn to fail")
	}
	if pauses := l.pausesNow(); len(pauses) != 0 {
		t.Errorf("no pause for the machine's absence: %+v", pauses)
	}
}
