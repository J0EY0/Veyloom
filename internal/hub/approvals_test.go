package hub

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// waitApproval waits for one pending approval to show up in the room.
func (l *loop) waitApproval() store.Approval {
	l.t.Helper()
	var pending []store.Approval
	eventually(l.t, func() bool {
		var err error
		pending, err = l.s.ListPendingRoomApprovals(l.ctx, l.room.ID)
		if err != nil {
			l.t.Fatal(err)
		}
		return len(pending) == 1
	}, "an approval to be pending")
	return pending[0]
}

func (l *loop) approval(id string) store.Approval {
	l.t.Helper()
	a, err := l.s.GetApproval(l.ctx, id)
	if err != nil {
		l.t.Fatal(err)
	}
	return a
}

func TestLoop_ApprovalAllowedRunsTheCommand(t *testing.T) {
	l := newLoop(t)
	careful := l.member("Careful", map[string]any{"approval": true, "reply": "built"})
	msg := l.say("@Careful build it", "", careful)
	thread := l.topic(msg)

	a := l.waitApproval()
	if a.Tool != "Bash" || string(a.Input) != `{"command":"make test"}` || a.MemberID != careful.ID || a.ThreadID != thread.ID {
		t.Fatalf("unexpected approval: %+v", a)
	}
	// The request is announced in the thread and the post is linked.
	notes := l.replies(thread.ID, store.SenderSystem)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "Careful wants to run `make test`") || !strings.Contains(notes[0].Body, a.ID) {
		t.Fatalf("expected an announcement naming the command and the approval, got %+v", notes)
	}
	if a.MessageID != notes[0].ID {
		t.Errorf("approval should link to the announcement %s, got %q", notes[0].ID, a.MessageID)
	}
	if l.turns()[0].Status != store.TurnRunning {
		t.Error("the turn waits while the approval is pending")
	}

	decided, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true})
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != store.ApprovalAllowed || decided.DecidedBy != l.user.ID {
		t.Errorf("unexpected decided approval: %+v", decided)
	}

	turns := l.waitTurns(1, store.TurnDone, "the turn to finish after approval")
	if root := l.root(thread); root.Body != "built" {
		t.Errorf("expected the reply after the allowed command in the root, got %+v", root)
	}
	// The decision rewrites the request note: one line per request.
	notes = l.replies(thread.ID, store.SenderSystem)
	if len(notes) != 1 || notes[0].ID != a.MessageID || !strings.Contains(notes[0].Body, "alice allowed Careful to run `make test`") {
		t.Errorf("expected the request note rewritten with the decision, got %+v", notes)
	}
	data, _ := os.ReadFile(turns[0].TranscriptPath)
	if !strings.Contains(string(data), `"kind":"approval_request"`) || !strings.Contains(string(data), `"kind":"approval_decision"`) || !strings.Contains(string(data), `"status":"allowed"`) {
		t.Errorf("transcript should record the request and the decision:\n%s", data)
	}
	if pending, _ := l.s.ListPendingRoomApprovals(l.ctx, l.room.ID); len(pending) != 0 {
		t.Errorf("nothing should be pending, got %+v", pending)
	}
}

func TestLoop_ApprovalDeniedReachesTheAgent(t *testing.T) {
	l := newLoop(t)
	careful := l.member("Careful", map[string]any{"approval": true})
	msg := l.say("@Careful build it", "", careful)
	thread := l.topic(msg)
	a := l.waitApproval()

	if _, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: false, Message: "not on main"}); err != nil {
		t.Fatal(err)
	}

	l.waitTurns(1, store.TurnDone, "the turn to finish after denial")
	if root := l.root(thread); root.Body != "Denied: not on main" {
		t.Errorf("the agent should see the denial message, got %+v", root)
	}
	notes := l.replies(thread.ID, store.SenderSystem)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "alice denied Careful running `make test`: not on main") {
		t.Errorf("expected the request note rewritten with the denial, got %+v", notes)
	}
	// The note is posted before the decision reaches the agent, so it
	// always reads in order: request, decision, then the closing message
	// the agent posts once it has answered.
	top := l.topLevel()
	closing := top[len(top)-1]
	if closing.SenderKind != store.SenderAgent || closing.Body != "@alice Denied: not on main" {
		t.Errorf("expected a closing message with the denial, got %+v", closing)
	}
	if len(notes) == 1 && notes[0].Seq > closing.Seq {
		t.Error("the decision note should precede the agent's closing message")
	}
	// Only the first decision counts.
	_, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true})
	if !errors.Is(err, store.ErrConflict) {
		t.Errorf("second decision: got %v, want ErrConflict", err)
	}
	if got := l.approval(a.ID); got.Status != store.ApprovalDenied || got.Message != "not on main" {
		t.Errorf("approval = %+v", got)
	}
}

