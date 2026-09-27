package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// A topic's reminders are listed as they stand, and a person takes one back
// once (docs/design.md 5.23.4).
func TestReminders_ListAndCancel(t *testing.T) {
	handler, _, chat := turnsHandler(t)
	var list RemindersResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/threads/th1/reminders", "", &list); rec.Code != http.StatusOK || len(list.Reminders) != 1 ||
		list.Reminders[0].ID != "rm1" || list.Reminders[0].SetMessageID != "n2" || list.Reminders[0].Status != store.ReminderPending {
		t.Errorf("reminders: %d %+v", rec.Code, list)
	}
	var none RemindersResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/threads/th2/reminders", "", &none); rec.Code != http.StatusOK || none.Reminders == nil || len(none.Reminders) != 0 {
		t.Errorf("no reminders: %d %+v", rec.Code, none)
	}

	chat.reminders = map[string]store.Reminder{"rm1": {ID: "rm1", Status: store.ReminderPending}}
	var cancelled ReminderResponse
	if rec := do(t, handler, http.MethodDelete, "/api/v1/reminders/rm1", "", &cancelled); rec.Code != http.StatusOK ||
		cancelled.Reminder.Status != store.ReminderCancelled {
		t.Errorf("cancel: %d %+v", rec.Code, cancelled)
	}
	if rec := do(t, handler, http.MethodDelete, "/api/v1/reminders/rm1", "", nil); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "reminderSettled") {
		t.Errorf("cancel again: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, handler, http.MethodDelete, "/api/v1/reminders/rm9", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown one: %d", rec.Code)
	}
}
