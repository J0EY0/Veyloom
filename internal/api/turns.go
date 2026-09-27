package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Chat is the hub's entry point for user messages, turn control and
// approval decisions. These go through the hub rather than the store so
// that routing to agents, and delivery to waiting turns, can never be
// bypassed.
type Chat interface {
	PostUserMessage(ctx context.Context, m store.NewMessage) (store.Message, error)
	// CancelTurn stops a running turn; with newSession the member's next
	// turn starts a new session (docs/design.md 5.23.8).
	CancelTurn(ctx context.Context, turnID string, newSession bool) error
	// QuietSince says, of the turns given, which went quiet and since when.
	QuietSince(turnIDs []string) map[string]time.Time
	// TranscriptSoFar writes out a running turn's transcript and says how
	// many bytes of it are whole records; false when it is not running.
	TranscriptSoFar(turnID string) (int64, bool)
	// ContinueRelay lets a wake a limit on agents waking one another held
	// back go on (docs/design.md 5.22), by the note that told of it.
	ContinueRelay(ctx context.Context, noteID string) error
	// DecideApproval settles a request; scope is how far an allow goes.
	DecideApproval(ctx context.Context, approvalID, userID string, d runtime.Decision, scope store.AllowScope) (store.Approval, error)
	// UntrustTurn has people asked again for a running turn's requests.
	UntrustTurn(ctx context.Context, turnID string) (store.Turn, error)
	Subscribe(roomID string) hub.Subscription
	SubscribeInbox(userID string) hub.Subscription
	// MarkInboxRead marks read what read picks of a person's inbox.
	MarkInboxRead(ctx context.Context, userID string, read store.InboxRead) (int, error)
	// Pauses are what keeps turns from starting now (docs/design.md
	// 5.23.3); LiftPause lifts one at a person's asking.
	Pauses(ctx context.Context) []store.Pause
	LiftPause(ctx context.Context, id, userID string) error
	// CancelReminder takes back a member's reminder not yet due at a
	// person's asking (docs/design.md 5.23.4).
	CancelReminder(ctx context.Context, id, userID string) (store.Reminder, error)
	// RunDraft does what a member drafted for a person, and DeclineDraft
	// turns it down (docs/design.md 5.23.5).
	RunDraft(ctx context.Context, id, userID string, edit hub.DraftEdit) (store.Draft, error)
	DeclineDraft(ctx context.Context, id, userID string) (store.Draft, error)
}

// TurnStore reads recorded turns.
type TurnStore interface {
	GetTurn(ctx context.Context, id string) (store.Turn, error)
	ListRoomTurns(ctx context.Context, roomID string, limit int) ([]store.Turn, error)
	ListRoomTurnsByStatus(ctx context.Context, roomID string, status store.TurnStatus, limit int) ([]store.Turn, error)
	ListThreadTurns(ctx context.Context, threadID string) ([]store.Turn, error)
	ListRunningTopics(ctx context.Context) ([]store.RunningTopic, error)
	// ListThreadRelayHolds lists the wakes held back that notes in a topic
	// tell of (docs/design.md 5.22).
	ListThreadRelayHolds(ctx context.Context, threadID string) ([]store.RelayHold, error)
	// ListThreadReminders lists the reminders members set in a topic, in
	// the order set (docs/design.md 5.23.4).
	ListThreadReminders(ctx context.Context, threadID string) ([]store.Reminder, error)
	// ListThreadDrafts lists what members drafted in a topic for a person
	// to run, in the order drafted (docs/design.md 5.23.5).
	ListThreadDrafts(ctx context.Context, threadID string) ([]store.Draft, error)
	MachineActivity(ctx context.Context, machineID string, q store.ActivityQuery) (store.MachineActivity, error)
}

// TopicsResponse is the body of GET /api/v1/topics?status=running: the
// topics with a turn in flight, across every project.
type TopicsResponse struct {
	Topics []store.RunningTopic `json:"topics"`
}

// listTopics answers GET /api/v1/topics. Only status=running exists so
// far: the sidebar's list of what agents are working on right now.
func (h *handlers) listTopics(w http.ResponseWriter, r *http.Request) {
	if status := r.URL.Query().Get("status"); status != string(store.TurnRunning) {
		writeError(w, http.StatusBadRequest, "status must be running")
		return
	}
	topics, err := h.deps.Turns.ListRunningTopics(r.Context())
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, TopicsResponse{Topics: topics})
}

// TurnResponse is the body of single-turn endpoints.
type TurnResponse struct {
	Turn store.Turn `json:"turn"`
}

// TurnsResponse is the body of GET /api/v1/rooms/{id}/turns, newest first.
type TurnsResponse struct {
	Turns []store.Turn `json:"turns"`
}

// CancelTurnRequest is the optional body of POST /api/v1/turns/{id}/cancel.
type CancelTurnRequest struct {
	// NewSession has the member's next turn start a new session.
	NewSession bool `json:"new_session"`
}

