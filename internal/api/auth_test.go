package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/auth"
	"github.com/J0EY0/veyloom/internal/store"
)

// fakeAuth stands in for internal/auth: one account, plain-text password,
// tokens t1, t2, ...
type fakeAuth struct {
	account  *store.User
	password string
	sessions map[string]store.User
}

func newFakeAuth() *fakeAuth { return &fakeAuth{sessions: map[string]store.User{}} }

func (f *fakeAuth) SetupRequired(context.Context) (bool, error) { return f.account == nil, nil }

func (f *fakeAuth) Setup(_ context.Context, name, password string) (store.User, string, error) {
	if f.account != nil {
		return store.User{}, "", auth.ErrSetupDone
	}
	if len(password) < auth.MinPasswordLen {
		return store.User{}, "", auth.ErrWeakPassword
	}
	if len(password) > auth.MaxPasswordBytes {
		return store.User{}, "", auth.ErrLongPassword
	}
	u := store.User{ID: "u1", Name: name}
	f.account, f.password = &u, password
	return u, f.open(u), nil
}

func (f *fakeAuth) Login(_ context.Context, name, password string) (store.User, string, error) {
	if f.account == nil || name != f.account.Name || password != f.password {
		return store.User{}, "", auth.ErrBadCredentials
	}
	return *f.account, f.open(*f.account), nil
}

func (f *fakeAuth) Logout(_ context.Context, token string) error {
	delete(f.sessions, token)
	return nil
}

func (f *fakeAuth) UserForToken(_ context.Context, token string) (store.User, error) {
	u, ok := f.sessions[token]
	if !ok {
		return store.User{}, auth.ErrNoSession
	}
	return u, nil
}

func (f *fakeAuth) ChangePassword(_ context.Context, _ string, current, next string) (string, error) {
	if current != f.password {
		return "", auth.ErrBadCredentials
	}
	if len(next) < auth.MinPasswordLen {
		return "", auth.ErrWeakPassword
	}
	if len(next) > auth.MaxPasswordBytes {
		return "", auth.ErrLongPassword
	}
	f.password = next
	f.sessions = map[string]store.User{}
	return f.open(*f.account), nil
}

func (f *fakeAuth) TTL() time.Duration { return time.Hour }

func (f *fakeAuth) open(u store.User) string {
	token := fmt.Sprintf("t%d", len(f.sessions)+1)
	f.sessions[token] = u
	return token
}

// send sends a request with an optional JSON body and session cookie.
func send(t *testing.T, handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != "" {
		reader = strings.NewReader(body)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: token})
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func sessionCookieOf(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	t.Fatalf("no %s cookie in %v", sessionCookie, rec.Header())
	return nil
}

func decodeStatus(t *testing.T, rec *httptest.ResponseRecorder) AuthStatusResponse {
	t.Helper()
	var res AuthStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	return res
}

