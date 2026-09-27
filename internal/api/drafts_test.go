package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// A topic's drafts are listed as they stand; a person runs one, with the
// merge's message changed or not, or turns one down, once
// (docs/design.md 5.23.5).
func TestDrafts_ListRunDecline(t *testing.T) {
	handler, _, chat := turnsHandler(t)
	var list DraftsResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/threads/th1/drafts", "", &list); rec.Code != http.StatusOK || len(list.Drafts) != 1 ||
		list.Drafts[0].Kind != store.DraftMerge || list.Drafts[0].Params.Message != "Add tags" || list.Drafts[0].MessageID != "n3" {
		t.Errorf("drafts: %d %+v", rec.Code, list)
	}
	var none DraftsResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/threads/th2/drafts", "", &none); rec.Code != http.StatusOK || none.Drafts == nil || len(none.Drafts) != 0 {
		t.Errorf("no drafts: %d %+v", rec.Code, none)
	}

	chat.drafts = map[string]store.Draft{"d1": {ID: "d1", Status: store.DraftPending}, "d2": {ID: "d2", Status: store.DraftPending}}
	var ran DraftResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/drafts/d1/run", `{"message":"Add tags\n\nAnd their tests."}`, &ran); rec.Code != http.StatusOK ||
		ran.Draft.Status != store.DraftDone || ran.Draft.Result.Commit == "" || len(chat.ran) != 1 || chat.ran[0] != "Add tags\n\nAnd their tests." {
		t.Errorf("run: %d %+v %q", rec.Code, ran, chat.ran)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/drafts/d1/run", "", nil); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "draftSettled") {
		t.Errorf("run again: %d %s", rec.Code, rec.Body)
	}
	var declined DraftResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/drafts/d2/decline", "", &declined); rec.Code != http.StatusOK || declined.Draft.Status != store.DraftDeclined {
		t.Errorf("decline: %d %+v", rec.Code, declined)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/drafts/d9/decline", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown one: %d", rec.Code)
	}
}
