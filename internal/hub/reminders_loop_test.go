package hub

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// remindCall is a fake turn's set_reminder call.
func remindCall(note, in string) map[string]any {
	return map[string]any{"tool": runtime.MessageToolRemind, "args": map[string]any{"note": note, "in": in}}
}

// quickReminders lets the loop's reminders come due as soon as a turn
// likes, not a minute on at least. Set before a turn sets one.
func (l *loop) quickReminders() {
	l.h.turns.reminders.min = 0
}

// reminders are the reminders set in a topic, in the order set.
func (l *loop) reminders(threadID string) []store.Reminder {
	l.t.Helper()
	rs, err := l.s.ListThreadReminders(l.ctx, threadID)
	if err != nil {
		l.t.Fatal(err)
	}
	return rs
}

// reminder is the one reminder set in a topic, once it is as want says.
func (l *loop) reminder(threadID string, want store.ReminderStatus) store.Reminder {
	l.t.Helper()
	var r store.Reminder
	eventually(l.t, func() bool {
		rs := l.reminders(threadID)
		if len(rs) == 1 {
			r = rs[0]
		}
		return len(rs) == 1 && rs[0].Status == want
	}, "the reminder to be "+string(want))
	return r
}

// A member's reminder wakes it in the topic it set it in, once due, as
// the turn that set it would: in its piece of work, for its person, the
// topic told when it was set and when it came due (design.md 5.23.4).
func TestLoop_AReminderWakesTheMemberInItsTopic(t *testing.T) {
	l := newLoop(t)
	l.quickReminders()
	slow := l.member("Slow", map[string]any{"tool_calls": []any{remindCall("check how CI went", "1s")}, "reply": "CI is running; I will look."})
	msg := l.say("@Slow run CI and see how it goes", "", slow)
	first := l.waitTurns(1, store.TurnDone, "the turn setting the reminder")[0]
	l.setOptions(slow, map[string]any{"reply": "CI is green."})
	thread := l.topic(msg)
	set := l.reminder(thread.ID, store.ReminderPending)
	if set.MemberID != slow.ID || set.TurnID != first.ID || set.Note != "check how CI went" || set.SetMessageID == "" ||
		time.Until(set.DueAt) > time.Second || time.Until(set.DueAt) < -time.Second {
		t.Errorf("reminder = %+v", set)
	}
	if tx := transcriptOf(t, first); !strings.Contains(tx, "Reminder "+set.ID+" set for ") || !strings.Contains(tx, "from now: you are woken in this topic then") {
		t.Errorf("what the member was told:\n%s", tx)
	}

	turns := l.settle(2, "the turn the reminder wakes")
	woken := turns[0]
	fired := l.reminder(thread.ID, store.ReminderFired)
	if fired.FiredMessageID == "" || woken.TriggerMessageID != fired.FiredMessageID || woken.ThreadID != thread.ID {
		t.Fatalf("the woken turn %+v, the reminder %+v", woken, fired)
	}
	if woken.WokenByTurnID != first.ID || woken.ChainMessageID != msg.ID {
		t.Errorf("the wake carries on the piece of work of the turn that set it: %+v", woken)
	}
	notes := l.notes(thread.ID)
	if countContaining(notes, "Slow set a reminder for ") != 1 || countContaining(notes, "Slow's reminder, due ") != 1 || countContaining(notes, ": check how CI went") != 2 {
		t.Errorf("the topic is told: %q", notes)
	}
	if prompt := specOf(t, woken).Prompt; !strings.Contains(prompt, ">> ") || !strings.Contains(prompt, "Slow's reminder, due ") {
		t.Errorf("the woken turn answers the reminder:\n%s", prompt)
	}
	last := slices.IndexFunc(l.replies(thread.ID, store.SenderAgent), func(m store.Message) bool {
		return m.TurnID == woken.ID && slices.Contains(m.Mentions, store.Mention{Kind: store.MentionUser, ID: l.user.ID})
	})
	if last < 0 {
		t.Errorf("what it came to reaches the person: %+v", l.replies(thread.ID, store.SenderAgent))
	}
}

