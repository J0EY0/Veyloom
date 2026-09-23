package api

import (
	"net/http"

	"github.com/J0EY0/veyloom/internal/hub"
)

// Every project's wiki at once (docs/design.md 5.18): the Wiki page of the
// sidebar lists them and searches through them all.

// WikisResponse is the body of GET /wikis.
type WikisResponse struct {
	Wikis []hub.WikiSummary `json:"wikis"`
}

// WikisSearchResponse is the body of GET /wikis/search.
type WikisSearchResponse struct {
	Hits []hub.WikiProjectHit `json:"hits"`
}

func (h *handlers) listWikis(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	list, err := wikis.Wikis(r.Context())
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikisResponse{Wikis: list})
}

func (h *handlers) searchWikis(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	limit, ok := h.listLimit(w, r)
	if !ok {
		return
	}
	hits, err := wikis.SearchWikis(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikisSearchResponse{Hits: hits})
}
