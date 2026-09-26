package hub

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// sumUpNotes are the notes in the topic asking a member to sum up.
func (l *loop) sumUpNotes(threadID string) []store.Message {
	l.t.Helper()
	var out []store.Message
	for _, m := range l.replies(threadID, store.SenderSystem) {
		if strings.Contains(m.Body, "handed on is done") {
			out = append(out, m)
		}
	}
	return out
}

// The member a person asked hands work on to two members at once; once
// both are done, it sums up for the person, once, in the topic it was
// asked in (docs/design.md 5.22).
func TestLoop_TheMemberAskedSumsUpWhatItHandedOn(t *testing.T) {
	l := newLoop(t)
	lead := l.member("Lead", map[string]any{
		"tool_calls":    []any{sendCall("@Coder @Tester split the login work", "room")},
		"reply":         "Sent.",
		"summary_reply": "Both are done.",
	})
	coder := l.member("Coder", map[string]any{"reply": "Mine is built."})
	tester := l.member("Tester", map[string]any{"reply": "Tests pass."})
	msg := l.say("@Lead organise it", "", lead)
	turns := l.settle(4, "the work handed on and its summing up")
	thread := l.topic(msg)

	sum := turns[0]
	if sum.MemberID != lead.ID || sum.ThreadID != thread.ID || sum.ChainMessageID != msg.ID || sum.WokenByTurnID != "" {
		t.Fatalf("the summing up: %+v", sum)
	}
	for _, turn := range turns[1:3] {
		if turn.MemberID != coder.ID && turn.MemberID != tester.ID {
			t.Errorf("before the summing up: %+v", turn)
		}
	}
	notes := l.sumUpNotes(thread.ID)
	if len(notes) != 1 || notes[0].Body != "The work Lead handed on is done (Coder, Tester); back to Lead." && notes[0].Body != "The work Lead handed on is done (Tester, Coder); back to Lead." {
		t.Fatalf("the note: %+v", notes)
	}
	if sum.TriggerMessageID != notes[0].ID {
		t.Errorf("the summing up answers the note, not %s", sum.TriggerMessageID)
	}
	prompt := specOf(t, sum).Prompt
	for _, want := range []string{runtime.HandedOnHeading, "- Coder (topic #2): Mine is built.", "- Tester (topic #2): Tests pass.", "Otherwise sum it up for the person who asked you"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the brief lacks %q:\n%s", want, prompt)
		}
	}
	// Its last word summing up reaches the person, and nothing else does:
	// neither what it said as it handed the work on, nor the others'.
	if root := l.root(thread); root.Body != "Sent." || len(root.Mentions) != 0 {
		t.Errorf("what it said as it handed the work on: %+v", root)
	}
	said := l.replies(thread.ID, store.SenderAgent)
	last := said[len(said)-1]
	if last.Body != "@alice Both are done." || !slices.Contains(last.Mentions, store.Mention{Kind: store.MentionUser, ID: l.user.ID}) {
		t.Errorf("the summing up says: %+v", last)
	}
	for _, turn := range turns[1:3] {
		reply, err := l.s.GetMessage(l.ctx, turn.ReplyMessageID)
		if err != nil || strings.HasPrefix(reply.Body, "@alice") {
			t.Errorf("a woken member's answer: %+v %v", reply, err)
		}
	}
}

// Naming a member in the reply hands the work on too: it is summed up the
// same way. Summing up, naming it again only tells of it.
func TestLoop_AHandOffIsSummedUp(t *testing.T) {
	l := newLoop(t)
	hander := l.member("Hander", map[string]any{"reply": "@Echo take a look", "summary_reply": "@Echo looked: fine."})
	echo := l.member("Echo", map[string]any{"reply": "Looked, fine."})
	msg := l.say("@Hander check it", "", hander)
	turns := l.settle(3, "the hand-off and its summing up")
	thread := l.topic(msg)
	if turns[1].MemberID != echo.ID || turns[0].MemberID != hander.ID || turns[0].ThreadID != thread.ID {
		t.Fatalf("the turns: %+v", turns)
	}
	said := l.replies(thread.ID, store.SenderAgent)
	if last := said[len(said)-1]; last.Body != "@alice @Echo looked: fine." {
		t.Errorf("the summing up says: %+v", last)
	}
}