// A person, or the member, takes a reminder back before it is due: it
// wakes no one, and taking it back twice is a conflict.
func TestLoop_AReminderTakenBackWakesNoOne(t *testing.T) {
	l := newLoop(t)
	l.quickReminders()
	slow := l.member("Slow", map[string]any{"tool_calls": []any{remindCall("look again", "1s")}, "reply": "Set."})
	msg := l.say("@Slow look at it in a while", "", slow)
	l.waitTurns(1, store.TurnDone, "the turn setting the reminder")
	l.setOptions(slow, map[string]any{"reply": "Looked."})
	thread := l.topic(msg)
	r := l.reminder(thread.ID, store.ReminderPending)
	cancelled, err := l.h.CancelReminder(l.ctx, r.ID, l.user.ID)
	if err != nil || cancelled.Status != store.ReminderCancelled || cancelled.CancelledBy != l.user.ID {
		t.Fatalf("cancelled = %+v, %v", cancelled, err)
	}
	var problem *store.Problem
	if _, err := l.h.CancelReminder(l.ctx, r.ID, l.user.ID); !errors.As(err, &problem) || problem.Code != "reminderSettled" {
		t.Errorf("a second cancel: %v", err)
	}
	time.Sleep(1500 * time.Millisecond)
	if n := len(l.turns()); n != 1 {
		t.Errorf("a cancelled reminder wakes no one: %d turns", n)
	}

	// The member's own, by its id.
	l.setOptions(slow, map[string]any{"tool_calls": []any{remindCall("look once more", "1h")}, "reply": "Set again."})
	l.say("@Slow once more, later", thread.ID, slow)
	l.waitTurns(2, store.TurnDone, "the turn setting another")
	rs := l.reminders(thread.ID)
	if len(rs) != 2 || rs[1].Status != store.ReminderPending {
		t.Fatalf("reminders = %+v", rs)
	}
	l.setOptions(slow, map[string]any{"tool_calls": []any{
		map[string]any{"tool": runtime.MessageToolCancelReminder, "args": map[string]any{"id": rs[1].ID}},
		map[string]any{"tool": runtime.MessageToolCancelReminder, "args": map[string]any{"id": rs[0].ID}},
	}})
	l.say("@Slow never mind", thread.ID, slow)
	third := l.waitTurns(3, store.TurnDone, "the turn taking it back")[0]
	if rs := l.reminders(thread.ID); rs[1].Status != store.ReminderCancelled || rs[1].CancelledBy != "" {
		t.Errorf("the member's own cancel: %+v", rs[1])
	}
	tx := transcriptOf(t, third)
	if !strings.Contains(tx, "Reminder "+rs[1].ID+" cancelled.") || !strings.Contains(tx, "reminder "+rs[0].ID+" is cancelled already") {
		t.Errorf("what the member was told:\n%s", tx)
	}
}

