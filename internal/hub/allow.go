package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// How far a person's allow goes (docs/design.md 4.6): the request alone;
// with the like of it for the rest of the turn, or from now on; or with
// whatever else the turn asks for.

// allowScope checks the scope a person chose for their decision on asked,
// and settles an unnamed one: a denial takes nothing in, and the old
// "similar" flag means the like of the request for the rest of the turn,
// when the runtime offered any.
func allowScope(asked store.Approval, d runtime.Decision, scope store.AllowScope) (store.AllowScope, error) {
	if !d.Allow {
		return store.ScopeOnce, nil
	}
	if scope == "" {
		if d.Similar && len(asked.Similar) > 0 {
			return store.ScopeSimilar, nil
		}
		return store.ScopeOnce, nil
	}
	switch scope {
	case store.ScopeOnce:
	case store.ScopeSimilar:
		if len(asked.Similar) == 0 {
			return "", fmt.Errorf("%w: the request offers nothing like it to allow", store.ErrInvalidInput)
		}
	case store.ScopeAlways:
		if len(standingRules(asked)) == 0 {
			return "", fmt.Errorf("%w: the request offers no rule to keep", store.ErrInvalidInput)
		}
	case store.ScopeTurn:
		if !trustable(asked.Kind, asked.Tool) {
			return "", fmt.Errorf("%w: only a tool's use can let the rest of the turn through", store.ErrInvalidInput)
		}
	default:
		return "", fmt.Errorf("%w: unknown allow scope %q", store.ErrInvalidInput, scope)
	}
	return scope, nil
}

// standingRules are the rules asked offers that can be kept for its member.
func standingRules(asked store.Approval) []string {
	if len(asked.Similar) == 0 {
		return nil
	}
	var offer runtime.Similar
	if json.Unmarshal(asked.Similar, &offer) != nil {
		return nil
	}
	return offer.Standing()
}

// trustable reports whether a request is one a trusted turn lets through:
// a tool's use, not a plan or a confirmation and nothing that asks a
// person for more than yes (a question, a form, a link).
func trustable(kind store.ApprovalKind, tool string) bool {
	return (kind == "" || kind == store.ApprovalToolUse) && tool != planTool && tool != confirmTool
}

// keepRules keeps what an "always" allow offered as the member's rules, for
// every turn after this one; this turn takes in the like of the request as
// "similar" does. The turn's runtime says whose terms they are in.
func (m *TurnManager) keepRules(ctx context.Context, a store.Approval, userID string) {
	turn, err := m.store.GetTurn(ctx, a.TurnID)
	if err != nil {
		m.logger.Error("keep rules: find the turn", "approval", a.ID, "err", err)
		return
	}
	_, err = m.store.AddMemberRules(ctx, store.NewMemberRules{
		MemberID: a.MemberID, Runtime: turn.Runtime, Rules: standingRules(a), ApprovalID: a.ID, CreatedBy: userID,
	})
	if err != nil {
		m.logger.Error("keep rules", "approval", a.ID, "err", err)
	}
}

// trust lets the rest of a turn's requests through for userID: recorded on
// the turn and kept in memory, announced, and applied at once to the
// requests of the turn still waiting for a person.
func (m *TurnManager) trust(ctx context.Context, at *activeTurn, userID string) {
	turn, err := m.store.TrustTurn(ctx, at.turn.ID, userID)
	if err != nil {
		if !errors.Is(err, store.ErrConflict) {
			m.logger.Error("trust turn", "turn", at.turn.ID, "err", err)
		}
		return
	}
	at.mu.Lock()
	at.trustedBy = userID
	at.turn.TrustedBy, at.turn.TrustedAt = turn.TrustedBy, turn.TrustedAt
	var waiting []string
	for id, p := range at.pending {
		if trustable(p.kind, p.tool) && p.recorded {
			waiting = append(waiting, id)
		}
	}
	at.mu.Unlock()
	m.publish(Event{Kind: EventTurnTrust, RoomID: turn.RoomID, Turn: &turn})
	for _, id := range waiting {
		m.allowWaiting(ctx, id, userID)
	}
}

