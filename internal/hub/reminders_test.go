package hub

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// A reminder is due in a span from now, days included, or at a time with
// its zone.
func TestReminderDue(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		in, at string
		want   time.Time
	}{
		{in: "30m", want: now.Add(30 * time.Minute)},
		{in: "2h", want: now.Add(2 * time.Hour)},
		{in: "1d", want: now.Add(24 * time.Hour)},
		{in: "1d12h", want: now.Add(36 * time.Hour)},
		{in: "1h 30m", want: now.Add(90 * time.Minute)},
		{at: "2026-09-28T09:00:00+08:00", want: time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)},
	} {
		got, err := reminderDue(now, c.in, c.at)
		if err != nil || !got.Equal(c.want) {
			t.Errorf("reminderDue(%q, %q) = %v, %v; want %v", c.in, c.at, got, err, c.want)
		}
	}
	for _, c := range []struct{ in, at string }{{in: "soon"}, {in: "-2h"}, {in: "0m"}, {at: "tomorrow"}, {at: "2026-09-28T09:00:00"}} {
		if _, err := reminderDue(now, c.in, c.at); err == nil {
			t.Errorf("reminderDue(%q, %q) took it", c.in, c.at)
		}
	}
}

// Spans are said to the minute, the way a reader counts them.
func TestSpanText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0m", 20 * time.Second: "0m", 45 * time.Minute: "45m", 3*time.Hour + 20*time.Minute: "3h20m",
		52 * time.Hour: "2d4h", 24 * time.Hour: "1d", 24*time.Hour + 5*time.Minute: "1d5m",
	} {
		if got := spanText(d); got != want {
			t.Errorf("spanText(%v) = %q, want %q", d, got, want)
		}
	}
}

// A reminder that comes due late says how late.
func TestReminderDueNote(t *testing.T) {
	r := store.Reminder{Note: "check CI", DueAt: time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)}
	due := localTime(r.DueAt)
	if got, want := reminderDueNote("Slow", r, 10*time.Second), "Slow's reminder, due "+due+": check CI"; got != want {
		t.Errorf("on time: %q, want %q", got, want)
	}
	if got, want := reminderDueNote("Slow", r, 2*time.Hour+10*time.Minute), "Slow's reminder, due "+due+" (2h10m late): check CI"; got != want {
		t.Errorf("late: %q, want %q", got, want)
	}
	if got, want := reminderSetNote("Slow", r), "Slow set a reminder for "+due+": check CI"; got != want {
		t.Errorf("set: %q, want %q", got, want)
	}
}

// The brief lists the member's reminders not yet due, by topic, as a part
// the session is not told again while they stay as they are.
func TestBriefListsTheMembersReminders(t *testing.T) {
	st, in := newBriefRoom()
	in.Triggers = []store.Message{st.messages["m1"]}
	due := time.Date(2026, 9, 28, 1, 0, 0, 0, time.UTC)
	st.threads = map[string]store.Thread{"t7": in.Thread, "t9": {ID: "t9", Number: 9}}
	st.reminders = []store.Reminder{
		{ID: "r1", MemberID: "a1", ThreadID: "t7", Note: "check CI\nand the logs", DueAt: due},
		{ID: "r2", MemberID: "a1", ThreadID: "t9", Note: strings.Repeat("x", 300), DueAt: due.Add(time.Hour)},
		{ID: "r3", MemberID: "a2", ThreadID: "t7", Note: "not Claude's", DueAt: due},
	}
	b := newBriefBuilder(st, briefLimits{Thread: 40, Room: 30, Topics: 10}, "")
	got, err := b.Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Your reminders not yet due, which set_reminder set and cancel_reminder takes back by the id:",
		"- " + localTime(due) + ", topic #7: check CI and the logs (id r1)",
		"topic #9: " + strings.Repeat("x", reminderExcerpt),
	} {
		if !strings.Contains(got.Prompt, want) {
			t.Errorf("the brief does not say %q:\n%s", want, got.Prompt)
		}
	}
	if strings.Contains(got.Prompt, "not Claude's") {
		t.Errorf("another member's reminder is listed:\n%s", got.Prompt)
	}

	// Seen as they are, they are not listed again; all gone, that is said.
	in.Session.BriefSeen = got.Parts
	again, err := b.Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(again.Prompt, "Your reminders not yet due") || !strings.Contains(again.Prompt, "your reminders") {
		t.Errorf("listed again:\n%s", again.Prompt)
	}
	st.reminders = st.reminders[2:]
	gone, err := b.Build(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gone.Prompt, "You have no reminders not yet due any more.") {
		t.Errorf("the brief does not say they are gone:\n%s", gone.Prompt)
	}
}
