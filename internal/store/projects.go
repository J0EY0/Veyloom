package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// Project is a codebase the team and its agents work on.
type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// RepoPath is where the code is checked out on the machines its members
	// run on; the members a project starts with work there.
	RepoPath string `json:"repo_path"`
	// Description says what the project is, in a paragraph: its purpose,
	// goals and stack. Every agent's brief opens with it.
	Description string `json:"description"`
	// MainRoomID is the project's group chat: to the UI a project is one
	// chat (docs/webui.md §3), and this is where it lives.
	MainRoomID string    `json:"main_room_id"`
	CreatedAt  time.Time `json:"created_at"`
}

// NewProject is the input to CreateProject. Each of AgentIDs joins the
// main room as a member working in RepoPath.
type NewProject struct {
	Name        string
	RepoPath    string
	Description string
	AgentIDs    []string
}

// RoomKind distinguishes a project's single main room from topic rooms.
type RoomKind string

const (
	RoomMain  RoomKind = "main"
	RoomTopic RoomKind = "topic"
)

// Room is a group chat attached to a project.
type Room struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Kind      RoomKind  `json:"kind"`
	CreatedAt time.Time `json:"created_at"`
}

// MainRoomName is the name given to every project's main room.
const MainRoomName = "main"

// CreateProject creates a project with its main room and first members,
// atomically: an unknown agent (ErrNotFound) leaves nothing behind. An
// agent named twice joins once.
func (s *Store) CreateProject(ctx context.Context, p NewProject) (Project, Room, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Project{}, Room{}, fmt.Errorf("begin: %w", err)
	}
	// Rollback after a successful Commit is a harmless no-op.
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	projectRow, err := q.CreateProject(ctx, db.CreateProjectParams{
		Name:        p.Name,
		RepoPath:    p.RepoPath,
		Description: p.Description,
	})
	if err != nil {
		return Project{}, Room{}, fmt.Errorf("create project %q: %w", p.Name, err)
	}
	roomRow, err := q.CreateRoom(ctx, db.CreateRoomParams{
		ProjectID: projectRow.ID,
		Name:      MainRoomName,
		Kind:      string(RoomMain),
	})
	if err != nil {
		return Project{}, Room{}, fmt.Errorf("create main room for %q: %w", p.Name, err)
	}
	added := make(map[string]bool, len(p.AgentIDs))
	for _, agentID := range p.AgentIDs {
		if added[agentID] {
			continue
		}
		added[agentID] = true
		member := NewMember{RoomID: uuidString(roomRow.ID), AgentID: agentID, RepoPath: p.RepoPath}
		if _, err := createMember(ctx, q, member); err != nil {
			return Project{}, Room{}, fmt.Errorf("add agent %s to %q: %w", agentID, p.Name, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Project{}, Room{}, fmt.Errorf("commit: %w", err)
	}
	project := toProject(projectRow)
	project.MainRoomID = uuidString(roomRow.ID)
	return project, toRoom(roomRow), nil
}

// GetProject returns one project, or ErrNotFound.
func (s *Store) GetProject(ctx context.Context, id string) (Project, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Project{}, err
	}
	row, err := s.q.GetProject(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, fmt.Errorf("project %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Project{}, fmt.Errorf("get project %s: %w", id, err)
	}
	return withMainRoom(toProject(row.Project), row.MainRoomID), nil
}

// ListProjects returns every project in creation order.
func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.q.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	out := make([]Project, 0, len(rows))
	for _, row := range rows {
		out = append(out, withMainRoom(toProject(row.Project), row.MainRoomID))
	}
	return out, nil
}

// ProjectPatch is the input to UpdateProject: nil means keep.
type ProjectPatch struct {
	Name        *string
	RepoPath    *string
	Description *string
}

