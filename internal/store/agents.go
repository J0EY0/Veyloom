package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/store/db"
)

// PermissionPreset says what an agent may do without asking. Each preset
// maps onto engine-specific flags when a turn starts.
type PermissionPreset string

const (
	PermissionReadOnly         PermissionPreset = engine.PermissionReadOnly
	PermissionEditWithApproval PermissionPreset = engine.PermissionEditWithApproval
	PermissionFullAuto         PermissionPreset = engine.PermissionFullAuto
)

// PermissionPresets lists every valid preset, for validation and UIs.
var PermissionPresets = []PermissionPreset{PermissionReadOnly, PermissionEditWithApproval, PermissionFullAuto}

// BranchMode says how an agent instance works in the repository.
type BranchMode string

const (
	// BranchWorktree gives the instance its own git worktree.
	BranchWorktree BranchMode = "worktree"
	// BranchShared makes the instance work in the checkout directly.
	BranchShared BranchMode = "shared"
)

// AgentTemplate is a reusable role definition.
type AgentTemplate struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Engine           string           `json:"engine"`
	Model            string           `json:"model"`
	RoleCard         string           `json:"role_card"`
	PermissionPreset PermissionPreset `json:"permission_preset"`
	EngineOptions    map[string]any   `json:"engine_options"`
	Builtin          bool             `json:"builtin"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

// NewAgentTemplate is the input to CreateAgentTemplate and
// UpdateAgentTemplate.
type NewAgentTemplate struct {
	Name             string
	Engine           string
	Model            string
	RoleCard         string
	PermissionPreset PermissionPreset
	EngineOptions    map[string]any
}

// AgentInstance is a template placed in a room.
type AgentInstance struct {
	ID          string     `json:"id"`
	RoomID      string     `json:"room_id"`
	TemplateID  string     `json:"template_id"`
	WorkerID    string     `json:"worker_id"`
	DisplayName string     `json:"display_name"`
	RepoPath    string     `json:"repo_path"`
	BranchMode  BranchMode `json:"branch_mode"`
	// Model and PermissionPreset override the template's when non-empty.
	Model            string           `json:"model"`
	PermissionPreset PermissionPreset `json:"permission_preset"`
	EngineSessionRef string           `json:"engine_session_ref"`
	Enabled          bool             `json:"enabled"`
	CreatedAt        time.Time        `json:"created_at"`
}

// NewAgentInstance is the input to CreateAgentInstance. An empty
// DisplayName defaults to the template's name; an empty BranchMode to
// BranchWorktree.
type NewAgentInstance struct {
	RoomID           string
	TemplateID       string
	WorkerID         string
	DisplayName      string
	RepoPath         string
	BranchMode       BranchMode
	Model            string
	PermissionPreset PermissionPreset
}

// CreateAgentTemplate adds a user-defined template. A duplicate name is
// ErrConflict.
func (s *Store) CreateAgentTemplate(ctx context.Context, t NewAgentTemplate) (AgentTemplate, error) {
	options, err := marshalOptions(t.EngineOptions)
	if err != nil {
		return AgentTemplate{}, err
	}
	row, err := s.q.CreateAgentTemplate(ctx, db.CreateAgentTemplateParams{
		Name:             t.Name,
		Engine:           t.Engine,
		Model:            t.Model,
		RoleCard:         t.RoleCard,
		PermissionPreset: string(t.PermissionPreset),
		EngineOptions:    options,
	})
	if err != nil {
		return AgentTemplate{}, mapAgentError("create agent template", err)
	}
	return toAgentTemplate(row)
}

// UpdateAgentTemplate replaces every editable field of a template.
func (s *Store) UpdateAgentTemplate(ctx context.Context, id string, t NewAgentTemplate) (AgentTemplate, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return AgentTemplate{}, err
	}
	options, err := marshalOptions(t.EngineOptions)
	if err != nil {
		return AgentTemplate{}, err
	}
	row, err := s.q.UpdateAgentTemplate(ctx, db.UpdateAgentTemplateParams{
		ID:               uid,
		Name:             t.Name,
		Engine:           t.Engine,
		Model:            t.Model,
		RoleCard:         t.RoleCard,
		PermissionPreset: string(t.PermissionPreset),
		EngineOptions:    options,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentTemplate{}, fmt.Errorf("agent template %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return AgentTemplate{}, mapAgentError("update agent template", err)
	}
	return toAgentTemplate(row)
}

// GetAgentTemplate returns one template, or ErrNotFound.
func (s *Store) GetAgentTemplate(ctx context.Context, id string) (AgentTemplate, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return AgentTemplate{}, err
	}
	row, err := s.q.GetAgentTemplate(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentTemplate{}, fmt.Errorf("agent template %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return AgentTemplate{}, fmt.Errorf("get agent template %s: %w", id, err)
	}
	return toAgentTemplate(row)
}

// ListAgentTemplates returns every template, builtins first, then by name.
func (s *Store) ListAgentTemplates(ctx context.Context) ([]AgentTemplate, error) {
	rows, err := s.q.ListAgentTemplates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list agent templates: %w", err)
	}
	out := make([]AgentTemplate, 0, len(rows))
	for _, row := range rows {
		t, err := toAgentTemplate(row)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// CreateAgentInstance places a template in a room. Unknown room, template
// or worker is ErrNotFound; a display name already used in the room is
// ErrConflict.
func (s *Store) CreateAgentInstance(ctx context.Context, in NewAgentInstance) (AgentInstance, error) {
	roomID, err := parseUUID(in.RoomID)
	if err != nil {
		return AgentInstance{}, err
	}
	templateID, err := parseUUID(in.TemplateID)
	if err != nil {
		return AgentInstance{}, err
	}
	workerID, err := parseUUID(in.WorkerID)
	if err != nil {
		return AgentInstance{}, err
	}

	if in.DisplayName == "" {
		template, err := s.GetAgentTemplate(ctx, in.TemplateID)
		if err != nil {
			return AgentInstance{}, err
		}
		in.DisplayName = template.Name
	}
	if in.BranchMode == "" {
		in.BranchMode = BranchWorktree
	}

	row, err := s.q.CreateAgentInstance(ctx, db.CreateAgentInstanceParams{
		RoomID:           roomID,
		TemplateID:       templateID,
		WorkerID:         workerID,
		DisplayName:      in.DisplayName,
		RepoPath:         in.RepoPath,
		BranchMode:       string(in.BranchMode),
		Model:            in.Model,
		PermissionPreset: string(in.PermissionPreset),
	})
	if err != nil {
		return AgentInstance{}, mapAgentError("create agent instance", err)
	}
	return toAgentInstance(row), nil
}

// GetAgentInstance returns one instance, or ErrNotFound.
func (s *Store) GetAgentInstance(ctx context.Context, id string) (AgentInstance, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return AgentInstance{}, err
	}
	row, err := s.q.GetAgentInstance(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentInstance{}, fmt.Errorf("agent instance %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return AgentInstance{}, fmt.Errorf("get agent instance %s: %w", id, err)
	}
	return toAgentInstance(row), nil
}

// ListRoomAgentInstances returns a room's agents in the order they were
// added. An unknown room yields an empty list.
func (s *Store) ListRoomAgentInstances(ctx context.Context, roomID string) ([]AgentInstance, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListRoomAgentInstances(ctx, rid)
	if err != nil {
		return nil, fmt.Errorf("list agents of room %s: %w", roomID, err)
	}
	out := make([]AgentInstance, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAgentInstance(row))
	}
	return out, nil
}

// mapAgentError turns constraint failures on the agent tables into the
// sentinel errors callers can act on.
func mapAgentError(op string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("%s: %w", op, err)
	}
	switch pgErr.Code {
	case "23505": // unique_violation
		return fmt.Errorf("%s: %w: %s", op, ErrConflict, pgErr.ConstraintName)
	case "23503": // foreign_key_violation
		return fmt.Errorf("%s: %w (%s)", op, ErrNotFound, pgErr.ConstraintName)
	case "23514": // check_violation
		return fmt.Errorf("%s: %w: %s", op, ErrInvalidInput, pgErr.ConstraintName)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// marshalOptions encodes engine options for the jsonb column; nil becomes {}.
func marshalOptions(o map[string]any) ([]byte, error) {
	if o == nil {
		o = map[string]any{}
	}
	raw, err := json.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("encode engine options: %w", err)
	}
	return raw, nil
}

func toAgentTemplate(row db.AgentTemplate) (AgentTemplate, error) {
	options := map[string]any{}
	if err := json.Unmarshal(row.EngineOptions, &options); err != nil {
		return AgentTemplate{}, fmt.Errorf("decode engine options of template %s: %w", uuidString(row.ID), err)
	}
	return AgentTemplate{
		ID:               uuidString(row.ID),
		Name:             row.Name,
		Engine:           row.Engine,
		Model:            row.Model,
		RoleCard:         row.RoleCard,
		PermissionPreset: PermissionPreset(row.PermissionPreset),
		EngineOptions:    options,
		Builtin:          row.Builtin,
		CreatedAt:        row.CreatedAt.Time,
		UpdatedAt:        row.UpdatedAt.Time,
	}, nil
}

func toAgentInstance(row db.AgentInstance) AgentInstance {
	return AgentInstance{
		ID:               uuidString(row.ID),
		RoomID:           uuidString(row.RoomID),
		TemplateID:       uuidString(row.TemplateID),
		WorkerID:         uuidString(row.WorkerID),
		DisplayName:      row.DisplayName,
		RepoPath:         row.RepoPath,
		BranchMode:       BranchMode(row.BranchMode),
		Model:            row.Model,
		PermissionPreset: PermissionPreset(row.PermissionPreset),
		EngineSessionRef: row.EngineSessionRef,
		Enabled:          row.Enabled,
		CreatedAt:        row.CreatedAt.Time,
	}
}
