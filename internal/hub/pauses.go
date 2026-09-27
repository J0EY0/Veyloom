package hub

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// A pause keeps turns from starting that would only fail (design.md
// 5.23.3): those of every member running on a machine with a runtime whose
// account cannot take turns now (signed out, its usage limit reached, too
// many requests, its provider failing), and those of a member whose turns
// keep failing. What they are asked meanwhile waits in their queue, and in
// the store, and goes on once the pause is lifted: when it runs out, when a
// person lifts it, or, for an account's, when a turn on the account goes
// well after all. The store keeps the pauses across hubs; the book here is
// what the hub goes by, read from the store on first use.

// failingTurns is how many failed turns in a row pause a member.
const failingTurns = 3

// accountKey names a runtime's account on a machine.
type accountKey struct{ machine, runtime string }

// pauseBook is the pauses in effect, by what they hold up; the last
// standing each account reported against its usage limits; and the
// failures in a row the lengths of pauses go by.
type pauseBook struct {
	// cooldown is how long an account rests after too many requests or its
	// provider failing, doubled each time it happens again in a row, up to
	// maxCooldown; grace how long past a usage limit's reset it rests still,
	// the limit's clock and the hub's being apart.
	cooldown, maxCooldown, grace time.Duration

	mu       sync.Mutex
	loaded   bool
	accounts map[accountKey]*pauseState
	members  map[string]*pauseState
	quotas   map[accountKey]runtime.Quota
	// strikes counts an account's rate-limit and server failures in a row;
	// failing a member's failed turns in a row. A turn that goes well
	// clears them.
	strikes map[accountKey]int
	failing map[string]int
	// lifts counts the pauses lifted: a member held while one was lifted,
	// and so passed by, is looked at again (letGo).
	lifts uint64
}

// pauseState is a pause in effect: the timer that lifts it when it runs
// out, and the places told what it holds up there, by thread (a room's
// own messages under "room:" and its id), each told once.
type pauseState struct {
	pause store.Pause
	timer *time.Timer
	told  map[string]bool
}

func newPauseBook() *pauseBook {
	return &pauseBook{
		cooldown: time.Minute, maxCooldown: 15 * time.Minute, grace: 30 * time.Second,
		accounts: make(map[accountKey]*pauseState),
		members:  make(map[string]*pauseState),
		quotas:   make(map[accountKey]runtime.Quota),
		strikes:  make(map[accountKey]int),
		failing:  make(map[string]int),
	}
}

// liftCount is how many pauses were lifted so far.
func (b *pauseBook) liftCount() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lifts
}

// inEffect reports whether a pause holds things up at now: one that ran
// out does not, whether or not its timer has lifted it yet.
func inEffect(p store.Pause, now time.Time) bool {
	return p.EndsAt == nil || p.EndsAt.After(now)
}

// loadPauses reads the pauses in effect from the store, once: those that
// ran out while no hub was there are lifted, the others set to run out.
// Failing to read leaves the book as it is, to be read next time.
func (m *TurnManager) loadPauses(ctx context.Context) {
	b := m.pauses
	b.mu.Lock()
	loaded := b.loaded
	b.mu.Unlock()
	if loaded {
		return
	}
	pauses, err := m.store.ListPauses(ctx)
	if err != nil {
		m.logger.Error("read the pauses", "err", err)
		return
	}
	b.mu.Lock()
	if b.loaded {
		b.mu.Unlock()
		return
	}
	b.loaded = true
	var over []store.Pause
	for _, p := range pauses {
		if !inEffect(p, time.Now()) {
			over = append(over, p)
			continue
		}
		b.put(m, p)
	}
	b.mu.Unlock()
	for _, p := range over {
		m.liftPause(context.WithoutCancel(ctx), p, "")
	}
}

// put files p as in effect, its timer set, keeping what was told of a
// pause of the same thing. Callers hold b.mu.
func (b *pauseBook) put(m *TurnManager, p store.Pause) *pauseState {
	var st *pauseState
	if p.Account() {
		key := accountKey{p.MachineID, p.Runtime}
		if st = b.accounts[key]; st == nil {
			st = &pauseState{told: make(map[string]bool)}
			b.accounts[key] = st
		}
	} else {
		if st = b.members[p.MemberID]; st == nil {
			st = &pauseState{told: make(map[string]bool)}
			b.members[p.MemberID] = st
		}
	}
	st.pause = p
	if st.timer != nil {
		st.timer.Stop()
		st.timer = nil
	}
	if p.EndsAt != nil {
		lifted := p
		st.timer = time.AfterFunc(time.Until(*p.EndsAt), func() {
			ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
			defer cancel()
			m.liftPause(ctx, lifted, "")
		})
	}
	return st
}

