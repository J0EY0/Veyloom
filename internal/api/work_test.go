package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// fakeWork answers the task board, one piece of work and usage, and keeps
// the usage query it was asked.
type fakeWork struct {
	asked store.UsageQuery
}

func (f *fakeWork) ListRoomTasks(_ context.Context, roomID string) ([]store.Task, error) {
	return []store.Task{{Chain: "c1", ThreadNumber: 1, MemberID: "m1", Title: "给 linkkeeper 加标签功能", State: store.TaskDone}}, nil
}

func (f *fakeWork) GetWork(_ context.Context, chain string) (store.Work, error) {
	if chain != "c1" {
		return store.Work{}, fmt.Errorf("work %s: %w", chain, store.ErrNotFound)
	}
	return store.Work{Chain: "c1", Title: "给 linkkeeper 加标签功能", Turns: []store.WorkTurn{}, Events: []store.WorkEvent{}}, nil
}

func (f *fakeWork) Usage(_ context.Context, q store.UsageQuery) (store.Usage, error) {
	f.asked = q
	return store.Usage{Range: q.Range, Step: store.UsageStepTurn, Turns: 3}, nil
}

func TestWork_TasksWorkAndUsage(t *testing.T) {
	projects := newFakeProjects()
	_, room, _ := projects.CreateProject(context.Background(), store.NewProject{Name: "p"})
	work := &fakeWork{}
	handler := NewHandler(Deps{Projects: projects, Work: work})

	var tasks TasksResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/"+room.ID+"/tasks", "", &tasks); rec.Code != http.StatusOK || len(tasks.Tasks) != 1 || tasks.Tasks[0].Title != "给 linkkeeper 加标签功能" {
		t.Errorf("tasks: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/r404/tasks", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown room: %d", rec.Code)
	}

	var one WorkResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/works/c1", "", &one); rec.Code != http.StatusOK || one.Work.Chain != "c1" {
		t.Errorf("work: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/works/c404", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown work: %d", rec.Code)
	}

	var usage UsageResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/usage", "", &usage); rec.Code != http.StatusOK || usage.Usage.Range != store.UsageToday || usage.Usage.Turns != 3 {
		t.Errorf("usage: %d %s", rec.Code, rec.Body)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/usage?range=7d&project=p1&tz=Asia/Shanghai", "", nil); rec.Code != http.StatusOK ||
		work.asked.Range != store.UsageWeek || work.asked.ProjectID != "p1" || work.asked.Location.String() != "Asia/Shanghai" {
		t.Errorf("usage for a week: %d, asked %+v", rec.Code, work.asked)
	}
	for _, bad := range []string{"range=1y", "tz=Mars/Base"} {
		if rec := do(t, handler, http.MethodGet, "/api/v1/usage?"+bad, "", nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}

	none := NewHandler(Deps{Projects: projects})
	for _, path := range []string{"/api/v1/rooms/" + room.ID + "/tasks", "/api/v1/works/c1", "/api/v1/usage"} {
		if rec := do(t, none, http.MethodGet, path, "", nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s without a work store: %d", path, rec.Code)
		}
	}
}
