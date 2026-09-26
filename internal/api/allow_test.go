package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// fakeRules keeps members' rules by member.
type fakeRules map[string][]store.MemberRule

func (f fakeRules) ListMemberRules(_ context.Context, memberID, runtime string) ([]store.MemberRule, error) {
	var out []store.MemberRule
	for _, r := range f[memberID] {
		if runtime == "" || r.Runtime == runtime {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f fakeRules) DeleteMemberRule(_ context.Context, memberID, ruleID string) error {
	for i, r := range f[memberID] {
		if r.ID == ruleID {
			f[memberID] = append(f[memberID][:i], f[memberID][i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("rule %s: %w", ruleID, store.ErrNotFound)
}

func TestMembers_Rules(t *testing.T) {
	projects := newFakeProjects()
	_, room, _ := projects.CreateProject(context.Background(), store.NewProject{Name: "p"})
	agents := newFakeAgents(projects, "w1")
	ctx := context.Background()
	agent, _ := agents.CreateAgent(ctx, store.NewAgent{Name: "Reviewer", MachineID: "w1", Runtime: "claude", PermissionPreset: store.PermissionEditWithApproval})
	member, _ := agents.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: agent.ID})
	rules := fakeRules{}
	handler := NewHandler(Deps{Projects: projects, Agents: agents, Rules: rules})
	path := "/api/v1/members/" + member.ID + "/rules"

	var got MemberRulesResponse
	if rec := do(t, handler, http.MethodGet, path, "", &got); rec.Code != http.StatusOK || got.Rules == nil || len(got.Rules) != 0 {
		t.Errorf("none yet: status %d, body %s", rec.Code, rec.Body)
	}
	rules[member.ID] = []store.MemberRule{{ID: "r1", MemberID: member.ID, Runtime: "claude", Rule: "Bash(go test:*)"}, {ID: "r2", MemberID: member.ID, Runtime: "codex", Rule: `["go","vet"]`}}
	if rec := do(t, handler, http.MethodGet, path, "", &got); rec.Code != http.StatusOK || len(got.Rules) != 2 || got.Rules[1].Rule != `["go","vet"]` {
		t.Errorf("both: status %d, body %s", rec.Code, rec.Body)
	}
	if rec := do(t, handler, http.MethodDelete, path+"/r1", "", nil); rec.Code != http.StatusNoContent || len(rules[member.ID]) != 1 {
		t.Errorf("delete: status %d, left %+v", rec.Code, rules[member.ID])
	}
	if rec := do(t, handler, http.MethodDelete, path+"/r1", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("delete twice: status %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/members/m404/rules", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown member: status %d, want 404", rec.Code)
	}
	if rec := do(t, NewHandler(Deps{Projects: projects, Agents: agents}), http.MethodGet, path, "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("no rules kept: status %d, want 404", rec.Code)
	}
}

func TestTurns_Untrust(t *testing.T) {
	handler, _, chat := approvalsHandler(t)
	chat.running = map[string]bool{"t1": true}
	var res TurnResponse
	if rec := do(t, handler, http.MethodDelete, "/api/v1/turns/t1/trust", "", &res); rec.Code != http.StatusOK || res.Turn.ID != "t1" || len(chat.untrusted) != 1 {
		t.Errorf("untrust: status %d, body %s", rec.Code, rec.Body)
	}
	chat.running["t1"] = false
	if rec := do(t, handler, http.MethodDelete, "/api/v1/turns/t1/trust", "", nil); rec.Code != http.StatusConflict {
		t.Errorf("a turn that is over: status %d, want 409", rec.Code)
	}
	if rec := do(t, handler, http.MethodDelete, "/api/v1/turns/t404/trust", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown turn: status %d, want 404", rec.Code)
	}
}

// How far an allow goes reaches the hub as asked; the older similar flag
// still works, and an unknown scope is refused.
func TestApprovals_DecideScope(t *testing.T) {
	handler, _, chat := approvalsHandler(t)
	var res ApprovalResponse
	if rec := do(t, handler, http.MethodPost, "/api/v1/approvals/a1/decide", `{"user_id":"u1","allow":true,"scope":"forever"}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown scope: status %d, want 400", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/approvals/a1/decide", `{"user_id":"u1","allow":true,"scope":"turn"}`, &res); rec.Code != http.StatusOK || res.Approval.Scope != store.ScopeTurn {
		t.Errorf("turn: status %d, body %s", rec.Code, rec.Body)
	}
	if len(chat.decided) != 1 {
		t.Errorf("decided %v", chat.decided)
	}
}
