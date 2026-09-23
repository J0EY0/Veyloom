package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

// ApprovalKind says what a person is asked for.
type ApprovalKind string

const (
	// ApprovalToolUse asks permission to call a tool with a given input.
	ApprovalToolUse ApprovalKind = "tool_use"
	// ApprovalQuestion asks one or more questions; the answer holds what
	// the person replied to each.
	ApprovalQuestion ApprovalKind = "question"
	// ApprovalForm asks for the fields of a form; the answer holds them.
	ApprovalForm ApprovalKind = "form"
	// ApprovalLink asks the person to open a link and say when done.
	ApprovalLink ApprovalKind = "link"
)

// Valid reports whether k is a kind the approvals table holds.
func (k ApprovalKind) Valid() bool {
	switch k {
	case ApprovalToolUse, ApprovalQuestion, ApprovalForm, ApprovalLink:
		return true
	}
	return false
}

// Approval is one request raised during a turn for a person to answer, or
// one a runtime settled on its own and says so (Reviewer).
type Approval struct {
	ID        string       `json:"id"`
	TurnID    string       `json:"turn_id"`
	RoomID    string       `json:"room_id"`
	ThreadID  string       `json:"thread_id"`
	MemberID  string       `json:"member_id"`
	RequestID string       `json:"request_id"`
	Kind      ApprovalKind `json:"kind"`
	// Tool and Input are the payload. For tool_use, the tool and its
	// complete input as JSON; for the other kinds, what is asked.
	Tool   string          `json:"tool"`
	Input  json.RawMessage `json:"input"`
	Status ApprovalStatus  `json:"status"`
	// Message is the decider's note; on a denial the agent sees it.
	Message string `json:"message,omitempty"`
	// MessageID is the thread post that presents the request.
	MessageID string `json:"message_id,omitempty"`
	// DecidedBy is the user who decided, empty for a timeout, a turn that
	// ended first, or a reviewer of the runtime's own.
	DecidedBy string `json:"decided_by,omitempty"`
	// Reviewer names the runtime's own reviewer when it decided rather than
	// a person, such as codex_auto_review.
	Reviewer string `json:"reviewer,omitempty"`
	// Answer is what came with the decision: a question's answers, a form's
	// content, or a reviewer's findings.
	Answer    json.RawMessage `json:"answer,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	DecidedAt *time.Time      `json:"decided_at,omitempty"`
}

// NewApproval is the input to CreateApproval.
type NewApproval struct {
	// ID is optional. The hub sets it so the approval can be announced and
	// tracked before the row exists; empty lets the database choose.
	ID        string
	TurnID    string
	RoomID    string
	ThreadID  string
	MemberID  string
	RequestID string
	// Kind defaults to tool_use.
	Kind ApprovalKind
	Tool string
	// Input is the tool input, normally JSON. Anything that is not valid
	// JSON is stored as a JSON string so the payload stays well-formed.
	Input     string
	MessageID string
}

// NewReviewedApproval is the input to CreateReviewedApproval: a request the
// runtime settled on its own, with how and by whom.
type NewReviewedApproval struct {
	NewApproval
	// Status must be one of the decided statuses.
	Status ApprovalStatus
	// Message is the reviewer's reasoning.
	Message string
	// Reviewer names who decided, such as codex_auto_review.
	Reviewer string
	// Answer holds the reviewer's findings as JSON; empty for none.
	Answer json.RawMessage
}

// ApprovalOutcome is the input to DecideApproval.
type ApprovalOutcome struct {
	// Status must be one of the decided statuses, never pending.
	Status  ApprovalStatus
	Message string
	// DecidedBy is the deciding user, or empty when the system decided.
	DecidedBy string
	// Answer is what came with the decision, as JSON; empty for none.
	Answer json.RawMessage
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
	r, err := a.parse()
	if err != nil {
		return Approval{}, err
	}
	row, err := s.q.CreateApproval(ctx, db.CreateApprovalParams{
		ID:        id,
		TurnID:    r.turnID,
		RoomID:    r.roomID,
		ThreadID:  r.threadID,
		MemberID:  r.memberID,
		RequestID: a.RequestID,
		Kind:      string(r.kind),
		Payload:   r.payload,
		MessageID: r.messageID,
	})
	if err != nil {
		return Approval{}, mapPGError("create approval", err)
	}
	return toApproval(row)
}

// CreateReviewedApproval records a request the runtime settled on its own,
// already decided. Nothing waits for it, so it never shows as pending.
func (s *Store) CreateReviewedApproval(ctx context.Context, a NewReviewedApproval) (Approval, error) {
	if a.Status == ApprovalPending || a.Status == "" {
		return Approval{}, fmt.Errorf("%w: a reviewed approval is already decided", ErrInvalidInput)
	}
	if a.Reviewer == "" {
		return Approval{}, fmt.Errorf("%w: a reviewed approval names its reviewer", ErrInvalidInput)
	}
	r, err := a.parse()
	if err != nil {
		return Approval{}, err
	}
	row, err := s.q.CreateReviewedApproval(ctx, db.CreateReviewedApprovalParams{
		TurnID:    r.turnID,
		RoomID:    r.roomID,
		ThreadID:  r.threadID,
		MemberID:  r.memberID,
		RequestID: a.RequestID,
		Kind:      string(r.kind),
		Payload:   r.payload,
		MessageID: r.messageID,
		Status:    string(a.Status),
		Message:   a.Message,
		Reviewer:  a.Reviewer,
		Answer:    answerColumn(a.Answer),
	})
	if err != nil {
		return Approval{}, mapPGError("create reviewed approval", err)
	}
	return toApproval(row)
}

// parsedApproval is a NewApproval with its ids parsed and payload encoded.
type parsedApproval struct {
	turnID, roomID, threadID, memberID, messageID pgtype.UUID
	kind                                          ApprovalKind
	payload                                       []byte
}

func (a NewApproval) parse() (parsedApproval, error) {
	var r parsedApproval
	var err error
	if r.turnID, err = parseUUID(a.TurnID); err != nil {
		return r, err
	}
	if r.roomID, err = parseUUID(a.RoomID); err != nil {
		return r, err
	}
	if r.threadID, err = parseUUID(a.ThreadID); err != nil {
		return r, err
	}
	if r.memberID, err = parseUUID(a.MemberID); err != nil {
		return r, err
	}
	if a.MessageID != "" {
		if r.messageID, err = parseUUID(a.MessageID); err != nil {
			return r, err
		}
	}
	if a.RequestID == "" {
		return r, fmt.Errorf("%w: approval request id is required", ErrInvalidInput)
	}
	r.kind = a.Kind
	if r.kind == "" {
		r.kind = ApprovalToolUse
	}
	if !r.kind.Valid() {
		return r, fmt.Errorf("%w: unknown approval kind %q", ErrInvalidInput, a.Kind)
	}
	if r.payload, err = json.Marshal(approvalPayload{Tool: a.Tool, Input: jsonOrString(a.Input)}); err != nil {
		return r, fmt.Errorf("encode approval payload: %w", err)
	}
	return r, nil
}

// answerColumn is the answer column's value: NULL for no answer, otherwise
// the JSON as given, wrapped as a string if it is not JSON.
func answerColumn(answer json.RawMessage) []byte {
	if len(answer) == 0 {
		return nil
	}
	return jsonOrString(string(answer))
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
		Answer:    answerColumn(out.Answer),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Either it does not exist or it is no longer pending; tell which.
		current, getErr := s.GetApproval(ctx, id)
		if getErr != nil {
			return Approval{}, getErr
		}
		return Approval{}, fmt.Errorf("approval %s: %w", id, approvalSettled(current.Status))
	}
	if err != nil {
		return Approval{}, mapPGError("decide approval", err)
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
		return nil, mapPGError("resolve approvals of turn "+turnID, err)
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

// PendingApproval is a request waiting for a person, with the names the
// "for me" page shows next to it.
type PendingApproval struct {
	Approval
	MemberName  string `json:"member_name"`
	ProjectName string `json:"project_name"`
}

// ListPendingApprovals returns every pending approval across every room,
// oldest first.
func (s *Store) ListPendingApprovals(ctx context.Context) ([]PendingApproval, error) {
	rows, err := s.q.ListPendingApprovals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending approvals: %w", err)
	}
	out := make([]PendingApproval, 0, len(rows))
	for _, row := range rows {
		a, err := toApproval(row.Approval)
		if err != nil {
			return nil, err
		}
		out = append(out, PendingApproval{Approval: a, MemberName: row.MemberName, ProjectName: row.ProjectName})
	}
	return out, nil
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
	// Hand out a compact form so callers see the same bytes no matter how
	// the value was spaced when it arrived; the keys keep their order.
	var compact bytes.Buffer
	if json.Compact(&compact, payload.Input) == nil {
		payload.Input = compact.Bytes()
	}
	a := Approval{
		ID:        uuidString(row.ID),
		TurnID:    uuidString(row.TurnID),
		RoomID:    uuidString(row.RoomID),
		ThreadID:  uuidString(row.ThreadID),
		MemberID:  uuidString(row.MemberID),
		RequestID: row.RequestID,
		Kind:      ApprovalKind(row.Kind),
		Tool:      payload.Tool,
		Input:     payload.Input,
		Status:    ApprovalStatus(row.Status),
		Message:   row.Message,
		MessageID: uuidString(row.MessageID),
		DecidedBy: uuidString(row.DecidedBy),
		Reviewer:  row.Reviewer,
		CreatedAt: row.CreatedAt.Time,
	}
	if len(row.Answer) > 0 {
		var compact bytes.Buffer
		if json.Compact(&compact, row.Answer) == nil {
			a.Answer = compact.Bytes()
		} else {
			a.Answer = row.Answer
		}
	}
	if row.DecidedAt.Valid {
		decided := row.DecidedAt.Time
		a.DecidedAt = &decided
	}
	return a, nil
}

// approvalSettled is what a decision on an approval that is not pending
// any more is told, by how it was settled: allowed, denied, expired or
// cancelled (approvalAllowed and so on).
func approvalSettled(status ApprovalStatus) error {
	code := "approvalSettled"
	if s := string(status); s != "" {
		code = "approval" + strings.ToUpper(s[:1]) + s[1:]
	}
	return Conflicting(code, Params{"status": string(status)}, "already %s", status)
}
