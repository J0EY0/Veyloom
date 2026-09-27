package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// What keeps turns from starting, listed and lifted (docs/design.md
// 5.23.3).
func TestPauses_ListAndLift(t *testing.T) {
	handler, _, chat := turnsHandler(t)
	var none PausesResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/pauses", "", &none); rec.Code != http.StatusOK || none.Pauses == nil || len(none.Pauses) != 0 {
		t.Errorf("no pauses: %d %+v", rec.Code, none)
	}

	resets := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	chat.pauses = []store.Pause{
		{ID: "p1", MachineID: "m1", Runtime: "claude", Reason: store.PauseQuota, Detail: "You've hit your limit", EndsAt: &resets},
		{ID: "p2", MemberID: "mb1", Reason: store.PauseFailing, Detail: "exit status 1"},
	}
	var list PausesResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/pauses", "", &list); rec.Code != http.StatusOK || len(list.Pauses) != 2 ||
		list.Pauses[0].Reason != store.PauseQuota || list.Pauses[0].EndsAt == nil || !list.Pauses[0].EndsAt.Equal(resets) || list.Pauses[1].MemberID != "mb1" {
		t.Errorf("pauses: %d %+v", rec.Code, list)
	}
	if rec := do(t, handler, http.MethodDelete, "/api/v1/pauses/p2", "", nil); rec.Code != http.StatusNoContent || len(chat.lifted) != 1 {
		t.Errorf("lift: %d %v", rec.Code, chat.lifted)
	}
	if rec := do(t, handler, http.MethodDelete, "/api/v1/pauses/p2", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("lift again: %d", rec.Code)
	}
}
