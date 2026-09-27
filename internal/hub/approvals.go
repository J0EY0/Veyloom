package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
	kind      store.ApprovalKind
	tool      string
	input     string
	// messageID is the thread note announcing the request; the decision
	// rewrites it rather than adding a second note.
	messageID string
	// timer denies the request when nobody decides in time; nil when
	// approvals never expire.
	timer *time.Timer
	// recorded is set once the approval's row exists: only then can it be
	// closed.
	recorded bool
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
	// Asking is a sign of life, and while a person is asked the turn is
	// not quiet (quiet.go).
	m.stir(at)

	at.mu.Lock()
	if at.transcript != nil {
		ev := runtime.Event{
			Kind: runtime.EventApprovalRequest, At: req.At, ApprovalID: req.ApprovalID, ApprovalKind: req.ApprovalKind, Tool: req.Tool, Input: req.Input,
			Reviewer: req.Reviewer, Verdict: req.Verdict, Text: req.Why, Detail: req.Detail,
		}
		if err := at.transcript.write(transcriptLine{Kind: "event", At: req.At, Event: &ev}); err != nil {
			m.logger.Warn("transcript", "turn", req.TurnID, "err", err)
		}
	}
	at.mu.Unlock()
	switch {
	case req.Reviewer == store.ReviewerRule:
		// A rule of the member's let it through: kept, and not told in
		// the thread, where nobody need see it again.
		go m.recordReviewed(at, req, false)
		return
	case req.Reviewer != "":
		// Settled already, and the turn goes on: the record takes its
		// place on the turn's executor, after what the agent said before
		// and ahead of what it says next and of the turn's end.
		m.closeSegment(context.Background(), at, false, true)
		at.enqueue(func() { m.recordReviewed(at, req, true) })
		return
	}
	if userID := at.trustedFor(req); userID != "" {
		go m.allowTrusted(at, req, userID)
		return
	}
	go m.raise(at, req)
}

// recordReviewed records a request the runtime settled on its own and, with
// announce, tells the thread, so people see what was decided for them, by
// whom and why. It is already decided: nothing is pending and no decision
// goes back. What is announced runs on the turn's executor.
func (m *TurnManager) recordReviewed(at *activeTurn, req protocol.ApprovalRequest, announce bool) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()

	status := store.ApprovalStatus(req.Verdict)
	switch status {
	case store.ApprovalAllowed, store.ApprovalDenied, store.ApprovalExpired, store.ApprovalCancelled:
	default:
		m.logger.Error("reviewed approval with an unknown verdict", "turn", at.turn.ID, "verdict", req.Verdict)
		return
	}

	var messageID string
	if announce {
		note, err := m.post(ctx, store.NewMessage{
			RoomID:     at.thread.RoomID,
			ThreadID:   at.thread.ID,
			SenderKind: store.SenderSystem,
			Body:       reviewedNote(req.Reviewer, at.member.DisplayName, describeToolUse(req.Tool, req.Input), status, req.Why),
			TurnID:     at.turn.ID,
		})
		if err != nil {
			m.logger.Error("announce reviewed approval", "turn", at.turn.ID, "err", err)
		} else {
			messageID = note.ID
		}
	}
	kind := store.ApprovalKind(req.ApprovalKind)
	if kind == "" {
		kind = store.ApprovalToolUse
	}
	a, err := m.store.CreateReviewedApproval(ctx, store.NewReviewedApproval{
		NewApproval: store.NewApproval{
			TurnID:    at.turn.ID,
			RoomID:    at.thread.RoomID,
			ThreadID:  at.thread.ID,
			MemberID:  at.member.ID,
			RequestID: req.ApprovalID,
			Kind:      kind,
			Tool:      req.Tool,
			Input:     req.Input,
			MessageID: messageID,
		},
		Status:   status,
		Message:  req.Why,
		Reviewer: req.Reviewer,
		Answer:   req.Detail,
	})
	if err != nil {
		m.logger.Error("record reviewed approval", "turn", at.turn.ID, "err", err)
		return
	}
	m.recordDecision(at, a)
	m.publish(approvalEvent(EventApprovalDecided, a))
}

