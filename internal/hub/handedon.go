package hub

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Work handed on, summed up (docs/design.md 5.22). A person asks a member;
// its turn wakes others, with send_message or by naming them in its reply,
// and they may wake others in turn. Once the last of them is done, the
// member the person asked is woken once more, in the topic they asked it
// in, to sum up what came of it: its last word is addressed to the person,
// and that is what reaches their inbox, once, however many worked. A wake
// the limits held back leaves the work to the person instead: the note of
// the hold reached them already. What is under way is kept in memory; work
// a restart of the hub cuts across is not summed up.

// errand is what one turn a person asked for set going, while it is under
// way: the turn itself, and every wake down the line.
type errand struct {
	// member and thread are the member asked and the topic it was asked
	// in; chain is the piece of work, which the summing up stays in.
	member store.Member
	thread store.Thread
	chain  string
	// open counts the asked turn while it runs, and the wakes whose turns
	// have not ended.
	open int
	// ok says the asked turn ended well; held that a wake down the line
	// was held back.
	ok, held bool
	// results are what the woken turns came to, the latest of each member.
	results []handedResult
}

// handedResult is what one woken turn came to, for the summing up.
type handedResult struct {
	MemberID string
	Member   string
	Topic    int
	Status   store.TurnStatus
	// Said is its last word; Error why it did not end well.
	Said  string
	Error string
}

// wakeKey is one wake: a member woken by a message.
type wakeKey struct {
	member  string
	message string
}

// handedResults caps the results a summing up is given.
const handedResults = 8

// record keeps what a woken turn came to, in place of what the same
// member came to before. The member asked is told, not told of.
func (e *errand) record(r handedResult) {
	if r.MemberID == e.member.ID {
		return
	}
	e.results = slices.DeleteFunc(e.results, func(old handedResult) bool { return old.MemberID == r.MemberID })
	e.results = append(e.results, r)
	if len(e.results) > handedResults {
		e.results = e.results[len(e.results)-handedResults:]
	}
}

// handOn notes that at wakes member with msg, as part of the work of the
// turn a person asked for that at is, or carries on. Called before the
// wake is set going.
func (m *TurnManager) handOn(at *activeTurn, member store.Member, msg store.Message) {
	origin := at.origin
	m.mu.Lock()
	defer m.mu.Unlock()
	if origin == "" {
		// at is the turn a person asked for: its own work, open while it
		// runs.
		origin = at.turn.ID
		if m.errands[origin] == nil {
			m.errands[origin] = &errand{member: at.member, thread: at.thread, chain: at.turn.ChainMessageID, open: 1}
		}
	}
	e := m.errands[origin]
	if e == nil {
		return
	}
	e.open++
	m.wakes[wakeKey{member: member.ID, message: msg.ID}] = origin
}

// handsOn says at hands its work on, as it ends with text: it woke members
// with send_message, or it names members its end wakes. Not where agents
// wake no one, nor, for a turn summing up, by naming.
func (m *TurnManager) handsOn(ctx context.Context, at *activeTurn, text string) bool {
	if at.relays == nil {
		return false
	}
	m.mu.Lock()
	_, woke := m.errands[at.turn.ID]
	m.mu.Unlock()
	if woke || len(at.handedOn) > 0 {
		return woke
	}
	at.mu.Lock()
	named := slices.ContainsFunc(at.relayTo, func(t relayTarget) bool { return t.member.Enabled && !at.named[t.member.ID] })
	at.mu.Unlock()
	if named || text == "" {
		return named
	}
	return slices.ContainsFunc(m.mentionedMembers(ctx, at, text), func(a store.Member) bool { return a.Enabled })
}

// heldBack notes that a wake by at was held back: the work it is part of
// is left to the person, not summed up.
func (m *TurnManager) heldBack(at *activeTurn) {
	origin := at.origin
	if origin == "" {
		origin = at.turn.ID
	}
	m.mu.Lock()
	if e := m.errands[origin]; e != nil {
		e.held = true
	}
	m.mu.Unlock()
}

// originOf is the turn a person asked for whose work a turn of memberID
// answering triggers carries on: that of the last trigger that woke it,
// "" when no agent's wake did.
func (m *TurnManager) originOf(memberID string, triggers []store.Message) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	origin := ""
	for _, t := range triggers {
		if o, ok := m.wakes[wakeKey{member: memberID, message: t.ID}]; ok {
			origin = o
		}
	}
	return origin
}

