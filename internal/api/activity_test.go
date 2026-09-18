package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

func TestMachineMembers(t *testing.T) {
	handler, room, agents := agentsHandler(t)
	ctx := context.Background()
	agent, _ := agents.CreateAgent(ctx, store.NewAgent{Name: "Reviewer", MachineID: "w1", Runtime: "claude", PermissionPreset: store.PermissionReadOnly})
	here, _ := agents.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: agent.ID})
	out, _ := agents.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: agent.ID, DisplayName: "Out"})
	if _, err := agents.RemoveMember(ctx, out.ID); err != nil {
		t.Fatal(err)
	}

	var got MachineMembersResponse
	rec := do(t, handler, http.MethodGet, "/api/v1/machines/w1/members", "", &got)
	if rec.Code != http.StatusOK || len(got.Members) != 1 || got.Members[0].Member.ID != here.ID || got.Members[0].ProjectName != "p" {
		t.Errorf("members: status = %d, body %s", rec.Code, rec.Body)
	}
	// A machine with none answers an empty list, not null.
	if rec := do(t, handler, http.MethodGet, "/api/v1/machines/w9/members", "", nil); rec.Code != http.StatusOK || rec.Body.String() != "{\"members\":[]}\n" {
		t.Errorf("no members: status = %d, body %s", rec.Code, rec.Body)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/machines/bad/members", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: status = %d, want 400", rec.Code)
	}
}

func TestMachineActivity(t *testing.T) {
	projects := newFakeProjects()
	now := time.Now()
	ended := now.Add(-time.Minute)
	turns := fakeTurns{
		"t1": {ID: "t1", MachineID: "w1", Runtime: "pi", Status: store.TurnDone, StartedAt: now.Add(-2 * time.Minute), EndedAt: &ended, Usage: runtime.Usage{InputTokens: 40, OutputTokens: 2}},
		"t2": {ID: "t2", MachineID: "w1", Runtime: "claude", Status: store.TurnFailed, StartedAt: now.Add(-3 * time.Hour), EndedAt: &ended},
		"t3": {ID: "t3", MachineID: "w2", Runtime: "pi", Status: store.TurnDone, StartedAt: now.Add(-time.Minute)},
		"t4": {ID: "t4", MachineID: "w1", Runtime: "pi", Status: store.TurnDone, StartedAt: now.Add(-5 * 24 * time.Hour)},
	}
	handler := NewHandler(Deps{Projects: projects, Turns: turns, Chat: &fakeChat{}})
	get := func(query string) (store.MachineActivity, int) {
		t.Helper()
		var got MachineActivityResponse
		rec := do(t, handler, http.MethodGet, "/api/v1/machines/w1/activity"+query, "", &got)
		return got.Activity, rec.Code
	}
	tokens := func(a store.MachineActivity) (sum int64) {
		for _, bucket := range a.Buckets {
			sum += bucket.Tokens
		}
		return sum
	}

	// Left out, the range is the last 24 hours, hour by hour.
	day, code := get("")
	if code != http.StatusOK || day.Range != store.ActivityDay || day.Step != store.StepHour || len(day.Buckets) != 24 || day.Turns != 2 || day.Failed != 1 {
		t.Errorf("24h: status %d, %+v", code, day)
	}
	if day.Usage != (runtime.Usage{InputTokens: 40, OutputTokens: 2}) || tokens(day) != 42 {
		t.Errorf("24h tokens: %+v, %d in the buckets", day.Usage, tokens(day))
	}
	if week, _ := get("?range=7d"); week.Step != store.StepDay || len(week.Buckets) != 7 || week.Turns != 3 {
		t.Errorf("7d: %+v", week)
	}
	// A month is days that begin at midnight where the browser is.
	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	month, _ := get("?range=30d&tz=Asia/Shanghai")
	if last := month.Buckets[len(month.Buckets)-1].Start.In(shanghai); month.Step != store.StepDay || len(month.Buckets) != 30 || month.Turns != 3 || last.Hour() != 0 || last.Minute() != 0 {
		t.Errorf("30d: %+v", month)
	}
	if pi, _ := get("?runtime=pi"); pi.Turns != 1 || pi.Failed != 0 {
		t.Errorf("one runtime: %+v", pi)
	}

	for _, query := range []string{"?range=90d", "?runtime=gpt", "?tz=Mars/Olympus_Mons"} {
		if _, code := get(query); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", query, code)
		}
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/machines/bad/activity", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad id: status = %d, want 400", rec.Code)
	}
}