// Work handed on down the line is summed up once, by the member the person
// asked, when the last of it is done.
func TestLoop_WorkHandedOnDownTheLineIsSummedUpOnce(t *testing.T) {
	l := newLoop(t)
	lead := l.member("Lead", map[string]any{"tool_calls": []any{sendCall("@Coder build the API", "")}, "reply": "Handed on.", "summary_reply": "Built and tested."})
	coder := l.member("Coder", map[string]any{"tool_calls": []any{sendCall("@Tester test the API", "")}, "reply": "Built.", "summary_reply": "Coder never sums up."})
	// Tester names Coder, which handed it the work: that only reports back.
	tester := l.member("Tester", map[string]any{"reply": "@Coder tested, all good."})
	msg := l.say("@Lead get the API done", "", lead)
	turns := l.settle(4, "the line and its summing up")
	thread := l.topic(msg)
	if turns[0].MemberID != lead.ID || !slices.ContainsFunc(turns, func(t store.Turn) bool { return t.MemberID == coder.ID }) || !slices.ContainsFunc(turns, func(t store.Turn) bool { return t.MemberID == tester.ID }) {
		t.Fatalf("the turns: %+v", turns)
	}
	if n := len(l.sumUpNotes(thread.ID)); n != 1 {
		t.Errorf("%d summings up asked for", n)
	}
	if prompt := specOf(t, turns[0]).Prompt; !strings.Contains(prompt, "- Coder (topic #1): Built.") || !strings.Contains(prompt, "- Tester (topic #1): @Coder tested, all good.") {
		t.Errorf("the brief:\n%s", prompt)
	}
	testerTurn, _ := turnOf(turns, tester, store.TurnChat)
	if prompt := specOf(t, testerTurn).Prompt; !strings.Contains(prompt, "Coder handed you this work, part of what alice asked Lead for.") || !strings.Contains(prompt, "Naming Coder or Lead does not wake them") {
		t.Errorf("Tester's brief:\n%s", prompt)
	}
}

// A wake the limits hold back leaves the work to the person: no summing
// up, the note of the hold reached them already.
func TestLoop_WorkHeldBackIsNotSummedUp(t *testing.T) {
	l := newLoop(t)
	l.setRelayLimit(1)
	lead := l.member("Lead", map[string]any{"tool_calls": []any{sendCall("@Coder build the API", "")}, "reply": "Handed on.", "summary_reply": "Should not be said."})
	// Coder hands on to Tester: the second wake of the piece of work, held.
	l.member("Coder", map[string]any{"reply": "@Tester test it"})
	l.member("Tester", map[string]any{"reply": "Tested."})
	msg := l.say("@Lead get it done", "", lead)
	l.settle(2, "the work, held back once")
	thread := l.topic(msg)
	if len(l.holds(thread.ID)) != 1 || len(l.sumUpNotes(thread.ID)) != 0 {
		t.Errorf("holds %d, summings up %d", len(l.holds(thread.ID)), len(l.sumUpNotes(thread.ID)))
	}
}

// Where agents wake no one, naming a member hands nothing on: the turn's
// last word is addressed to the person as it would be.
func TestLoop_NamingHandsNothingOnWhereAgentsWakeNoOne(t *testing.T) {
	l := newLoop(t)
	l.setRelayLimit(-1)
	hander := l.member("Hander", map[string]any{"tool": true, "reply": "@Echo take a look"})
	l.member("Echo", map[string]any{"reply": "Looked."})
	msg := l.say("@Hander check it", "", hander)
	l.settle(1, "Hander alone")
	if root := l.root(l.topic(msg)); root.Body != "@alice @Echo take a look" || !slices.Contains(root.Mentions, store.Mention{Kind: store.MentionUser, ID: l.user.ID}) {
		t.Errorf("the last word: %+v", root)
	}
}

// A member the work was handed on to that names the member asked reports
// back: that wakes it not, neither by message nor in the answer; what it
// said comes back with the rest when all of it is done. The member asked
// is not among those it hears from.
func TestLoop_NamingTheMemberAskedReportsBack(t *testing.T) {
	l := newLoop(t)
	lead := l.member("Lead", map[string]any{"tool_calls": []any{sendCall("@Coder build the API", "")}, "reply": "Handed on.", "summary_reply": "Summed up."})
	coder := l.member("Coder", map[string]any{"tool_calls": []any{sendCall("@Lead the API is built", "")}, "reply": "@Lead Built. Low or high by default?"})
	msg := l.say("@Lead get it done", "", lead)
	// Lead, Coder, and Lead summing up: nothing more.
	turns := l.settle(3, "the work and its summing up")
	thread := l.topic(msg)
	if turns[1].MemberID != coder.ID || turns[0].MemberID != lead.ID || turns[0].WokenByTurnID != "" {
		t.Fatalf("the turns: %+v", turns)
	}
	if tx := transcriptOf(t, turns[1]); !strings.Contains(tx, "Not woken: Lead (it handed you the work: your answer reaches it once all of it is done") {
		t.Errorf("what Coder was told:\n%s", tx)
	}
	coderSpec := specOf(t, turns[1])
	if !strings.Contains(coderSpec.Prompt, "Lead handed on this work for alice, and you take part in it.") || strings.Contains(coderSpec.Prompt, "you are woken once with what they came to") {
		t.Errorf("Coder's brief:\n%s", coderSpec.Prompt)
	}
	if notes := l.sumUpNotes(thread.ID); len(notes) != 1 || notes[0].Body != "The work Lead handed on is done (Coder); back to Lead." {
		t.Errorf("the note: %+v", notes)
	}
	prompt := specOf(t, turns[0]).Prompt
	if !strings.Contains(prompt, "- Coder (topic #1): @Lead Built. Low or high by default?") || !strings.Contains(prompt, "answer a question") {
		t.Errorf("the brief summing up:\n%s", prompt)
	}
	if leadSpec := specOf(t, turns[2]); !strings.Contains(leadSpec.Prompt, "you are woken once with what they came to") {
		t.Errorf("the brief of the member asked:\n%s", leadSpec.Prompt)
	}
}