func TestAuth_SetupSignsInAndGuardsTheRest(t *testing.T) {
	users := newFakeUsers()
	handler := NewHandler(Deps{Users: users, Auth: newFakeAuth()})

	// Nothing but the sign-in routes before the account exists.
	if rec := send(t, handler, http.MethodGet, "/api/v1/me", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("me without a session: %d", rec.Code)
	}
	if rec := send(t, handler, http.MethodGet, "/api/v1/users", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("users without a session: %d", rec.Code)
	}
	status := decodeStatus(t, send(t, handler, http.MethodGet, "/api/v1/auth/status", "", ""))
	if !status.SetupRequired || status.User != nil {
		t.Errorf("status before setup: %+v", status)
	}

	// Named by a code, for the web client to tell in the person's language.
	short := send(t, handler, http.MethodPost, "/api/v1/auth/setup", `{"name":"jinghao","password":"short"}`, "")
	var weak ErrorResponse
	if json.Unmarshal(short.Body.Bytes(), &weak); short.Code != http.StatusBadRequest || weak.Code != "weakPassword" || weak.Params["min"] != strconv.Itoa(auth.MinPasswordLen) || weak.Error == "" {
		t.Errorf("weak password: %d %s", short.Code, short.Body.String())
	}
	// bcrypt takes 72 bytes at most: 25 Chinese characters are 75.
	long := send(t, handler, http.MethodPost, "/api/v1/auth/setup", `{"name":"jinghao","password":"`+strings.Repeat("长", 25)+`"}`, "")
	var tooLong ErrorResponse
	if json.Unmarshal(long.Body.Bytes(), &tooLong); long.Code != http.StatusBadRequest || tooLong.Code != "passwordTooLong" || tooLong.Params["max"] != "72" {
		t.Errorf("a password over 72 bytes: %d %s", long.Code, long.Body.String())
	}
	if rec := send(t, handler, http.MethodPost, "/api/v1/auth/setup", `{"name":"  ","password":"correct horse"}`, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: %d", rec.Code)
	}
	rec := send(t, handler, http.MethodPost, "/api/v1/auth/setup", `{"name":" jinghao ","password":"correct horse"}`, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	cookie := sessionCookieOf(t, rec)
	if !cookie.HttpOnly || cookie.Path != "/" || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 3600 || cookie.Secure {
		t.Errorf("cookie attributes: %+v", cookie)
	}
	token := cookie.Value

	// The session opens everything; the status says who.
	if rec := send(t, handler, http.MethodGet, "/api/v1/me", "", token); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"jinghao"`) {
		t.Errorf("me with a session: %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(t, handler, http.MethodGet, "/api/v1/users", "", token); rec.Code != http.StatusOK {
		t.Errorf("users with a session: %d", rec.Code)
	}
	status = decodeStatus(t, send(t, handler, http.MethodGet, "/api/v1/auth/status", "", token))
	if status.SetupRequired || status.User == nil || status.User.Name != "jinghao" {
		t.Errorf("status after setup: %+v", status)
	}
	if rec := send(t, handler, http.MethodPost, "/api/v1/auth/setup", `{"name":"again","password":"correct horse"}`, ""); rec.Code != http.StatusConflict {
		t.Errorf("second setup: %d", rec.Code)
	}
	if rec := send(t, handler, http.MethodGet, "/api/v1/me", "", "forged"); rec.Code != http.StatusUnauthorized {
		t.Errorf("forged token: %d", rec.Code)
	}
}

func TestAuth_LoginLogoutAndPassword(t *testing.T) {
	fake := newFakeAuth()
	handler := NewHandler(Deps{Users: newFakeUsers(), Auth: fake})
	send(t, handler, http.MethodPost, "/api/v1/auth/setup", `{"name":"jinghao","password":"correct horse"}`, "")

	if rec := send(t, handler, http.MethodPost, "/api/v1/auth/login", `{"name":"jinghao","password":"wrong"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong password: %d", rec.Code)
	}
	if rec := send(t, handler, http.MethodPost, "/api/v1/auth/login", `{"name":"","password":"correct horse"}`, ""); rec.Code != http.StatusBadRequest {
		t.Errorf("empty name: %d", rec.Code)
	}
	rec := send(t, handler, http.MethodPost, "/api/v1/auth/login", `{"name":"jinghao","password":"correct horse"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	token := sessionCookieOf(t, rec).Value

	rec = send(t, handler, http.MethodPost, "/api/v1/auth/logout", "", token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}
	if c := sessionCookieOf(t, rec); c.Value != "" || c.MaxAge != -1 {
		t.Errorf("logout should clear the cookie: %+v", c)
	}
	if rec := send(t, handler, http.MethodGet, "/api/v1/me", "", token); rec.Code != http.StatusUnauthorized {
		t.Errorf("me after logout: %d", rec.Code)
	}

	// Change the password: the current one is checked, the new one must
	// be long enough, other sessions end, this browser gets a new one.
	token = sessionCookieOf(t, send(t, handler, http.MethodPost, "/api/v1/auth/login", `{"name":"jinghao","password":"correct horse"}`, "")).Value
	other := sessionCookieOf(t, send(t, handler, http.MethodPost, "/api/v1/auth/login", `{"name":"jinghao","password":"correct horse"}`, "")).Value
	if rec := send(t, handler, http.MethodPost, "/api/v1/me/password", `{"current":"wrong","new":"battery staple"}`, token); rec.Code != http.StatusForbidden {
		t.Errorf("wrong current password: %d", rec.Code)
	}
	if rec := send(t, handler, http.MethodPost, "/api/v1/me/password", `{"current":"correct horse","new":"short"}`, token); rec.Code != http.StatusBadRequest {
		t.Errorf("weak new password: %d", rec.Code)
	}
	rec = send(t, handler, http.MethodPost, "/api/v1/me/password", `{"current":"correct horse","new":"battery staple"}`, token)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change password: %d %s", rec.Code, rec.Body.String())
	}
	fresh := sessionCookieOf(t, rec).Value
	if rec := send(t, handler, http.MethodGet, "/api/v1/me", "", other); rec.Code != http.StatusUnauthorized {
		t.Error("the other browser should be signed out")
	}
	if rec := send(t, handler, http.MethodGet, "/api/v1/me", "", fresh); rec.Code != http.StatusOK {
		t.Error("this browser should stay signed in")
	}
	if rec := send(t, handler, http.MethodPost, "/api/v1/auth/login", `{"name":"jinghao","password":"battery staple"}`, ""); rec.Code != http.StatusOK {
		t.Error("the new password should sign in")
	}
}

func TestMe_RenamesThroughTheStore(t *testing.T) {
	users := newFakeUsers()
	fake := newFakeAuth()
	handler := NewHandler(Deps{Users: users, Auth: fake})
	token := sessionCookieOf(t, send(t, handler, http.MethodPost, "/api/v1/auth/setup", `{"name":"jinghao","password":"correct horse"}`, "")).Value
	// The fake authenticator's user is not in fakeUsers; put them there.
	users.users["u1"] = store.User{ID: "u1", Name: "jinghao"}

	rec := send(t, handler, http.MethodPatch, "/api/v1/me", `{"name":" Jinghao Xian "}`, token)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"Jinghao Xian"`) {
		t.Errorf("rename: %d %s", rec.Code, rec.Body.String())
	}
	if rec := send(t, handler, http.MethodPatch, "/api/v1/me", `{"name":"  "}`, token); rec.Code != http.StatusBadRequest {
		t.Errorf("blank name: %d", rec.Code)
	}
}

func TestAuth_WithoutAnAuthenticatorEverythingIsOpen(t *testing.T) {
	handler := NewHandler(Deps{Users: newFakeUsers()})
	if rec := send(t, handler, http.MethodGet, "/api/v1/users", "", ""); rec.Code != http.StatusOK {
		t.Errorf("users: %d", rec.Code)
	}
	if status := decodeStatus(t, send(t, handler, http.MethodGet, "/api/v1/auth/status", "", "")); status.SetupRequired || status.User != nil {
		t.Errorf("status: %+v", status)
	}
	if rec := send(t, handler, http.MethodPost, "/api/v1/auth/login", `{"name":"x","password":"y"}`, ""); rec.Code != http.StatusNotImplemented {
		t.Errorf("login: %d", rec.Code)
	}
	if rec := send(t, handler, http.MethodGet, "/api/v1/me", "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("me: %d", rec.Code)
	}
}

func TestAuth_SessionDecidesWhoActs(t *testing.T) {
	ctx := context.Background()
	projects := newFakeProjects()
	_, room, _ := projects.CreateProject(ctx, store.NewProject{Name: "p"})
	turns := fakeTurns{"t1": {ID: "t1", RoomID: room.ID, Status: store.TurnRunning}}
	approvals := fakeApprovals{"a1": {ID: "a1", TurnID: "t1", RoomID: room.ID, Tool: "Bash", Input: json.RawMessage(`{"command":"make test"}`), Status: store.ApprovalPending}}
	chat := &fakeChat{approvals: approvals}
	handler := NewHandler(Deps{Projects: projects, Turns: turns, Approvals: approvals, Chat: chat, Users: newFakeUsers(), Auth: newFakeAuth()})
	token := sessionCookieOf(t, send(t, handler, http.MethodPost, "/api/v1/auth/setup", `{"name":"jinghao","password":"correct horse"}`, "")).Value

	// Whatever user_id the body claims, the session's user decides.
	rec := send(t, handler, http.MethodPost, "/api/v1/approvals/a1/decide", `{"user_id":"someone-else","allow":true}`, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("decide: %d %s", rec.Code, rec.Body.String())
	}
	if got := approvals["a1"].DecidedBy; got != "u1" {
		t.Errorf("decided by %q, want the session's u1", got)
	}
}
