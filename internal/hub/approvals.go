package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// pendingApproval is an approval request awaiting a decision.
type pendingApproval struct {
	// requestID is the runtime's id, which the decision is routed back with.
	requestID string
	tool      string
	input     string
	// messageID is the thread note announcing the request; the decision
	// rewrites it rather than adding a second note.
	messageID string
	// timer denies the request when nobody decides in time; nil when
	// approvals never expire.
	timer *time.Timer
}

// OnApproval takes a permission request from a running turn. It returns at
// once, like OnEvent; recording the request and telling the room happen on
// a goroutine. A request for a turn the hub no longer tracks is denied on
// the spot so the runtime does not wait for an answer that cannot come.
func (m *TurnManager) OnApproval(conn protocol.Conn, req protocol.ApprovalRequest) {
	m.mu.Lock()
	at := m.active[req.TurnID]
	m.mu.Unlock()
	if at == nil {
		go m.send(conn, protocol.ApprovalDecision{TurnID: req.TurnID, ApprovalID: req.ApprovalID, Decision: runtime.Decision{Allow: false, Message: "the turn is no longer running"}})
		return
	}

	at.mu.Lock()
	if at.transcript != nil {
		ev := runtime.Event{Kind: runtime.EventApprovalRequest, At: req.At, ApprovalID: req.ApprovalID, Tool: req.Tool, Input: req.Input}
		if err := at.transcript.write(transcriptLine{Kind: "event", At: req.At, Event: &ev}); err != nil {
			m.logger.Warn("transcript", "turn", req.TurnID, "err", err)
		}
	}
	at.mu.Unlock()
	go m.raise(at, req)
}

// raise registers the request, announces it in the thread and records it.
// The approval id is minted here so the request is tracked in memory
// before the row exists: a decision can then never arrive for an approval
// the hub does not know about.
func (m *TurnManager) raise(at *activeTurn, req protocol.ApprovalRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()

	id := store.NewID()
	p := &pendingApproval{requestID: req.ApprovalID, tool: req.Tool, input: req.Input}
	at.mu.Lock()
	if at.finished {
		at.mu.Unlock()
		return
	}
	at.pending[id] = p
	at.mu.Unlock()
	m.mu.Lock()
	m.approvals[id] = at
	m.mu.Unlock()

	// Asking permission ends whatever the agent was saying: that text is
	// stored first so the request note reads after it.
	m.closeSegment(ctx, at, true)

	name := at.member.DisplayName
	what := describeToolUse(req.Tool, req.Input)
	var messageID string
	note, err := m.post(ctx, store.NewMessage{
		RoomID:     at.thread.RoomID,
		ThreadID:   at.thread.ID,
		SenderKind: store.SenderSystem,
		Body:       fmt.Sprintf("%s wants to run %s (approval %s pending)", name, what, id),
		TurnID:     at.turn.ID,
	})
	if err != nil {
		m.logger.Error("announce approval", "turn", at.turn.ID, "err", err)
	} else {
		messageID = note.ID
		at.mu.Lock()
		p.messageID = note.ID
		at.mu.Unlock()
	}

	a, err := m.store.CreateApproval(ctx, store.NewApproval{
		ID:        id,
		TurnID:    at.turn.ID,
		RoomID:    at.thread.RoomID,
		ThreadID:  at.thread.ID,
		MemberID:  at.member.ID,
		RequestID: req.ApprovalID,
		Tool:      req.Tool,
		Input:     req.Input,
		MessageID: messageID,
	})
	if err != nil {
		// Unrecorded means undecidable: deny so the agent can move on.
		m.logger.Error("record approval", "turn", at.turn.ID, "err", err)
		if at, p := m.settle(id); at != nil {
			m.deliver(at, p, runtime.Decision{Allow: false, Message: "veyloom could not record the approval request"})
		}
		return
	}
	m.publish(approvalEvent(EventApprovalRequested, a))

	if m.approvalTimeout > 0 {
		at.mu.Lock()
		// Still pending: a decision cannot have arrived yet, but the turn
		// may have ended, in which case abandonApprovals handled it.
		if _, ok := at.pending[id]; ok {
			p.timer = time.AfterFunc(m.approvalTimeout, func() { m.expire(id) })
		}
		at.mu.Unlock()
	}
}

