package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store/db"
)

// PermissionPreset says what an agent may do without asking. Each preset
// maps onto runtime-specific flags when a turn starts.
type PermissionPreset string

const (
	PermissionReadOnly         PermissionPreset = runtime.PermissionReadOnly
	PermissionEditWithApproval PermissionPreset = runtime.PermissionEditWithApproval
	PermissionAutoReview       PermissionPreset = runtime.PermissionAutoReview
	PermissionFullAuto         PermissionPreset = runtime.PermissionFullAuto
)

// PermissionPresets lists every valid preset, for validation and UIs.
var PermissionPresets = []PermissionPreset{PermissionReadOnly, PermissionEditWithApproval, PermissionAutoReview, PermissionFullAuto}

// BranchMode says how a member works in the repository.
type BranchMode string

const (
	// BranchWorktree gives the member its own git worktree.
	BranchWorktree BranchMode = "worktree"
	// BranchShared makes the member work in the checkout directly.
	BranchShared BranchMode = "shared"
)

// Agent is an agent set up on one machine's runtime.
type Agent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Avatar names the picture uploaded for the agent, a file in the hub's
	// avatar directory; empty shows its runtime's mark.
	Avatar string `json:"avatar"`
	// MachineID is the machine it is set up on, where its members run;
	// MachineName is that machine's name, connected or not.
	MachineID   string `json:"machine_id"`
	MachineName string `json:"machine_name"`
	// Projects names the projects it is a current member of, sorted: while
	// there are any it cannot move to another machine or be deleted.
	Projects         []string         `json:"projects"`
	Runtime          string           `json:"runtime"`
	Model            string           `json:"model"`
	RoleCard         string           `json:"role_card"`
	PermissionPreset PermissionPreset `json:"permission_preset"`
	RuntimeOptions   map[string]any   `json:"runtime_options"`
	// Skills names the skills of the skill library installed for it: its
	// turns are given these and no others (docs/design.md 5.15).
	Skills    []string  `json:"skills"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// NewAgent is the input to CreateAgent and UpdateAgent.
type NewAgent struct {
	Name   string
	Avatar string
	// MachineID is the machine the agent is set up on.
	MachineID        string
	Runtime          string
	Model            string
	RoleCard         string
	PermissionPreset PermissionPreset
	RuntimeOptions   map[string]any
	// Skills installed for it, by name.
	Skills []string
}

// Member is an agent added to a room.
type Member struct {
	ID          string     `json:"id"`
	RoomID      string     `json:"room_id"`
	AgentID     string     `json:"agent_id"`
	MachineID   string     `json:"machine_id"`
	DisplayName string     `json:"display_name"`
	RepoPath    string     `json:"repo_path"`
	BranchMode  BranchMode `json:"branch_mode"`
	// Model and PermissionPreset override the agent's when non-empty.
	Model            string           `json:"model"`
	PermissionPreset PermissionPreset `json:"permission_preset"`
	Enabled          bool             `json:"enabled"`
	CreatedAt        time.Time        `json:"created_at"`
	// WorktreeDir is the member's git worktree once made (docs/design.md
	// 5.21), WorkDir where in it the member works and Branch the branch
	// checked out there; PreparedAt is when it was got ready for work, nil
	// until then. All empty for a member working in the checkout itself.
	WorktreeDir string     `json:"worktree_dir,omitempty"`
	WorkDir     string     `json:"work_dir,omitempty"`
	Branch      string     `json:"branch,omitempty"`
	PreparedAt  *time.Time `json:"prepared_at,omitempty"`
	// OverlapsNoted are the files of its work the chat was told another
	// member's work changed too.
	OverlapsNoted []string `json:"-"`
	// RemovedAt is when the member was taken out of its project. The row
	// stays so its history keeps a name; AgentID is empty once the
	// agent it came from is deleted.
	RemovedAt *time.Time `json:"removed_at,omitempty"`
}

// Removed reports whether the member has been taken out of its project.
// A removed member takes no turns, is not @-mentionable and is not edited.
func (m Member) Removed() bool { return m.RemovedAt != nil }

// NewMember is the input to CreateMember. An empty
// DisplayName defaults to the agent's name; an empty BranchMode to
// BranchWorktree. The machine is the agent's.
type NewMember struct {
	RoomID           string
	AgentID          string
	DisplayName      string
	RepoPath         string
	BranchMode       BranchMode
	Model            string
	PermissionPreset PermissionPreset
}

// CreateAgent sets up an agent on a machine. A duplicate name is
// ErrConflict; an unknown machine is ErrNotFound.
func (s *Store) CreateAgent(ctx context.Context, t NewAgent) (Agent, error) {
	machineID, err := parseUUID(t.MachineID)
	if err != nil {
		return Agent{}, err
	}
	options, err := marshalOptions(t.RuntimeOptions)
	if err != nil {
		return Agent{}, err
	}
	id, err := s.q.CreateAgent(ctx, db.CreateAgentParams{
		Name:             t.Name,
		Avatar:           t.Avatar,
		MachineID:        machineID,
		Runtime:          t.Runtime,
		Model:            t.Model,
		RoleCard:         t.RoleCard,
		PermissionPreset: string(t.PermissionPreset),
		RuntimeOptions:   options,
		Skills:           nonNil(t.Skills),
	})
	if err != nil {
		return Agent{}, mapPGError("create agent", err)
	}
	return s.GetAgent(ctx, uuidString(id))
}

// UpdateAgent replaces every editable field of an agent. Moving
// it to another machine while it is a member of some project is a
// *AgentInUseError, as its members run on the machine it has.
func (s *Store) UpdateAgent(ctx context.Context, id string, t NewAgent) (Agent, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Agent{}, err
	}
	machineID, err := parseUUID(t.MachineID)
	if err != nil {
		return Agent{}, err
	}
	options, err := marshalOptions(t.RuntimeOptions)
	if err != nil {
		return Agent{}, err
	}
	_, err = s.q.UpdateAgent(ctx, db.UpdateAgentParams{
		ID:               uid,
		Name:             t.Name,
		Avatar:           t.Avatar,
		MachineID:        machineID,
		Runtime:          t.Runtime,
		Model:            t.Model,
		RoleCard:         t.RoleCard,
		PermissionPreset: string(t.PermissionPreset),
		RuntimeOptions:   options,
		Skills:           nonNil(t.Skills),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Unknown, or still in a project and asked to move.
		current, gerr := s.GetAgent(ctx, id)
		if gerr != nil {
			return Agent{}, gerr
		}
		return Agent{}, &AgentInUseError{ID: id, Projects: current.Projects}
	}
	if err != nil {
		return Agent{}, mapPGError("update agent", err)
	}
	return s.GetAgent(ctx, id)
}

// GetAgent returns one agent, or ErrNotFound.
func (s *Store) GetAgent(ctx context.Context, id string) (Agent, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Agent{}, err
	}
	row, err := s.q.GetAgent(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Agent{}, fmt.Errorf("agent %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Agent{}, fmt.Errorf("get agent %s: %w", id, err)
	}
	return toAgent(row.Agent, row.MachineName, row.Projects)
}

// AgentInUseError is how DeleteAgent refuses an agent that is
// still a member of some project, and UpdateAgent a move of one to
// another machine; it matches ErrConflict.
type AgentInUseError struct {
	ID string
	// Projects names where it is still a member, sorted.
	Projects []string
}

func (e *AgentInUseError) Error() string {
	return fmt.Sprintf("agent %s: %v: still a member of %s", e.ID, ErrConflict, strings.Join(e.Projects, ", "))
}

func (e *AgentInUseError) Unwrap() error { return ErrConflict }

// DeleteAgent removes an agent. An unknown one is ErrNotFound.
// While it is still a member of a project the answer is a
// *AgentInUseError: current members read their runtime and role card
// from it on every turn. Members already taken out keep their history and
// lose only the link.
func (s *Store) DeleteAgent(ctx context.Context, id string) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	n, err := s.q.DeleteAgent(ctx, uid)
	if isConstraintViolation(err, "23514", "members_current_have_agent") {
		projects, lerr := s.q.ListAgentProjects(ctx, uid)
		if lerr != nil {
			return fmt.Errorf("delete agent %s: list its projects: %w", id, lerr)
		}
		return &AgentInUseError{ID: id, Projects: projects}
	}
	if err != nil {
		return fmt.Errorf("delete agent %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("agent %s: %w", id, ErrNotFound)
	}
	return nil
}

// AgentsWithAvatar counts the agents that show an avatar file.
func (s *Store) AgentsWithAvatar(ctx context.Context, avatar string) (int64, error) {
	n, err := s.q.CountAgentsWithAvatar(ctx, avatar)
	if err != nil {
		return 0, fmt.Errorf("count agents with avatar %s: %w", avatar, err)
	}
	return n, nil
}

// ListAgents returns every agent, oldest first.
func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	rows, err := s.q.ListAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	out := make([]Agent, 0, len(rows))
	for _, row := range rows {
		t, err := toAgent(row.Agent, row.MachineName, row.Projects)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// CreateMember adds an agent to a room, running on the machine the
// agent is set up on. Unknown room or agent is ErrNotFound; a
// display name already used in the room is ErrConflict.
func (s *Store) CreateMember(ctx context.Context, in NewMember) (Member, error) {
	return createMember(ctx, s.q, in)
}

// createMember is CreateMember on q, which may belong to a transaction.
func createMember(ctx context.Context, q *db.Queries, in NewMember) (Member, error) {
	roomID, err := parseUUID(in.RoomID)
	if err != nil {
		return Member{}, err
	}
	agentID, err := parseUUID(in.AgentID)
	if err != nil {
		return Member{}, err
	}
	if in.BranchMode == "" {
		in.BranchMode = BranchWorktree
	}

	row, err := q.CreateMember(ctx, db.CreateMemberParams{
		RoomID:           roomID,
		AgentID:          agentID,
		DisplayName:      in.DisplayName,
		RepoPath:         in.RepoPath,
		BranchMode:       string(in.BranchMode),
		Model:            in.Model,
		PermissionPreset: string(in.PermissionPreset),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, fmt.Errorf("agent %s: %w", in.AgentID, ErrNotFound)
	}
	if err != nil {
		return Member{}, mapPGError("create member", err)
	}
	return toMember(row), nil
}

// MemberPatch is the input to UpdateMember: nil means keep.
type MemberPatch struct {
	DisplayName      *string
	Model            *string
	PermissionPreset *PermissionPreset
	RepoPath         *string
	Enabled          *bool
}

// UpdateMember changes what a person may edit after adding an
// member. An unknown id, or a member taken out of its project, is
// ErrNotFound.
func (s *Store) UpdateMember(ctx context.Context, id string, patch MemberPatch) (Member, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Member{}, err
	}
	params := db.UpdateMemberParams{
		ID:          uid,
		DisplayName: optionalText(patch.DisplayName),
		Model:       optionalText(patch.Model),
		RepoPath:    optionalText(patch.RepoPath),
	}
	if patch.PermissionPreset != nil {
		params.PermissionPreset = pgtype.Text{String: string(*patch.PermissionPreset), Valid: true}
	}
	if patch.Enabled != nil {
		params.Enabled = pgtype.Bool{Bool: *patch.Enabled, Valid: true}
	}
	row, err := s.q.UpdateMember(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, fmt.Errorf("member %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Member{}, mapPGError("update member", err)
	}
	return toMember(row), nil
}

func optionalText(v *string) pgtype.Text {
	if v == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *v, Valid: true}
}

// optionalBool is a patch's boolean: nil leaves the column as it is.
func optionalBool(v *bool) pgtype.Bool {
	if v == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *v, Valid: true}
}

// RemoveMember takes a member out of its project and returns it with
// RemovedAt set. An unknown id, or one already taken out, is ErrNotFound;
// one with a turn still running is ErrConflict, since that turn would go
// on replying for a member who is gone.
func (s *Store) RemoveMember(ctx context.Context, id string) (Member, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Member{}, err
	}
	row, err := s.removeMember(ctx, uid)
	if err == nil {
		return toMember(row), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Member{}, fmt.Errorf("remove member %s: %w", id, err)
	}
	// Nothing changed: say which of the three reasons it was.
	current, err := s.GetMember(ctx, id)
	if err != nil {
		return Member{}, err
	}
	if current.Removed() {
		return Member{}, fmt.Errorf("member %s: %w: already removed", id, ErrNotFound)
	}
	return Member{}, fmt.Errorf("member %s: %w", id, stillRunning())
}

// removeMember marks the member removed and ends its open session with it:
// a member that is gone takes no more turns, so nothing will resume it.
// pgx.ErrNoRows means nothing changed.
func (s *Store) removeMember(ctx context.Context, id pgtype.UUID) (db.Member, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.Member{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	q := s.q.WithTx(tx)

	row, err := q.RemoveMember(ctx, id)
	if err != nil {
		return db.Member{}, err
	}
	if _, err := q.EndOpenSession(ctx, db.EndOpenSessionParams{MemberID: id, EndReason: string(SessionMemberRemoved)}); err != nil {
		return db.Member{}, err
	}
	if err := q.ClearMemberRoles(ctx, id); err != nil {
		return db.Member{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Member{}, err
	}
	return row, nil
}

// GetMember returns one member, or ErrNotFound.
func (s *Store) GetMember(ctx context.Context, id string) (Member, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Member{}, err
	}
	row, err := s.q.GetMember(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Member{}, fmt.Errorf("member %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Member{}, fmt.Errorf("get member %s: %w", id, err)
	}
	return toMember(row), nil
}

// ListRoomMembers returns a room's members in the order they were
// added, including those taken out of the project (Removed), whose history
// still needs their names. An unknown room yields an empty list.
func (s *Store) ListRoomMembers(ctx context.Context, roomID string) ([]Member, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListRoomMembers(ctx, rid)
	if err != nil {
		return nil, fmt.Errorf("list agents of room %s: %w", roomID, err)
	}
	out := make([]Member, 0, len(rows))
	for _, row := range rows {
		out = append(out, toMember(row))
	}
	return out, nil
}

// isConstraintViolation reports whether err is the Postgres error code
// raised by the named constraint.
func isConstraintViolation(err error, code, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code && pgErr.ConstraintName == constraint
}

// mapPGError turns Postgres constraint failures into the
// sentinel errors callers can act on.
func mapPGError(op string, err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return fmt.Errorf("%s: %w", op, err)
	}
	if p := constraintProblem(pgErr); p != nil {
		return fmt.Errorf("%s: %w", op, p)
	}
	return fmt.Errorf("%s: %w", op, err)
}

// constraintProblems are the constraints a person can run into, as the
// problems they are.
var constraintProblems = map[string]Problem{
	"agents_name_key":            {Kind: ErrConflict, Code: "agentNameTaken", Text: "another agent already has this name"},
	"members_name_per_room":      {Kind: ErrConflict, Code: "memberNameTaken", Text: "another member of this chat already has this name"},
	"attachments_filename_check": {Kind: ErrInvalidInput, Code: "attachmentUnnamed", Text: "an attachment needs a file name"},
}

// constraintProblem is why the database refused a write, as a Problem:
// one a person can run into by its code, the others, which only a bug
// reaches, by the kind of constraint and its name. Nil for an error that
// is no constraint's.
func constraintProblem(pgErr *pgconn.PgError) *Problem {
	if p, ok := constraintProblems[pgErr.ConstraintName]; ok {
		return &p
	}
	switch pgErr.Code {
	case "23505": // unique_violation
		return &Problem{Kind: ErrConflict, Text: "it clashes with one already there (" + pgErr.ConstraintName + ")"}
	case "23503": // foreign_key_violation
		return &Problem{Kind: ErrNotFound, Text: "something it refers to is gone (" + pgErr.ConstraintName + ")"}
	case "23514": // check_violation
		return &Problem{Kind: ErrInvalidInput, Text: "a value is not allowed (" + pgErr.ConstraintName + ")"}
	}
	return nil
}

// marshalOptions encodes runtime options for the jsonb column; nil becomes {}.
func marshalOptions(o map[string]any) ([]byte, error) {
	if o == nil {
		o = map[string]any{}
	}
	raw, err := json.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("encode runtime options: %w", err)
	}
	return raw, nil
}

func toAgent(row db.Agent, machineName string, projects []string) (Agent, error) {
	options := map[string]any{}
	if err := json.Unmarshal(row.RuntimeOptions, &options); err != nil {
		return Agent{}, fmt.Errorf("decode runtime options of agent %s: %w", uuidString(row.ID), err)
	}
	if projects == nil {
		projects = []string{}
	}
	return Agent{
		ID:               uuidString(row.ID),
		Name:             row.Name,
		Avatar:           row.Avatar,
		MachineID:        uuidString(row.MachineID),
		MachineName:      machineName,
		Projects:         projects,
		Runtime:          row.Runtime,
		Model:            row.Model,
		RoleCard:         row.RoleCard,
		PermissionPreset: PermissionPreset(row.PermissionPreset),
		RuntimeOptions:   options,
		Skills:           nonNil(row.Skills),
		CreatedAt:        row.CreatedAt.Time,
		UpdatedAt:        row.UpdatedAt.Time,
	}, nil
}

// AgentRef names an agent, for lists of them.
type AgentRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SetAgentSkill installs a skill for an agent, or takes it off.
func (s *Store) SetAgentSkill(ctx context.Context, agentID, skill string, installed bool) error {
	id, err := parseUUID(agentID)
	if err != nil {
		return err
	}
	n, err := s.q.SetAgentSkill(ctx, db.SetAgentSkillParams{ID: id, Skill: skill, Installed: installed})
	if err != nil {
		return fmt.Errorf("set skill %s of agent %s: %w", skill, agentID, err)
	}
	if n == 0 {
		return fmt.Errorf("agent %s: %w", agentID, ErrNotFound)
	}
	return nil
}

// ListSkillAgents names the agents a skill is installed for.
func (s *Store) ListSkillAgents(ctx context.Context, skill string) ([]AgentRef, error) {
	rows, err := s.q.ListSkillAgents(ctx, skill)
	if err != nil {
		return nil, fmt.Errorf("list the agents of skill %s: %w", skill, err)
	}
	out := make([]AgentRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, AgentRef{ID: uuidString(r.ID), Name: r.Name})
	}
	return out, nil
}

func toMember(row db.Member) Member {
	a := Member{
		ID:               uuidString(row.ID),
		RoomID:           uuidString(row.RoomID),
		AgentID:          uuidString(row.AgentID),
		MachineID:        uuidString(row.MachineID),
		DisplayName:      row.DisplayName,
		RepoPath:         row.RepoPath,
		BranchMode:       BranchMode(row.BranchMode),
		Model:            row.Model,
		PermissionPreset: PermissionPreset(row.PermissionPreset),
		Enabled:          row.Enabled,
		CreatedAt:        row.CreatedAt.Time,
		WorktreeDir:      row.WorktreeDir,
		WorkDir:          row.WorkDir,
		Branch:           row.Branch,
		OverlapsNoted:    row.OverlapsNoted,
	}
	if row.PreparedAt.Valid {
		prepared := row.PreparedAt.Time
		a.PreparedAt = &prepared
	}
	if row.RemovedAt.Valid {
		removed := row.RemovedAt.Time
		a.RemovedAt = &removed
	}
	return a
}
