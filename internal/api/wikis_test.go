package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/J0EY0/veyloom/internal/hub"
)

func (f *fakeWikis) Wikis(context.Context) ([]hub.WikiSummary, error) {
	f.record("wikis")
	return []hub.WikiSummary{{ProjectID: "p1", ProjectName: "Veyloom", RoomID: "r1", Pages: 3, Due: 1}}, nil
}

func (f *fakeWikis) SearchWikis(_ context.Context, query string, limit int) ([]hub.WikiProjectHit, error) {
	f.record("wikis search %q %d", query, limit)
	return []hub.WikiProjectHit{{WikiHit: hub.WikiHit{WikiPageInfo: hub.WikiPageInfo{Path: "/facts/port.md", Title: "Port"}}, ProjectID: "p1", ProjectName: "Veyloom", RoomID: "r1"}}, nil
}

// Every project's wiki at once: the list, and a search through them all.
func TestWikis(t *testing.T) {
	handler, wikis := wikiHandler()
	var list WikisResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/wikis", "", &list); rec.Code != http.StatusOK || len(list.Wikis) != 1 || list.Wikis[0].Due != 1 {
		t.Errorf("the list: %d %+v", rec.Code, list)
	}
	var hits WikisSearchResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/wikis/search?q=port+7788&limit=30", "", &hits); rec.Code != http.StatusOK || hits.Hits[0].ProjectName != "Veyloom" {
		t.Errorf("the search: %d %+v", rec.Code, hits)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/wikis/search?q=x&limit=-1", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a bad limit: %d", rec.Code)
	}
	if got := wikis.asked; len(got) < 2 || got[len(got)-2] != "wikis" || got[len(got)-1] != `wikis search "port 7788" 30` {
		t.Errorf("asked: %q", got)
	}
	if rec := do(t, NewHandler(Deps{}), http.MethodGet, "/api/v1/wikis", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("without wikis: %d", rec.Code)
	}
}
