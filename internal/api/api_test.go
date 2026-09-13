package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/hub"
)

type stubDiscovery []engine.Info

func (s stubDiscovery) Run(context.Context) []engine.Info { return s }

type stubWorkers []hub.WorkerInfo

func (s stubWorkers) Workers() []hub.WorkerInfo { return s }

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestEnginesEndpoint(t *testing.T) {
	handler := NewHandler(Deps{
		Engines: stubDiscovery{
			{Name: "claude", Binary: "claude", Version: "2.1.85", Status: engine.StatusReady},
			{Name: "codex", Binary: "codex", Status: engine.StatusNotInstalled, Detail: `"codex" not found on PATH`},
		},
		Workers: stubWorkers{},
	})

	rec := get(t, handler, "/api/v1/engines")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body EnginesResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Engines) != 2 || body.Engines[0].Name != "claude" || body.Engines[1].Status != engine.StatusNotInstalled {
		t.Errorf("unexpected body: %+v", body)
	}
}

func TestWorkersEndpoint(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	handler := NewHandler(Deps{
		Engines: stubDiscovery{},
		Workers: stubWorkers{{
			ID:          "w1",
			Name:        "laptop",
			Engines:     []engine.Info{{Name: "claude", Status: engine.StatusReady}},
			ConnectedAt: now,
			LastSeen:    now,
		}},
	})

	rec := get(t, handler, "/api/v1/workers")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var body WorkersResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Workers) != 1 || body.Workers[0].ID != "w1" || body.Workers[0].Engines[0].Name != "claude" {
		t.Errorf("unexpected body: %+v", body)
	}
	if !body.Workers[0].LastSeen.Equal(now) {
		t.Errorf("LastSeen = %v, want %v", body.Workers[0].LastSeen, now)
	}
}

func TestEndpoints_RejectOtherMethods(t *testing.T) {
	handler := NewHandler(Deps{Engines: stubDiscovery{}, Workers: stubWorkers{}})

	for _, path := range []string{"/api/v1/engines", "/api/v1/workers"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: status = %d, want 405", path, rec.Code)
		}
	}
}
