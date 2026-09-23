package api

import (
	"net/http"

	"github.com/J0EY0/veyloom/internal/hub"
)

// The wikis as graphs (docs/design.md 5.17): what the relation graph
// draws. Worked out from the pages each time; nothing of it is stored.

// WikiGraphResponse is the body of GET /projects/{id}/wiki/graph and
// GET /library/graph.
type WikiGraphResponse struct {
	Graph hub.WikiGraph `json:"graph"`
}

func (h *handlers) wikiGraph(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	graph, err := wikis.WikiGraph(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiGraphResponse{Graph: graph})
}

func (h *handlers) libraryGraph(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	graph, err := wikis.LibraryGraph(r.Context())
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiGraphResponse{Graph: graph})
}
