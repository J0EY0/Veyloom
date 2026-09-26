package hub

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// asking is member for one who works under a preset that asks people.
func (l *loop) asking(name string, options map[string]any) store.Member {
	l.t.Helper()
	m := l.member(name, options)
	preset := store.PermissionEditWithApproval
	m, err := l.s.UpdateMember(l.ctx, m.ID, store.MemberPatch{PermissionPreset: &preset})
	if err != nil {
		l.t.Fatal(err)
	}
	return m
}

// turnOf is the turn a message started, once it is over.
func (l *loop) turnOf(msg store.Message) store.Turn {
	l.t.Helper()
	var turn store.Turn
	eventually(l.t, func() bool {
		for _, t := range l.turns() {
			if t.TriggerMessageID == msg.ID && t.Status != store.TurnRunning {
				turn = t
				return true
			}
		}
		return false
	}, "the turn for "+msg.Body+" to end")
	return turn
}

// Allowed always, the rule the runtime offered is kept for the member, and
// its next turns carry it: the request is not put to anyone again.
func TestLoop_AllowAlwaysKeepsTheRule(t *testing.T) {
	l := newLoop(t)
	careful := l.asking("Careful", map[string]any{"approval": true, "similar": []any{"Bash(make test)"}})
	first := l.say("@Careful build it", "", careful)
	thread := l.topic(first)
	a := l.waitApproval()
	decided, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}, store.ScopeAlways)
	if err != nil || decided.Scope != store.ScopeAlways {
		t.Fatalf("decided: %+v %v", decided, err)
	}
	l.turnOf(first)
	if root := l.root(thread); root.Body != "@alice Allowed, and the like of it" {
		t.Errorf("the runtime heard %q", root.Body)
	}
	if notes := l.replies(thread.ID, store.SenderSystem); len(notes) != 1 || !strings.Contains(notes[0].Body, "alice allowed Careful to run `make test`, and the like of it from now on") {
		t.Errorf("the note: %+v", notes)
	}
	rules, err := l.s.ListMemberRules(l.ctx, careful.ID, "")
	if err != nil || len(rules) != 1 || rules[0].Rule != "Bash(make test)" || rules[0].Runtime != "fake" || rules[0].ApprovalID != a.ID || rules[0].CreatedBy != l.user.ID {
		t.Fatalf("rules: %+v %v", rules, err)
	}

	second := l.say("@Careful build it again", "", careful)
	turn := l.turnOf(second)
	if root := l.root(l.topic(second)); root.Body != "@alice Allowed by a rule" {
		t.Errorf("the second turn heard %q", root.Body)
	}
	if asked, _ := l.s.ListTurnApprovals(l.ctx, turn.ID); len(asked) != 0 {
		t.Errorf("asked again: %+v", asked)
	}
	if data, _ := os.ReadFile(turn.TranscriptPath); !strings.Contains(string(data), `"allowed_rules":["Bash(make test)"]`) {
		t.Errorf("the spec should carry the rule:\n%s", data)
	}

	// A member that asks nobody is given no rules.
	preset := store.PermissionReadOnly
	if _, err := l.s.UpdateMember(l.ctx, careful.ID, store.MemberPatch{PermissionPreset: &preset}); err != nil {
		t.Fatal(err)
	}
	third := l.say("@Careful build it once more", "", careful)
	l.waitApproval()
	if data, _ := os.ReadFile(l.topicTurn(third).TranscriptPath); strings.Contains(string(data), "allowed_rules") {
		t.Errorf("read_only should carry no rules:\n%s", data)
	}
}

// topicTurn is the turn a message started, running or not.
func (l *loop) topicTurn(msg store.Message) store.Turn {
	l.t.Helper()
	var turn store.Turn
	eventually(l.t, func() bool {
		for _, t := range l.turns() {
			if t.TriggerMessageID == msg.ID {
				turn = t
				return true
			}
		}
		return false
	}, "the turn for "+msg.Body)
	return turn
}

