package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// MemberRule is something a member may always do without a person being
// asked, in its runtime's own terms (docs/design.md 4.6): a permission rule
// Claude Code matches itself, such as Bash(go test:*), or for Codex a
// command prefix as a JSON array, such as ["go","test"]. A person adds one
// by allowing a request "always", and can take it back.
type MemberRule struct {
	ID       string `json:"id"`
	MemberID string `json:"member_id"`
	Runtime  string `json:"runtime"`
	Rule     string `json:"rule"`
	// ApprovalID is the request it was allowed with, empty once that is
	// gone; CreatedBy is who allowed it.
	ApprovalID string    `json:"approval_id,omitempty"`
	CreatedBy  string    `json:"created_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewMemberRules is rules a person allows a member at once, with the
// request they allowed them from.
type NewMemberRules struct {
	MemberID   string
	Runtime    string
	Rules      []string
	ApprovalID string
	CreatedBy  string
}

// ListMemberRules returns a member's rules, oldest first: those of one
// runtime, or with runtime empty every runtime's.
func (s *Store) ListMemberRules(ctx context.Context, memberID, runtime string) ([]MemberRule, error) {
	mid, err := parseUUID(memberID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListMemberRules(ctx, db.ListMemberRulesParams{MemberID: mid, Runtime: pgtype.Text{String: runtime, Valid: runtime != ""}})
	if err != nil {
		return nil, fmt.Errorf("list rules of member %s: %w", memberID, err)
	}
	out := make([]MemberRule, len(rows))
	for i, row := range rows {
		out[i] = toMemberRule(row)
	}
	return out, nil
}

// AddMemberRules keeps rules for a member, together: a rule it already has
// stays as it was. It returns the rules as kept.
func (s *Store) AddMemberRules(ctx context.Context, in NewMemberRules) ([]MemberRule, error) {
	mid, err := parseUUID(in.MemberID)
	if err != nil {
		return nil, err
	}
	if in.Runtime == "" {
		return nil, fmt.Errorf("%w: a rule needs its runtime", ErrInvalidInput)
	}
	var approvalID, createdBy pgtype.UUID
	if in.ApprovalID != "" {
		if approvalID, err = parseUUID(in.ApprovalID); err != nil {
			return nil, err
		}
	}
	if in.CreatedBy != "" {
		if createdBy, err = parseUUID(in.CreatedBy); err != nil {
			return nil, err
		}
	}
	for _, rule := range in.Rules {
		if strings.TrimSpace(rule) == "" || strings.ContainsAny(rule, "\r\n") {
			return nil, fmt.Errorf("%w: rule %q", ErrInvalidInput, rule)
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	q := s.q.WithTx(tx)
	kept := make([]MemberRule, 0, len(in.Rules))
	for _, rule := range in.Rules {
		row, err := q.AddMemberRule(ctx, db.AddMemberRuleParams{
			MemberID: mid, Runtime: in.Runtime, Rule: rule, ApprovalID: approvalID, CreatedBy: createdBy,
		})
		if err != nil {
			return nil, mapPGError("add member rule", err)
		}
		kept = append(kept, toMemberRule(row))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return kept, nil
}

// DeleteMemberRule takes one of a member's rules back, or ErrNotFound.
func (s *Store) DeleteMemberRule(ctx context.Context, memberID, ruleID string) error {
	mid, err := parseUUID(memberID)
	if err != nil {
		return err
	}
	rid, err := parseUUID(ruleID)
	if err != nil {
		return err
	}
	n, err := s.q.DeleteMemberRule(ctx, db.DeleteMemberRuleParams{MemberID: mid, ID: rid})
	if err != nil {
		return mapPGError("delete member rule", err)
	}
	if n == 0 {
		return fmt.Errorf("rule %s of member %s: %w", ruleID, memberID, ErrNotFound)
	}
	return nil
}

func toMemberRule(row db.MemberRule) MemberRule {
	return MemberRule{
		ID:         uuidString(row.ID),
		MemberID:   uuidString(row.MemberID),
		Runtime:    row.Runtime,
		Rule:       row.Rule,
		ApprovalID: uuidString(row.ApprovalID),
		CreatedBy:  uuidString(row.CreatedBy),
		CreatedAt:  row.CreatedAt.Time,
	}
}