// UpdateProject renames a project or moves its checkout. Members work under
// the checkout, so moving it moves the project's current members with it in
// the same transaction: a member working below the old checkout keeps its
// place below the new one, every other current member works in the new
// checkout. Members taken out of the project keep the path they last had. An
// unknown id is ErrNotFound; a blank name is ErrInvalidInput.
func (s *Store) UpdateProject(ctx context.Context, id string, patch ProjectPatch) (Project, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Project{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Project{}, fmt.Errorf("begin: %w", err)
	}
	// Rollback after a successful Commit is a harmless no-op.
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	row, err := q.UpdateProject(ctx, db.UpdateProjectParams{ID: uid, Name: optionalText(patch.Name), RepoPath: optionalText(patch.RepoPath), Description: optionalText(patch.Description)})
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, fmt.Errorf("project %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Project{}, mapPGError("update project", err)
	}
	if row.RepoPath != row.OldRepoPath {
		err := q.MoveProjectMembers(ctx, db.MoveProjectMembersParams{ProjectID: uid, OldPath: row.OldRepoPath, NewPath: row.RepoPath})
		if err != nil {
			return Project{}, fmt.Errorf("move the members of project %s: %w", id, err)
		}
	}
	updated, err := q.GetProject(ctx, uid)
	if err != nil {
		return Project{}, fmt.Errorf("get project %s: %w", id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, fmt.Errorf("commit: %w", err)
	}
	return withMainRoom(toProject(updated.Project), updated.MainRoomID), nil
}

// ProjectRemains is what a deleted project leaves on disk for its caller
// to remove: its uploads, as paths under the attachment directory, and its
// turns, whose transcripts are named after them.
type ProjectRemains struct {
	AttachmentPaths []string
	TurnIDs         []string
}

// DeleteProject deletes a project with its whole chat: rooms, members,
// messages, topics, turns, approvals and upload records go in one
// transaction. Agents stay, in one project fewer. While one of its turns is
// running it is ErrConflict, since that turn would go on writing to a chat
// that is gone; an unknown id is ErrNotFound.
func (s *Store) DeleteProject(ctx context.Context, id string) (ProjectRemains, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return ProjectRemains{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ProjectRemains{}, fmt.Errorf("begin: %w", err)
	}
	// Rollback after a successful Commit is a harmless no-op.
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	paths, err := q.ProjectAttachmentPaths(ctx, uid)
	if err != nil {
		return ProjectRemains{}, fmt.Errorf("list the uploads of project %s: %w", id, err)
	}
	turns, err := q.ProjectTurnIDs(ctx, uid)
	if err != nil {
		return ProjectRemains{}, fmt.Errorf("list the turns of project %s: %w", id, err)
	}
	deleted, err := q.DeleteProject(ctx, uid)
	if err != nil {
		return ProjectRemains{}, fmt.Errorf("delete project %s: %w", id, err)
	}
	if deleted == 0 {
		if _, err := q.GetProject(ctx, uid); errors.Is(err, pgx.ErrNoRows) {
			return ProjectRemains{}, fmt.Errorf("project %s: %w", id, ErrNotFound)
		} else if err != nil {
			return ProjectRemains{}, fmt.Errorf("get project %s: %w", id, err)
		}
		return ProjectRemains{}, fmt.Errorf("project %s: %w: a turn is still running", id, ErrConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectRemains{}, fmt.Errorf("commit: %w", err)
	}
	remains := ProjectRemains{AttachmentPaths: paths, TurnIDs: make([]string, 0, len(turns))}
	for _, turn := range turns {
		remains.TurnIDs = append(remains.TurnIDs, uuidString(turn))
	}
	return remains, nil
}

// CreateRoom adds a topic room to a project. The main room is created with
// the project and cannot be added here. An unknown project is ErrNotFound.
func (s *Store) CreateRoom(ctx context.Context, projectID, name string) (Room, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return Room{}, err
	}
	row, err := s.q.CreateRoom(ctx, db.CreateRoomParams{
		ProjectID: pid,
		Name:      name,
		Kind:      string(RoomTopic),
	})
	if isForeignKeyViolation(err) {
		return Room{}, fmt.Errorf("project %s: %w", projectID, ErrNotFound)
	}
	if err != nil {
		return Room{}, fmt.Errorf("create room %q: %w", name, err)
	}
	return toRoom(row), nil
}

// GetRoom returns one room, or ErrNotFound.
func (s *Store) GetRoom(ctx context.Context, id string) (Room, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return Room{}, err
	}
	row, err := s.q.GetRoom(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Room{}, fmt.Errorf("room %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return Room{}, fmt.Errorf("get room %s: %w", id, err)
	}
	return toRoom(row), nil
}

// ListRooms returns a project's rooms, main room first. An unknown project
// yields an empty list, not an error.
func (s *Store) ListRooms(ctx context.Context, projectID string) ([]Room, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListRoomsByProject(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("list rooms of project %s: %w", projectID, err)
	}
	out := make([]Room, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRoom(row))
	}
	return out, nil
}

func toProject(row db.Project) Project {
	return Project{
		ID:          uuidString(row.ID),
		Name:        row.Name,
		RepoPath:    row.RepoPath,
		Description: row.Description,
		CreatedAt:   row.CreatedAt.Time,
	}
}

// RoomProject returns the project a room belongs to, or ErrNotFound.
func (s *Store) RoomProject(ctx context.Context, roomID string) (Project, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return Project{}, err
	}
	row, err := s.q.GetRoomProject(ctx, rid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, fmt.Errorf("project of room %s: %w", roomID, ErrNotFound)
	}
	if err != nil {
		return Project{}, fmt.Errorf("project of room %s: %w", roomID, err)
	}
	return toProject(row), nil
}

// withMainRoom fills the chat id the listing queries compute alongside
// the project; a project without one (never, in practice) stays empty.
func withMainRoom(p Project, id pgtype.UUID) Project {
	if id.Valid {
		p.MainRoomID = uuidString(id)
	}
	return p
}

func toRoom(row db.Room) Room {
	return Room{
		ID:        uuidString(row.ID),
		ProjectID: uuidString(row.ProjectID),
		Name:      row.Name,
		Kind:      RoomKind(row.Kind),
		CreatedAt: row.CreatedAt.Time,
	}
}

// isForeignKeyViolation reports whether err is Postgres error 23503, which
// is how an insert referencing a missing parent row fails.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