// A command prefix, as Codex offers, is kept too; later the runner settles
// what it covers, and the hub records that as the rule's without a line
// in the thread.
func TestLoop_AllowAlwaysKeepsACommandPrefix(t *testing.T) {
	l := newLoop(t)
	careful := l.asking("Careful", map[string]any{"approval": true, "prefix": []any{"make", "test"}})
	first := l.say("@Careful build it", "", careful)
	a := l.waitApproval()
	if string(a.Similar) != `{"prefix":["make","test"]}` {
		t.Fatalf("the offer: %s", a.Similar)
	}
	if _, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}, store.ScopeAlways); err != nil {
		t.Fatal(err)
	}
	l.turnOf(first)
	if rules, _ := l.s.ListMemberRules(l.ctx, careful.ID, "fake"); len(rules) != 1 || rules[0].Rule != `["make","test"]` {
		t.Fatalf("rules: %+v", rules)
	}

	second := l.say("@Careful build it again", "", careful)
	turn := l.turnOf(second)
	thread := l.topic(second)
	if root := l.root(thread); root.Body != "@alice Allowed by a rule" {
		t.Errorf("the second turn heard %q", root.Body)
	}
	var ruled []store.Approval
	eventually(t, func() bool {
		ruled, _ = l.s.ListTurnApprovals(l.ctx, turn.ID)
		return len(ruled) == 1
	}, "the rule's approval to be recorded")
	if r := ruled[0]; r.Reviewer != store.ReviewerRule || r.Status != store.ApprovalAllowed || r.MessageID != "" || string(r.Answer) != `{"prefix":["make","test"]}` {
		t.Errorf("recorded: %+v (answer %s)", r, r.Answer)
	}
	if notes := l.replies(thread.ID, store.SenderSystem); len(notes) != 0 {
		t.Errorf("nothing should be told in the thread: %+v", notes)
	}
}

// Allowed with the rest of the turn, the turn's later requests go through
// as the person's, each a line in the thread, and the room hears that the
// turn is trusted.
func TestLoop_AllowTheRestOfTheTurn(t *testing.T) {
	l := newLoop(t)
	careful := l.member("Careful", map[string]any{"approval": true, "approvals": float64(3)})
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()
	msg := l.say("@Careful build it", "", careful)
	thread := l.topic(msg)
	a := l.waitApproval()
	decided, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}, store.ScopeTurn)
	if err != nil || decided.Scope != store.ScopeTurn {
		t.Fatalf("decided: %+v %v", decided, err)
	}
	events := collectUntil(t, sub, EventTurnFinished)
	turn := l.turnOf(msg)
	if root := l.root(thread); root.Body != "@alice Allowed 3 of 3" {
		t.Errorf("the runtime heard %q", root.Body)
	}
	if turn.TrustedBy != l.user.ID || turn.TrustedAt == nil {
		t.Errorf("the turn: %+v", turn)
	}
	trusted := 0
	for _, ev := range events {
		if ev.Kind == EventTurnTrust {
			trusted++
			if ev.Turn == nil || ev.Turn.ID != turn.ID || ev.Turn.TrustedBy != l.user.ID {
				t.Errorf("turn_trust: %+v", ev.Turn)
			}
		}
	}
	if trusted != 1 {
		t.Errorf("%d turn_trust events, want 1", trusted)
	}

	all, err := l.s.ListTurnApprovals(l.ctx, turn.ID)
	if err != nil || len(all) != 3 {
		t.Fatalf("approvals: %+v %v", all, err)
	}
	notes := l.replies(thread.ID, store.SenderSystem)
	if len(notes) != 3 || !strings.Contains(notes[0].Body, "alice allowed Careful to run `make test`, and whatever else it asks for the rest of the turn") {
		t.Fatalf("the notes: %+v", notes)
	}
	for i, later := range all[1:] {
		if later.Reviewer != store.ReviewerTurn || later.DecidedBy != l.user.ID || later.Status != store.ApprovalAllowed || later.MessageID != notes[i+1].ID {
			t.Errorf("let through: %+v", later)
		}
		if notes[i+1].Body != "alice allowed Careful to run `make test`, with the rest of the turn" {
			t.Errorf("its line: %q", notes[i+1].Body)
		}
	}
}

