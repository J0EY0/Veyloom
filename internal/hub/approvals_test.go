package hub

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strconv"
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

	decided, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if decided.Status != store.ApprovalAllowed || decided.DecidedBy != l.user.ID {
		t.Errorf("unexpected decided approval: %+v", decided)
	}

	turns := l.waitTurns(1, store.TurnDone, "the turn to finish after approval")
	if root := l.root(thread); root.Body != "@alice built" || !slices.Contains(root.Mentions, store.Mention{Kind: store.MentionUser, ID: l.user.ID}) {
		t.Errorf("expected the reply after the allowed command in the root, addressed to whoever asked, got %+v", root)
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

// A request that offers the like of it is allowed with it when a person
// says so: recorded, told in the thread, and heard by the runtime. One
// that offers nothing is allowed alone, whatever the person asked.
func TestLoop_ApprovalAllowsTheLikeOfIt(t *testing.T) {
	l := newLoop(t)
	careful := l.member("Careful", map[string]any{"approval": true, "similar": []any{"Bash(make test)"}})
	msg := l.say("@Careful build it", "", careful)
	thread := l.topic(msg)
	a := l.waitApproval()
	if string(a.Similar) != `{"rules":["Bash(make test)"]}` {
		t.Fatalf("the offer: %s", a.Similar)
	}
	decided, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true, Similar: true}, "")
	if err != nil || decided.Scope != store.ScopeSimilar {
		t.Fatalf("decided: %+v %v", decided, err)
	}
	l.waitTurns(1, store.TurnDone, "the turn to finish")
	if root := l.root(thread); root.Body != "@alice Allowed, and the like of it" {
		t.Errorf("the runtime heard %q", root.Body)
	}
	notes := l.replies(thread.ID, store.SenderSystem)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "alice allowed Careful to run `make test`, and the like of it for the rest of the turn") {
		t.Errorf("the note: %+v", notes)
	}

	plain := l.member("Plain", map[string]any{"approval": true})
	l.say("@Plain build it", "", plain)
	b := l.waitApproval()
	if len(b.Similar) != 0 {
		t.Fatalf("an offer out of nothing: %s", b.Similar)
	}
	if decided, err := l.h.DecideApproval(l.ctx, b.ID, l.user.ID, runtime.Decision{Allow: true, Similar: true}, ""); err != nil || decided.Scope != store.ScopeOnce {
		t.Errorf("allowed with what was not offered: %+v %v", decided, err)
	}
}

func TestLoop_ApprovalDeniedReachesTheAgent(t *testing.T) {
	l := newLoop(t)
	careful := l.member("Careful", map[string]any{"approval": true})
	msg := l.say("@Careful build it", "", careful)
	thread := l.topic(msg)
	a := l.waitApproval()

	if _, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: false, Message: "not on main"}, ""); err != nil {
		t.Fatal(err)
	}

	l.waitTurns(1, store.TurnDone, "the turn to finish after denial")
	if root := l.root(thread); root.Body != "@alice Denied: not on main" {
		t.Errorf("the agent should see the denial message, got %+v", root)
	}
	notes := l.replies(thread.ID, store.SenderSystem)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "alice denied Careful running `make test`: not on main") {
		t.Errorf("expected the request note rewritten with the denial, got %+v", notes)
	}
	// The answer is addressed to whoever asked, in its topic: the room
	// holds the question and the answer heading the topic, nothing more.
	if top := l.topLevel(); len(top) != 2 || top[0].ID != msg.ID || top[1].ID != thread.RootMessageID {
		t.Errorf("the room should hold the question and the root only, got %+v", top)
	}
	// Only the first decision counts.
	_, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}, "")
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
	if _, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}, ""); !errors.Is(err, store.ErrConflict) {
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
	if _, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true}, ""); !errors.Is(err, store.ErrConflict) {
		t.Errorf("deciding a cancelled approval: got %v, want ErrConflict", err)
	}
	data, _ := os.ReadFile(turns[0].TranscriptPath)
	if !strings.Contains(string(data), `"status":"cancelled"`) {
		t.Errorf("transcript should record the cancelled approval:\n%s", data)
	}
}

func TestLoop_DecideUnknownApproval(t *testing.T) {
	l := newLoop(t)
	_, err := l.h.DecideApproval(l.ctx, store.NewID(), l.user.ID, runtime.Decision{Allow: true}, "")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
	_, err = l.h.DecideApproval(l.ctx, "nope", l.user.ID, runtime.Decision{Allow: true}, "")
	if !errors.Is(err, store.ErrInvalidID) {
		t.Errorf("got %v, want ErrInvalidID", err)
	}
}

