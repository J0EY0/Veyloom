package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

func (f *fakeWikis) Memory(_ context.Context, projectID string) (hub.MemoryView, error) {
	if err := f.project(projectID); err != nil {
		return hub.MemoryView{}, err
	}
	return hub.MemoryView{Entries: []wiki.MemoryEntry{{Text: "Reply in Chinese.", Date: "2026-09-23", Source: "Joey"}}, Chars: 38, Budget: 3000, Hash: "h1"}, nil
}

func (f *fakeWikis) SetMemory(_ context.Context, projectID string, entries []string, hash, userID string) (hub.MemoryView, error) {
	f.record("memory %q %q from %s by %s", projectID, strings.Join(entries, " | "), hash, userID)
	if hash != "h1" {
		return hub.MemoryView{}, fmt.Errorf("%w: the memory changed since it was read", store.ErrConflict)
	}
	return hub.MemoryView{Entries: []wiki.MemoryEntry{}, Budget: 3000, Hash: "h2"}, nil
}

func (f *fakeWikis) MemoryHistory(_ context.Context, limit int) ([]hub.WikiCommit, error) {
	f.record("memory history %d", limit)
	return []hub.WikiCommit{{SHA: "m1", Undoable: true}}, nil
}

func (f *fakeWikis) RevertMemory(_ context.Context, sha, userID, reason string) (string, error) {
	f.record("memory revert %s by %s: %s", sha, userID, reason)
	return "m2", nil
}

// The personal memory is at /memory, a project's under the project; a
// person saves one as they left it, from the hash they read.
func TestMemory(t *testing.T) {
	handler, fake := wikiHandler()
	var got MemoryResponse
	for _, path := range []string{"/api/v1/memory", "/api/v1/projects/p1/memory"} {
		if rec := do(t, handler, http.MethodGet, path, "", &got); rec.Code != http.StatusOK || len(got.Memory.Entries) != 1 || got.Memory.Hash != "h1" {
			t.Errorf("GET %s: %d %+v", path, rec.Code, got)
		}
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/projects/p404/memory", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("no such project: %d", rec.Code)
	}

	if rec := do(t, handler, http.MethodPut, "/api/v1/memory", `{"user_id":"u1","entries":["a","b"],"hash":"h1"}`, &got); rec.Code != http.StatusOK || got.Memory.Hash != "h2" {
		t.Errorf("save: %d %+v", rec.Code, got)
	}
	do(t, handler, http.MethodPut, "/api/v1/projects/p1/memory", `{"user_id":"u1","entries":[],"hash":"h1"}`, nil)
	want := []string{`memory "" "a | b" from h1 by u1`, `memory "p1" "" from h1 by u1`}
	if !slices.Equal(fake.asked, want) {
		t.Errorf("asked %q, want %q", fake.asked, want)
	}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"entries":["a"],"hash":"h1"}`, http.StatusBadRequest},
		{`{"user_id":"u1","hash":"h1"}`, http.StatusBadRequest},
		{`{"user_id":"u1","entries":["a"],"hash":"stale"}`, http.StatusConflict},
	} {
		if rec := do(t, handler, http.MethodPut, "/api/v1/memory", tc.body, nil); rec.Code != tc.status {
			t.Errorf("PUT %s = %d, want %d: %s", tc.body, rec.Code, tc.status, rec.Body)
		}
	}

	fake.asked = nil
	var history WikiHistoryResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/memory/history?limit=5", "", &history); rec.Code != http.StatusOK || len(history.Commits) != 1 {
		t.Errorf("history: %d %+v", rec.Code, history)
	}
	var reverted RevertWikiResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/memory/revert", `{"user_id":"u1","sha":"m1","reason":"too strict"}`, &reverted); rec.Code != http.StatusOK || reverted.Commit != "m2" {
		t.Errorf("revert: %d %+v", rec.Code, reverted)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/memory/revert", `{"user_id":"u1"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("revert without a sha: %d", rec.Code)
	}
	if want := []string{"memory history 5", "memory revert m1 by u1: too strict"}; !slices.Equal(fake.asked, want) {
		t.Errorf("asked %q, want %q", fake.asked, want)
	}
}
