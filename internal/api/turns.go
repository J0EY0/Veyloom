package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

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
	CancelTurn(ctx context.Context, turnID string) error
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

func (h *handlers) getTurn(w http.ResponseWriter, r *http.Request) {
	turn, err := h.deps.Turns.GetTurn(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, TurnResponse{Turn: turn})
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
	writeJSON(w, http.StatusOK, TurnsResponse{Turns: turns})
}

// cancelTurn stops a running turn. The turn is looked up first so that an
// unknown id is a 404 while a known but finished turn is a 409.
func (h *handlers) cancelTurn(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := h.deps.Turns.GetTurn(r.Context(), id); err != nil {
		h.writeStoreError(w, r, err)
		return
	}
	if err := h.deps.Chat.CancelTurn(r.Context(), id); err != nil {
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
// transcript directory for a turn still running.
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

	w.Header().Set("Content-Type", "application/x-ndjson")
	if turn.Status == store.TurnRunning {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		// A finished transcript never changes.
		w.Header().Set("Cache-Control", "private, max-age=3600")
	}
	_, _ = io.Copy(w, file)
}

func validTurnStatus(s string) bool {
	switch store.TurnStatus(s) {
	case store.TurnRunning, store.TurnDone, store.TurnFailed, store.TurnCancelled:
		return true
	}
	return false
}
