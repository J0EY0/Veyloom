package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_AllowsListedOriginsOnly(t *testing.T) {
	handler := NewHandler(Deps{
		Runtimes: stubDiscovery{},
		Machines: stubMachines{},
		Events:   EventsOptions{AllowedOrigins: []string{"localhost:*", "app.example.com"}},
	})

	// A listed origin gets the headers, on the answer and on the preflight.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtimes", nil)
	req.Header.Set("Origin", "http://localhost:5174")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5174" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q", got)
	}

	pre := httptest.NewRequest(http.MethodOptions, "/api/v1/projects", nil)
	pre.Header.Set("Origin", "https://APP.example.com")
	pre.Header.Set("Access-Control-Request-Method", "POST")
	pre.Header.Set("Access-Control-Request-Headers", "content-type")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, pre)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d; body: %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("preflight without Allow-Methods")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "content-type" {
		t.Errorf("Allow-Headers = %q", got)
	}

	// An unlisted origin is answered without any of it.
	other := httptest.NewRequest(http.MethodGet, "/api/v1/runtimes", nil)
	other.Header.Set("Origin", "http://evil.example.com")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, other)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Errorf("unlisted origin: status = %d, Allow-Origin = %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_OffWithoutOrigins(t *testing.T) {
	handler := NewHandler(Deps{Runtimes: stubDiscovery{}, Machines: stubMachines{}})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runtimes", nil)
	req.Header.Set("Origin", "http://localhost:5174")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("no origins configured, yet a CORS header was sent")
	}
}
