package api

import (
	"bytes"
	"errors"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/hub"
)

// The project wiki over HTTP (docs/design.md 5.14, step 4, and 5.15):
// what the Wiki tab reads, and what a person changes there. A page is
// named by its path from the wiki's root, which goes in the query or the
// body rather than the URL path: it has slashes of its own.

// WikiCatalogResponse is the body of GET /projects/{id}/wiki.
type WikiCatalogResponse struct {
	Wiki hub.WikiCatalog `json:"wiki"`
}

// WikiPageResponse is the body of the endpoints that return one page.
type WikiPageResponse struct {
	Page hub.WikiPageView `json:"page"`
}

// WikiSearchResponse is the body of GET /projects/{id}/wiki/search.
type WikiSearchResponse struct {
	Hits []hub.WikiHit `json:"hits"`
}

// WikiHistoryResponse is the body of GET /projects/{id}/wiki/history,
// newest first.
type WikiHistoryResponse struct {
	Commits []hub.WikiCommit `json:"commits"`
}

// RevertWikiRequest is the body of POST /projects/{id}/wiki/revert.
type RevertWikiRequest struct {
	UserID string `json:"user_id"`
	SHA    string `json:"sha"`
	// Reason says why, for the wiki's log; it may be left out.
	Reason string `json:"reason"`
}

// RevertWikiResponse names the commit that undid the change.
type RevertWikiResponse struct {
	Commit string `json:"commit"`
}

// WikiPageRequest is the body of POST /projects/{id}/wiki/verify and
// /resident. The signed-in person makes the change; user_id counts only
// when the API runs without sign-in (tests).
type WikiPageRequest struct {
	UserID   string `json:"user_id"`
	Path     string `json:"path"`
	Resident bool   `json:"resident"`
}

// WikiQuestionResponse answers POST /projects/{id}/wiki/question: the
// wiki topic, opened if it was not, and the member to ask there.
type WikiQuestionResponse struct {
	Question hub.WikiQuestion `json:"question"`
}

// wikis answers when the hub keeps no wikis, and says so.
func (h *handlers) wikis(w http.ResponseWriter) (Wikis, bool) {
	if h.deps.Wikis == nil {
		writeError(w, http.StatusNotFound, hub.ErrNoWikis.Error())
		return nil, false
	}
	return h.deps.Wikis, true
}

// writeWikiError is writeStoreError, with a hub that keeps no wikis a 404.
func (h *handlers) writeWikiError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, hub.ErrNoWikis) {
		writeReason(w, http.StatusNotFound, err)
		return
	}
	h.writeStoreError(w, r, err)
}

// deciderID is the person a request acts for: the signed-in one, else the
// body's user_id (tests).
func deciderID(r *http.Request, fromBody string) (string, bool) {
	if user, ok := userFrom(r.Context()); ok {
		return user.ID, true
	}
	return fromBody, strings.TrimSpace(fromBody) != ""
}

func (h *handlers) wikiCatalog(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	catalog, err := wikis.WikiCatalog(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiCatalogResponse{Wiki: catalog})
}

func (h *handlers) wikiPage(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	page, err := wikis.WikiPage(r.Context(), r.PathValue("id"), path)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiPageResponse{Page: page})
}

// wikiFile serves a file the project's wiki keeps: shown in place when it is
// safe to be (a picture), downloaded otherwise, as attachments are.
func (h *handlers) wikiFile(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	p := r.URL.Query().Get("path")
	if p == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	data, err := wikis.WikiFile(r.Context(), r.PathValue("id"), p)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	name := path.Base(p)
	mediaType := mime.TypeByExtension(path.Ext(name))
	if ext := path.Ext(name); strings.EqualFold(ext, ".markdown") {
		// A markdown file is kept as .markdown, a .md being a page
		// (wiki.AssetPath): it reads as text and saves as the .md it was.
		mediaType, name = "text/plain; charset=utf-8", strings.TrimSuffix(name, ext)+".md"
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	disposition := "attachment"
	if inlineSafe(mediaType) {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (h *handlers) searchWiki(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	limit, ok := h.listLimit(w, r)
	if !ok {
		return
	}
	hits, err := wikis.SearchWiki(r.Context(), r.PathValue("id"), r.URL.Query().Get("q"), limit)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiSearchResponse{Hits: hits})
}

func (h *handlers) wikiHistory(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	limit, ok := h.listLimit(w, r)
	if !ok {
		return
	}
	commits, err := wikis.WikiHistory(r.Context(), r.PathValue("id"), r.URL.Query().Get("path"), limit)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiHistoryResponse{Commits: commits})
}

func (h *handlers) revertWiki(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	var req RevertWikiRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	userID, ok := deciderID(r, req.UserID)
	if !ok {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if strings.TrimSpace(req.SHA) == "" {
		writeError(w, http.StatusBadRequest, "sha is required")
		return
	}
	undo, err := wikis.RevertWiki(r.Context(), r.PathValue("id"), req.SHA, userID, req.Reason)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, RevertWikiResponse{Commit: undo})
}

func (h *handlers) verifyWikiPage(w http.ResponseWriter, r *http.Request) {
	h.personChangesPage(w, r, func(wikis Wikis, userID string, req WikiPageRequest) (hub.WikiPageView, error) {
		return wikis.VerifyWikiPage(r.Context(), r.PathValue("id"), req.Path, userID)
	})
}

func (h *handlers) setWikiResident(w http.ResponseWriter, r *http.Request) {
	h.personChangesPage(w, r, func(wikis Wikis, userID string, req WikiPageRequest) (hub.WikiPageView, error) {
		return wikis.SetWikiResident(r.Context(), r.PathValue("id"), req.Path, req.Resident, userID)
	})
}

func (h *handlers) personChangesPage(w http.ResponseWriter, r *http.Request, change func(Wikis, string, WikiPageRequest) (hub.WikiPageView, error)) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	var req WikiPageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	userID, ok := deciderID(r, req.UserID)
	if !ok {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	page, err := change(wikis, userID, req)
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiPageResponse{Page: page})
}

func (h *handlers) wikiQuestion(w http.ResponseWriter, r *http.Request) {
	wikis, ok := h.wikis(w)
	if !ok {
		return
	}
	q, err := wikis.WikiQuestion(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeWikiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WikiQuestionResponse{Question: q})
}
