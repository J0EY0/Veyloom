package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// A member's reminder to itself (design.md 5.23.4): set_reminder has the
// hub wake the member later, in the topic its turn is in. When it comes
// due the hub says so in that topic and wakes the member with that
// message, as the turn that set it would have: the wake carries on that
// turn's piece of work and passes the limits on agents waking one another.
// The store keeps the reminders; their timers are set going as the
// members' machine connects, and one due while the machine is away comes
// due once it is back, saying how late.

const (
	// reminderMax is how far ahead a reminder may be; remindersWaiting how
	// many a member may have not yet due; remindersPerTurn how many a turn
	// may set; reminderNoteMax how long a note may be, in characters.
	reminderMax      = 30 * 24 * time.Hour
	remindersWaiting = 10
	remindersPerTurn = 5
	reminderNoteMax  = 2000
	// reminderLate is how late a reminder comes due before it says so.
	reminderLate = time.Minute
	// reminderExcerpt is how much of a note the brief lists.
	reminderExcerpt = 200
)

// reminderTimers holds the timer of each reminder not yet due, by id; min
// is how soon a reminder may be, a minute but in tests.
type reminderTimers struct {
	min    time.Duration
	mu     sync.Mutex
	timers map[string]*time.Timer
}

// answerRemind sets a reminder for at's member in at's topic.
func (m *TurnManager) answerRemind(ctx context.Context, at *activeTurn, q runtime.RoomQuery) (string, error) {
	var args struct {
		Note string `json:"note"`
		In   string `json:"in"`
		At   string `json:"at"`
	}
	if len(q.Args) > 0 {
		if err := json.Unmarshal(q.Args, &args); err != nil {
			return "", fmt.Errorf("%s: %w", runtime.MessageToolRemind, err)
		}
	}
	note := strings.TrimSpace(args.Note)
	in, when := strings.TrimSpace(args.In), strings.TrimSpace(args.At)
	switch {
	case at.turn.Kind != store.TurnChat:
		return "", errors.New("set_reminder is for turns in the chat")
	case note == "":
		return "", errors.New("set_reminder needs a note: what to do then")
	case utf8.RuneCountInString(note) > reminderNoteMax:
		return "", fmt.Errorf("set_reminder takes a note of at most %d characters", reminderNoteMax)
	case (in == "") == (when == ""):
		return "", errors.New("set_reminder takes in or at, one of them")
	}
	now := time.Now()
	due, err := reminderDue(now, in, when)
	if err != nil {
		return "", err
	}
	switch ahead := due.Sub(now); {
	case ahead < m.reminders.min:
		return "", fmt.Errorf("a reminder is at least %s ahead; this one is at %s", spanText(m.reminders.min), localTime(due))
	case ahead > reminderMax:
		return "", fmt.Errorf("a reminder is at most 30 days ahead; this one is at %s", localTime(due))
	}
	// A turn's share is taken before the store is asked, so two calls at
	// once cannot both take the last of it, and given back on a failure.
	at.mu.Lock()
	if at.reminded >= remindersPerTurn {
		at.mu.Unlock()
		return "", fmt.Errorf("a turn sets at most %d reminders", remindersPerTurn)
	}
	at.reminded++
	at.mu.Unlock()
	r, err := m.setReminder(ctx, at, note, due)
	if err != nil {
		at.mu.Lock()
		at.reminded--
		at.mu.Unlock()
		return "", err
	}
	answer := fmt.Sprintf("Reminder %s set for %s, %s from now: you are woken in this topic then, with your note.", r.ID, localTime(due), spanText(due.Sub(now)))
	if project, err := m.store.RoomProject(ctx, at.thread.RoomID); err == nil && project.RelayLimit < 0 {
		answer = fmt.Sprintf("Reminder %s set for %s, %s from now: since only people wake members in this project, the person is asked then to let it wake you in this topic.", r.ID, localTime(due), spanText(due.Sub(now)))
	}
	return answer + " People see it in the topic and may cancel it; " + runtime.MessageToolCancelReminder + " takes it back.", nil
}