func TestDescribeToolUse(t *testing.T) {
	if got := describeToolUse("Bash", `{"command":"make test"}`); got != "`make test`" {
		t.Errorf("bash: %q", got)
	}
	if got := describeToolUse("commandExecution", `{"command":"touch a.txt","cwd":"/repo"}`); got != "`touch a.txt`" {
		t.Errorf("codex command: %q", got)
	}
	if got := describeToolUse("WebFetch", `{"url":"https://x"}`); got != `WebFetch {"url":"https://x"}` {
		t.Errorf("other tool: %q", got)
	}
	long := strings.Repeat("é", 600)
	if got := describeToolUse("Bash", `{"command":"`+long+`"}`); !strings.HasSuffix(got, "…`") || len(got) > maxToolUseSummary+8 {
		t.Errorf("long command should be cut on a rune boundary, got %d bytes", len(got))
	}
}

// A request the runtime settled with a reviewer of its own is recorded
// already decided and told in the thread; a notice reaches the transcript.
// Nobody is asked and nothing goes back to the machine.
func TestLoop_ReviewedApprovalAndNoticeAreShown(t *testing.T) {
	l := newLoop(t)
	auto := l.member("Auto", map[string]any{"reviewed": "allowed", "notice": "config.toml: unknown key", "reply": "fetched", "preamble": "Checking first.", "tool": true})
	strict := l.member("Strict", map[string]any{"reviewed": "denied", "reply": "stopped"})

	thread := l.topic(l.say("@Auto fetch it", "", auto))
	turns := l.waitTurns(1, store.TurnDone, "the reviewed turn to finish")
	if pending, _ := l.s.ListPendingRoomApprovals(l.ctx, l.room.ID); len(pending) != 0 {
		t.Fatalf("nothing should wait for a person, got %+v", pending)
	}
	all, err := l.s.ListTurnApprovals(l.ctx, turns[0].ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("turn approvals = %+v, %v", all, err)
	}
	a := all[0]
	if a.Status != store.ApprovalAllowed || a.Reviewer != "fake_review" || a.DecidedBy != "" || a.Message != "the fake reviewer's reasons" || string(a.Answer) != `{"risk":"low"}` {
		t.Errorf("reviewed approval = %+v", a)
	}
	notes := l.replies(thread.ID, store.SenderSystem)
	if len(notes) != 1 || notes[0].ID != a.MessageID || !strings.Contains(notes[0].Body, "fake_review allowed Auto to run `curl -sI https://example.com`: the fake reviewer's reasons") {
		t.Errorf("the thread should say who decided and why, got %+v", notes)
	}
	// In the order it happened: what was said before, the verdict, then
	// what was said after, though the turn never waited for the verdict.
	if root := l.root(thread); root.Body != "Checking first." {
		t.Errorf("root = %q, want what the agent said first", root.Body)
	}
	said, err := l.s.ListThreadMessages(l.ctx, thread.ID, 0, 200)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, m := range said {
		if m.ID != thread.RootMessageID {
			order = append(order, string(m.SenderKind)+": "+m.Body)
		}
	}
	if len(order) != 2 || !strings.HasPrefix(order[0], "system: fake_review allowed") || order[1] != "agent: @alice fetched" {
		t.Errorf("thread after the root = %q, want the verdict and then the reply", order)
	}
	data, _ := os.ReadFile(turns[0].TranscriptPath)
	for _, want := range []string{`"kind":"notice"`, `"level":"warning"`, "config.toml: unknown key", `"reviewer":"fake_review"`, `"verdict":"allowed"`, `"kind":"approval_decision"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("transcript should have %s:\n%s", want, data)
		}
	}

	denied := l.topic(l.say("@Strict fetch it", "", strict))
	l.waitTurns(2, store.TurnDone, "the denied turn to finish")
	notes = l.replies(denied.ID, store.SenderSystem)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "fake_review denied Strict running `curl -sI https://example.com`") {
		t.Errorf("the thread should say the reviewer denied it, got %+v", notes)
	}
}

