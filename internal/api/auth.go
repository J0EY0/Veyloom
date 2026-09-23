package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/auth"
	"github.com/J0EY0/veyloom/internal/store"
)

// Authenticator signs the person in and out; it is internal/auth.
type Authenticator interface {
	SetupRequired(ctx context.Context) (bool, error)
	Setup(ctx context.Context, name, password string) (store.User, string, error)
	Login(ctx context.Context, name, password string) (store.User, string, error)
	Logout(ctx context.Context, token string) error
	UserForToken(ctx context.Context, token string) (store.User, error)
	ChangePassword(ctx context.Context, userID, current, next string) (string, error)
	TTL() time.Duration
}

// sessionCookie carries the session token. Path / so the API sees it
// wherever it is mounted; HttpOnly so scripts cannot read it; Lax so the
// client page (same site, or the dev server's proxy) sends it and a
// cross-site form cannot ride on it.
const sessionCookie = "veyloom_session"

// AuthStatusResponse is the body of GET /api/v1/auth/status: whether the
// account still has to be created, and who is signed in, if anyone.
type AuthStatusResponse struct {
	SetupRequired bool        `json:"setup_required"`
	User          *store.User `json:"user"`
}

// CredentialsRequest is the body of POST /api/v1/auth/setup and /login.
type CredentialsRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
}

// ChangePasswordRequest is the body of POST /api/v1/me/password.
type ChangePasswordRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

type userKey struct{}

func withUser(ctx context.Context, user store.User) context.Context {
	return context.WithValue(ctx, userKey{}, user)
}

// userFrom returns the signed-in user the middleware put on the request.
func userFrom(ctx context.Context) (store.User, bool) {
	user, ok := ctx.Value(userKey{}).(store.User)
	return user, ok
}

// requireSession puts the signed-in user on the request and turns away
// everyone else, except from the routes that sign people in. Without an
// Authenticator (tests) every route stays open and nobody is signed in.
func (h *handlers) requireSession(next http.Handler) http.Handler {
	if h.deps.Auth == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			user, err := h.deps.Auth.UserForToken(r.Context(), c.Value)
			switch {
			case err == nil:
				r = r.WithContext(withUser(r.Context(), user))
			case !errors.Is(err, auth.ErrNoSession):
				h.deps.Logger.Error("session lookup failed", "err", err)
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		}
		if _, ok := userFrom(r.Context()); !ok && !openRoute(r) {
			writeError(w, http.StatusUnauthorized, "sign in first")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// openRoute names what an anonymous request may reach: CORS preflights
// and the sign-in routes themselves.
func openRoute(r *http.Request) bool {
	if r.Method == http.MethodOptions {
		return true
	}
	switch r.URL.Path {
	case "/api/v1/auth/status", "/api/v1/auth/setup", "/api/v1/auth/login":
		return true
	}
	return false
}

func (h *handlers) authStatus(w http.ResponseWriter, r *http.Request) {
	res := AuthStatusResponse{}
	if user, ok := userFrom(r.Context()); ok {
		res.User = &user
	}
	if h.deps.Auth != nil {
		required, err := h.deps.Auth.SetupRequired(r.Context())
		if err != nil {
			h.writeStoreError(w, r, err)
			return
		}
		res.SetupRequired = required
	}
	writeJSON(w, http.StatusOK, res)
}

// authSetup creates the one account and signs it in. 409 once it exists.
func (h *handlers) authSetup(w http.ResponseWriter, r *http.Request) {
	req, ok := h.credentials(w, r)
	if !ok {
		return
	}
	user, token, err := h.deps.Auth.Setup(r.Context(), req.Name, req.Password)
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	h.setSessionCookie(w, r, token)
	writeJSON(w, http.StatusCreated, UserResponse{User: user})
}

// authLogin checks the password and signs in. 401 for a wrong username or
// password, without saying which.
func (h *handlers) authLogin(w http.ResponseWriter, r *http.Request) {
	req, ok := h.credentials(w, r)
	if !ok {
		return
	}
	user, token, err := h.deps.Auth.Login(r.Context(), req.Name, req.Password)
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	h.setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, UserResponse{User: user})
}

func (h *handlers) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		if err := h.deps.Auth.Logout(r.Context(), c.Value); err != nil {
			h.writeStoreError(w, r, err)
			return
		}
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// changePassword needs the current password, signs every other browser
// out and hands this one a fresh session.
func (h *handlers) changePassword(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	var req ChangePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return
	}
	token, err := h.deps.Auth.ChangePassword(r.Context(), user.ID, req.Current, req.New)
	if errors.Is(err, auth.ErrBadCredentials) {
		// Not 401: the session is fine, the current password is not.
		writeError(w, http.StatusForbidden, "the current password is wrong")
		return
	}
	if err != nil {
		h.writeAuthError(w, r, err)
		return
	}
	h.setSessionCookie(w, r, token)
	w.WriteHeader(http.StatusNoContent)
}

// credentials decodes a name and password, refusing an empty name here so
// the response names the field.
func (h *handlers) credentials(w http.ResponseWriter, r *http.Request) (CredentialsRequest, bool) {
	if h.deps.Auth == nil {
		writeError(w, http.StatusNotImplemented, "sign-in is not configured")
		return CredentialsRequest{}, false
	}
	var req CredentialsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return CredentialsRequest{}, false
	}
	name, err := requireName("name", req.Name)
	if err != nil {
		writeReason(w, http.StatusBadRequest, err)
		return CredentialsRequest{}, false
	}
	req.Name = name
	return req, true
}

func (h *handlers) writeAuthError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrSetupDone):
		writeError(w, http.StatusConflict, "the account already exists; sign in instead")
	case errors.Is(err, auth.ErrWeakPassword):
		writeCoded(w, http.StatusBadRequest, "weakPassword", store.Params{"min": strconv.Itoa(auth.MinPasswordLen)}, strings.TrimPrefix(err.Error(), "auth: "))
	case errors.Is(err, auth.ErrLongPassword):
		writeCoded(w, http.StatusBadRequest, "passwordTooLong", store.Params{"max": strconv.Itoa(auth.MaxPasswordBytes)}, strings.TrimPrefix(err.Error(), "auth: "))
	case errors.Is(err, auth.ErrBadCredentials):
		writeError(w, http.StatusUnauthorized, "wrong username or password")
	default:
		h.writeStoreError(w, r, err)
	}
}

func (h *handlers) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.deps.Auth.TTL() / time.Second),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// isHTTPS is true behind TLS, directly or through a proxy that says so.
func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
