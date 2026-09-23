package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/J0EY0/veyloom/internal/store"
)

// maxBodyBytes bounds request bodies; every request this API accepts is a
// small JSON document.
const maxBodyBytes = 1 << 20

// ErrorResponse is the body of every non-2xx response. Error is the reason
// in English. Code and Params name a failure a person can run into, for the
// web client to tell them of in their own language (store.Problem); a
// failure only a bug reaches has neither.
type ErrorResponse struct {
	Error  string            `json:"error"`
	Code   string            `json:"code,omitempty"`
	Params map[string]string `json:"params,omitempty"`
}

// writeError sends a JSON error with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

// writeReason sends err with the given status: its reason in words
// (store.Reason), and the code a store.Problem names it by.
func writeReason(w http.ResponseWriter, status int, err error) {
	resp := ErrorResponse{Error: store.Reason(err)}
	var p *store.Problem
	if errors.As(err, &p) {
		resp.Code, resp.Params = p.Code, p.Params
	}
	writeJSON(w, status, resp)
}

// writeCoded sends a failure a person can run into that the API finds
// itself, named by code as a store.Problem would be.
func writeCoded(w http.ResponseWriter, status int, code string, params store.Params, text string) {
	writeJSON(w, status, ErrorResponse{Error: text, Code: code, Params: params})
}

// writeStoreError maps an error from the store to an HTTP status. Unknown
// errors are logged and reported as 500 without leaking their text.
func (h *handlers) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeReason(w, http.StatusNotFound, err)
	case errors.Is(err, store.ErrInvalidID), errors.Is(err, store.ErrInvalidInput):
		writeReason(w, http.StatusBadRequest, err)
	case errors.Is(err, store.ErrConflict):
		writeReason(w, http.StatusConflict, err)
	default:
		h.deps.Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// decodeJSON reads a JSON request body into v.
// A body over maxBodyBytes is refused as such, not cut short into JSON that
// does not parse.
func decodeJSON(r *http.Request, v any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if len(data) > maxBodyBytes {
		return store.Invalid("requestTooLarge", store.Params{"mb": strconv.Itoa(maxBodyBytes >> 20)}, "the request is over %d MB", maxBodyBytes>>20)
	}
	if err := json.Unmarshal(data, v); err != nil {
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