func (h *handlers) getTurn(w http.ResponseWriter, r *http.Request) {
	turn, err := h.deps.Turns.GetTurn(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	one := []store.Turn{turn}
	h.withQuiet(one)
	writeJSON(w, http.StatusOK, TurnResponse{Turn: one[0]})
}

func (h *handlers) listRoomTurns(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("id")
	if _, err := h.deps.Projects.GetRoom(r.Context(), roomID); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	limit, _, err := queryInt(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "limit: "+err.Error())
		return
	}
	var turns []store.Turn
	if status := r.URL.Query().Get("status"); status != "" {
		if !validTurnStatus(status) {
			writeError(w, http.StatusBadRequest, "status must be running, done, failed or cancelled")
			return
		}
		turns, err = h.deps.Turns.ListRoomTurnsByStatus(r.Context(), roomID, store.TurnStatus(status), int(limit))
	} else {
		turns, err = h.deps.Turns.ListRoomTurns(r.Context(), roomID, int(limit))
	}
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	h.withQuiet(turns)
	writeJSON(w, http.StatusOK, TurnsResponse{Turns: turns})
}

// withQuiet says of the running turns among turns which went quiet, and
// since when: the hub's to know, not the store's.
func (h *handlers) withQuiet(turns []store.Turn) {
	var running []string
	for _, t := range turns {
		if t.Status == store.TurnRunning {
			running = append(running, t.ID)
		}
	}
	if len(running) == 0 {
		return
	}
	quiet := h.deps.Chat.QuietSince(running)
	for i := range turns {
		if since, ok := quiet[turns[i].ID]; ok {
			turns[i].QuietSince = &since
		}
	}
}

// cancelTurn stops a running turn. The turn is looked up first so that an
// unknown id is a 404 while a known but finished turn is a 409.
func (h *handlers) cancelTurn(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.deps.Turns.GetTurn(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	var req CancelTurnRequest
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeReason(w, http.StatusBadRequest, err)
			return
		}
	}
	if err := h.deps.Chat.CancelTurn(r.Context(), id, req.NewSession); err != nil {
		if errors.Is(err, hub.ErrUnknownTurn) {
			writeCoded(w, http.StatusConflict, "turnNotRunning", nil, "turn is not running")
			return
		}
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// RelayHoldsResponse is the body of GET /api/v1/threads/{id}/relay-holds:
// the wakes held back that notes in the topic tell of, let go on or not.
type RelayHoldsResponse struct {
	Holds []store.RelayHold `json:"holds"`
}

// threadRelayHolds lists the wakes held back in a topic, for its notes to
// offer letting them go on.
func (h *handlers) threadRelayHolds(w http.ResponseWriter, r *http.Request) {
	holds, err := h.deps.Turns.ListThreadRelayHolds(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, RelayHoldsResponse{Holds: holds})
}

// continueRelay lets a held wake go on: 202, as the member's turn runs on
// after the answer.
func (h *handlers) continueRelay(w http.ResponseWriter, r *http.Request) {
	if err := h.deps.Chat.ContinueRelay(r.Context(), r.PathValue("id")); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

// turnTranscript streams a turn's JSONL transcript as it was written. The
// path is the one recorded on the turn, or the conventional one under the
// transcript directory for a turn still running, whose transcript is read
// as far as it is written whole: the page lays it under the live events it
// missed, before it was opened or while its stream was down.
func (h *handlers) turnTranscript(w http.ResponseWriter, r *http.Request) {
	turn, err := h.deps.Turns.GetTurn(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	path := turn.TranscriptPath
	if path == "" {
		if h.deps.TranscriptDir == "" {
			writeCoded(w, http.StatusNotFound, "transcriptGone", nil, "transcript not available")
			return
		}
		path = filepath.Join(h.deps.TranscriptDir, turn.ID+".jsonl")
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeCoded(w, http.StatusNotFound, "transcriptGone", nil, "transcript not available")
			return
		}
		h.deps.Logger.Error("open transcript", "turn", turn.ID, "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	defer file.Close()

	var body io.Reader = file
	w.Header().Set("Content-Type", "application/x-ndjson")
	if turn.Status == store.TurnRunning {
		w.Header().Set("Cache-Control", "no-store")
		// The hub may be writing past what it has flushed.
		if n, ok := h.deps.Chat.TranscriptSoFar(turn.ID); ok {
			body = io.LimitReader(file, n)
		}
	} else {
		// A finished transcript never changes.
		w.Header().Set("Cache-Control", "private, max-age=3600")
	}
	_, _ = io.Copy(w, body)
}

func validTurnStatus(s string) bool {
	switch store.TurnStatus(s) {
	case store.TurnRunning, store.TurnDone, store.TurnFailed, store.TurnCancelled:
		return true
	}
	return false
}