// Decide applies a person's decision to a pending approval and forwards it
// to the turn. The database settles who decided first; a decision that
// loses that race is store.ErrConflict.
func (m *TurnManager) Decide(ctx context.Context, approvalID, userID string, d runtime.Decision) (store.Approval, error) {
	status := store.ApprovalDenied
	if d.Allow {
		status = store.ApprovalAllowed
	}
	a, err := m.store.DecideApproval(ctx, approvalID, store.ApprovalOutcome{Status: status, Message: d.Message, DecidedBy: userID})
	if err != nil {
		return store.Approval{}, err
	}
	m.publish(approvalEvent(EventApprovalDecided, a))

	at, p := m.settle(approvalID)
	if at == nil {
		// Recorded but nobody is waiting: the turn ended in the same
		// instant, or the hub restarted since the request was raised.
		m.logger.Warn("approval decided for a turn that is not running", "approval", approvalID, "turn", a.TurnID)
		return a, nil
	}
	m.recordDecision(at, a)

	// Tell the thread before the machine, so the note always precedes
	// whatever the agent does with the decision.
	who := userID
	if user, err := m.store.GetUser(ctx, userID); err == nil {
		who = user.Name
	}
	what := describeToolUse(p.tool, p.input)
	if d.Allow {
		m.noteDecision(ctx, at, p, fmt.Sprintf("%s allowed %s to run %s", who, at.member.DisplayName, what))
	} else {
		m.noteDecision(ctx, at, p, fmt.Sprintf("%s denied %s running %s%s", who, at.member.DisplayName, what, suffix(d.Message)))
	}
	m.deliver(at, p, d)
	return a, nil
}

// expire denies a request nobody decided on in time. Losing the race to a
// person's decision is fine: then there is nothing left to do.
func (m *TurnManager) expire(approvalID string) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()

	reason := fmt.Sprintf("nobody decided within %s", m.approvalTimeout)
	a, err := m.store.DecideApproval(ctx, approvalID, store.ApprovalOutcome{Status: store.ApprovalExpired, Message: reason})
	if err != nil {
		if !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrNotFound) {
			m.logger.Error("expire approval", "approval", approvalID, "err", err)
		}
		return
	}
	m.publish(approvalEvent(EventApprovalDecided, a))
	at, p := m.settle(approvalID)
	if at == nil {
		return
	}
	m.recordDecision(at, a)
	m.noteDecision(ctx, at, p, fmt.Sprintf("%s's request to run %s expired: %s; denied", at.member.DisplayName, describeToolUse(p.tool, p.input), reason))
	m.deliver(at, p, runtime.Decision{Allow: false, Message: "approval timed out: " + reason})
}

// noteDecision records how a request was settled in the note that
// announced it, so the thread holds one line per request rather than two.
// The rewritten note is re-sent as a message event. Without a note to
// rewrite (its post failed) a fresh one is posted.
func (m *TurnManager) noteDecision(ctx context.Context, at *activeTurn, p *pendingApproval, text string) {
	at.mu.Lock()
	messageID := p.messageID
	at.mu.Unlock()
	if messageID == "" {
		m.postSystem(ctx, at.thread, at.turn.ID, text)
		return
	}
	msg, err := m.store.UpdateMessageBody(ctx, messageID, text, at.turn.ID, nil)
	if err != nil {
		m.logger.Error("update approval note", "turn", at.turn.ID, "err", err)
		m.postSystem(ctx, at.thread, at.turn.ID, text)
		return
	}
	m.publish(messageEvent(msg))
}

