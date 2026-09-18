package api

import (
	"net/http"
	"net/url"
	"path"
	"strings"
)

// withCORS lets a browser page served from another origin call the API,
// when that origin's host matches one of the configured patterns (the
// same list that admits WebSocket connections). Everything else passes
// through untouched: a same-origin page, or the dev server's proxy, needs
// no headers, and an unlisted origin gets none, which is how the browser
// learns to refuse it.
func withCORS(patterns []string, next http.Handler) http.Handler {
	if len(patterns) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" || !originAllowed(patterns, origin) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Add("Vary", "Origin")
		// The session travels in a cookie, which needs this to cross origins.
		h.Set("Access-Control-Allow-Credentials", "true")
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE")
			if requested := r.Header.Get("Access-Control-Request-Headers"); requested != "" {
				h.Set("Access-Control-Allow-Headers", requested)
			} else {
				h.Set("Access-Control-Allow-Headers", "Content-Type")
			}
			h.Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed matches the origin's host against the patterns the way the
// WebSocket library does: case-insensitive, with path.Match wildcards, so
// "localhost:*" admits any port on localhost.
func originAllowed(patterns []string, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Host)
	for _, pattern := range patterns {
		if ok, err := path.Match(strings.ToLower(pattern), host); err == nil && ok {
			return true
		}
	}
	return false
}
