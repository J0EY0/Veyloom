package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

type fakeTurns map[string]store.Turn

func (f fakeTurns) GetTurn(_ context.Context, id string) (store.Turn, error) {
	if err := checkID(id); err != nil {
		return store.Turn{}, err
	}
	t, ok := f[id]
	if !ok {
		return store.Turn{}, fmt.Errorf("turn %s: %w", id, store.ErrNotFound)
	}
	return t, nil
}

func (f fakeTurns) ListRoomTurns(_ context.Context, roomID string, limit int) ([]store.Turn, error) {
	out := []store.Turn{}
	for _, t := range f {
		if t.RoomID == roomID {
			out = append(out, t)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func turnsHandler(t *testing.T) (http.Handler, store.Room, *fakeChat) {
	t.Helper()
	projects := newFakeProjects()
	_, room, _ := projects.CreateProject(context.Background(), store.NewProject{Name: "p"})
	turns := fakeTurns{
		"t1": {ID: "t1", RoomID: room.ID, Status: store.TurnRunning},
		"t2": {ID: "t2", RoomID: room.ID, Status: store.TurnDone},
	}
	chat := &fakeChat{running: map[string]bool{"t1": true}}
	return NewHandler(Deps{Projects: projects, Turns: turns, Chat: chat}), room, chat
}

func TestTurns_GetAndList(t *testing.T) {
	handler, room, _ := turnsHandler(t)

	var one TurnResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/turns/t1", "", &one); rec.Code != http.StatusOK || one.Turn.Status != store.TurnRunning {
		t.Errorf("get: status = %d, turn = %+v", rec.Code, one.Turn)
	}
	var list TurnsResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/"+room.ID+"/turns", "", &list); rec.Code != http.StatusOK || len(list.Turns) != 2 {
		t.Errorf("list: status = %d, turns = %+v", rec.Code, list.Turns)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/"+room.ID+"/turns?limit=1", "", &list); rec.Code != http.StatusOK || len(list.Turns) != 1 {
		t.Errorf("limit: status = %d, turns = %+v", rec.Code, list.Turns)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/"+room.ID+"/turns?limit=x", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad limit: status = %d, want 400", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/turns/t404", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown turn: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/r404/turns", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown room: status = %d, want 404", rec.Code)
	}
}

func TestTurns_Cancel(t *testing.T) {
	handler, _, chat := turnsHandler(t)

	if rec := do(t, handler, http.MethodPost, "/api/v1/turns/t1/cancel", "", nil); rec.Code != http.StatusAccepted {
		t.Errorf("cancel running: status = %d, want 202; body: %s", rec.Code, rec.Body)
	}
	if len(chat.cancelled) != 1 || chat.cancelled[0] != "t1" {
		t.Errorf("hub should have been asked to cancel t1, got %v", chat.cancelled)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/turns/t2/cancel", "", nil); rec.Code != http.StatusConflict {
		t.Errorf("cancel finished: status = %d, want 409", rec.Code)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/turns/t404/cancel", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("cancel unknown: status = %d, want 404", rec.Code)
	}
}