// setReminder records a reminder of at's member due at due, and has it
// told of in at's topic, in order with what the turn says there.
func (m *TurnManager) setReminder(ctx context.Context, at *activeTurn, note string, due time.Time) (store.Reminder, error) {
	waiting, err := m.store.CountPendingReminders(ctx, at.member.ID)
	if err != nil {
		return store.Reminder{}, err
	}
	if waiting >= remindersWaiting {
		return store.Reminder{}, fmt.Errorf("you have %d reminders not yet due, the most there may be: cancel one first", waiting)
	}
	r, err := m.store.CreateReminder(ctx, store.NewReminder{
		MemberID: at.member.ID, RoomID: at.thread.RoomID, ThreadID: at.thread.ID, TurnID: at.turn.ID, Note: note, DueAt: due,
	})
	if err != nil {
		return store.Reminder{}, err
	}
	told := make(chan error, 1)
	if !at.enqueue(func() { told <- m.tellReminder(at, &r) }) {
		m.untold(r.ID)
		return store.Reminder{}, errors.New("the turn is over")
	}
	select {
	case err := <-told:
		if err != nil {
			return store.Reminder{}, err
		}
		return r, nil
	case <-ctx.Done():
		return store.Reminder{}, fmt.Errorf("the reminder was not confirmed in time; the topic shows whether it was set: %w", ctx.Err())
	}
}

// tellReminder tells of r in at's topic, on the turn's executor, and sets
// it going. One that could not be told of is not kept.
func (m *TurnManager) tellReminder(at *activeTurn, r *store.Reminder) error {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()
	note, err := m.post(ctx, store.NewMessage{
		RoomID: at.thread.RoomID, ThreadID: at.thread.ID, SenderKind: store.SenderSystem,
		Body: reminderSetNote(at.member.DisplayName, *r), TurnID: at.turn.ID,
	})
	if err != nil {
		m.untold(r.ID)
		return err
	}
	if err := m.store.SetReminderMessage(ctx, r.ID, note.ID); err != nil {
		m.logger.Warn("note the note telling of a reminder", "reminder", r.ID, "err", err)
	} else {
		r.SetMessageID = note.ID
	}
	m.scheduleReminder(*r)
	m.publish(reminderEvent(*r))
	m.logger.Info("reminder set", "reminder", r.ID, "member", at.member.DisplayName, "due", r.DueAt)
	return nil
}

// untold takes back a reminder the topic was not told of: nobody could see
// it to cancel it.
func (m *TurnManager) untold(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()
	if _, err := m.store.CancelReminder(ctx, id, ""); err != nil {
		m.logger.Error("take back a reminder not told of", "reminder", id, "err", err)
	}
}

// answerCancelReminder takes back a reminder of at's member not yet due.
func (m *TurnManager) answerCancelReminder(ctx context.Context, at *activeTurn, q runtime.RoomQuery) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if len(q.Args) > 0 {
		if err := json.Unmarshal(q.Args, &args); err != nil {
			return "", fmt.Errorf("%s: %w", runtime.MessageToolCancelReminder, err)
		}
	}
	id := strings.TrimSpace(args.ID)
	r, err := m.store.GetReminder(ctx, id)
	switch {
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return "", err
	case err != nil || r.MemberID != at.member.ID:
		return "", fmt.Errorf("you have no reminder %s; the brief lists yours not yet due", id)
	case r.Status != store.ReminderPending:
		return "", fmt.Errorf("reminder %s is %s already", id, r.Status)
	}
	if _, err := m.cancelReminder(ctx, id, ""); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("reminder %s came due meanwhile", id)
		}
		return "", err
	}
	return fmt.Sprintf("Reminder %s cancelled.", id), nil
}

// CancelReminder takes back a reminder not yet due at a person's asking.
// One that came due, or was taken back, already is a conflict.
func (h *Hub) CancelReminder(ctx context.Context, id, userID string) (store.Reminder, error) {
	r, err := h.store.GetReminder(ctx, id)
	if err != nil {
		return store.Reminder{}, err
	}
	if r.Status == store.ReminderPending {
		if r, err = h.turns.cancelReminder(ctx, id, userID); err == nil {
			return r, nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return store.Reminder{}, err
		}
		if r, err = h.store.GetReminder(ctx, id); err != nil {
			return store.Reminder{}, err
		}
	}
	return store.Reminder{}, store.Conflicting("reminderSettled", store.Params{"status": string(r.Status)}, "the reminder is %s already", r.Status)
}

