package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// ListRunningTopics lists one topic per running turn's thread, named
// after the agent id, which is enough for the handler's test.
func (f fakeTurns) ListThreadRelayHolds(_ context.Context, threadID string) ([]store.RelayHold, error) {
	if threadID != "th1" {
		return []store.RelayHold{}, nil
	}
	return []store.RelayHold{{MessageID: "n1", MemberID: "m1", ThreadID: "th1", Reason: store.HoldIdle}}, nil
}

// ListThreadReminders has one reminder, not yet due, in topic th1.
func (f fakeTurns) ListThreadReminders(_ context.Context, threadID string) ([]store.Reminder, error) {
	if threadID != "th1" {
		return nil, nil
	}
	return []store.Reminder{{ID: "rm1", MemberID: "m1", ThreadID: "th1", Note: "check CI", Status: store.ReminderPending, SetMessageID: "n2"}}, nil
}

// ListThreadDrafts has one merge drafted, not yet run, in topic th1.
func (f fakeTurns) ListThreadDrafts(_ context.Context, threadID string) ([]store.Draft, error) {
	if threadID != "th1" {
		return nil, nil
	}
	return []store.Draft{{ID: "d1", MemberID: "m1", TargetID: "m2", ThreadID: "th1", Kind: store.DraftMerge, Status: store.DraftPending,
		Params: store.DraftParams{Message: "Add tags"}, MessageID: "n3"}}, nil
}

func (f fakeTurns) ListRunningTopics(context.Context) ([]store.RunningTopic, error) {
	out := []store.RunningTopic{}
	seen := map[string]bool{}
	for _, t := range f {
		if t.Status != store.TurnRunning || seen[t.ThreadID] {
			continue
		}
		seen[t.ThreadID] = true
		out = append(out, store.RunningTopic{ThreadID: t.ThreadID, RoomID: t.RoomID, RootBody: "ask", Members: []string{t.MemberID}, StartedAt: t.StartedAt})
	}
	return out, nil
}

func (f fakeTurns) MachineActivity(_ context.Context, machineID string, q store.ActivityQuery) (store.MachineActivity, error) {
	if err := checkID(machineID); err != nil {
		return store.MachineActivity{}, err
	}
	var spans []store.TurnSpan
	for _, t := range f {
		if t.MachineID == machineID && (q.Runtime == "" || t.Runtime == q.Runtime) {
			spans = append(spans, store.TurnSpan{Status: t.Status, StartedAt: t.StartedAt, EndedAt: t.EndedAt, Usage: t.Usage})
		}
	}
	return store.SummarizeActivity(spans, q), nil
}

func (f fakeTurns) ListRoomTurnsByStatus(ctx context.Context, roomID string, status store.TurnStatus, limit int) ([]store.Turn, error) {
	all, err := f.ListRoomTurns(ctx, roomID, 0)
	if err != nil {
		return nil, err
	}
	out := []store.Turn{}
	for _, t := range all {
		if t.Status == status {
			out = append(out, t)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f fakeTurns) ListThreadTurns(_ context.Context, threadID string) ([]store.Turn, error) {
	out := []store.Turn{}
	for _, t := range f {
		if t.ThreadID == threadID {
			out = append(out, t)
		}
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
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/"+room.ID+"/turns?status=running", "", &list); rec.Code != http.StatusOK || len(list.Turns) != 1 || list.Turns[0].ID != "t1" {
		t.Errorf("running only: status = %d, turns = %+v", rec.Code, list.Turns)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/"+room.ID+"/turns?status=bogus", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad status: status = %d, want 400", rec.Code)
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
	// With a new session for the member's next turn (docs/design.md 5.23.8).
	if rec := do(t, handler, http.MethodPost, "/api/v1/turns/t1/cancel", `{"new_session": true}`, nil); rec.Code != http.StatusAccepted || len(chat.startedOver) != 1 || chat.startedOver[0] != "t1" {
		t.Errorf("cancel with a new session: %d %v", rec.Code, chat.startedOver)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/turns/t1/cancel", `{"new_session": "yes"`, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("a body that does not parse: %d", rec.Code)
	}
}

// A running turn gone quiet says since when; the store knows nothing of it.
func TestTurns_Quiet(t *testing.T) {
	handler, _, chat := turnsHandler(t)
	since := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	chat.quiet = map[string]time.Time{"t1": since, "t2": since}
	var list TurnsResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/rooms/r1/turns", "", &list); rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
	for _, turn := range list.Turns {
		quiet := turn.QuietSince != nil && turn.QuietSince.Equal(since)
		if quiet != (turn.ID == "t1") {
			t.Errorf("%s (%s) quiet since %v", turn.ID, turn.Status, turn.QuietSince)
		}
	}
	var one TurnResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/turns/t1", "", &one); rec.Code != http.StatusOK || one.Turn.QuietSince == nil {
		t.Errorf("the turn itself: %d %+v", rec.Code, one.Turn.QuietSince)
	}
}

// A wake a limit held back, let go on by its note (docs/design.md 5.22).
func TestRelays_Continue(t *testing.T) {
	handler, _, chat := turnsHandler(t)
	if rec := do(t, handler, http.MethodPost, "/api/v1/relays/n1/continue", "", nil); rec.Code != http.StatusAccepted || len(chat.continued) != 1 || chat.continued[0] != "n1" {
		t.Errorf("continue: %d %v", rec.Code, chat.continued)
	}
	if rec := do(t, handler, http.MethodPost, "/api/v1/relays/gone/continue", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("no wake held there: %d", rec.Code)
	}
	var holds RelayHoldsResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/threads/th1/relay-holds", "", &holds); rec.Code != http.StatusOK || len(holds.Holds) != 1 || holds.Holds[0].Reason != store.HoldIdle {
		t.Errorf("a topic's held wakes: %d %+v", rec.Code, holds)
	}
}