// What a reminder may be: a note, in or at, from a minute to 30 days
// ahead; five a turn and ten not yet due a member. The brief lists them.
func TestLoop_RemindersAreCheckedAndCounted(t *testing.T) {
	l := newLoop(t)
	at := func(d time.Duration) map[string]any {
		return map[string]any{"tool": runtime.MessageToolRemind, "args": map[string]any{"note": "later", "at": time.Now().Add(d).Format(time.RFC3339)}}
	}
	calls := []any{
		map[string]any{"tool": runtime.MessageToolRemind, "args": map[string]any{"in": "1h"}},
		map[string]any{"tool": runtime.MessageToolRemind, "args": map[string]any{"note": "x", "in": "1h", "at": "2030-01-01T00:00:00Z"}},
		remindCall("too soon", "30s"),
		remindCall("not a span", "soon"),
		at(31 * 24 * time.Hour),
		map[string]any{"tool": runtime.MessageToolRemind, "args": map[string]any{"note": "no zone", "at": "2030-01-01T09:00:00"}},
	}
	for range remindersPerTurn + 1 {
		calls = append(calls, remindCall("check the nightly build", "1d12h"))
	}
	slow := l.member("Slow", map[string]any{"tool_calls": calls, "reply": "Set."})
	msg := l.say("@Slow keep an eye on the builds", "", slow)
	first := l.waitTurns(1, store.TurnDone, "the turn setting them")[0]
	tx := transcriptOf(t, first)
	for _, want := range []string{
		"set_reminder needs a note", "set_reminder takes in or at, one of them", "a reminder is at least 1m ahead",
		"in is a span such as 30m, 2h, 1d or 1h30m, not ", "a reminder is at most 30 days ahead", "at is an RFC 3339 time with its zone",
		"a turn sets at most 5 reminders", "1d12h from now",
	} {
		if !strings.Contains(tx, want) {
			t.Errorf("the member was not told %q:\n%s", want, tx)
		}
	}
	thread := l.topic(msg)
	if rs := l.reminders(thread.ID); len(rs) != remindersPerTurn {
		t.Fatalf("five set: %+v", rs)
	}

	l.setOptions(slow, map[string]any{"tool_calls": calls[len(calls)-remindersPerTurn:], "reply": "Set more."})
	l.say("@Slow and the weekly ones", thread.ID, slow)
	second := l.waitTurns(2, store.TurnDone, "the turn setting five more")[0]
	prompt := specOf(t, second).Prompt
	if !strings.Contains(prompt, "Your reminders not yet due") || strings.Count(prompt, "topic #1: check the nightly build (id ") != remindersPerTurn {
		t.Errorf("the brief lists the member's reminders:\n%s", prompt)
	}
	l.setOptions(slow, map[string]any{"tool_calls": calls[len(calls)-1:], "reply": "One more."})
	l.say("@Slow and one more", thread.ID, slow)
	third := l.waitTurns(3, store.TurnDone, "the turn with ten waiting")[0]
	if tx := transcriptOf(t, third); !strings.Contains(tx, "you have 10 reminders not yet due, the most there may be") {
		t.Errorf("ten at most:\n%s", tx)
	}
	if prompt := specOf(t, third).Prompt; strings.Count(prompt, "check the nightly build (id ") != 2*remindersPerTurn {
		t.Errorf("the brief lists the ten:\n%s", prompt)
	}
	l.setOptions(slow, map[string]any{"reply": "Nothing new."})
	l.say("@Slow how is it going?", thread.ID, slow)
	fourth := l.waitTurns(4, store.TurnDone, "a turn with the reminders as they were")[0]
	if prompt := specOf(t, fourth).Prompt; strings.Contains(prompt, "Your reminders not yet due") || !strings.Contains(prompt, "your reminders") {
		t.Errorf("reminders the session saw are not listed again:\n%s", prompt)
	}
}

// A reminder's wake passes the limits on agents waking one another, in the
// piece of work of the turn that set it: one held back waits for the
// person, who may let it go on.
func TestLoop_AReminderPassesTheRelayLimit(t *testing.T) {
	l := newLoop(t)
	l.quickReminders()
	l.setRelayLimit(1)
	// Every turn of Slow's sets another: the first wake is within the
	// limit, the second is held.
	slow := l.member("Slow", map[string]any{"tool_calls": []any{remindCall("look again", "300ms")}, "reply": "Looking.", "tool": true})
	msg := l.say("@Slow watch it", "", slow)
	thread := l.topic(msg)
	eventually(t, func() bool { return len(l.holds(thread.ID)) == 1 }, "the second wake to be held")
	l.setOptions(slow, map[string]any{"reply": "Looked."})
	turns := l.settle(2, "the turn asked and the one its reminder woke")
	hold := l.holds(thread.ID)[0]
	if !strings.Contains(hold.Body, "Slow's reminder came due, but agents have woken 1 turns in this piece of work since a person last spoke; it waits for a person now.") ||
		!slices.Contains(hold.Mentions, store.Mention{Kind: store.MentionUser, ID: l.user.ID}) {
		t.Errorf("hold = %+v", hold)
	}
	if rs := l.reminders(thread.ID); len(rs) != 2 || rs[1].Status != store.ReminderFired {
		t.Errorf("both came due: %+v", rs)
	}
	if err := l.h.ContinueRelay(l.ctx, hold.ID); err != nil {
		t.Fatal(err)
	}
	turns = l.settle(3, "the wake let go on")
	if turns[0].ChainMessageID != hold.ID || turns[0].TriggerMessageID != l.reminders(thread.ID)[1].FiredMessageID {
		t.Errorf("the wake let go on: %+v", turns[0])
	}
}