// pauseHolding returns the pause that holds memberID up, nil when none
// does: the member's own, or its account's. Only with an account's pause
// in effect is the member read, to know its account.
func (m *TurnManager) pauseHolding(ctx context.Context, memberID string) *store.Pause {
	m.loadPauses(ctx)
	b := m.pauses
	now := time.Now()
	b.mu.Lock()
	if st := b.members[memberID]; st != nil && inEffect(st.pause, now) {
		p := st.pause
		b.mu.Unlock()
		return &p
	}
	accounts := len(b.accounts) > 0
	b.mu.Unlock()
	if !accounts {
		return nil
	}
	key, err := m.accountOf(ctx, memberID)
	if err != nil {
		m.logger.Warn("the account a member runs on", "member", memberID, "err", err)
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if st := b.accounts[key]; st != nil && inEffect(st.pause, now) {
		p := st.pause
		return &p
	}
	return nil
}

// accountOf is the account memberID runs on: its machine and its agent's
// runtime.
func (m *TurnManager) accountOf(ctx context.Context, memberID string) (accountKey, error) {
	member, err := m.store.GetMember(ctx, memberID)
	if err != nil {
		return accountKey{}, err
	}
	agent, err := m.store.GetAgent(ctx, member.AgentID)
	if err != nil {
		return accountKey{}, err
	}
	return accountKey{member.MachineID, agent.Runtime}, nil
}

// pauseReasons are the failures of an account's that pause it.
var pauseReasons = map[runtime.FailureKind]store.PauseReason{
	runtime.FailureAuth:      store.PauseAuth,
	runtime.FailureQuota:     store.PauseQuota,
	runtime.FailureRateLimit: store.PauseRateLimit,
	runtime.FailureServer:    store.PauseServer,
}

// turnSettled weighs a chat turn's outcome for the pauses, as it ends: one
// that went well lifts its account's pause and its member's and clears
// their counts; a failure of the account's pauses the account, even of a
// turn done all the same, the asking for its reply failing so
// (askedAnswer); any other failure counts against the member, and enough
// in a row pause it. A cancelled turn says nothing. Runs on the turn's
// executor. It returns the pause the turn's end put in effect, nil when
// none.
func (m *TurnManager) turnSettled(ctx context.Context, at *activeTurn, done doneOutcome) *store.Pause {
	m.loadPauses(ctx)
	key := accountKey{at.member.MachineID, at.agent.Runtime}
	b := m.pauses
	_, account := pauseReasons[done.failure]
	switch {
	case done.cancelled:
		return nil
	case done.err == "" && !account:
		// It went well: the account can take turns, unless the turn itself
		// said its limit was reached as it ended.
		at.mu.Lock()
		limited := at.quotaLimited
		at.mu.Unlock()
		b.mu.Lock()
		delete(b.strikes, key)
		delete(b.failing, at.member.ID)
		account, member := b.accounts[key], b.members[at.member.ID]
		if limited {
			account = nil
		}
		b.mu.Unlock()
		for _, st := range []*pauseState{account, member} {
			if st != nil {
				m.liftPause(ctx, st.pause, "")
			}
		}
		return nil
	}
	if reason, ok := pauseReasons[done.failure]; ok {
		var ends *time.Time
		switch reason {
		case store.PauseQuota:
			if !done.retryAt.IsZero() {
				t := done.retryAt.Add(b.grace)
				ends = &t
			}
		case store.PauseRateLimit, store.PauseServer:
			b.mu.Lock()
			b.strikes[key]++
			strikes := b.strikes[key]
			b.mu.Unlock()
			rest := min(b.cooldown<<(min(strikes, 5)-1), b.maxCooldown)
			t := time.Now().Add(rest)
			ends = &t
		}
		p, err := m.store.PauseAccount(ctx, key.machine, key.runtime, reason, done.detail, ends)
		if err != nil {
			m.logger.Error("pause an account", "machine", key.machine, "runtime", key.runtime, "err", err)
			return nil
		}
		m.putPause(p)
		return &p
	}
	at.mu.Lock()
	hubs := at.noRetry
	at.mu.Unlock()
	if hubs {
		// The hub's own doing, or its machine's going (failHere): the
		// member is not at fault.
		return nil
	}
	b.mu.Lock()
	b.failing[at.member.ID]++
	failing := b.failing[at.member.ID]
	b.mu.Unlock()
	if failing < failingTurns {
		return nil
	}
	p, err := m.store.PauseMember(ctx, at.member.ID, store.PauseFailing, done.detail, nil)
	if err != nil {
		m.logger.Error("pause a member", "member", at.member.ID, "err", err)
		return nil
	}
	m.putPause(p)
	return &p
}

// doneOutcome is how a turn ended, as the pauses weigh it: err is why it
// failed, detail what the runtime said last it failed, that or the asking
// for its reply failing (askedAnswer).
type doneOutcome struct {
	err, detail string
	cancelled   bool
	failure     runtime.FailureKind
	retryAt     time.Time
}

// quotaReported takes in how a turn's account stands against its usage
// limits: it is kept for the machines' page, and a limit reached pauses
// the account until it resets. Runs on the turn's executor.
func (m *TurnManager) quotaReported(ctx context.Context, at *activeTurn, q runtime.Quota) {
	key := accountKey{at.member.MachineID, at.agent.Runtime}
	b := m.pauses
	b.mu.Lock()
	b.quotas[key] = q
	b.mu.Unlock()
	at.mu.Lock()
	at.quotaLimited = q.Limited
	at.mu.Unlock()
	if !q.Limited {
		return
	}
	m.loadPauses(ctx)
	var ends *time.Time
	if !q.ResetsAt.IsZero() {
		t := q.ResetsAt.Add(b.grace)
		ends = &t
	}
	detail := "usage limit reached"
	if q.Window != "" {
		detail = fmt.Sprintf("usage limit reached (%s)", q.Window)
	}
	p, err := m.store.PauseAccount(ctx, key.machine, key.runtime, store.PauseQuota, detail, ends)
	if err != nil {
		m.logger.Error("pause an account", "machine", key.machine, "runtime", key.runtime, "err", err)
		return
	}
	m.putPause(p)
}

// Quotas returns the last standing each runtime on machineID reported
// against its usage limits, by runtime.
func (m *TurnManager) Quotas(machineID string) map[string]runtime.Quota {
	b := m.pauses
	b.mu.Lock()
	defer b.mu.Unlock()
	var out map[string]runtime.Quota
	for key, q := range b.quotas {
		if key.machine == machineID {
			if out == nil {
				out = make(map[string]runtime.Quota)
			}
			out[key.runtime] = q
		}
	}
	return out
}

// putPause files a pause the store now holds and tells everyone of it.
func (m *TurnManager) putPause(p store.Pause) {
	m.pauses.mu.Lock()
	m.pauses.put(m, p)
	m.pauses.mu.Unlock()
	m.publish(Event{Kind: EventPause, At: time.Now(), Pause: &p})
}

// Pauses returns the pauses in effect, the oldest first.
func (m *TurnManager) Pauses(ctx context.Context) []store.Pause {
	m.loadPauses(ctx)
	b := m.pauses
	now := time.Now()
	b.mu.Lock()
	var out []store.Pause
	for _, st := range slices.Concat(slices.Collect(maps.Values(b.accounts)), slices.Collect(maps.Values(b.members))) {
		if inEffect(st.pause, now) {
			out = append(out, st.pause)
		}
	}
	b.mu.Unlock()
	slices.SortFunc(out, func(a, b store.Pause) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return out
}

// LiftPause lifts the pause id at a person's asking, and what it held up
// goes on. ErrUnknownPause when no such pause is in effect.
func (m *TurnManager) LiftPause(ctx context.Context, id, userID string) error {
	for _, p := range m.Pauses(ctx) {
		if p.ID == id {
			m.liftPause(ctx, p, userID)
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrUnknownPause, id)
}

// liftPause lifts p, by a person when userID is set, unless a pause of the
// same thing came after it; what waited goes on. A pause lifted twice is
// lifted once.
func (m *TurnManager) liftPause(ctx context.Context, p store.Pause, userID string) {
	b := m.pauses
	b.mu.Lock()
	var st *pauseState
	if p.Account() {
		key := accountKey{p.MachineID, p.Runtime}
		if st = b.accounts[key]; st != nil && st.pause.ID == p.ID {
			delete(b.accounts, key)
		}
	} else if st = b.members[p.MemberID]; st != nil && st.pause.ID == p.ID {
		delete(b.members, p.MemberID)
	}
	if st != nil && st.pause.ID == p.ID && st.timer != nil {
		st.timer.Stop()
	}
	b.lifts++
	if userID != "" {
		// A person looked into it: the counts start over.
		if p.Account() {
			delete(b.strikes, accountKey{p.MachineID, p.Runtime})
		} else {
			delete(b.failing, p.MemberID)
		}
	}
	b.mu.Unlock()

	var err error
	if p.Account() {
		_, err = m.store.LiftAccountPause(ctx, p.MachineID, p.Runtime)
	} else {
		_, err = m.store.LiftMemberPause(ctx, p.MemberID)
	}
	if err != nil {
		m.logger.Error("lift a pause", "pause", p.ID, "err", err)
	}
	m.logger.Info("pause lifted", "pause", p.ID, "reason", p.Reason, "machine", p.MachineID, "runtime", p.Runtime, "member", p.MemberID, "by", userID)
	lifted := p
	m.publish(Event{Kind: EventPause, At: time.Now(), Pause: &lifted, Lifted: true, UserID: userID})
	// What waited for nothing but a pause, now perhaps lifted: taking it
	// up looks at the pauses again.
	m.resume(func(string, *memberState) bool { return true })
}

// tellPaused says, once per pause in each place, why what was asked there
// waits: in t's topic, or in the room for a message to the room.
func (m *TurnManager) tellPaused(ctx context.Context, p store.Pause, member store.Member, thread store.Thread) {
	place := thread.ID
	if place == "" {
		place = "room:" + member.RoomID
	}
	b := m.pauses
	b.mu.Lock()
	var st *pauseState
	if p.Account() {
		st = b.accounts[accountKey{p.MachineID, p.Runtime}]
	} else {
		st = b.members[p.MemberID]
	}
	told := st == nil || st.told[place]
	if !told {
		st.told[place] = true
	}
	b.mu.Unlock()
	if told {
		return
	}
	machine := ""
	if p.Account() {
		if rec, err := m.store.GetMachine(ctx, p.MachineID); err == nil {
			machine = rec.Name
		}
	}
	if thread.ID == "" {
		thread = store.Thread{RoomID: member.RoomID}
	}
	m.postSystem(ctx, thread, "", pauseNote(member.DisplayName, p, machine))
}

// pauseNote says what keeps member waiting, in the hub's words, which the
// UI says its own way (systemNote.ts): the times as RFC 3339, for it to
// show in the reader's own zone.
func pauseNote(member string, p store.Pause, machine string) string {
	at := ""
	if p.EndsAt != nil {
		at = p.EndsAt.UTC().Format(time.RFC3339)
	}
	switch {
	case p.Reason == store.PauseQuota && at != "":
		return fmt.Sprintf("%s waits for %s's usage limit on %s to reset at %s.", member, p.Runtime, machine, at)
	case p.Reason == store.PauseQuota:
		return fmt.Sprintf("%s waits: %s's usage limit on %s is reached; resume it once the account has more.", member, p.Runtime, machine)
	case p.Reason == store.PauseAuth:
		return fmt.Sprintf("%s waits: %s is signed out on %s; sign it in again there, then resume it.", member, p.Runtime, machine)
	case p.Reason == store.PauseRateLimit:
		return fmt.Sprintf("%s waits: %s on %s was turned down for too many requests; it tries again at %s.", member, p.Runtime, machine, at)
	case p.Reason == store.PauseServer:
		return fmt.Sprintf("%s waits: %s's service failed on %s; it tries again at %s.", member, p.Runtime, machine, at)
	}
	return fmt.Sprintf("%s waits: its last %d turns failed; resume it once what failed is seen to.", member, failingTurns)
}
