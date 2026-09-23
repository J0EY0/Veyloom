package api

import (
	"net/http"

	"github.com/J0EY0/veyloom/internal/store"
)

// The account's settings the hub acts on (docs/design.md 5.19): which
// memories the members' turns use.

// MemoryPrefsStore keeps the memory switches, with the account.
type MemoryPrefsStore interface {
	MemoryPrefs() store.MemoryPrefs
	SetMemoryPrefs(store.MemoryPrefs) error
}

// MemoryPrefsResponse is the body of GET and PUT /settings/memory.
type MemoryPrefsResponse struct {
	Memory store.MemoryPrefs `json:"memory"`
}

func (h *handlers) memoryPrefs(w http.ResponseWriter, r *http.Request) {
	if h.deps.Prefs == nil {
		writeError(w, http.StatusNotFound, "this Veyloom keeps no settings")
		return
	}
	writeJSON(w, http.StatusOK, MemoryPrefsResponse{Memory: h.deps.Prefs.MemoryPrefs()})
}

func (h *handlers) setMemoryPrefs(w http.ResponseWriter, r *http.Request) {
	if h.deps.Prefs == nil {
		writeError(w, http.StatusNotFound, "this Veyloom keeps no settings")
		return
	}
	var prefs store.MemoryPrefs
	if err := decodeJSON(r, &prefs); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	if err := h.deps.Prefs.SetMemoryPrefs(prefs); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, MemoryPrefsResponse{Memory: prefs})
}
