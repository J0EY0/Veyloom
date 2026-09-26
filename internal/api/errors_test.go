package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A request its client gave up on, as a page does that is left, is no
// failure of the server's: it is not logged as one. Anything else unknown
// is, and answered 500.
func TestStoreErrorOfARequestGivenUp(t *testing.T) {
	var logged bytes.Buffer
	h := &handlers{deps: Deps{Logger: slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelInfo}))}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	left := httptest.NewRequest(http.MethodGet, "/api/v1/topics", nil).WithContext(ctx)
	h.writeStoreError(httptest.NewRecorder(), left, fmt.Errorf("list running topics: %w", context.Canceled))
	if logged.Len() != 0 {
		t.Errorf("logged: %s", logged.String())
	}

	rec := httptest.NewRecorder()
	h.writeStoreError(rec, httptest.NewRequest(http.MethodGet, "/api/v1/topics", nil), errors.New("boom"))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(logged.String(), "request failed") {
		t.Errorf("an unknown failure: %d, logged %q", rec.Code, logged.String())
	}
}
