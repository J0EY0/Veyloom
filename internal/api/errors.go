package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
)

// maxBodyBytes bounds request bodies; every request this API accepts is a
// small JSON document.
const maxBodyBytes = 1 << 20

// ErrorResponse is the body of every non-2xx response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// writeError sends a JSON error with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

// writeStoreError maps an error from the store to an HTTP status. Unknown
// errors are logged and reported as 500 without leaking their text.
func (h *handlers) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrInvalidID), errors.Is(err, store.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, err.Error())
	default:
		h.deps.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// decodeJSON reads a JSON request body into v.
func decodeJSON(r *http.Request, v any) error {
	body := io.LimitReader(r.Body, maxBodyBytes)
	if err := json.NewDecoder(body).Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

// requireName validates a user-supplied name and returns it trimmed.
func requireName(field, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	return value, nil
}