// UntrustTurn takes back letting a running turn's requests through: from
// now on people are asked again. A turn not running is ErrUnknownTurn.
func (m *TurnManager) UntrustTurn(ctx context.Context, turnID string) (store.Turn, error) {
	m.mu.Lock()
	at := m.active[turnID]
	m.mu.Unlock()
	if at == nil {
		return store.Turn{}, fmt.Errorf("%w: %s", ErrUnknownTurn, turnID)
	}
	// Forgotten first, so no request slips through while the row changes.
	at.mu.Lock()
	at.trustedBy = ""
	at.mu.Unlock()
	turn, err := m.store.UntrustTurn(ctx, turnID)
	if err != nil {
		return store.Turn{}, err
	}
	at.mu.Lock()
	at.turn.TrustedBy, at.turn.TrustedAt = "", nil
	at.mu.Unlock()
	m.publish(Event{Kind: EventTurnTrust, RoomID: turn.RoomID, Turn: &turn})
	return turn, nil
}

// trustedFor is who let the rest of the turn's requests through when req is
// one they cover; empty when nobody did or req asks for more.
func (at *activeTurn) trustedFor(req protocol.ApprovalRequest) string {
	if !trustable(store.ApprovalKind(req.ApprovalKind), req.Tool) {
		return ""
	}
	at.mu.Lock()
	defer at.mu.Unlock()
	return at.trustedBy
}

// allowTrusted lets a request of a trusted turn through for the person who
// trusted it. It is recorded as theirs, with the hub as reviewer, and told
// in the thread in its place among what the agent said, where the room
// draws it as a settled line. A request that cannot be recorded goes to
// people after all.
func (m *TurnManager) allowTrusted(at *activeTurn, req protocol.ApprovalRequest, userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()
	m.closeSegment(ctx, at, true, true)
	var messageID string
	note, err := m.post(ctx, store.NewMessage{
		RoomID:     at.thread.RoomID,
		ThreadID:   at.thread.ID,
		SenderKind: store.SenderSystem,
		Body:       trustedNote(m.userName(ctx, userID), at.member.DisplayName, describeToolUse(req.Tool, req.Input)),
		TurnID:     at.turn.ID,
	})
	if err != nil {
		m.logger.Error("announce trusted approval", "turn", at.turn.ID, "err", err)
	} else {
		messageID = note.ID
	}
	a, err := m.store.CreateReviewedApproval(ctx, store.NewReviewedApproval{
		NewApproval: store.NewApproval{
			TurnID:    at.turn.ID,
			RoomID:    at.thread.RoomID,
			ThreadID:  at.thread.ID,
			MemberID:  at.member.ID,
			RequestID: req.ApprovalID,
			Kind:      store.ApprovalToolUse,
			Tool:      req.Tool,
			Input:     req.Input,
			MessageID: messageID,
		},
		Status:    store.ApprovalAllowed,
		Reviewer:  store.ReviewerTurn,
		DecidedBy: userID,
	})
	if err != nil {
		m.logger.Error("record trusted approval", "turn", at.turn.ID, "err", err)
		m.raise(at, req)
		return
	}
	m.recordDecision(at, a)
	m.publish(approvalEvent(EventApprovalDecided, a))
	m.answer(at, req.ApprovalID, runtime.Decision{Allow: true})
}

// allowWaiting lets through a request that was waiting for a person when
// its turn came to be trusted, as the person who trusted it. A person's
// decision that got in first stands.
func (m *TurnManager) allowWaiting(ctx context.Context, approvalID, userID string) {
	a, err := m.store.DecideApproval(ctx, approvalID, store.ApprovalOutcome{
		Status: store.ApprovalAllowed, DecidedBy: userID, Reviewer: store.ReviewerTurn,
	})
	if err != nil {
		if !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrNotFound) {
			m.logger.Error("allow a waiting request", "approval", approvalID, "err", err)
		}
		return
	}
	m.publish(approvalEvent(EventApprovalDecided, a))
	at, p := m.settle(approvalID)
	if at == nil {
		return
	}
	m.recordDecision(at, a)
	m.noteDecision(ctx, at, p, trustedNote(m.userName(ctx, userID), at.member.DisplayName, describeToolUse(p.tool, p.input)))
	m.deliver(at, p, runtime.Decision{Allow: true})
}

// trustedNote is the thread's line for a request let through because who
// trusted the rest of the turn.
func trustedNote(who, member, what string) string {
	return fmt.Sprintf("%s allowed %s to run %s, with the rest of the turn", who, member, what)
}

// userName is a person's name for a thread line, their id failing that.
func (m *TurnManager) userName(ctx context.Context, userID string) string {
	if user, err := m.store.GetUser(ctx, userID); err == nil {
		return user.Name
	}
	return userID
}
