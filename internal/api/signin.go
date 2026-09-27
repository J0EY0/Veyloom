package api

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// Wrong passwords are limited (docs/design.md 5.23.9): a guess costs a
// guesser no more than a bcrypt comparison, so once signInFailures of them
// come from one place within signInWindow, it is refused until the oldest
// of them is signInWindow old. A right password clears the count. Sign-in
// is counted by address, not for the account: there is one account, and
// nobody is to lock its owner out from elsewhere. A change of password is
// counted by the account, whose session it takes.
const (
	signInFailures = 5
	signInWindow   = 15 * time.Minute
	// signInKeys caps the places remembered; past it the stalest go.
	signInKeys = 10_000
)

// signInLimiter counts wrong passwords by where they came from.
type signInLimiter struct {
	mu  sync.Mutex
	now func() time.Time
	// failed are each place's latest tries within the window, oldest first,
	// signInFailures of them at most: its wrong passwords, and its tries
	// not checked yet, counted as wrong until they prove right.
	failed map[string][]time.Time
}

func newSignInLimiter(now func() time.Time) *signInLimiter {
	if now == nil {
		now = time.Now
	}
	return &signInLimiter{now: now, failed: make(map[string][]time.Time)}
}

// signInTry is a try at a password, counted as a wrong one from the start
// (begin): tries made at once cannot all get past the count before the
// first of them is checked, bcrypt taking its time over each.
type signInTry struct {
	l   *signInLimiter
	key string
	at  time.Time
}

// begin takes a try from key, or says how long key must wait before it may
// try again.
func (l *signInLimiter) begin(key string) (signInTry, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	failed := l.recent(key, now)
	if len(failed) >= signInFailures {
		return signInTry{}, failed[0].Add(signInWindow).Sub(now)
	}
	l.failed[key] = append(failed, now)
	if len(l.failed) > signInKeys {
		l.prune(now)
	}
	return signInTry{l: l, key: key, at: now}, 0
}

// right settles a try whose password was right: its place's count is
// cleared. A wrong one needs settling no further: it is counted.
func (t signInTry) right() {
	t.l.mu.Lock()
	defer t.l.mu.Unlock()
	delete(t.l.failed, t.key)
}

// void takes back a try that checked no password, failing for another
// reason.
func (t signInTry) void() {
	t.l.mu.Lock()
	defer t.l.mu.Unlock()
	failed := t.l.failed[t.key]
	i := slices.IndexFunc(failed, t.at.Equal)
	if i < 0 {
		return
	}
	if failed = slices.Delete(slices.Clone(failed), i, i+1); len(failed) == 0 {
		delete(t.l.failed, t.key)
	} else {
		t.l.failed[t.key] = failed
	}
}

// recent is key's tries counted within the window as of now; older ones
// are forgotten. Called with l.mu held.
func (l *signInLimiter) recent(key string, now time.Time) []time.Time {
	failed := l.failed[key]
	for len(failed) > 0 && now.Sub(failed[0]) >= signInWindow {
		failed = failed[1:]
	}
	if len(failed) == 0 {
		delete(l.failed, key)
		return nil
	}
	l.failed[key] = failed
	return failed
}

// prune forgets the places whose wrong passwords are all past, then, were
// that not enough, those whose latest is oldest. Called with l.mu held.
func (l *signInLimiter) prune(now time.Time) {
	for key := range l.failed {
		l.recent(key, now)
	}
	for len(l.failed) > signInKeys {
		var stalest string
		var at time.Time
		for key, failed := range l.failed {
			if last := failed[len(failed)-1]; stalest == "" || last.Before(at) {
				stalest, at = key, last
			}
		}
		delete(l.failed, stalest)
	}
}

// clientAddr is where a request came from, without its port; an IPv6
// address by its /64, any address of which one host may take. Behind a
// reverse proxy it is the proxy's, which then counts for everyone.
func clientAddr(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = h
	}
	if ip, err := netip.ParseAddr(host); err == nil && ip.Is6() && !ip.Is4In6() {
		if p, err := ip.WithZone("").Prefix(64); err == nil {
			return p.String()
		}
	}
	return host
}

// writeTooManyAttempts refuses a try that came too soon after too many
// wrong passwords, saying when the next may come.
func writeTooManyAttempts(w http.ResponseWriter, wait time.Duration) {
	seconds := int((wait + time.Second - 1) / time.Second)
	minutes := (seconds + 59) / 60
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeCoded(w, http.StatusTooManyRequests, "tooManyAttempts",
		store.Params{"seconds": strconv.Itoa(seconds), "minutes": strconv.Itoa(minutes)},
		fmt.Sprintf("too many wrong passwords; try again in %d seconds", seconds))
}