func TestTurns_Transcript(t *testing.T) {
	dir := t.TempDir()
	finished := filepath.Join(dir, "done.jsonl")
	if err := os.WriteFile(finished, []byte("{\"kind\":\"start\"}\n{\"kind\":\"done\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A running turn's file: one record whole, the next still being written.
	if err := os.WriteFile(filepath.Join(dir, "t-run.jsonl"), []byte("{\"kind\":\"start\"}\n{\"kind\":\"ev"), 0o600); err != nil {
		t.Fatal(err)
	}
	projects := newFakeProjects()
	_, room, _ := projects.CreateProject(context.Background(), store.NewProject{Name: "p"})
	turns := fakeTurns{
		"t-done":    {ID: "t-done", RoomID: room.ID, Status: store.TurnDone, TranscriptPath: finished},
		"t-run":     {ID: "t-run", RoomID: room.ID, Status: store.TurnRunning},
		"t-lost":    {ID: "t-lost", RoomID: room.ID, Status: store.TurnFailed, TranscriptPath: filepath.Join(dir, "gone.jsonl")},
		"t-nowhere": {ID: "t-nowhere", RoomID: room.ID, Status: store.TurnRunning},
	}
	handler := NewHandler(Deps{Projects: projects, Turns: turns, Chat: &fakeChat{written: map[string]int64{"t-run": 17}}, TranscriptDir: dir})

	rec := do(t, handler, http.MethodGet, "/api/v1/turns/t-done/transcript", "", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"kind\":\"start\"}\n{\"kind\":\"done\"}\n" {
		t.Errorf("finished: status = %d, body = %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("Content-Type = %q", ct)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/turns/t-run/transcript", "", nil); rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" || rec.Body.String() != "{\"kind\":\"start\"}\n" {
		t.Errorf("running turn reads the conventional path, as far as it is whole: status = %d, cache = %q, body = %q", rec.Code, rec.Header().Get("Cache-Control"), rec.Body)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/turns/t-lost/transcript", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("missing file: status = %d, want 404", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/turns/t404/transcript", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("unknown turn: status = %d, want 404", rec.Code)
	}
	bare := NewHandler(Deps{Projects: projects, Turns: turns, Chat: &fakeChat{}})
	if rec := do(t, bare, http.MethodGet, "/api/v1/turns/t-nowhere/transcript", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("no directory configured: status = %d, want 404", rec.Code)
	}
}

func TestTopics_ListsRunningOnes(t *testing.T) {
	turns := fakeTurns{
		"t1": {ID: "t1", RoomID: "r1", ThreadID: "th1", MemberID: "a1", Status: store.TurnRunning},
		"t2": {ID: "t2", RoomID: "r1", ThreadID: "th2", MemberID: "a2", Status: store.TurnDone},
	}
	handler := NewHandler(Deps{Turns: turns})
	var res TopicsResponse
	if rec := do(t, handler, http.MethodGet, "/api/v1/topics?status=running", "", &res); rec.Code != http.StatusOK || len(res.Topics) != 1 || res.Topics[0].ThreadID != "th1" || res.Topics[0].Members[0] != "a1" {
		t.Errorf("running topics: status = %d, topics = %+v", rec.Code, res.Topics)
	}
	if rec := do(t, handler, http.MethodGet, "/api/v1/topics", "", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("without a status: %d, want 400", rec.Code)
	}
}