// Where only people wake members, a reminder coming due asks the person.
func TestLoop_WhereOnlyPeopleWakeMembersAReminderAsksThePerson(t *testing.T) {
	l := newLoop(t)
	l.quickReminders()
	l.setRelayLimit(-1)
	slow := l.member("Slow", map[string]any{"tool_calls": []any{remindCall("look again", "300ms")}, "reply": "Set."})
	msg := l.say("@Slow look at it later", "", slow)
	first := l.waitTurns(1, store.TurnDone, "the turn setting it")[0]
	l.setOptions(slow, map[string]any{"reply": "Looked."})
	if tx := transcriptOf(t, first); !strings.Contains(tx, "since only people wake members in this project, the person is asked then") {
		t.Errorf("the member is told:\n%s", tx)
	}
	if system := specOf(t, first).SystemPrompt; !strings.Contains(system, "Since only people wake members here, the person is asked then to let it wake you.") {
		t.Errorf("the standing instructions say so:\n%s", system)
	}
	thread := l.topic(msg)
	eventually(t, func() bool { return len(l.holds(thread.ID)) == 1 }, "the wake to be held")
	hold := l.holds(thread.ID)[0]
	eventually(t, func() bool {
		_, err := l.s.GetRelayHold(l.ctx, hold.ID)
		return err == nil
	}, "the held wake to be kept")
	if !strings.Contains(hold.Body, "Slow's reminder came due, but only people wake members in this project; it waits for a person now.") {
		t.Errorf("hold = %q", hold.Body)
	}
	if err := l.h.ContinueRelay(l.ctx, hold.ID); err != nil {
		t.Fatal(err)
	}
	woken := l.settle(2, "the wake the person let go on")[0]
	// Let go on by the person the note was addressed to, it answers them.
	if !slices.ContainsFunc(l.replies(thread.ID, store.SenderAgent), func(m store.Message) bool {
		return m.TurnID == woken.ID && slices.Contains(m.Mentions, store.Mention{Kind: store.MentionUser, ID: l.user.ID})
	}) {
		t.Errorf("the turn let go on answers the person: %+v", l.replies(thread.ID, store.SenderAgent))
	}
}

// A reminder due while its machine is away comes due once it is back,
// with a hub started anew too; one whose member is switched off as it is
// due is dropped.
func TestLoop_AReminderWaitsForItsMachine(t *testing.T) {
	l := newLoop(t)
	l.quickReminders()
	slow := l.member("Slow", map[string]any{"tool_calls": []any{remindCall("look again", "2s")}, "reply": "Set."})
	other := l.member("Other", map[string]any{"tool_calls": []any{remindCall("never", "2s")}, "reply": "Set too."})
	msg := l.say("@Slow @Other look at it later", "", slow, other)
	l.waitTurns(2, store.TurnDone, "the turns setting them")
	l.dropMachine()
	l.setOptions(slow, map[string]any{"reply": "Looked."})
	off := false
	if _, err := l.s.UpdateMember(l.ctx, other.ID, store.MemberPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	thread := l.topic(msg)
	rs := l.reminders(thread.ID)
	if len(rs) != 2 {
		t.Fatalf("reminders = %+v", rs)
	}
	time.Sleep(time.Until(rs[1].DueAt) + 500*time.Millisecond)
	for _, r := range l.reminders(thread.ID) {
		// The switched-off member's is dropped as it is due, the machine
		// away or not.
		if want := map[bool]store.ReminderStatus{true: store.ReminderDropped, false: store.ReminderPending}[r.MemberID == other.ID]; r.Status != want {
			t.Fatalf("with the machine away: %+v", r)
		}
	}
	l.restart()
	l.settle(3, "the turn the reminder woke once the machine was back")
	byMember := map[string]store.ReminderStatus{}
	for _, r := range l.reminders(thread.ID) {
		byMember[r.MemberID] = r.Status
	}
	if byMember[slow.ID] != store.ReminderFired || byMember[other.ID] != store.ReminderDropped {
		t.Errorf("reminders = %+v", l.reminders(thread.ID))
	}
	if notes := l.notes(thread.ID); countContaining(notes, "Other's reminder") != 0 || countContaining(notes, "Slow's reminder, due ") != 1 {
		t.Errorf("a dropped reminder says nothing: %q", notes)
	}
}
