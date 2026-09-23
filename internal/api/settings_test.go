package api

import (
	"errors"
	"net/http"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// fakePrefs keeps the memory switches in memory; failing makes the next
// save fail.
type fakePrefs struct {
	prefs   store.MemoryPrefs
	failing bool
}

func (f *fakePrefs) MemoryPrefs() store.MemoryPrefs { return f.prefs }

func (f *fakePrefs) SetMemoryPrefs(p store.MemoryPrefs) error {
	if f.failing {
		return errors.New("disk full")
	}
	f.prefs = p
	return nil
}

// The memory switches (docs/design.md 5.19) are read and set as a whole.
func TestMemoryPrefs(t *testing.T) {
	prefs := &fakePrefs{prefs: store.DefaultMemoryPrefs}
	handler := NewHandler(Deps{Prefs: prefs})
	var got MemoryPrefsResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/settings/memory", "", &got); rec.Code != http.StatusOK || got.Memory != store.DefaultMemoryPrefs {
		t.Errorf("read: %d %+v", rec.Code, got)
	}
	off := store.MemoryPrefs{Enabled: true, Personal: false, Project: true}
	if rec := do(t, handler, http.MethodPut, "/api/v1/settings/memory", `{"enabled":true,"personal":false,"project":true}`, &got); rec.Code != http.StatusOK || got.Memory != off || prefs.prefs != off {
		t.Errorf("set: %d %+v %+v", rec.Code, got, prefs.prefs)
	}
	if rec := do(t, handler, http.MethodPut, "/api/v1/settings/memory", `{"enabled":`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a broken body: %d", rec.Code)
	}
	prefs.failing = true
	if rec := do(t, handler, http.MethodPut, "/api/v1/settings/memory", `{"enabled":false}`, nil); rec.Code != http.StatusInternalServerError || prefs.prefs != off {
		t.Errorf("a failed save changes nothing: %d %+v", rec.Code, prefs.prefs)
	}
	if rec := do(t, NewHandler(Deps{}), http.MethodGet, "/api/v1/settings/memory", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("without settings: %d", rec.Code)
	}
}
