package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/runtime"
)

type stubDiscovery []runtime.Info

func (s stubDiscovery) Run(context.Context) []runtime.Info { return s }

type stubMachines []hub.MachineInfo

func (s stubMachines) Machines() []hub.MachineInfo { return s }

func (s stubMachines) Probe(_ context.Context, id string) error {
	for _, w := range s {
		if w.ID == id {
			return nil
		}
	}
	return hub.ErrUnknownMachine
}

// probeRecorder remembers which machines were asked to probe, and can fail
// the way a dropped connection does.
type probeRecorder struct {
	stubMachines
	probed []string
	fail   error
}

func (p *probeRecorder) Probe(ctx context.Context, id string) error {
	if err := p.stubMachines.Probe(ctx, id); err != nil {
		return err
	}
	p.probed = append(p.probed, id)
	return p.fail
}

func get(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestRuntimesEndpoint(t *testing.T) {
	handler := NewHandler(Deps{
		Runtimes: stubDiscovery{
			{Name: "claude", Binary: "claude", Version: "2.1.85", Status: runtime.StatusReady},
			{Name: "codex", Binary: "codex", Status: runtime.StatusNotInstalled, Detail: `"codex" not found on PATH`},
		},
		Machines: stubMachines{},
	})

	rec := get(t, handler, "/api/v1/runtimes")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body RuntimesResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Runtimes) != 2 || body.Runtimes[0].Name != "claude" || body.Runtimes[1].Status != runtime.StatusNotInstalled {
		t.Errorf("unexpected body: %+v", body)
	}
}

func TestMachinesEndpoint(t *testing.T) {
	now := time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC)
	handler := NewHandler(Deps{
		Runtimes: stubDiscovery{},
		Machines: stubMachines{{
			ID:          "w1",
			Name:        "laptop",
			Runtimes:    []runtime.Info{{Name: "claude", Status: runtime.StatusReady}},
			ConnectedAt: now,
			LastSeen:    now,
		}},
	})

	rec := get(t, handler, "/api/v1/machines")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body)
	}
	var body MachinesResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(body.Machines) != 1 || body.Machines[0].ID != "w1" || body.Machines[0].Runtimes[0].Name != "claude" {
		t.Errorf("unexpected body: %+v", body)
	}
	if !body.Machines[0].LastSeen.Equal(now) {
		t.Errorf("LastSeen = %v, want %v", body.Machines[0].LastSeen, now)
	}
}

func TestProbeMachineEndpoint(t *testing.T) {
	machines := &probeRecorder{stubMachines: stubMachines{{ID: "w1", Name: "laptop"}}}
	handler := NewHandler(Deps{Runtimes: stubDiscovery{}, Machines: machines})

	if rec := do(t, handler, http.MethodPost, "/api/v1/machines/w1/probe", "", nil); rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body: %s", rec.Code, rec.Body)
	}
	if len(machines.probed) != 1 || machines.probed[0] != "w1" {
		t.Errorf("probed %v, want [w1]", machines.probed)
	}

	if rec := do(t, handler, http.MethodPost, "/api/v1/machines/w404/probe", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown machine: status = %d, want 404", rec.Code)
	}

	machines.fail = errors.New("pipe closed")
	if rec := do(t, handler, http.MethodPost, "/api/v1/machines/w1/probe", "", nil); rec.Code != http.StatusBadGateway {
		t.Errorf("unreachable machine: status = %d, want 502", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/machines/w1/probe", "", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status = %d, want 405", rec.Code)
	}
}

func TestEndpoints_RejectOtherMethods(t *testing.T) {
	handler := NewHandler(Deps{Runtimes: stubDiscovery{}, Machines: stubMachines{}})

	for _, path := range []string{"/api/v1/runtimes", "/api/v1/machines"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: status = %d, want 405", path, rec.Code)
		}
	}
}