// ThreadReminders lists the reminders set in a topic, in the order set.
func (h *Hub) ThreadReminders(ctx context.Context, threadID string) ([]store.Reminder, error) {
	return h.store.ListThreadReminders(ctx, threadID)
}

// cancelReminder takes back reminder id, by the person userID, or by its
// member when that is empty. store.ErrNotFound when it is not pending.
func (m *TurnManager) cancelReminder(ctx context.Context, id, userID string) (store.Reminder, error) {
	r, err := m.store.CancelReminder(ctx, id, userID)
	if err != nil {
		return store.Reminder{}, err
	}
	m.unscheduleReminder(id)
	m.publish(reminderEvent(r))
	return r, nil
}

// scheduleReminders sets going, as machineID connects, the timers of the
// reminders of the members it runs; one due already comes due now.
func (m *TurnManager) scheduleReminders(ctx context.Context, machineID string) {
	reminders, err := m.store.ListPendingReminders(ctx, machineID)
	if err != nil {
		m.logger.Error("the reminders of a machine's members", "machine", machineID, "err", err)
		return
	}
	for _, r := range reminders {
		m.scheduleReminder(r)
	}
}

// scheduleReminder sets r's timer going, in place of any it had.
func (m *TurnManager) scheduleReminder(r store.Reminder) {
	t := &m.reminders
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.timers == nil {
		t.timers = make(map[string]*time.Timer)
	}
	if old := t.timers[r.ID]; old != nil {
		old.Stop()
	}
	id := r.ID
	t.timers[id] = time.AfterFunc(max(time.Until(r.DueAt), 0), func() { m.fireReminder(id) })
}

// unscheduleReminder stops reminder id's timer.
func (m *TurnManager) unscheduleReminder(id string) {
	t := &m.reminders
	t.mu.Lock()
	defer t.mu.Unlock()
	if old := t.timers[id]; old != nil {
		old.Stop()
		delete(t.timers, id)
	}
}

// fireReminder has reminder id come due: said in its topic, and the
// member woken with that, as the turn that set it would. A member taken
// out of the project or switched off has it no more; one whose machine is
// away has it come due once the machine is back.
func (m *TurnManager) fireReminder(id string) {
	m.unscheduleReminder(id)
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()
	r, err := m.store.GetReminder(ctx, id)
	if err != nil || r.Status != store.ReminderPending {
		return
	}
	member, err := m.store.GetMember(ctx, r.MemberID)
	if err != nil {
		m.logger.Error("the member of a reminder", "reminder", id, "err", err)
		return
	}
	if member.Removed() || !member.Enabled {
		if r, err := m.store.DropReminder(ctx, id); err == nil {
			m.publish(reminderEvent(r))
		}
		return
	}
	if _, online := m.connFor(member.MachineID); !online {
		return
	}
	thread, err := m.store.GetThread(ctx, r.ThreadID)
	if err != nil {
		m.logger.Error("the topic of a reminder", "reminder", id, "err", err)
		return
	}
	if r, err = m.store.FireReminder(ctx, id); err != nil {
		// Cancelled meanwhile.
		return
	}
	msg, err := m.post(ctx, store.NewMessage{
		RoomID: r.RoomID, ThreadID: r.ThreadID, SenderKind: store.SenderSystem,
		Body: reminderDueNote(member.DisplayName, r, time.Since(r.DueAt)), TurnID: r.TurnID,
	})
	if err != nil {
		m.logger.Error("say a reminder came due", "reminder", id, "err", err)
		return
	}
	if err := m.store.SetReminderFired(ctx, id, msg.ID); err != nil {
		m.logger.Warn("note the message a reminder came due as", "reminder", id, "err", err)
	} else {
		r.FiredMessageID = msg.ID
	}
	m.publish(reminderEvent(r))
	m.logger.Info("reminder came due", "reminder", id, "member", member.DisplayName)

	// As the turn that set it: its piece of work, its limits.
	w := waker{member: member, thread: thread, reminder: true}
	if r.TurnID != "" {
		if w.turn, err = m.store.GetTurn(ctx, r.TurnID); err != nil {
			m.logger.Warn("the turn that set a reminder", "reminder", id, "err", err)
		}
	}
	if !m.mayWake(ctx, w, member, msg, thread) {
		return
	}
	if err := m.TriggerIn(ctx, member, msg, thread); err != nil {
		m.logger.Error("wake a member for its reminder", "reminder", id, "err", err)
	}
}