// askedOf is the member the person asked whose work origin set going; the
// zero member when there is none.
func (m *TurnManager) askedOf(origin string) store.Member {
	if origin == "" {
		return store.Member{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.errands[origin]; e != nil {
		return e.member
	}
	return store.Member{}
}

// reportsTo says naming memberID in at only reports back: it is the member
// asked, or the one that handed at the work, and hears of it when all the
// work handed on is done.
func (at *activeTurn) reportsTo(memberID string) bool {
	return memberID != "" && (memberID == at.asked.ID || memberID == at.waker.ID)
}

// handedBy is, for a woken turn's brief, whose work it is part of: the
// member asked and the person who asked it, and the member that handed it
// on, when that is another.
type handedBy struct {
	Asked  string
	Waker  string
	Person string
}

// handedByOf is whose work at is part of, nil for a turn no agent woke.
func (m *TurnManager) handedByOf(ctx context.Context, at *activeTurn) *handedBy {
	if at.asked.ID == "" {
		return nil
	}
	h := &handedBy{Asked: at.asked.DisplayName}
	if at.waker.ID != "" && at.waker.ID != at.asked.ID {
		h.Waker = at.waker.DisplayName
	}
	if id := m.chainPerson(ctx, at.turn.ChainMessageID); id != "" {
		if user, err := m.store.GetUser(ctx, id); err == nil {
			h.Person = user.Name
		}
	}
	return h
}

// handedByLine tells a member woken to take part in work handed on that
// its answer goes back when all of it is done, and naming whoever handed
// it the work wakes them not.
func handedByLine(h handedBy) string {
	person := "the person"
	if h.Person != "" {
		person = h.Person
	}
	var line string
	if h.Waker != "" {
		line = fmt.Sprintf("\n%s handed you this work, part of what %s asked %s for. When you are done, just say in your answer what you did: it goes back to %s once all the work handed on is done, "+
			"with what anyone you hand work to came to, and %s takes it from there. Naming %s or %s does not wake them, so do not send them a message to report; put a question for them in your answer.",
			h.Waker, person, h.Asked, h.Asked, h.Asked, h.Waker, h.Asked)
	} else {
		line = fmt.Sprintf("\n%s handed on this work for %s, and you take part in it. When you are done, just say in your answer what you did: it goes back to %s once all the work handed on is done, "+
			"with what anyone you hand work to came to, and %s takes it from there. Naming %s does not wake it, so do not send it a message to report; put a question for it in your answer.",
			h.Asked, person, h.Asked, h.Asked, h.Asked)
	}
	if h.Person != "" {
		line += fmt.Sprintf(" If only %s can settle something, mention @%s.", h.Person, h.Person)
	}
	return line + "\n"
}

// takeSumUp is the errand a turn answering triggers sums up, when one of
// them is the note that asks for it.
func (m *TurnManager) takeSumUp(triggers []store.Message) *errand {
	m.mu.Lock()
	defer m.mu.Unlock()
	var e *errand
	for _, t := range triggers {
		if s, ok := m.sumUps[t.ID]; ok {
			delete(m.sumUps, t.ID)
			e = s
		}
	}
	return e
}

// turnEnded settles the work at took part in, once its own wakes are set
// going: the wakes it answered, with what it came to, and, when a person
// asked for it, its own part. Work with nothing left open is summed up.
func (m *TurnManager) turnEnded(at *activeTurn, status store.TurnStatus, reason string) {
	at.mu.Lock()
	result := handedResult{MemberID: at.member.ID, Member: at.member.DisplayName, Topic: at.thread.Number, Status: status, Said: at.lastReply, Error: reason}
	at.mu.Unlock()
	done := m.settleWakes(at.member.ID, at.triggers, &result)
	m.mu.Lock()
	if e := m.errands[at.turn.ID]; e != nil {
		e.ok = status == store.TurnDone
		if e.open--; e.open == 0 {
			delete(m.errands, at.turn.ID)
			done = append(done, e)
		}
	}
	m.mu.Unlock()
	for _, e := range done {
		m.sumUp(e)
	}
}

// dropWakes settles the wakes of a turn of memberID that never began.
func (m *TurnManager) dropWakes(memberID string, triggers []store.Message) {
	for _, e := range m.settleWakes(memberID, triggers, nil) {
		m.sumUp(e)
	}
}

// settleWakes ends the wakes of memberID by triggers, keeping result
// with the work each was part of, and returns the work left with nothing
// open.
func (m *TurnManager) settleWakes(memberID string, triggers []store.Message, result *handedResult) []*errand {
	m.mu.Lock()
	defer m.mu.Unlock()
	var done []*errand
	recorded := map[string]bool{}
	for _, t := range triggers {
		key := wakeKey{member: memberID, message: t.ID}
		origin, ok := m.wakes[key]
		if !ok {
			continue
		}
		delete(m.wakes, key)
		e := m.errands[origin]
		if e == nil {
			continue
		}
		if result != nil && !recorded[origin] {
			e.record(*result)
			recorded[origin] = true
		}
		if e.open--; e.open == 0 {
			delete(m.errands, origin)
			done = append(done, e)
		}
	}
	return done
}

// sumUp wakes the member a person asked, now that the work its turn set
// going is done, to tell them what came of it: in the topic it was asked
// in, after a note saying why, in the same piece of work. Work the asked
// turn did not see through, or that a limit held back, is not.
func (m *TurnManager) sumUp(e *errand) {
	if !e.ok || e.held || len(e.results) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
		defer cancel()
		member, err := m.store.GetMember(ctx, e.member.ID)
		if err != nil || member.Removed() || !member.Enabled {
			return
		}
		names := make([]string, len(e.results))
		for i, r := range e.results {
			names[i] = r.Member
		}
		note, err := m.post(ctx, store.NewMessage{
			RoomID: e.thread.RoomID, ThreadID: e.thread.ID, SenderKind: store.SenderSystem,
			Body: sumUpNote(member.DisplayName, names),
		})
		if err != nil {
			m.logger.Error("tell of work handed on being done", "member", member.ID, "err", err)
			return
		}
		m.mu.Lock()
		m.sumUps[note.ID] = e
		m.mu.Unlock()
		if err := m.triggerFrom(ctx, member, note, e.thread, e.chain); err != nil {
			m.logger.Error("wake a member to sum up", "member", member.ID, "err", err)
		}
	}()
}

// sumUpNote says, in words the UI knows to put its own way, that the work
// asked of a member and handed on by it is done, and goes back to it.
func sumUpNote(asked string, members []string) string {
	return fmt.Sprintf("The work %s handed on is done (%s); back to %s.", asked, strings.Join(members, ", "), asked)
}

// chainPerson is the person whose message started a piece of work, ""
// when none did.
func (m *TurnManager) chainPerson(ctx context.Context, chain string) string {
	if chain == "" {
		return ""
	}
	msg, err := m.store.GetMessage(ctx, chain)
	if err != nil {
		return ""
	}
	return msg.UserID
}

// handedOnSection tells a member summing up what came of the work it
// handed on, and what to do with it.
func handedOnSection(results []handedResult) string {
	var b strings.Builder
	b.WriteString(runtime.HandedOnHeading + "\n")
	for _, r := range results {
		where := r.Member
		if r.Topic > 0 {
			where = fmt.Sprintf("%s (topic #%d)", r.Member, r.Topic)
		}
		switch r.Status {
		case store.TurnDone:
			fmt.Fprintf(&b, "- %s: %s\n", where, excerpt(oneLine(r.Said), handedExcerpt))
		case store.TurnCancelled:
			fmt.Fprintf(&b, "- %s: its turn was cancelled.\n", where)
		default:
			fmt.Fprintf(&b, "- %s: its turn failed: %s\n", where, excerpt(oneLine(r.Error), handedExcerpt))
		}
	}
	b.WriteString("Their turns have ended. If they need more from you, see to it: answer a question, act on what they found, or hand on with " + runtime.MessageToolSend +
		" the next step that waited on this, such as tests of the code just written; you are woken again once that is done. " +
		"Otherwise sum it up for the person who asked you, in a few lines: what was done, what is left, what needs them; your answer reaches them. " +
		"Begin with the work itself, not with how the turns went or what this note says. " +
		"Read the topics if you need more, and do not do their work again. Naming a member here only tells of it; to hand it work, use " + runtime.MessageToolSend + ".\n")
	return b.String()
}

// handedExcerpt caps what a summing up is told of one member's last word.
const handedExcerpt = 600
