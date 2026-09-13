package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// Project is a codebase the team and its agents work on.
type Project struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	RepoURL       string    `json:"repo_url"`
	DefaultBranch string    `json:"default_branch"`
	CreatedAt     time.Time `json:"created_at"`
}

// NewProject is the input to CreateProject. An empty DefaultBranch means
// "main".
type NewProject struct {
	Name          string
	RepoURL       string
	DefaultBranch string
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

// CreateProject creates a project together with its main room, atomically.
func (s *Store) CreateProject(ctx context.Context, p NewProject) (Project, Room, error) {
	if p.DefaultBranch == "" {
		p.DefaultBranch = "main"
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Project{}, Room{}, fmt.Errorf("begin: %w", err)
	}
	// Rollback after a successful Commit is a harmless no-op.
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	projectRow, err := q.CreateProject(ctx, db.CreateProjectParams{
		Name:          p.Name,
		RepoUrl:       p.RepoURL,
		DefaultBranch: p.DefaultBranch,
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

	if err := tx.Commit(ctx); err != nil {
		return Project{}, Room{}, fmt.Errorf("commit: %w", err)
	}
	return toProject(projectRow), toRoom(roomRow), nil
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
	return toProject(row), nil
}

// ListProjects returns every project in creation order.
func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.q.ListProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	out := make([]Project, 0, len(rows))
	for _, row := range rows {
		out = append(out, toProject(row))
	}
	return out, nil
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
		ID:            uuidString(row.ID),
		Name:          row.Name,
		RepoURL:       row.RepoUrl,
		DefaultBranch: row.DefaultBranch,
		CreatedAt:     row.CreatedAt.Time,
	}
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
