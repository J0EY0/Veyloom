package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// A member's rules are kept once each, per runtime, with where they came
// from, and each can be taken back.
func TestMemberRules_AddListDelete(t *testing.T) {
	f := newApprovalFixture(t)
	ctx := context.Background()
	a, err := f.s.CreateApproval(ctx, f.newApproval("r1"))
	if err != nil {
		t.Fatal(err)
	}

	kept, err := f.s.AddMemberRules(ctx, store.NewMemberRules{
		MemberID: f.member.ID, Runtime: "claude", Rules: []string{"Bash(go test:*)", "Bash(go vet:*)"}, ApprovalID: a.ID, CreatedBy: f.user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 2 || kept[0].Rule != "Bash(go test:*)" || kept[0].ApprovalID != a.ID || kept[0].CreatedBy != f.user.ID || kept[0].Runtime != "claude" || kept[0].MemberID != f.member.ID {
		t.Fatalf("kept = %+v", kept)
	}
	// Allowed again, a rule stays as it was first kept.
	again, err := f.s.AddMemberRules(ctx, store.NewMemberRules{MemberID: f.member.ID, Runtime: "claude", Rules: []string{"Bash(go test:*)"}})
	if err != nil || len(again) != 1 || again[0].ID != kept[0].ID || again[0].ApprovalID != a.ID {
		t.Fatalf("again = %+v, %v", again, err)
	}
	if _, err := f.s.AddMemberRules(ctx, store.NewMemberRules{MemberID: f.member.ID, Runtime: "codex", Rules: []string{`["go","test"]`}}); err != nil {
		t.Fatal(err)
	}

	all, err := f.s.ListMemberRules(ctx, f.member.ID, "")
	if err != nil || len(all) != 3 {
		t.Fatalf("all = %+v, %v", all, err)
	}
	claude, err := f.s.ListMemberRules(ctx, f.member.ID, "claude")
	if err != nil || len(claude) != 2 {
		t.Fatalf("claude = %+v, %v", claude, err)
	}

	if err := f.s.DeleteMemberRule(ctx, f.member.ID, kept[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteMemberRule(ctx, f.member.ID, kept[0].ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleted twice: %v", err)
	}
	other, err := f.s.CreateMember(ctx, store.NewMember{RoomID: f.room.ID, AgentID: f.agent.ID, DisplayName: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteMemberRule(ctx, other.ID, kept[1].ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("another member's rule: %v", err)
	}
	if left, _ := f.s.ListMemberRules(ctx, f.member.ID, "claude"); len(left) != 1 || left[0].Rule != "Bash(go vet:*)" {
		t.Errorf("left = %+v", left)
	}

	for _, bad := range []store.NewMemberRules{
		{MemberID: f.member.ID, Rules: []string{"Bash(ls)"}},
		{MemberID: f.member.ID, Runtime: "claude", Rules: []string{" "}},
		{MemberID: f.member.ID, Runtime: "claude", Rules: []string{"Bash(ls)\nBash(rm)"}},
	} {
		if _, err := f.s.AddMemberRules(ctx, bad); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("%+v: got %v, want ErrInvalidInput", bad, err)
		}
	}
}

// A person lets a running turn's requests through and takes it back; a
// turn that is over cannot be trusted, and keeps who trusted it.
func TestTurns_Trust(t *testing.T) {
	f := newApprovalFixture(t)
	ctx := context.Background()

	trusted, err := f.s.TrustTurn(ctx, f.turn.ID, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if trusted.TrustedBy != f.user.ID || trusted.TrustedAt == nil {
		t.Fatalf("trusted = %+v", trusted)
	}
	untrusted, err := f.s.UntrustTurn(ctx, f.turn.ID)
	if err != nil || untrusted.TrustedBy != "" || untrusted.TrustedAt != nil {
		t.Fatalf("untrusted = %+v, %v", untrusted, err)
	}

	if _, err := f.s.TrustTurn(ctx, f.turn.ID, f.user.ID); err != nil {
		t.Fatal(err)
	}
	finished, err := f.s.FinishTurn(ctx, f.turn.ID, store.TurnOutcome{Status: store.TurnDone})
	if err != nil || finished.TrustedBy != f.user.ID {
		t.Fatalf("finished = %+v, %v", finished, err)
	}
	if _, err := f.s.TrustTurn(ctx, f.turn.ID, f.user.ID); !errors.Is(err, store.ErrConflict) {
		t.Errorf("trusting a finished turn: %v", err)
	}
	if _, err := f.s.UntrustTurn(ctx, store.NewID()); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("an unknown turn: %v", err)
	}
}

// How far an allow went is kept, and the hub answering for a person names
// itself as reviewer.
func TestApprovals_Scopes(t *testing.T) {
	f := newApprovalFixture(t)
	ctx := context.Background()

	for scope, want := range map[store.AllowScope]store.AllowScope{"": store.ScopeOnce, store.ScopeSimilar: store.ScopeSimilar, store.ScopeAlways: store.ScopeAlways, store.ScopeTurn: store.ScopeTurn} {
		a, err := f.s.CreateApproval(ctx, f.newApproval("scope-"+string(scope)))
		if err != nil {
			t.Fatal(err)
		}
		if a.Scope != store.ScopeOnce {
			t.Errorf("a pending approval's scope: %q", a.Scope)
		}
		decided, err := f.s.DecideApproval(ctx, a.ID, store.ApprovalOutcome{Status: store.ApprovalAllowed, DecidedBy: f.user.ID, Scope: scope})
		if err != nil || decided.Scope != want || decided.Reviewer != "" {
			t.Errorf("%q: %+v, %v", scope, decided, err)
		}
	}
	a, err := f.s.CreateApproval(ctx, f.newApproval("bad"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DecideApproval(ctx, a.ID, store.ApprovalOutcome{Status: store.ApprovalAllowed, Scope: "forever"}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("an unknown scope: %v", err)
	}
	byTrust, err := f.s.DecideApproval(ctx, a.ID, store.ApprovalOutcome{Status: store.ApprovalAllowed, DecidedBy: f.user.ID, Reviewer: store.ReviewerTurn})
	if err != nil || byTrust.Reviewer != store.ReviewerTurn || byTrust.DecidedBy != f.user.ID {
		t.Errorf("allowed with the turn: %+v, %v", byTrust, err)
	}

	in := store.NewReviewedApproval{NewApproval: f.newApproval("trusted"), Status: store.ApprovalAllowed, Reviewer: store.ReviewerTurn, DecidedBy: f.user.ID}
	reviewed, err := f.s.CreateReviewedApproval(ctx, in)
	if err != nil || reviewed.DecidedBy != f.user.ID || reviewed.Reviewer != store.ReviewerTurn || reviewed.Scope != store.ScopeOnce {
		t.Errorf("reviewed for a person: %+v, %v", reviewed, err)
	}
}
