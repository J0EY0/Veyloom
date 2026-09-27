package api

import (
	"net/http"

	"github.com/J0EY0/veyloom/internal/store"
)

// RemindersResponse is the body of GET /api/v1/threads/{id}/reminders: the
// reminders members set in the topic, in the order set, as they stand
// (docs/design.md 5.23.4). The topic's notes of them are drawn by these.
type RemindersResponse struct {
	Reminders []store.Reminder `json:"reminders"`
}

// ReminderResponse is the body of DELETE /api/v1/reminders/{id}: the
// reminder as it stands once taken back.
type ReminderResponse struct {
	Reminder store.Reminder `json:"reminder"`
}

func (h *handlers) threadReminders(w http.ResponseWriter, r *http.Request) {
	reminders, err := h.deps.Turns.ListThreadReminders(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if reminders == nil {
		reminders = []store.Reminder{}
	}
	writeJSON(w, http.StatusOK, RemindersResponse{Reminders: reminders})
}

// cancelReminder takes back a reminder not yet due. One that came due or
// was taken back already is a 409 (reminderSettled), an unknown one a 404.
func (h *handlers) cancelReminder(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	reminder, err := h.deps.Chat.CancelReminder(r.Context(), r.PathValue("id"), user.ID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ReminderResponse{Reminder: reminder})
}