// Trusting a turn lets through what it was already waiting on, as the
// person who trusted it; the line for it says so.
func TestLoop_TrustingATurnAllowsWhatWaits(t *testing.T) {
	l := newLoop(t)
	careful := l.member("Careful", map[string]any{"approval": true, "approvals": float64(2), "together": true})
	msg := l.say("@Careful build it", "", careful)
	thread := l.topic(msg)
	var pending []store.Approval
	eventually(t, func() bool {
		pending, _ = l.s.ListPendingRoomApprovals(l.ctx, l.room.ID)
		return len(pending) == 2
	}, "both requests to be pending")
	if _, err := l.h.DecideApproval(l.ctx, pending[0].ID, l.user.ID, runtime.Decision{Allow: true}, store.ScopeTurn); err != nil {
		t.Fatal(err)
	}
	l.turnOf(msg)
	if root := l.root(thread); root.Body != "@alice Allowed 2 of 2" {
		t.Errorf("the runtime heard %q", root.Body)
	}
	other := l.approval(pending[1].ID)
	if other.Status != store.ApprovalAllowed || other.Reviewer != store.ReviewerTurn || other.DecidedBy != l.user.ID {
		t.Errorf("the other request: %+v", other)
	}
	notes := l.replies(thread.ID, store.SenderSystem)
	var bodies []string
	for _, n := range notes {
		bodies = append(bodies, n.Body)
	}
	if len(notes) != 2 || !strings.Contains(strings.Join(bodies, "\n"), "alice allowed Careful to run `make test`, with the rest of the turn") {
		t.Errorf("the notes: %q", bodies)
	}
}

// Taken back, the turn's requests go to people again; only a running turn
// can be.
func TestLoop_UntrustTurn(t *testing.T) {
	l := newLoop(t)
	careful := l.member("Careful", map[string]any{"approval": true, "delay_ms": float64(60000)})
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()
	msg := l.say("@Careful build it", "", careful)
	a := l.waitApproval()
	if _, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}, store.ScopeTurn); err != nil {
		t.Fatal(err)
	}
	collectUntil(t, sub, EventTurnTrust)
	turn := l.topicTurn(msg)
	untrusted, err := l.h.UntrustTurn(l.ctx, turn.ID)
	if err != nil || untrusted.TrustedBy != "" || untrusted.TrustedAt != nil {
		t.Fatalf("untrusted: %+v %v", untrusted, err)
	}
	if ev := collectUntil(t, sub, EventTurnTrust); ev[len(ev)-1].Turn.TrustedBy != "" {
		t.Errorf("turn_trust after taking it back: %+v", ev[len(ev)-1].Turn)
	}
	if err := l.h.CancelTurn(l.ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	l.turnOf(msg)
	if _, err := l.h.UntrustTurn(l.ctx, turn.ID); !errors.Is(err, ErrUnknownTurn) {
		t.Errorf("a turn that is over: %v", err)
	}
}

// A scope reaches no further than the request offers: nothing like it to
// take in, no rule to keep, or no tool's use to trust the turn with.
func TestLoop_ScopesMustBeOffered(t *testing.T) {
	l := newLoop(t)
	plain := l.member("Plain", map[string]any{"approval": true})
	l.say("@Plain build it", "", plain)
	a := l.waitApproval()
	for _, scope := range []store.AllowScope{store.ScopeSimilar, store.ScopeAlways, "forever"} {
		if _, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}, scope); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("%s: %v", scope, err)
		}
	}
	// A denial takes nothing in, whatever it names.
	if denied, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: false}, store.ScopeAlways); err != nil || denied.Scope != store.ScopeOnce {
		t.Errorf("denied: %+v %v", denied, err)
	}

	asker := l.member("Asker", map[string]any{"question": "Which colour?"})
	l.say("@Asker pick", "", asker)
	q := l.waitApproval()
	if _, err := l.h.DecideApproval(l.ctx, q.ID, l.user.ID, runtime.Decision{Allow: true, Answer: []byte(`{"answers":{"1":["red"]}}`)}, store.ScopeTurn); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("trusting the turn on a question: %v", err)
	}
}
