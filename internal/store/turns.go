package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// TurnStatus is the lifecycle state of a turn.
type TurnStatus string

const (
	TurnRunning   TurnStatus = "running"
	TurnDone      TurnStatus = "done"
	TurnFailed    TurnStatus = "failed"
	TurnCancelled TurnStatus = "cancelled"
)

// Turn is one run of an agent instance's engine.
type Turn struct {
	ID               string     `json:"id"`
	AgentInstanceID  string     `json:"agent_instance_id"`
	RoomID           string     `json:"room_id"`
	ThreadID         string     `json:"thread_id"`
	TriggerMessageID string     `json:"trigger_message_id,omitempty"`
	WorkerID         string     `json:"worker_id"`
	Status           TurnStatus `json:"status"`
	Error            string     `json:"error,omitempty"`
	ReplyMessageID   string     `json:"reply_message_id,omitempty"`
	TranscriptPath   string     `json:"transcript_path,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
}

// NewTurn is the input to CreateTurn.
type NewTurn struct {
	AgentInstanceID  string
	RoomID           string
	ThreadID         string
	TriggerMessageID string
	WorkerID         string
	TranscriptPath   string
}

// TurnOutcome is the input to FinishTurn.
type TurnOutcome struct {
	Status         TurnStatus
	Error          string
	ReplyMessageID string
	// TranscriptPath is where the event stream was written; recorded at
	// the end because the file is named after the turn's id.
	TranscriptPath string
}

// CreateTurn records a turn that is starting, in status running.
func (s *Store) CreateTurn(ctx context.Context, t NewTurn) (Turn, error) {
	instanceID, err := parseUUID(t.AgentInstanceID)
	if err != nil {
		return Turn{}, err
	}
	roomID, err := parseUUID(t.RoomID)
	if err != nil {
		return Turn{}, err
	}
	threadID, err := parseUUID(t.ThreadID)
	if err != nil {
		return Turn{}, err
	}
	workerID, err := parseUUID(t.WorkerID)
	if err != nil {
		return Turn{}, err
	}
	var triggerID pgtype.UUID
	if t.TriggerMessageID != "" {
		if triggerID, err = parseUUID(t.TriggerMessageID); err != nil {
			return Turn{}, err
		}
	}

	row, err := s.q.CreateTurn(ctx, db.CreateTurnParams{
		AgentInstanceID:  instanceID,
		RoomID:           roomID,
		ThreadID:         threadID,
		TriggerMessageID: triggerID,
		WorkerID:         workerID,
		TranscriptPath:   t.TranscriptPath,
	})
	if err != nil {
		return Turn{}, mapAgentError("create turn", err)
	}
	return toTurn(row), nil
}

// FinishTurn records how a turn ended.
func (s *Store) FinishTurn(ctx context.Context, id string, out TurnOutcome) (Turn, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Turn{}, err
	}
	var replyID pgtype.UUID
	if out.ReplyMessageID != "" {
		if replyID, err = parseUUID(out.ReplyMessageID); err != nil {
			return Turn{}, err
		}
	}
	row, err := s.q.FinishTurn(ctx, db.FinishTurnParams{
		ID:             uid,
		Status:         string(out.Status),
		Error:          out.Error,
		ReplyMessageID: replyID,
		TranscriptPath: out.TranscriptPath,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Turn{}, fmt.Errorf("turn %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Turn{}, mapAgentError("finish turn", err)
	}
	return toTurn(row), nil
}

// GetTurn returns one turn, or ErrNotFound.
func (s *Store) GetTurn(ctx context.Context, id string) (Turn, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Turn{}, err
	}
	row, err := s.q.GetTurn(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Turn{}, fmt.Errorf("turn %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Turn{}, fmt.Errorf("get turn %s: %w", id, err)
	}
	return toTurn(row), nil
}

// ListRoomTurns returns a room's most recent turns, newest first.
func (s *Store) ListRoomTurns(ctx context.Context, roomID string, limit int) ([]Turn, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListRoomTurns(ctx, db.ListRoomTurnsParams{RoomID: rid, Limit: clampLimit(limit)})
	if err != nil {
		return nil, fmt.Errorf("list turns of room %s: %w", roomID, err)
	}
	out := make([]Turn, 0, len(rows))
	for _, row := range rows {
		out = append(out, toTurn(row))
	}
	return out, nil
}

// UpdateAgentInstanceSession stores the engine session to resume next turn.
func (s *Store) UpdateAgentInstanceSession(ctx context.Context, instanceID, sessionRef string) error {
	uid, err := parseUUID(instanceID)
	if err != nil {
		return err
	}
	if err := s.q.UpdateAgentInstanceSession(ctx, db.UpdateAgentInstanceSessionParams{ID: uid, EngineSessionRef: sessionRef}); err != nil {
		return fmt.Errorf("update session of agent instance %s: %w", instanceID, err)
	}
	return nil
}

// LastAgentMessageInThread returns the most recent agent reply in a thread,
// or ErrNotFound when no agent has spoken there.
func (s *Store) LastAgentMessageInThread(ctx context.Context, threadID string) (Message, error) {
	tid, err := parseUUID(threadID)
	if err != nil {
		return Message{}, err
	}
	row, err := s.q.LastAgentMessageInThread(ctx, tid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, fmt.Errorf("agent message in thread %s: %w", threadID, ErrNotFound)
	}
	if err != nil {
		return Message{}, fmt.Errorf("last agent message in thread %s: %w", threadID, err)
	}
	return toMessage(row)
}

func toTurn(row db.Turn) Turn {
	t := Turn{
		ID:               uuidString(row.ID),
		AgentInstanceID:  uuidString(row.AgentInstanceID),
		RoomID:           uuidString(row.RoomID),
		ThreadID:         uuidString(row.ThreadID),
		TriggerMessageID: uuidString(row.TriggerMessageID),
		WorkerID:         uuidString(row.WorkerID),
		Status:           TurnStatus(row.Status),
		Error:            row.Error,
		ReplyMessageID:   uuidString(row.ReplyMessageID),
		TranscriptPath:   row.TranscriptPath,
		StartedAt:        row.StartedAt.Time,
	}
	if row.EndedAt.Valid {
		ended := row.EndedAt.Time
		t.EndedAt = &ended
	}
	return t
}