// remindersText lists the member's reminders not yet due, for the brief:
// when each comes due, in which topic, what for, and its id, so it sets
// none twice and knows what to cancel. false when they could not be read.
func (b *briefBuilder) remindersText(ctx context.Context, member store.Member) (string, bool) {
	reminders, err := b.store.ListMemberPendingReminders(ctx, member.ID)
	if err != nil {
		return "", false
	}
	if len(reminders) == 0 {
		return "", true
	}
	var sb strings.Builder
	sb.WriteString("\nYour reminders not yet due, which " + runtime.MessageToolRemind + " set and " + runtime.MessageToolCancelReminder + " takes back by the id:\n")
	topics := make(map[string]int)
	for _, r := range reminders {
		number, ok := topics[r.ThreadID]
		if !ok {
			if thread, err := b.store.GetThread(ctx, r.ThreadID); err == nil {
				number = thread.Number
			}
			topics[r.ThreadID] = number
		}
		fmt.Fprintf(&sb, "- %s, topic #%d: %s (id %s)\n", localTime(r.DueAt), number, excerpt(oneLine(r.Note), reminderExcerpt), r.ID)
	}
	return sb.String(), true
}

// reminderEvent is the live event for a reminder that was set, came due,
// or was cancelled or dropped.
func reminderEvent(r store.Reminder) Event {
	return Event{Kind: EventReminder, RoomID: r.RoomID, At: time.Now(), Reminder: &r}
}

// reminderSetNote says, in the hub's words, that member set r; the UI says
// it its own way (systemNote.ts), the time in the reader's zone.
func reminderSetNote(member string, r store.Reminder) string {
	return fmt.Sprintf("%s set a reminder for %s: %s", member, localTime(r.DueAt), r.Note)
}

// reminderDueNote says r came due, late by late, as the message that
// wakes its member.
func reminderDueNote(member string, r store.Reminder, late time.Duration) string {
	if late >= reminderLate {
		return fmt.Sprintf("%s's reminder, due %s (%s late): %s", member, localTime(r.DueAt), spanText(late), r.Note)
	}
	return fmt.Sprintf("%s's reminder, due %s: %s", member, localTime(r.DueAt), r.Note)
}

// localTime is t as the hub's clock reads it, with its zone: the zone the
// people using it are most likely in, and one that an agent can compare
// the times it is told with.
func localTime(t time.Time) string {
	return t.Local().Format(time.RFC3339)
}

// daysIn finds the days of a span such as 1d12h, which Go's durations
// have no unit for.
var daysIn = regexp.MustCompile(`(\d+)d`)

// reminderDue is when a reminder comes due: in, a span from now such as
// 30m, 2h, 1d or 1h30m, or at, an RFC 3339 time.
func reminderDue(now time.Time, in, at string) (time.Time, error) {
	if at != "" {
		t, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return time.Time{}, fmt.Errorf("at is an RFC 3339 time with its zone, such as %s, not %q", localTime(now.Add(time.Hour).Truncate(time.Hour)), at)
		}
		return t, nil
	}
	span := daysIn.ReplaceAllStringFunc(strings.ReplaceAll(in, " ", ""), func(d string) string {
		n, _ := strconv.Atoi(strings.TrimSuffix(d, "d"))
		return strconv.Itoa(n*24) + "h"
	})
	d, err := time.ParseDuration(span)
	if err != nil || d <= 0 {
		return time.Time{}, fmt.Errorf("in is a span such as 30m, 2h, 1d or 1h30m, not %q", in)
	}
	return now.Add(d), nil
}

// spanText says a span the way a reader counts it, to the minute: 45m,
// 3h20m, 2d4h.
func spanText(d time.Duration) string {
	d = d.Round(time.Minute)
	days, hours, mins := int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour), int(d%time.Hour/time.Minute)
	var b strings.Builder
	if days > 0 {
		fmt.Fprintf(&b, "%dd", days)
	}
	if hours > 0 {
		fmt.Fprintf(&b, "%dh", hours)
	}
	if mins > 0 || b.Len() == 0 {
		fmt.Fprintf(&b, "%dm", mins)
	}
	return b.String()
}
