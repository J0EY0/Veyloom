package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

type fakeApprovals map[string]store.Approval

func (f fakeApprovals) GetApproval(_ context.Context, id string) (store.Approval, error) {
	if err := checkID(id); err != nil {
		return store.Approval{}, err
	}
	a, ok := f[id]
	if !ok {
		return store.Approval{}, fmt.Errorf("approval %s: %w", id, store.ErrNotFound)
	}
	return a, nil
}

func (f fakeApprovals) ListPendingRoomApprovals(_ context.Context, roomID string) ([]store.Approval, error) {
	out := []store.Approval{}
	for _, a := range f {
		if a.RoomID == roomID && a.Status == store.ApprovalPending {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f fakeApprovals) ListPendingApprovals(context.Context) ([]store.PendingApproval, error) {
	out := []store.PendingApproval{}
	for _, a := range f {
		if a.Status == store.ApprovalPending {
			out = append(out, store.PendingApproval{Approval: a, MemberName: "agent " + a.MemberID, ProjectName: "p"})
		}
	}
	return out, nil
}

func (f fakeApprovals) ListTurnApprovals(_ context.Context, turnID string) ([]store.Approval, error) {
	out := []store.Approval{}
	for _, a := range f {
		if a.TurnID == turnID {
			out = append(out, a)
		}
	}
	return out, nil
}

// DecideApproval mirrors the hub: the first decision wins, later ones
// conflict, unknown ids are not found.
func (c *fakeChat) DecideApproval(ctx context.Context, id, userID string, d runtime.Decision) (store.Approval, error) {
	a, err := c.approvals.GetApproval(ctx, id)
	if err != nil {
		return store.Approval{}, err
	}
	if a.Status != store.ApprovalPending {
		return store.Approval{}, fmt.Errorf("approval %s: %w: already %s", id, store.ErrConflict, a.Status)
	}
	a.Status = store.ApprovalDenied
	if d.Allow {
		a.Status = store.ApprovalAllowed
	}
	a.Message, a.DecidedBy, a.Answer = d.Message, userID, d.Answer
	c.approvals[id] = a
	c.decided = append(c.decided, id)
	return a, nil
}

func approvalsHandler(t *testing.T) (http.Handler, store.Room, *fakeChat) {
	t.Helper()
	projects := newFakeProjects()
	_, room, _ := projects.CreateProject(context.Background(), store.NewProject{Name: "p"})
	turns := fakeTurns{"t1": {ID: "t1", RoomID: room.ID, Status: store.TurnRunning}}
	approvals := fakeApprovals{
		"a1": {ID: "a1", TurnID: "t1", RoomID: room.ID, Tool: "Bash", Input: json.RawMessage(`{"command":"make test"}`), Status: store.ApprovalPending},
		"a2": {ID: "a2", TurnID: "t1", RoomID: room.ID, Tool: "Bash", Input: json.RawMessage(`{"command":"rm -rf x"}`), Status: store.ApprovalDenied, Message: "no"},
	}
	chat := &fakeChat{approvals: approvals}
	return NewHandler(Deps{Projects: projects, Turns: turns, Approvals: approvals, Chat: chat}), room, chat
}

func TestApprovals_GetAndList(t *testing.T) {
	handler, room, _ := approvalsHandler(t)

	var one ApprovalResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/approvals/a1", "", &one); rec.Code != http.StatusOK || one.Approval.Tool != "Bash" || string(one.Approval.Input) != `{"command":"make test"}` {
		t.Errorf("get: status = %d, approval = %+v", rec.Code, one.Approval)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/approvals/a404", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/approvals/bad", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: status = %d, want 400", rec.Code)
	}

	var list ApprovalsResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/"+room.ID+"/approvals", "", &list); rec.Code != http.StatusOK || len(list.Approvals) != 1 || list.Approvals[0].ID != "a1" {
		t.Errorf("room pending: status = %d, approvals = %+v", rec.Code, list.Approvals)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/r404/approvals", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown room: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/turns/t1/approvals", "", &list); rec.Code != http.StatusOK || len(list.Approvals) != 2 {
		t.Errorf("turn approvals: status = %d, approvals = %+v", rec.Code, list.Approvals)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/turns/t404/approvals", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown turn: status = %d, want 404", rec.Code)
	}
}

func TestApprovals_Decide(t *testing.T) {
	handler, _, chat := approvalsHandler(t)

	var res ApprovalResponse
	rec := do(t, handler, http.MethodPost, "/api/v1/approvals/a1/decide", `{"user_id":"u1","allow":false,"message":" not now "}`, &res)
	if rec.Code != http.StatusOK || res.Approval.Status != store.ApprovalDenied || res.Approval.Message != "not now" || res.Approval.DecidedBy != "u1" {
		t.Errorf("decide: status = %d, approval = %+v; body: %s", rec.Code, res.Approval, rec.Body)
	}
	if len(chat.decided) != 1 || chat.decided[0] != "a1" {
		t.Errorf("hub should have been asked to decide a1, got %v", chat.decided)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/approvals/a1/decide", `{"user_id":"u1","allow":true}`, nil); rec.Code != http.StatusConflict {
		t.Errorf("decide twice: status = %d, want 409", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/approvals/a2/decide", `{"user_id":"u1","allow":true}`, nil); rec.Code != http.StatusConflict {
		t.Errorf("decide settled: status = %d, want 409", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/approvals/a404/decide", `{"user_id":"u1","allow":true}`, nil); rec.Code != http.StatusNotFound {
		t.Errorf("decide unknown: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/approvals/a1/decide", `{"allow":true}`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("missing user: status = %d, want 400", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/approvals/a1/decide", `{`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad json: status = %d, want 400", rec.Code)
	}
}

func TestApprovals_ListsWhatWaitsAcrossRooms(t *testing.T) {
	handler, _, _ := approvalsHandler(t)
	var res PendingApprovalsResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/approvals?status=pending", "", &res); rec.Code != http.StatusOK || len(res.Approvals) != 1 || res.Approvals[0].ID != "a1" || res.Approvals[0].ProjectName != "p" {
		t.Errorf("pending across rooms: status = %d, approvals = %+v", rec.Code, res.Approvals)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/approvals", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("without a status: %d, want 400", rec.Code)
	}
}

func TestApprovals_DecideWithAnswers(t *testing.T) {
	handler, _, _ := approvalsHandler(t)
	for _, bad := range []string{`"blue"`, `["blue"]`, `1`} {
		if rec := do(t, handler, http.MethodPost, "/api/v1/approvals/a1/decide", `{"user_id":"u1","allow":true,"answer":`+bad+`}`, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("answer %s: status = %d, want 400", bad, rec.Code)
		}
	}
	var res ApprovalResponse
	rec := do(t, handler, http.MethodPost, "/api/v1/approvals/a1/decide", `{"user_id":"u1","allow":true,"answer":{"answers":{"1":["blue"]}}}`, &res)
	if rec.Code != http.StatusOK || string(res.Approval.Answer) != `{"answers":{"1":["blue"]}}` {
		t.Errorf("decide with answers: status = %d, approval = %+v; body: %s", rec.Code, res.Approval, rec.Body)
	}
}