// A question is answered on its card; what reaches the agent is the answer
// as given, what is kept leaves the secret out.
func TestLoop_QuestionAnsweredAndDeclined(t *testing.T) {
	l := newLoop(t)
	asker := l.member("Asker", map[string]any{"question": "Which colour?"})

	thread := l.topic(l.say("@Asker pick one", "", asker))
	a := l.waitApproval()
	var set runtime.QuestionSet
	if err := json.Unmarshal(a.Input, &set); err != nil || a.Kind != store.ApprovalQuestion || a.Tool != "AskUserQuestion" || len(set.Questions) != 2 || !set.Questions[1].Secret {
		t.Fatalf("question approval = %+v (%v)", a, err)
	}
	notes := l.replies(thread.ID, store.SenderSystem)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "Asker asks “Which colour?” and 1 more") {
		t.Errorf("the thread should say what is asked, got %+v", notes)
	}
	decided, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true, Answer: json.RawMessage(`{"answers":{"1":["blue"],"2":["hunter2"]}}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(decided.Answer) != `{"answers":{"1":["blue"],"2":["••••••"]}}` {
		t.Errorf("kept answer = %s, want the secret left out", decided.Answer)
	}
	l.waitTurns(1, store.TurnDone, "the answered turn to finish")
	if root := l.root(thread); root.Body != "@alice Answer: blue (passphrase of 7 characters)" {
		t.Errorf("root = %q: the agent should get the answers as given", root.Body)
	}
	if notes := l.replies(thread.ID, store.SenderSystem); len(notes) != 1 || !strings.Contains(notes[0].Body, "alice answered Asker: “Which colour?” and 1 more") {
		t.Errorf("the note should say who answered, got %+v", notes)
	}

	declined := l.topic(l.say("@Asker pick again", "", asker))
	b := l.waitApproval()
	if _, err := l.h.DecideApproval(l.ctx, b.ID, l.user.ID, runtime.Decision{Allow: false, Message: "later"}, ""); err != nil {
		t.Fatal(err)
	}
	l.waitTurns(2, store.TurnDone, "the declined turn to finish")
	if root := l.root(declined); root.Body != "@alice No answer: later" {
		t.Errorf("root = %q, want the agent told no answer came", root.Body)
	}
	if notes := l.replies(declined.ID, store.SenderSystem); len(notes) != 1 || !strings.Contains(notes[0].Body, "alice declined to answer Asker: “Which colour?” and 1 more: later") {
		t.Errorf("the note should say it was declined, got %+v", notes)
	}
}

// An MCP server's form is filled in on its card and comes back as content;
// a link it asks to be opened can be declined.
func TestLoop_FormFilledAndLinkDeclined(t *testing.T) {
	l := newLoop(t)
	filler := l.member("Filler", map[string]any{"form": "Deploy where?"})
	opener := l.member("Opener", map[string]any{"link": "https://login.example.com/device"})

	thread := l.topic(l.say("@Filler deploy", "", filler))
	a := l.waitApproval()
	if a.Kind != store.ApprovalForm || a.Tool != "elicitation" || !strings.Contains(string(a.Input), `"message":"Deploy where?"`) {
		t.Fatalf("form approval = %+v", a)
	}
	if notes := l.replies(thread.ID, store.SenderSystem); len(notes) != 1 || !strings.Contains(notes[0].Body, "Filler needs a form filled in: fake “Deploy where?”") {
		t.Errorf("the thread should say a form waits, got %+v", notes)
	}
	decided, err := l.h.DecideApproval(l.ctx, a.ID, l.user.ID, runtime.Decision{Allow: true, Answer: json.RawMessage(`{"content":{"name":"alice","size":"l"}}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if string(decided.Answer) != `{"content":{"name":"alice","size":"l"}}` {
		t.Errorf("kept answer = %s", decided.Answer)
	}
	l.waitTurns(1, store.TurnDone, "the form turn to finish")
	if root := l.root(thread); root.Body != `@alice Form: {"name":"alice","size":"l"}` {
		t.Errorf("root = %q, want the form's content back", root.Body)
	}
	if notes := l.replies(thread.ID, store.SenderSystem); len(notes) != 1 || !strings.Contains(notes[0].Body, "alice filled in Filler's form") {
		t.Errorf("the note should say who filled it in, got %+v", notes)
	}

	linked := l.topic(l.say("@Opener sign in", "", opener))
	b := l.waitApproval()
	if b.Kind != store.ApprovalLink || !strings.Contains(string(b.Input), "https://login.example.com/device") {
		t.Fatalf("link approval = %+v", b)
	}
	if _, err := l.h.DecideApproval(l.ctx, b.ID, l.user.ID, runtime.Decision{Allow: false, Message: "later"}, ""); err != nil {
		t.Fatal(err)
	}
	l.waitTurns(2, store.TurnDone, "the link turn to finish")
	if root := l.root(linked); root.Body != "@alice No link: later" {
		t.Errorf("root = %q", root.Body)
	}
	if notes := l.replies(linked.ID, store.SenderSystem); len(notes) != 1 || !strings.Contains(notes[0].Body, "alice declined Opener's link: fake “Sign in to continue” at https://login.example.com/device: later") {
		t.Errorf("the note should say it was declined, got %+v", notes)
	}
}

