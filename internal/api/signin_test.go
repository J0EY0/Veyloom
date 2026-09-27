package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSignInLimiter(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	l := newSignInLimiter(func() time.Time { return now })
	// Five wrong passwords, a minute apart.
	for i := range signInFailures {
		if _, w := l.begin("a"); w != 0 {
			t.Fatalf("try %d: wait %v", i+1, w)
		}
		if i < signInFailures-1 {
			now = now.Add(time.Minute)
		}
	}
	// The oldest of the five was at 12:00, the fifth at 12:04: till 12:15.
	if _, w := l.begin("a"); w != 11*time.Minute {
		t.Errorf("wait %v, want 11m", w)
	}
	if try, w := l.begin("b"); w != 0 {
		t.Errorf("another place is held too: %v", w)
	} else {
		try.right()
	}
	// Once the oldest is past, four are left: it may try again, and the
	// try counts before it is checked.
	now = now.Add(11 * time.Minute)
	try, w := l.begin("a")
	if w != 0 {
		t.Fatalf("at 12:15, wait %v", w)
	}
	if _, w := l.begin("a"); w != time.Minute {
		t.Errorf("the next oldest, 12:01, holds it till 12:16: wait %v", w)
	}
	// A try that checked no password is taken back; a right one clears
	// the count.
	try.void()
	again, w := l.begin("a")
	if w != 0 {
		t.Fatalf("the try taken back, wait %v", w)
	}
	again.right()
	if len(l.failed) != 0 {
		t.Errorf("cleared, remembered %v", l.failed)
	}
	// What is past is forgotten.
	l.begin("c")
	now = now.Add(signInWindow)
	if try, w := l.begin("c"); w != 0 || len(l.failed["c"]) != 1 {
		t.Errorf("a try a window ago: wait %v, remembered %v", w, l.failed)
	} else {
		try.void()
	}
	if len(l.failed) != 0 {
		t.Errorf("remembered %v", l.failed)
	}
}

// Tries made at once are counted as they come, before any is checked: a
// guesser sending many together gets no more than the few.
func TestSignInLimiter_TriesAtOnce(t *testing.T) {
	l := newSignInLimiter(nil)
	var wg sync.WaitGroup
	var tried atomic.Int32
	for range 100 {
		wg.Go(func() {
			if _, w := l.begin("a"); w == 0 {
				tried.Add(1)
			}
		})
	}
	wg.Wait()
	if n := tried.Load(); n != signInFailures {
		t.Errorf("%d tries got through, want %d", n, signInFailures)
	}
}

// A place is an address, an IPv6 one by its /64.
func TestClientAddr(t *testing.T) {
	for remote, want := range map[string]string{
		"192.0.2.7:51234":            "192.0.2.7",
		"[2001:db8:1:2:3:4:5:6]:443": "2001:db8:1:2::/64",
		"[2001:db8:1:2:ffff::1]:443": "2001:db8:1:2::/64",
		"[2001:db8:1:3::1]:443":      "2001:db8:1:3::/64",
		"[::ffff:192.0.2.7]:80":      "::ffff:192.0.2.7",
		"[fe80::1%en0]:80":           "fe80::/64",
		"not an address":             "not an address",
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
		r.RemoteAddr = remote
		if got := clientAddr(r); got != want {
			t.Errorf("%s: %s, want %s", remote, got, want)
		}
	}
}

func TestAuth_TooManyWrongPasswords(t *testing.T) {
	now := time.Now()
	handler := NewHandler(Deps{Users: newFakeUsers(), Auth: newFakeAuth(), Now: func() time.Time { return now }})
	if rec := send(t, handler, http.MethodPost, "/api/v1/auth/setup", `{"name":"jinghao","password":"correct horse"}`, ""); rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d", rec.Code)
	}
	for range signInFailures {
		if rec := send(t, handler, http.MethodPost, "/api/v1/auth/login", `{"name":"jinghao","password":"wrong horse"}`, ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("a wrong password: %d", rec.Code)
		}
	}
	// The right one too waits its turn.
	rec := send(t, handler, http.MethodPost, "/api/v1/auth/login", `{"name":"jinghao","password":"correct horse"}`, "")
	var refused ErrorResponse
	if json.Unmarshal(rec.Body.Bytes(), &refused); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "900" ||
		refused.Code != "tooManyAttempts" || refused.Params["minutes"] != "15" || refused.Params["seconds"] != "900" {
		t.Fatalf("after five wrong passwords: %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	now = now.Add(signInWindow)
	if rec := send(t, handler, http.MethodPost, "/api/v1/auth/login", `{"name":"jinghao","password":"correct horse"}`, ""); rec.Code != http.StatusOK {
		t.Fatalf("a window later: %d", rec.Code)
	}
	// Signed in, the count starts over.
	if rec := send(t, handler, http.MethodPost, "/api/v1/auth/login", `{"name":"jinghao","password":"wrong horse"}`, ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("after signing in, a wrong password: %d", rec.Code)
	}
}

// A session is no way to guess the password either.
func TestAuth_TooManyWrongCurrentPasswords(t *testing.T) {
	handler := NewHandler(Deps{Users: newFakeUsers(), Auth: newFakeAuth()})
	token := sessionCookieOf(t, send(t, handler, http.MethodPost, "/api/v1/auth/setup", `{"name":"jinghao","password":"correct horse"}`, "")).Value
	for range signInFailures {
		if rec := send(t, handler, http.MethodPost, "/api/v1/me/password", `{"current":"wrong horse","new":"battery staple"}`, token); rec.Code != http.StatusForbidden {
			t.Fatalf("a wrong current password: %d", rec.Code)
		}
	}
	if rec := send(t, handler, http.MethodPost, "/api/v1/me/password", `{"current":"correct horse","new":"battery staple"}`, token); rec.Code != http.StatusTooManyRequests {
		t.Errorf("after five wrong ones: %d", rec.Code)
	}
}