func TestLoop_ApprovalExpiresIntoDenial(t *testing.T) {
	l := newLoopWith(t, Config{ApprovalTimeout: 100 * time.Millisecond})
	careful := l.member("Careful", map[string]any{"approval": true})
	msg := l.say("@Careful build it", "", careful)
	thread := l.topic(msg)
	a := l.waitApproval()

	l.waitTurns(1, store.TurnDone, "the turn to finish after the approval expired")
	if got := l.approval(a.ID); got.Status != store.ApprovalExpired || got.DecidedBy != "" || got.DecidedAt == nil {
		t.Errorf("approval = %+v", got)
	}
	if root := l.root(thread); !strings.Contains(root.Body, "Denied: approval timed out") {
		t.Errorf("the agent should see the timeout as a denial, got %+v", root)
	}
	if notes := l.replies(thread.ID, store.SenderSystem); len(notes) != 1 || !strings.Contains(notes[0].Body, "expired") {
		t.Errorf("expected the request note rewritten with the expiry, got %+v", notes)
	}
	if _, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("deciding an expired approval: got %v, want ErrConflict", err)
	}
}

func TestLoop_CancelWhileApprovalPending(t *testing.T) {
	l := newLoop(t)
	careful := l.member("Careful", map[string]any{"approval": true})
	l.say("@Careful build it", "", careful)
	a := l.waitApproval()

	if err := l.h.CancelTurn(l.ctx, a.TurnID); err != nil {
		t.Fatal(err)
	}

	turns := l.waitTurns(1, store.TurnCancelled, "the turn to be cancelled")
	if got := l.approval(a.ID); got.Status != store.ApprovalCancelled {
		t.Errorf("approval = %+v, want cancelled", got)
	}
	if _, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("deciding a cancelled approval: got %v, want ErrConflict", err)
	}
	data, _ := os.ReadFile(turns[0].TranscriptPath)
	if !strings.Contains(string(data), `"status":"cancelled"`) {
		t.Errorf("transcript should record the cancelled approval:\n%s", data)
	}
}

func TestLoop_DecideUnknownApproval(t *testing.T) {
	l := newLoop(t)
	_, err := l.h.DecideApproval(l.ctx, store.NewID(), l.user.ID, runtime.Decision{Allow: true})
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
	_, err = l.h.DecideApproval(l.ctx, "nope", l.user.ID, runtime.Decision{Allow: true})
	if !errors.Is(err, store.ErrInvalidID) {
		t.Errorf("got %v, want ErrInvalidID", err)
	}
}

func TestDescribeToolUse(t *testing.T) {
	if got := describeToolUse("Bash", `{"command":"make test"}`); got != "`make test`" {
		t.Errorf("bash: %q", got)
	}
	if got := describeToolUse("WebFetch", `{"url":"https://x"}`); got != `WebFetch {"url":"https://x"}` {
		t.Errorf("other tool: %q", got)
	}
	long := strings.Repeat("é", 600)
	if got := describeToolUse("Bash", `{"command":"`+long+`"}`); !strings.HasSuffix(got, "…`") || len(got) > maxToolUseSummary+8 {
		t.Errorf("long command should be cut on a rune boundary, got %d bytes", len(got))
	}
}