// A request the runtime takes back is closed as withdrawn and the thread
// says so, whether the hub had recorded it by then or not.
func TestLoop_WithdrawnApprovalIsClosed(t *testing.T) {
	for _, wait := range []int{0, 300} {
		t.Run(strconv.Itoa(wait)+"ms", func(t *testing.T) {
			l := newLoop(t)
			m := l.member("Waverer", map[string]any{"withdraw_ms": wait, "delay_ms": 1500})
			thread := l.topic(l.say("@Waverer build", "", m))
			// The turn runs a second and a half on purpose: time enough to
			// take the request back while it waits.
			var turns []store.Turn
			eventuallyWithin(t, 5*time.Second, func() bool {
				turns = l.turns()
				return len(turns) == 1 && turns[0].Status == store.TurnDone
			}, "the turn to finish")
			approvals, err := l.s.ListTurnApprovals(l.ctx, turns[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(approvals) != 1 || approvals[0].Status != store.ApprovalCancelled || approvals[0].Message != withdrawnReason || approvals[0].DecidedBy != "" {
				t.Fatalf("approvals = %+v, want the one request closed as withdrawn", approvals)
			}
			if notes := l.replies(thread.ID, store.SenderSystem); len(notes) != 1 || notes[0].Body != "Waverer took back its request to run `make test`" {
				t.Errorf("notes = %+v", notes)
			}
			if root := l.root(thread); root.Body != "@alice Withdrawn" {
				t.Errorf("root = %q", root.Body)
			}
		})
	}
}

// A plan put up for approval is named as a plan, by its first line.
func TestNotes_Plans(t *testing.T) {
	input := `{"plan":"\n# Tidy up the parser\n1. Remove dead code"}`
	p := &pendingApproval{kind: store.ApprovalToolUse, tool: "ExitPlanMode", input: input}
	for got, want := range map[string]string{
		askedNote(store.ApprovalToolUse, "Claude", "ExitPlanMode", input, "a1"):             "Claude asks for its plan “Tidy up the parser” to be approved (approval a1 pending)",
		decidedNote(p, "alice", "Claude", runtime.Decision{Allow: true}, ""):                "alice approved Claude's plan “Tidy up the parser”",
		decidedNote(p, "alice", "Claude", runtime.Decision{Message: "smaller, please"}, ""): "alice sent back Claude's plan “Tidy up the parser”: smaller, please",
		expiredNote(p, "Claude", "nobody decided within 1h0m0s"):                            "Claude's plan “Tidy up the parser” went unapproved: nobody decided within 1h0m0s",
		withdrawnNote(p, "Claude"):  "Claude took back its plan “Tidy up the parser”",
		describePlan(`{"plan":""}`): "(no plan given)",
	} {
		if got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	}
}

// An extension's yes-or-no question is named as one, by its title and
// message.
func TestNotes_Confirmations(t *testing.T) {
	input := `{"title":"Dangerous command","message":"Allow rm -rf build?"}`
	p := &pendingApproval{kind: store.ApprovalToolUse, tool: "confirm", input: input}
	for got, want := range map[string]string{
		askedNote(store.ApprovalToolUse, "Pi", "confirm", input, "a1"):   "Pi asks you to confirm “Dangerous command: Allow rm -rf build?” (approval a1 pending)",
		decidedNote(p, "alice", "Pi", runtime.Decision{Allow: true}, ""): "alice confirmed “Dangerous command: Allow rm -rf build?” for Pi",
		decidedNote(p, "alice", "Pi", runtime.Decision{}, ""):            "alice did not confirm “Dangerous command: Allow rm -rf build?” for Pi",
		expiredNote(p, "Pi", "nobody decided within 1h0m0s"):             "Pi's “Dangerous command: Allow rm -rf build?” went unconfirmed: nobody decided within 1h0m0s",
		withdrawnNote(p, "Pi"):                               "Pi took back “Dangerous command: Allow rm -rf build?”",
		describeConfirm(`{"title":"","message":"Proceed?"}`): "“Proceed?”",
	} {
		if got != want {
			t.Errorf("got  %q\nwant %q", got, want)
		}
	}
}