// Under the topic a person's ask began in, the whole piece of work is
// counted, across the topics the members worked in: all its turns, from
// the first one's start to the last one's end. The members' own topics
// count their own turns only. The events of each turn carry the piece of
// work as it stands.
func TestLoop_TheTopicAskedInCountsTheWholeWork(t *testing.T) {
	l := newLoop(t)
	sub := l.h.Subscribe(l.room.ID)
	defer sub.Close()
	lead := l.member("Lead", map[string]any{
		"tool_calls":    []any{sendCall("@Coder @Tester split the login work", "room")},
		"reply":         "Sent.",
		"summary_reply": "Both are done.",
	})
	l.member("Coder", map[string]any{"reply": "Mine is built."})
	l.member("Tester", map[string]any{"reply": "Tests pass."})
	msg := l.say("@Lead organise it", "", lead)
	turns := l.settle(4, "the work handed on and its summing up")
	thread := l.topic(msg)

	var roots []string
	for _, m := range l.topLevel() {
		if m.SenderKind == store.SenderAgent {
			roots = append(roots, m.ID)
		}
	}
	summaries, err := l.s.ThreadSummaries(l.ctx, roots)
	if err != nil {
		t.Fatal(err)
	}
	var whole *store.WorkSummary
	for _, s := range summaries {
		if s.ID == thread.ID {
			whole = s.Work
		} else if s.Work != nil {
			t.Errorf("a member's topic counts the whole work: %+v", s)
		}
	}
	first, last := turns[len(turns)-1], turns[0]
	if whole == nil || whole.Turns != 4 || whole.Running || whole.Chain != msg.ID || whole.ThreadID != thread.ID ||
		!whole.StartedAt.Equal(first.StartedAt) || whole.EndedAt == nil || !whole.EndedAt.Equal(*last.EndedAt) {
		t.Fatalf("the whole work: %+v, turns %+v", whole, turns)
	}

	work, err := l.s.ChainWork(l.ctx, msg.ID)
	if err != nil || work.ThreadNumber != thread.Number || len(work.Members) != 3 || work.Members[0] != lead.ID || work.Turns != 4 {
		t.Errorf("the piece of work as a topic reads it: %+v %v", work, err)
	}

	var seen []store.WorkSummary
	for len(seen) < 8 {
		select {
		case ev := <-sub.Events():
			if (ev.Kind == EventTurnStarted || ev.Kind == EventTurnFinished) && ev.Work != nil {
				seen = append(seen, *ev.Work)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("the turn events carried %d pieces of work: %+v", len(seen), seen)
		}
	}
	if w := seen[0]; w.Turns != 1 || !w.Running || w.ThreadID != thread.ID {
		t.Errorf("as the first turn began: %+v", w)
	}
	if w := seen[len(seen)-1]; w.Turns != 4 || w.Running || w.EndedAt == nil {
		t.Errorf("as the last turn ended: %+v", w)
	}
}

// Asked at once with another member, one that hands work on to it in the
// topic hears back from it, and the person does not: their inbox gets each
// member's answer to what it was asked, and the summing up, no more
// (docs/webui.md 4.17).
func TestLoop_AWakeInATopicTwoWereAskedInReachesThePersonOnce(t *testing.T) {
	l := newLoop(t)
	tester := l.member("Tester", map[string]any{"reply": "tested"})
	coder := l.member("Coder", map[string]any{"tool": true, "reply": "built, @Tester please test it", "summary_reply": "all done"})
	l.say("@Coder build it, @Tester check the docs", "", coder, tester)
	l.settle(4, "Coder's and Tester's turns, Tester's woken turn and Coder summing up")

	inbox, err := l.s.ListUserMentions(l.ctx, l.user.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, item := range inbox {
		bodies = append(bodies, item.Body)
	}
	slices.Sort(bodies)
	if !slices.Equal(bodies, []string{"@alice all done", "@alice tested"}) {
		t.Errorf("the inbox: %q", bodies)
	}
}