// reviewedNote is the thread's line for a request a runtime's own reviewer
// settled; the room draws the approval card in its place.
func reviewedNote(reviewer, member, what string, status store.ApprovalStatus, why string) string {
	by := reviewerName(reviewer)
	switch status {
	case store.ApprovalAllowed:
		return fmt.Sprintf("%s allowed %s to run %s%s", by, member, what, suffix(why))
	case store.ApprovalDenied:
		return fmt.Sprintf("%s denied %s running %s%s", by, member, what, suffix(why))
	default:
		return fmt.Sprintf("%s did not decide on %s running %s (%s)%s", by, member, what, status, suffix(why))
	}
}

// reviewerName names a runtime's reviewer for a thread line.
func reviewerName(reviewer string) string {
	switch reviewer {
	case "codex_auto_review":
		return "Codex's automatic review"
	}
	return reviewer
}

// raise registers the request, announces it in the thread and records it.
// The approval id is minted here so the request is tracked in memory
// before the row exists: a decision can then never arrive for an approval
// the hub does not know about.
func (m *TurnManager) raise(at *activeTurn, req protocol.ApprovalRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()

	id := store.NewID()
	kind := store.ApprovalKind(req.ApprovalKind)
	if kind == "" {
		kind = store.ApprovalToolUse
	}
	p := &pendingApproval{requestID: req.ApprovalID, kind: kind, tool: req.Tool, input: req.Input}
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
	m.closeSegment(ctx, at, true, true)

	body := askedNote(kind, at.member.DisplayName, req.Tool, req.Input, id)
	var messageID string
	note, err := m.post(ctx, store.NewMessage{
		RoomID:     at.thread.RoomID,
		ThreadID:   at.thread.ID,
		SenderKind: store.SenderSystem,
		Body:       body,
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

	var similar json.RawMessage
	if req.Similar != nil {
		similar, _ = json.Marshal(req.Similar)
	}
	a, err := m.store.CreateApproval(ctx, store.NewApproval{
		ID:        id,
		TurnID:    at.turn.ID,
		RoomID:    at.thread.RoomID,
		ThreadID:  at.thread.ID,
		MemberID:  at.member.ID,
		RequestID: req.ApprovalID,
		Kind:      kind,
		Tool:      req.Tool,
		Input:     req.Input,
		MessageID: messageID,
		Similar:   similar,
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

	at.mu.Lock()
	p.recorded = true
	withdrawn := at.withdrawn[req.ApprovalID]
	trustedBy := ""
	if trustable(kind, req.Tool) {
		trustedBy = at.trustedBy
	}
	at.mu.Unlock()
	if withdrawn {
		m.closeWithdrawn(id)
		return
	}
	if trustedBy != "" {
		// The turn came to be trusted while this was being recorded.
		m.allowWaiting(ctx, id, trustedBy)
		return
	}

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
// to the turn. scope is how far an allow goes (docs/design.md 4.6); empty
// takes it from the decision as the runtime has it. A scope the request
// does not offer is store.ErrInvalidInput. The database settles who
// decided first; a decision that loses that race is store.ErrConflict.
func (m *TurnManager) Decide(ctx context.Context, approvalID, userID string, d runtime.Decision, scope store.AllowScope) (store.Approval, error) {
	status := store.ApprovalDenied
	if d.Allow {
		status = store.ApprovalAllowed
	}
	asked, err := m.store.GetApproval(ctx, approvalID)
	if err != nil {
		return store.Approval{}, err
	}
	if scope, err = allowScope(asked, d, scope); err != nil {
		return store.Approval{}, err
	}
	// The runtime takes in the like of the request for the rest of the
	// turn, whether or not it is kept for the member beyond it.
	d.Similar = scope == store.ScopeSimilar || scope == store.ScopeAlways
	// The answer as given goes to the runtime; what is kept leaves out
	// anything asked as a secret.
	a, err := m.store.DecideApproval(ctx, approvalID, store.ApprovalOutcome{
		Status: status, Message: d.Message, DecidedBy: userID, Answer: storedAnswer(asked, d.Answer), Scope: scope,
	})
	if err != nil {
		return store.Approval{}, err
	}
	m.publish(approvalEvent(EventApprovalDecided, a))
	if scope == store.ScopeAlways {
		m.keepRules(ctx, a, userID)
	}

	at, p := m.settle(approvalID)
	if at == nil {
		// Recorded but nobody is waiting: the turn ended in the same
		// instant, or the hub restarted since the request was raised.
		m.logger.Warn("approval decided for a turn that is not running", "approval", approvalID, "turn", a.TurnID)
		return a, nil
	}
	m.recordDecision(at, a)
	if scope == store.ScopeTurn {
		// Before the runtime hears of the allow: whatever it asks next
		// must find the turn trusted.
		m.trust(ctx, at, userID)
	}

	// Tell the thread before the machine, so the note always precedes
	// whatever the agent does with the decision.
	who := userID
	if user, err := m.store.GetUser(ctx, userID); err == nil {
		who = user.Name
	}
	m.noteDecision(ctx, at, p, decidedNote(p, who, at.member.DisplayName, d, scope))
	m.deliver(at, p, d)
	return a, nil
}

// askedNote is the thread's line announcing a request, by what it asks.
func askedNote(kind store.ApprovalKind, member, tool, input, id string) string {
	switch kind {
	case store.ApprovalQuestion:
		return fmt.Sprintf("%s asks %s (question %s pending)", member, describeQuestions(input), id)
	case store.ApprovalForm:
		return fmt.Sprintf("%s needs a form filled in: %s (form %s pending)", member, describeElicitation(input), id)
	case store.ApprovalLink:
		return fmt.Sprintf("%s needs a link opened: %s (link %s pending)", member, describeElicitation(input), id)
	}
	switch tool {
	case planTool:
		return fmt.Sprintf("%s asks for its plan %s to be approved (approval %s pending)", member, describePlan(input), id)
	case confirmTool:
		return fmt.Sprintf("%s asks you to confirm %s (approval %s pending)", member, describeConfirm(input), id)
	}
	return fmt.Sprintf("%s wants to run %s (approval %s pending)", member, describeToolUse(tool, input), id)
}

// decidedNote is the thread's line for a request once a person settled it,
// saying how far an allow went.
func decidedNote(p *pendingApproval, who, member string, d runtime.Decision, scope store.AllowScope) string {
	switch p.kind {
	case store.ApprovalQuestion:
		if d.Allow {
			return fmt.Sprintf("%s answered %s: %s", who, member, describeQuestions(p.input))
		}
		return fmt.Sprintf("%s declined to answer %s: %s%s", who, member, describeQuestions(p.input), suffix(d.Message))
	case store.ApprovalForm:
		if d.Allow {
			return fmt.Sprintf("%s filled in %s's form: %s", who, member, describeElicitation(p.input))
		}
		return fmt.Sprintf("%s declined %s's form: %s%s", who, member, describeElicitation(p.input), suffix(d.Message))
	case store.ApprovalLink:
		if d.Allow {
			return fmt.Sprintf("%s opened %s's link: %s", who, member, describeElicitation(p.input))
		}
		return fmt.Sprintf("%s declined %s's link: %s%s", who, member, describeElicitation(p.input), suffix(d.Message))
	}
	switch p.tool {
	case planTool:
		if d.Allow {
			return fmt.Sprintf("%s approved %s's plan %s", who, member, describePlan(p.input))
		}
		return fmt.Sprintf("%s sent back %s's plan %s%s", who, member, describePlan(p.input), suffix(d.Message))
	case confirmTool:
		if d.Allow {
			return fmt.Sprintf("%s confirmed %s for %s", who, describeConfirm(p.input), member)
		}
		return fmt.Sprintf("%s did not confirm %s for %s%s", who, describeConfirm(p.input), member, suffix(d.Message))
	}
	what := describeToolUse(p.tool, p.input)
	switch {
	case d.Allow && scope == store.ScopeTurn:
		return fmt.Sprintf("%s allowed %s to run %s, and whatever else it asks for the rest of the turn", who, member, what)
	case d.Allow && scope == store.ScopeAlways:
		return fmt.Sprintf("%s allowed %s to run %s, and the like of it from now on", who, member, what)
	case d.Allow && d.Similar:
		return fmt.Sprintf("%s allowed %s to run %s, and the like of it for the rest of the turn", who, member, what)
	case d.Allow:
		return fmt.Sprintf("%s allowed %s to run %s", who, member, what)
	}
	return fmt.Sprintf("%s denied %s running %s%s", who, member, what, suffix(d.Message))
}

// expiredNote is the thread's line for a request nobody settled in time.
func expiredNote(p *pendingApproval, member, reason string) string {
	switch p.kind {
	case store.ApprovalQuestion:
		return fmt.Sprintf("%s's question %s went unanswered: %s", member, describeQuestions(p.input), reason)
	case store.ApprovalForm, store.ApprovalLink:
		return fmt.Sprintf("%s's request %s went unanswered: %s", member, describeElicitation(p.input), reason)
	}
	switch p.tool {
	case planTool:
		return fmt.Sprintf("%s's plan %s went unapproved: %s", member, describePlan(p.input), reason)
	case confirmTool:
		return fmt.Sprintf("%s's %s went unconfirmed: %s", member, describeConfirm(p.input), reason)
	}
	return fmt.Sprintf("%s's request to run %s expired: %s; denied", member, describeToolUse(p.tool, p.input), reason)
}

// describeElicitation says what an MCP server's form or link asks, in one
// line: the server, its message and, for a link, where it goes.
func describeElicitation(input string) string {
	var r struct {
		Server  string `json:"server"`
		Message string `json:"message"`
		URL     string `json:"url"`
	}
	if json.Unmarshal([]byte(input), &r) != nil {
		return truncateText(input, maxToolUseSummary)
	}
	text := "“" + truncateText(r.Message, maxToolUseSummary) + "”"
	if r.Server != "" {
		text = r.Server + " " + text
	}
	if r.URL != "" {
		text += " at " + truncateText(r.URL, maxToolUseSummary)
	}
	return text
}

// storedAnswer is an answer as it is kept: whatever was asked as a secret
// reached the runtime and is kept as a mark saying only that it was given.
func storedAnswer(asked store.Approval, answer json.RawMessage) json.RawMessage {
	if asked.Kind != store.ApprovalQuestion || len(answer) == 0 {
		return answer
	}
	var set runtime.QuestionSet
	var given runtime.Answers
	if json.Unmarshal(asked.Input, &set) != nil || json.Unmarshal(answer, &given) != nil {
		return answer
	}
	hidden := false
	for _, q := range set.Questions {
		if _, ok := given.Answers[q.ID]; ok && q.Secret {
			given.Answers[q.ID] = []string{secretMark}
			hidden = true
		}
	}
	if !hidden {
		return answer
	}
	kept, err := json.Marshal(given)
	if err != nil {
		return nil
	}
	return kept
}

// secretMark stands in for a secret answer in what is kept.
const secretMark = "••••••"

// describeQuestions says what a question approval asks, in one line: the
// first question, and how many more there are.
func describeQuestions(input string) string {
	var set runtime.QuestionSet
	if json.Unmarshal([]byte(input), &set) != nil || len(set.Questions) == 0 {
		return truncateText(input, maxToolUseSummary)
	}
	text := "“" + truncateText(set.Questions[0].Question, maxToolUseSummary) + "”"
	if more := len(set.Questions) - 1; more > 0 {
		text += fmt.Sprintf(" and %d more", more)
	}
	return text
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
	m.noteDecision(ctx, at, p, expiredNote(p, at.member.DisplayName, reason))
	m.deliver(at, p, runtime.Decision{Allow: false, Message: "approval timed out: " + reason})
}

// withdraw closes a request the runtime took back while its turn goes on,
// as when one of Claude Code's hooks settled a permission first: people
// are no longer asked, and the thread says so. A request whose row is not
// there yet is closed as soon as it is.
func (m *TurnManager) withdraw(at *activeTurn, requestID string) {
	at.mu.Lock()
	id := ""
	for pid, p := range at.pending {
		if p.requestID == requestID {
			id = pid
			break
		}
	}
	if id == "" || !at.pending[id].recorded {
		if at.withdrawn == nil {
			at.withdrawn = make(map[string]bool)
		}
		at.withdrawn[requestID] = true
		at.mu.Unlock()
		return
	}
	at.mu.Unlock()
	go m.closeWithdrawn(id)
}

// withdrawnReason is kept as the message of a withdrawn approval.
const withdrawnReason = "the runtime took the request back"

// closeWithdrawn records a withdrawn request as cancelled. Nothing goes back
// to the runtime, which no longer waits; a person's decision that got in
// first stands.
func (m *TurnManager) closeWithdrawn(approvalID string) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()

	a, err := m.store.DecideApproval(ctx, approvalID, store.ApprovalOutcome{Status: store.ApprovalCancelled, Message: withdrawnReason})
	if err != nil {
		if !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrNotFound) {
			m.logger.Error("withdraw approval", "approval", approvalID, "err", err)
		}
		return
	}
	m.publish(approvalEvent(EventApprovalDecided, a))
	at, p := m.settle(approvalID)
	if at == nil {
		return
	}
	m.recordDecision(at, a)
	m.noteDecision(ctx, at, p, withdrawnNote(p, at.member.DisplayName))
}

// withdrawnNote is the thread's line for a request the runtime took back.
func withdrawnNote(p *pendingApproval, member string) string {
	switch p.kind {
	case store.ApprovalQuestion:
		return fmt.Sprintf("%s took back its question %s", member, describeQuestions(p.input))
	case store.ApprovalForm, store.ApprovalLink:
		return fmt.Sprintf("%s took back its request %s", member, describeElicitation(p.input))
	}
	switch p.tool {
	case planTool:
		return fmt.Sprintf("%s took back its plan %s", member, describePlan(p.input))
	case confirmTool:
		return fmt.Sprintf("%s took back %s", member, describeConfirm(p.input))
	}
	return fmt.Sprintf("%s took back its request to run %s", member, describeToolUse(p.tool, p.input))
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
	// Answered, the turn's quiet starts over from now, not from before it
	// asked.
	m.stir(at)
	return at, p
}

// deliver sends a decision to the machine running the turn. If the machine is
// gone the turn is being failed by MachineGone anyway.
func (m *TurnManager) deliver(at *activeTurn, p *pendingApproval, d runtime.Decision) {
	m.answer(at, p.requestID, d)
}

// answer is deliver for the runtime's request requestID, pending here or
// not.
func (m *TurnManager) answer(at *activeTurn, requestID string, d runtime.Decision) {
	conn, ok := m.connFor(at.member.MachineID)
	if !ok {
		m.logger.Warn("approval decided while its machine is offline", "turn", at.turn.ID)
		return
	}
	m.send(conn, protocol.ApprovalDecision{TurnID: at.turn.ID, ApprovalID: requestID, Decision: d})
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
// who decide on it. A shell command (Claude Code's Bash, Codex's
// commandExecution) is shown as itself; anything else as the tool name and
// its input.
func describeToolUse(tool, input string) string {
	if tool == "Bash" || tool == "commandExecution" {
		var in struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(input), &in) == nil && in.Command != "" {
			return "`" + truncateText(in.Command, maxToolUseSummary) + "`"
		}
	}
	return tool + " " + truncateText(input, maxToolUseSummary)
}

// planTool is Claude Code's tool for putting up a plan made in plan mode
// for approval; its input carries the plan as markdown.
const planTool = "ExitPlanMode"

// describePlan names a plan by its first line, quoted.
func describePlan(input string) string {
	var in struct {
		Plan string `json:"plan"`
	}
	if json.Unmarshal([]byte(input), &in) != nil || strings.TrimSpace(in.Plan) == "" {
		return "(no plan given)"
	}
	for _, line := range strings.Split(in.Plan, "\n") {
		if line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#")); line != "" {
			return "“" + truncateText(line, maxPlanTitle) + "”"
		}
	}
	return "(no plan given)"
}

// maxPlanTitle bounds the line a plan is named by.
const maxPlanTitle = 120

// confirmTool is a yes-or-no question an extension puts to the person
// through pi; its input is the dialog's title and message.
const confirmTool = "confirm"

// describeConfirm quotes a confirmation's title and message.
func describeConfirm(input string) string {
	var in struct {
		Title   string `json:"title"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(input), &in) != nil {
		return "“" + truncateText(input, maxToolUseSummary) + "”"
	}
	text := strings.TrimSpace(in.Title)
	if message := strings.TrimSpace(in.Message); message != "" {
		if text != "" {
			text += ": "
		}
		text += message
	}
	return "“" + truncateText(text, maxToolUseSummary) + "”"
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