// abandonApprovals closes whatever a finishing turn still had pending. The
// database is updated first so that a decision racing with completion
// either lands before this and reaches the machine, or fails as a conflict.
func (m *TurnManager) abandonApprovals(ctx context.Context, at *activeTurn) {
	resolved, err := m.store.ResolveTurnApprovals(ctx, at.turn.ID, store.ApprovalCancelled, "the turn ended before a decision")
	if err != nil {
		m.logger.Error("resolve approvals", "turn", at.turn.ID, "err", err)
	}
	for _, a := range resolved {
		m.recordDecision(at, a)
		m.publish(approvalEvent(EventApprovalDecided, a))
	}

	at.mu.Lock()
	at.finished = true
	pending := at.pending
	at.pending = make(map[string]*pendingApproval)
	at.mu.Unlock()

	m.mu.Lock()
	for id := range pending {
		delete(m.approvals, id)
	}
	m.mu.Unlock()
	for _, p := range pending {
		if p.timer != nil {
			p.timer.Stop()
		}
	}
}

// settle removes a pending approval from the turn that raised it and stops
// its timer. It returns nil when the approval is not pending here.
func (m *TurnManager) settle(approvalID string) (*activeTurn, *pendingApproval) {
	m.mu.Lock()
	at := m.approvals[approvalID]
	delete(m.approvals, approvalID)
	m.mu.Unlock()
	if at == nil {
		return nil, nil
	}
	at.mu.Lock()
	p := at.pending[approvalID]
	delete(at.pending, approvalID)
	at.mu.Unlock()
	if p == nil {
		return nil, nil
	}
	if p.timer != nil {
		p.timer.Stop()
	}
	return at, p
}

// deliver sends a decision to the machine running the turn. If the machine is
// gone the turn is being failed by MachineGone anyway.
func (m *TurnManager) deliver(at *activeTurn, p *pendingApproval, d runtime.Decision) {
	conn, ok := m.connFor(at.member.MachineID)
	if !ok {
		m.logger.Warn("approval decided while its machine is offline", "turn", at.turn.ID)
		return
	}
	m.send(conn, protocol.ApprovalDecision{TurnID: at.turn.ID, ApprovalID: p.requestID, Decision: d})
}

// send delivers one message to a machine within the store timeout.
func (m *TurnManager) send(conn protocol.Conn, msg protocol.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()
	if err := conn.Send(ctx, msg); err != nil {
		m.logger.Warn("send to machine", "kind", msg.Kind(), "err", err)
	}
}

// recordDecision appends the settled approval to the turn's transcript.
func (m *TurnManager) recordDecision(at *activeTurn, a store.Approval) {
	at.mu.Lock()
	defer at.mu.Unlock()
	if at.transcript == nil {
		return
	}
	line := transcriptLine{Kind: "approval_decision", Approval: &transcriptApproval{
		ID: a.ID, RequestID: a.RequestID, Status: a.Status, Message: a.Message, DecidedBy: a.DecidedBy,
	}}
	if err := at.transcript.write(line); err != nil {
		m.logger.Warn("transcript", "turn", at.turn.ID, "err", err)
	}
}

// describeToolUse says what a tool call does in one line, for the people
// who decide on it. A shell command is shown as itself; anything else as
// the tool name and its input.
func describeToolUse(tool, input string) string {
	if tool == "Bash" {
		var in struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(input), &in) == nil && in.Command != "" {
			return "`" + truncateText(in.Command, maxToolUseSummary) + "`"
		}
	}
	return tool + " " + truncateText(input, maxToolUseSummary)
}

// maxToolUseSummary bounds how much of a tool input goes into a message.
const maxToolUseSummary = 500

// truncateText cuts s to at most max bytes on a rune boundary.
func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// approvalEvent is the live event for an approval in its current state.
func approvalEvent(kind EventKind, a store.Approval) Event {
	return Event{Kind: kind, RoomID: a.RoomID, Approval: &a}
}

func suffix(message string) string {
	if message == "" {
		return ""
	}
	return ": " + message
}
