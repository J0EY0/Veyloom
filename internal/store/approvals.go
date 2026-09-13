package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// ApprovalStatus is the lifecycle state of an approval.
type ApprovalStatus string

const (
	// ApprovalPending means nobody has decided yet.
	ApprovalPending ApprovalStatus = "pending"
	// ApprovalAllowed and ApprovalDenied are a person's decisions.
	ApprovalAllowed ApprovalStatus = "allowed"
	ApprovalDenied  ApprovalStatus = "denied"
	// ApprovalExpired means the hub gave up waiting for a decision.
	ApprovalExpired ApprovalStatus = "expired"
	// ApprovalCancelled means the turn ended before a decision.
	ApprovalCancelled ApprovalStatus = "cancelled"
)

// ApprovalKind says what kind of permission is being asked for.
type ApprovalKind string

// ApprovalToolUse asks permission to call a tool with a given input.
const ApprovalToolUse ApprovalKind = "tool_use"

// Approval is one permission request raised during a turn.
type Approval struct {
	ID              string       `json:"id"`
	TurnID          string       `json:"turn_id"`
	RoomID          string       `json:"room_id"`
	ThreadID        string       `json:"thread_id"`
	AgentInstanceID string       `json:"agent_instance_id"`
	RequestID       string       `json:"request_id"`
	Kind            ApprovalKind `json:"kind"`
	// Tool and Input are the payload of a tool_use approval. Input is the
	// tool's complete input as JSON.
	Tool   string          `json:"tool"`
	Input  json.RawMessage `json:"input"`
	Status ApprovalStatus  `json:"status"`
	// Message is the decider's note; on a denial the agent sees it.
	Message string `json:"message,omitempty"`
	// MessageID is the thread post that presents the request.
	MessageID string `json:"message_id,omitempty"`
	// DecidedBy is the user who decided, empty for a timeout or a turn that
	// ended first.
	DecidedBy string     `json:"decided_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	DecidedAt *time.Time `json:"decided_at,omitempty"`
}

// NewApproval is the input to CreateApproval.
type NewApproval struct {
	// ID is optional. The hub sets it so the approval can be announced and
	// tracked before the row exists; empty lets the database choose.
	ID              string
	TurnID          string
	RoomID          string
	ThreadID        string
	AgentInstanceID string
	RequestID       string
	Tool            string
	// Input is the tool input, normally JSON. Anything that is not valid
	// JSON is stored as a JSON string so the payload stays well-formed.
	Input     string
	MessageID string
}

// ApprovalOutcome is the input to DecideApproval.
type ApprovalOutcome struct {
	// Status must be one of the decided statuses, never pending.
	Status  ApprovalStatus
	Message string
	// DecidedBy is the deciding user, or empty when the system decided.
	DecidedBy string
}

// approvalPayload is the JSON shape of the payload column.
type approvalPayload struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

// CreateApproval records a pending request. A second request with the same
// id in one turn is ErrConflict.
func (s *Store) CreateApproval(ctx context.Context, a NewApproval) (Approval, error) {
	var id pgtype.UUID
	if a.ID != "" {
		var err error
		if id, err = parseUUID(a.ID); err != nil {
			return Approval{}, err
		}
	}
	turnID, err := parseUUID(a.TurnID)
	if err != nil {
		return Approval{}, err
	}
	roomID, err := parseUUID(a.RoomID)
	if err != nil {
		return Approval{}, err
	}
	threadID, err := parseUUID(a.ThreadID)
	if err != nil {
		return Approval{}, err
	}
	instanceID, err := parseUUID(a.AgentInstanceID)
	if err != nil {
		return Approval{}, err
	}
	var messageID pgtype.UUID
	if a.MessageID != "" {
		if messageID, err = parseUUID(a.MessageID); err != nil {
			return Approval{}, err
		}
	}
	if a.RequestID == "" {
		return Approval{}, fmt.Errorf("%w: approval request id is required", ErrInvalidInput)
	}
	payload, err := json.Marshal(approvalPayload{Tool: a.Tool, Input: jsonOrString(a.Input)})
	if err != nil {
		return Approval{}, fmt.Errorf("encode approval payload: %w", err)
	}

	row, err := s.q.CreateApproval(ctx, db.CreateApprovalParams{
		ID:              id,
		TurnID:          turnID,
		RoomID:          roomID,
		ThreadID:        threadID,
		AgentInstanceID: instanceID,
		RequestID:       a.RequestID,
		Kind:            string(ApprovalToolUse),
		Payload:         payload,
		MessageID:       messageID,
	})
	if err != nil {
		return Approval{}, mapAgentError("create approval", err)
	}
	return toApproval(row)
}

// GetApproval returns one approval, or ErrNotFound.
func (s *Store) GetApproval(ctx context.Context, id string) (Approval, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Approval{}, err
	}
	row, err := s.q.GetApproval(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Approval{}, fmt.Errorf("approval %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Approval{}, fmt.Errorf("get approval %s: %w", id, err)
	}
	return toApproval(row)
}

// DecideApproval settles a pending approval. An approval that was already
// decided is ErrConflict; an unknown one is ErrNotFound.
func (s *Store) DecideApproval(ctx context.Context, id string, out ApprovalOutcome) (Approval, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Approval{}, err
	}
	if out.Status == ApprovalPending || out.Status == "" {
		return Approval{}, fmt.Errorf("%w: a decision cannot leave an approval pending", ErrInvalidInput)
	}
	var decidedBy pgtype.UUID
	if out.DecidedBy != "" {
		if decidedBy, err = parseUUID(out.DecidedBy); err != nil {
			return Approval{}, err
		}
	}

	row, err := s.q.DecideApproval(ctx, db.DecideApprovalParams{
		ID:        uid,
		Status:    string(out.Status),
		Message:   out.Message,
		DecidedBy: decidedBy,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Either it does not exist or it is no longer pending; tell which.
		current, getErr := s.GetApproval(ctx, id)
		if getErr != nil {
			return Approval{}, getErr
		}
		return Approval{}, fmt.Errorf("approval %s: %w: already %s", id, ErrConflict, current.Status)
	}
	if err != nil {
		return Approval{}, mapAgentError("decide approval", err)
	}
	return toApproval(row)
}

// ResolveTurnApprovals closes every pending approval of a turn with the
// given status and returns the ones it closed.
func (s *Store) ResolveTurnApprovals(ctx context.Context, turnID string, status ApprovalStatus, message string) ([]Approval, error) {
	tid, err := parseUUID(turnID)
	if err != nil {
		return nil, err
	}
	if status == ApprovalPending || status == "" {
		return nil, fmt.Errorf("%w: resolving cannot leave approvals pending", ErrInvalidInput)
	}
	rows, err := s.q.ResolveTurnApprovals(ctx, db.ResolveTurnApprovalsParams{TurnID: tid, Status: string(status), Message: message})
	if err != nil {
		return nil, mapAgentError("resolve approvals of turn "+turnID, err)
	}
	return toApprovals(rows)
}

// ListPendingRoomApprovals returns what is waiting for a decision in a
// room, oldest first.
func (s *Store) ListPendingRoomApprovals(ctx context.Context, roomID string) ([]Approval, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListPendingRoomApprovals(ctx, rid)
	if err != nil {
		return nil, fmt.Errorf("list pending approvals of room %s: %w", roomID, err)
	}
	return toApprovals(rows)
}

// ListTurnApprovals returns every approval of a turn, oldest first.
func (s *Store) ListTurnApprovals(ctx context.Context, turnID string) ([]Approval, error) {
	tid, err := parseUUID(turnID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListTurnApprovals(ctx, tid)
	if err != nil {
		return nil, fmt.Errorf("list approvals of turn %s: %w", turnID, err)
	}
	return toApprovals(rows)
}

// jsonOrString returns s as raw JSON when it is valid JSON, otherwise as a
// JSON string literal.
func jsonOrString(s string) json.RawMessage {
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	quoted, _ := json.Marshal(s)
	return quoted
}

func toApprovals(rows []db.Approval) ([]Approval, error) {
	out := make([]Approval, 0, len(rows))
	for _, row := range rows {
		a, err := toApproval(row)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func toApproval(row db.Approval) (Approval, error) {
	var payload approvalPayload
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return Approval{}, fmt.Errorf("decode payload of approval %s: %w", uuidString(row.ID), err)
	}
	// jsonb normalises whitespace on the way in; hand out a compact form so
	// callers see the same bytes no matter which way the value arrived.
	var compact bytes.Buffer
	if json.Compact(&compact, payload.Input) == nil {
		payload.Input = compact.Bytes()
	}
	a := Approval{
		ID:              uuidString(row.ID),
		TurnID:          uuidString(row.TurnID),
		RoomID:          uuidString(row.RoomID),
		ThreadID:        uuidString(row.ThreadID),
		AgentInstanceID: uuidString(row.AgentInstanceID),
		RequestID:       row.RequestID,
		Kind:            ApprovalKind(row.Kind),
		Tool:            payload.Tool,
		Input:           payload.Input,
		Status:          ApprovalStatus(row.Status),
		Message:         row.Message,
		MessageID:       uuidString(row.MessageID),
		DecidedBy:       uuidString(row.DecidedBy),
		CreatedAt:       row.CreatedAt.Time,
	}
	if row.DecidedAt.Valid {
		decided := row.DecidedAt.Time
		a.DecidedAt = &decided
	}
	return a, nil
}
