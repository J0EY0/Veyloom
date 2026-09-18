package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/runtime"
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

// Turn is one run of a member's runtime.
type Turn struct {
	ID               string `json:"id"`
	MemberID         string `json:"member_id"`
	RoomID           string `json:"room_id"`
	ThreadID         string `json:"thread_id"`
	TriggerMessageID string `json:"trigger_message_id,omitempty"`
	MachineID        string `json:"machine_id"`
	// SessionID is the member's session the turn ran in; empty for a turn
	// that failed before it had one.
	SessionID string `json:"session_id,omitempty"`
	// Runtime is the runtime that ran the turn.
	Runtime        string     `json:"runtime"`
	Status         TurnStatus `json:"status"`
	Error          string     `json:"error,omitempty"`
	ReplyMessageID string     `json:"reply_message_id,omitempty"`
	TranscriptPath string     `json:"transcript_path,omitempty"`
	// Usage is the tokens the turn spent; zero until it ends.
	Usage runtime.Usage `json:"usage"`
	// FilesChanged are the files the turn wrote, each once, in the order
	// first touched; known once it ends.
	FilesChanged []string   `json:"files_changed,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
}

// NewTurn is the input to CreateTurn.
type NewTurn struct {
	MemberID         string
	RoomID           string
	ThreadID         string
	TriggerMessageID string
	MachineID        string
	// Runtime is the runtime the turn runs on, as the agent names it.
	Runtime        string
	TranscriptPath string
	// SessionID is the member's session the turn runs in, when it has one.
	SessionID string
}

// TurnOutcome is the input to FinishTurn.
type TurnOutcome struct {
	Status         TurnStatus
	Error          string
	ReplyMessageID string
	// TranscriptPath is where the event stream was written; recorded at
	// the end because the file is named after the turn's id.
	TranscriptPath string
	// Usage is the tokens the turn spent, however it ended.
	Usage runtime.Usage
	// FilesChanged are the files the turn wrote.
	FilesChanged []string
}

// CreateTurn records a turn that is starting, in status running.
func (s *Store) CreateTurn(ctx context.Context, t NewTurn) (Turn, error) {
	memberID, err := parseUUID(t.MemberID)
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
	machineID, err := parseUUID(t.MachineID)
	if err != nil {
		return Turn{}, err
	}
	var triggerID pgtype.UUID
	if t.TriggerMessageID != "" {
		if triggerID, err = parseUUID(t.TriggerMessageID); err != nil {
			return Turn{}, err
		}
	}

	var sessionID pgtype.UUID
	if t.SessionID != "" {
		if sessionID, err = parseUUID(t.SessionID); err != nil {
			return Turn{}, err
		}
	}

	row, err := s.q.CreateTurn(ctx, db.CreateTurnParams{
		MemberID:         memberID,
		RoomID:           roomID,
		ThreadID:         threadID,
		TriggerMessageID: triggerID,
		MachineID:        machineID,
		Runtime:          t.Runtime,
		TranscriptPath:   t.TranscriptPath,
		SessionID:        sessionID,
	})
	if err != nil {
		return Turn{}, mapPGError("create turn", err)
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
		ID:               uid,
		Status:           string(out.Status),
		Error:            out.Error,
		ReplyMessageID:   replyID,
		TranscriptPath:   out.TranscriptPath,
		InputTokens:      out.Usage.InputTokens,
		CacheReadTokens:  out.Usage.CacheReadTokens,
		CacheWriteTokens: out.Usage.CacheWriteTokens,
		OutputTokens:     out.Usage.OutputTokens,
		FilesChanged:     nonNil(out.FilesChanged),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Turn{}, fmt.Errorf("turn %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Turn{}, mapPGError("finish turn", err)
	}
	return toTurn(row), nil
}

// SetTurnSession records that a turn ended up running in another session
// than the one it started in.
func (s *Store) SetTurnSession(ctx context.Context, turnID, sessionID string) error {
	tid, err := parseUUID(turnID)
	if err != nil {
		return err
	}
	sid, err := parseUUID(sessionID)
	if err != nil {
		return err
	}
	n, err := s.q.SetTurnSession(ctx, db.SetTurnSessionParams{ID: tid, SessionID: sid})
	if err != nil {
		return mapPGError("set session of turn", err)
	}
	if n == 0 {
		return fmt.Errorf("turn %s: %w", turnID, ErrNotFound)
	}
	return nil
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

// ListRoomTurnsByStatus returns a room's most recent turns in one status,
// newest first.
func (s *Store) ListRoomTurnsByStatus(ctx context.Context, roomID string, status TurnStatus, limit int) ([]Turn, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListRoomTurnsByStatus(ctx, db.ListRoomTurnsByStatusParams{RoomID: rid, Status: string(status), Limit: clampLimit(limit)})
	if err != nil {
		return nil, fmt.Errorf("list %s turns of room %s: %w", status, roomID, err)
	}
	out := make([]Turn, 0, len(rows))
	for _, row := range rows {
		out = append(out, toTurn(row))
	}
	return out, nil
}

// FailRunningTurns marks every running turn failed with reason and cancels
// the approvals still waiting on them. The hub calls it when it starts:
// a turn left running by its last stop will never finish on its own.
func (s *Store) FailRunningTurns(ctx context.Context, reason string) ([]Turn, error) {
	rows, err := s.q.FailRunningTurns(ctx, reason)
	if err != nil {
		return nil, fmt.Errorf("fail running turns: %w", err)
	}
	out := make([]Turn, 0, len(rows))
	for _, row := range rows {
		turn := toTurn(row)
		if _, err := s.ResolveTurnApprovals(ctx, turn.ID, ApprovalCancelled, reason); err != nil {
			return nil, err
		}
		out = append(out, turn)
	}
	return out, nil
}

// RunningTopic is a topic with a turn in flight: what the sidebar lists
// under a project while its members work there.
type RunningTopic struct {
	ThreadID      string `json:"thread_id"`
	RoomID        string `json:"room_id"`
	RootMessageID string `json:"root_message_id"`
	RootBody      string `json:"root_body"`
	// Members names the members working in it.
	Members   []string  `json:"members"`
	StartedAt time.Time `json:"started_at"`
}

// ListRunningTopics returns the topics with a running turn, across every
// room, oldest first.
func (s *Store) ListRunningTopics(ctx context.Context) ([]RunningTopic, error) {
	rows, err := s.q.ListRunningTopics(ctx)
	if err != nil {
		return nil, fmt.Errorf("list running topics: %w", err)
	}
	out := make([]RunningTopic, 0, len(rows))
	for _, row := range rows {
		members := row.Members
		if members == nil {
			members = []string{}
		}
		out = append(out, RunningTopic{
			ThreadID:      uuidString(row.ThreadID),
			RoomID:        uuidString(row.RoomID),
			RootMessageID: uuidString(row.RootMessageID),
			RootBody:      row.RootBody,
			Members:       members,
			StartedAt:     row.StartedAt.Time,
		})
	}
	return out, nil
}

// ListThreadTurns returns every turn of a topic, oldest first.
func (s *Store) ListThreadTurns(ctx context.Context, threadID string) ([]Turn, error) {
	tid, err := parseUUID(threadID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListThreadTurns(ctx, tid)
	if err != nil {
		return nil, fmt.Errorf("list turns of thread %s: %w", threadID, err)
	}
	out := make([]Turn, 0, len(rows))
	for _, row := range rows {
		out = append(out, toTurn(row))
	}
	return out, nil
}

func toTurn(row db.Turn) Turn {
	t := Turn{
		ID:               uuidString(row.ID),
		MemberID:         uuidString(row.MemberID),
		RoomID:           uuidString(row.RoomID),
		ThreadID:         uuidString(row.ThreadID),
		TriggerMessageID: uuidString(row.TriggerMessageID),
		MachineID:        uuidString(row.MachineID),
		SessionID:        uuidString(row.SessionID),
		Runtime:          row.Runtime,
		Status:           TurnStatus(row.Status),
		Error:            row.Error,
		ReplyMessageID:   uuidString(row.ReplyMessageID),
		TranscriptPath:   row.TranscriptPath,
		Usage:            usageOf(row.InputTokens, row.CacheReadTokens, row.CacheWriteTokens, row.OutputTokens),
		FilesChanged:     row.FilesChanged,
		StartedAt:        row.StartedAt.Time,
	}
	if row.EndedAt.Valid {
		ended := row.EndedAt.Time
		t.EndedAt = &ended
	}
	return t
}

// nonNil is s, or an empty slice for the NOT NULL array column.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// usageOf is a turn's token columns as one Usage.
func usageOf(input, cacheRead, cacheWrite, output int64) runtime.Usage {
	return runtime.Usage{InputTokens: input, CacheReadTokens: cacheRead, CacheWriteTokens: cacheWrite, OutputTokens: output}
}
