package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

// The project's task board, a piece of work's page and the usage page
// (docs/webui.md 4.20).

// WorkStore reads the tasks, the pieces of work and where the tokens went.
type WorkStore interface {
	ListRoomTasks(ctx context.Context, roomID string) ([]store.Task, error)
	GetWork(ctx context.Context, chain string) (store.Work, error)
	Usage(ctx context.Context, q store.UsageQuery) (store.Usage, error)
}

// TasksResponse is the body of GET /api/v1/rooms/{id}/tasks: the tasks of
// the room's latest pieces of work, in the order they began.
type TasksResponse struct {
	Tasks []store.Task `json:"tasks"`
}

// WorkResponse is the body of GET /api/v1/works/{chain}.
type WorkResponse struct {
	Work store.Work `json:"work"`
}

// UsageResponse is the body of GET /api/v1/usage.
type UsageResponse struct {
	Usage store.Usage `json:"usage"`
}

// listRoomTasks answers the task board; 404 for an unknown room.
func (h *handlers) listRoomTasks(w http.ResponseWriter, r *http.Request) {
	if h.deps.Work == nil {
		writeError(w, http.StatusNotFound, "tasks are not kept here")
		return
	}
	roomID := r.PathValue("id")
	if _, err := h.deps.Projects.GetRoom(r.Context(), roomID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	tasks, err := h.deps.Work.ListRoomTasks(r.Context(), roomID)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, TasksResponse{Tasks: tasks})
}

// getWork answers a piece of work's page, by the message a person began
// it with; 404 for a message that set nothing going.
func (h *handlers) getWork(w http.ResponseWriter, r *http.Request) {
	if h.deps.Work == nil {
		writeError(w, http.StatusNotFound, "work is not kept here")
		return
	}
	work, err := h.deps.Work.GetWork(r.Context(), r.PathValue("chain"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, WorkResponse{Work: work})
}

// usage answers where the tokens went. ?range= is today (when left out),
// 7d or 30d; ?project= keeps one project's turns; ?tz= is the IANA time
// zone whose days the range follows, UTC when left out.
func (h *handlers) usage(w http.ResponseWriter, r *http.Request) {
	if h.deps.Work == nil {
		writeError(w, http.StatusNotFound, "usage is not kept here")
		return
	}
	params := r.URL.Query()
	q := store.UsageQuery{Range: store.UsageRange(params.Get("range")), ProjectID: params.Get("project"), Location: time.UTC, Now: time.Now()}
	if q.Range == "" {
		q.Range = store.UsageToday
	}
	if !q.Range.Valid() {
		writeError(w, http.StatusBadRequest, "range must be one of today, 7d, 30d")
		return
	}
	if tz := params.Get("tz"); tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown time zone %q", tz))
			return
		}
		q.Location = loc
	}
	usage, err := h.deps.Work.Usage(r.Context(), q)
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, UsageResponse{Usage: usage})
}
