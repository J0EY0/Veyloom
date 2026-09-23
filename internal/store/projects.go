package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	// WikiSlug names the folder of the project's wiki under the hub's state
	// dir. It is set once, from the name, and never changes.
	WikiSlug string `json:"wiki_slug"`
	// WikiThreadID is the project's "wiki" topic, where its wiki maintainer
	// works; empty until first needed.
	WikiThreadID string `json:"wiki_thread_id,omitempty"`
	// WikiMaintainerMemberID is the member a person chose to keep the wiki
	// (docs/design.md 5.12); empty is none.
	WikiMaintainerMemberID string `json:"wiki_maintainer_member_id,omitempty"`
	// WikiMaintainerTrigger is when that member goes over the chat.
	WikiMaintainerTrigger UpkeepTrigger `json:"wiki_maintainer_trigger"`
	// WikiOfferMessageID is the note in the chat that offered a wiki
	// maintainer to a project that had none (docs/design.md 5.16), which the
	// chat draws as a card; empty until offered, and offered once.
	WikiOfferMessageID string `json:"wiki_offer_message_id,omitempty"`
	// WikiOfferDeclinedAt is when a person said no to a maintainer, which is
	// then not offered again; nil when nobody did.
	WikiOfferDeclinedAt *time.Time `json:"wiki_offer_declined_at,omitempty"`
	// WikiSeenSeq is how far the maintainer has gone over what people said
	// in the chat, as messages.seq (docs/design.md 5.16).
	WikiSeenSeq int64 `json:"-"`
	// WikiExternalBundles are the folders of the OKF bundles the wiki
	// mounts, read-only (docs/design.md 5.9).
	WikiExternalBundles []string `json:"wiki_external_bundles"`
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
	// WikiMaintainerAgentID, one of AgentIDs, makes that agent's member the
	// wiki maintainer from the start, running as WikiMaintainerTrigger
	// (daily when empty); empty chooses none (docs/design.md 5.16).
	WikiMaintainerAgentID string
	WikiMaintainerTrigger UpkeepTrigger
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

	// The wiki's folder takes the name's words; a name taken already gets
	// a number, the first one free.
	var projectRow db.Project
	base := wikiSlug(p.Name)
	for n := 1; ; n++ {
		slug := base
		if n > 1 {
			slug = fmt.Sprintf("%s-%d", base, n)
		}
		projectRow, err = q.CreateProject(ctx, db.CreateProjectParams{
			Name:        p.Name,
			RepoPath:    p.RepoPath,
			Description: p.Description,
			WikiSlug:    slug,
		})
		if err == nil {
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) || n >= 1000 {
			return Project{}, Room{}, mapPGError(fmt.Sprintf("create project %q", p.Name), err)
		}
	}
	roomRow, err := q.CreateRoom(ctx, db.CreateRoomParams{
		ProjectID: projectRow.ID,
		Name:      MainRoomName,
		Kind:      string(RoomMain),
	})
	if err != nil {
		return Project{}, Room{}, fmt.Errorf("create main room for %q: %w", p.Name, err)
	}
	trigger := p.WikiMaintainerTrigger
	if trigger == "" {
		trigger = UpkeepDaily
	}
	if !trigger.Valid() {
		return Project{}, Room{}, invalidTrigger(trigger)
	}
	added := make(map[string]bool, len(p.AgentIDs))
	var maintainer Member
	for _, agentID := range p.AgentIDs {
		if added[agentID] {
			continue
		}
		added[agentID] = true
		member := NewMember{RoomID: uuidString(roomRow.ID), AgentID: agentID, RepoPath: p.RepoPath}
		created, err := createMember(ctx, q, member)
		if err != nil {
			return Project{}, Room{}, fmt.Errorf("add agent %s to %q: %w", agentID, p.Name, err)
		}
		if agentID == p.WikiMaintainerAgentID {
			maintainer = created
		}
	}
	if p.WikiMaintainerAgentID != "" {
		if maintainer.ID == "" {
			return Project{}, Room{}, fmt.Errorf("%w: the wiki maintainer must be one of the agents the project starts with", ErrInvalidInput)
		}
		memberID, _ := parseUUID(maintainer.ID)
		if err := q.SetProjectMaintainer(ctx, db.SetProjectMaintainerParams{ID: projectRow.ID, MemberID: memberID, Trigger: string(trigger)}); err != nil {
			return Project{}, Room{}, fmt.Errorf("set the wiki maintainer of %q: %w", p.Name, err)
		}
		projectRow.WikiMaintainerMemberID, projectRow.WikiMaintainerTrigger = memberID, string(trigger)
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
	// WikiMaintainer is the member who keeps the wiki, one of the project's
	// current members; "" is none.
	WikiMaintainer *string
	// WikiMaintainerTrigger is when the maintainer runs.
	WikiMaintainerTrigger *UpkeepTrigger
	// WikiExternalBundles replaces the folders the wiki mounts; the caller
	// has checked them.
	WikiExternalBundles *[]string
	// DeclineWikiOffer records that a person said no to a wiki maintainer,
	// which is then not offered again.
	DeclineWikiOffer bool
}

// UpkeepTrigger is when a project's wiki maintainer goes over what the
// chat did.
type UpkeepTrigger string

const (
	// UpkeepIdle goes over a topic once it has been quiet a while.
	UpkeepIdle UpkeepTrigger = "idle"
	// UpkeepDaily goes over the day's turns once a day, the default.
	UpkeepDaily UpkeepTrigger = "daily"
	// UpkeepEvery3Days goes over the turns every three days.
	UpkeepEvery3Days UpkeepTrigger = "every_3_days"
	// UpkeepWeekly goes over the week's turns once a week, the longest a
	// wiki is left (docs/design.md 5.16).
	UpkeepWeekly UpkeepTrigger = "weekly"
	// UpkeepManual waits for a person to ask.
	UpkeepManual UpkeepTrigger = "manual"
)

// Valid reports whether t is one of the triggers.
func (t UpkeepTrigger) Valid() bool {
	switch t {
	case UpkeepIdle, UpkeepDaily, UpkeepEvery3Days, UpkeepWeekly, UpkeepManual:
		return true
	}
	return false
}

// invalidTrigger says a trigger is none of the known ones.
func invalidTrigger(t UpkeepTrigger) error {
	return fmt.Errorf("%w: the wiki maintainer runs when a topic is idle, daily, every 3 days, weekly or manually, not %q", ErrInvalidInput, t)
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

	params := db.UpdateProjectParams{
		ID: uid, Name: optionalText(patch.Name), RepoPath: optionalText(patch.RepoPath), Description: optionalText(patch.Description),
		DeclineOffer: patch.DeclineWikiOffer,
	}
	if t := patch.WikiMaintainerTrigger; t != nil {
		if !t.Valid() {
			return Project{}, invalidTrigger(*t)
		}
		params.MaintainerTrigger = pgtype.Text{String: string(*t), Valid: true}
	}
	if b := patch.WikiExternalBundles; b != nil {
		params.ExternalBundles = nonNil(*b)
	}
	if m := patch.WikiMaintainer; m != nil {
		params.SetMaintainer = true
		if *m != "" {
			memberID, err := parseUUID(*m)
			if err != nil {
				return Project{}, err
			}
			current, err := q.IsCurrentProjectMember(ctx, db.IsCurrentProjectMemberParams{MemberID: memberID, ProjectID: uid})
			if err != nil {
				return Project{}, fmt.Errorf("check the wiki maintainer: %w", err)
			}
			if !current {
				return Project{}, Invalid("maintainerNotMember", nil, "the wiki maintainer must be one of the project's members")
			}
			params.MaintainerMemberID = memberID
		}
	}
	row, err := q.UpdateProject(ctx, params)
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
	ProjectID       string
	AttachmentPaths []string
	TurnIDs         []string
	// WikiSlug is the folder the project's wiki was kept in.
	WikiSlug string
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

	project, err := q.GetProject(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ProjectRemains{}, fmt.Errorf("project %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return ProjectRemains{}, fmt.Errorf("get project %s: %w", id, err)
	}
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
		return ProjectRemains{}, fmt.Errorf("project %s: %w", id, stillRunning())
	}
	if err := tx.Commit(ctx); err != nil {
		return ProjectRemains{}, fmt.Errorf("commit: %w", err)
	}
	remains := ProjectRemains{ProjectID: id, AttachmentPaths: paths, TurnIDs: make([]string, 0, len(turns)), WikiSlug: project.Project.WikiSlug}
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
	p := Project{
		ID:                     uuidString(row.ID),
		Name:                   row.Name,
		RepoPath:               row.RepoPath,
		Description:            row.Description,
		WikiSlug:               row.WikiSlug,
		WikiThreadID:           uuidString(row.WikiThreadID),
		WikiMaintainerMemberID: uuidString(row.WikiMaintainerMemberID),
		WikiMaintainerTrigger:  UpkeepTrigger(row.WikiMaintainerTrigger),
		WikiOfferMessageID:     uuidString(row.WikiOfferMessageID),
		WikiSeenSeq:            row.WikiSeenSeq,
		WikiExternalBundles:    nonNil(row.WikiExternalBundles),
		CreatedAt:              row.CreatedAt.Time,
	}
	if row.WikiOfferDeclinedAt.Valid {
		declined := row.WikiOfferDeclinedAt.Time
		p.WikiOfferDeclinedAt = &declined
	}
	return p
}

// ListMaintainedProjects lists the projects given a wiki maintainer.
func (s *Store) ListMaintainedProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.q.ListMaintainedProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list maintained projects: %w", err)
	}
	out := make([]Project, 0, len(rows))
	for _, row := range rows {
		out = append(out, withMainRoom(toProject(row.Project), row.MainRoomID))
	}
	return out, nil
}

// ListUnmaintainedProjects lists the projects a wiki maintainer may yet be
// offered to: none chosen, offered or declined.
func (s *Store) ListUnmaintainedProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.q.ListUnmaintainedProjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list projects without a wiki maintainer: %w", err)
	}
	out := make([]Project, 0, len(rows))
	for _, row := range rows {
		out = append(out, withMainRoom(toProject(row.Project), row.MainRoomID))
	}
	return out, nil
}

// SetProjectWikiOffer records the note that offered the project a wiki
// maintainer. False says one was chosen, offered or declined meanwhile, and
// the offer stands for nothing.
func (s *Store) SetProjectWikiOffer(ctx context.Context, projectID, messageID string) (bool, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return false, err
	}
	mid, err := parseUUID(messageID)
	if err != nil {
		return false, err
	}
	n, err := s.q.SetProjectWikiOffer(ctx, db.SetProjectWikiOfferParams{ID: pid, MessageID: mid})
	if err != nil {
		return false, mapPGError("record the wiki maintainer offer of project "+projectID, err)
	}
	return n == 1, nil
}

// GetProjectBySlug returns the project whose wiki folder is slug: the team
// that owns the skills naming it. ErrNotFound when no project has it, as
// once its project was deleted.
func (s *Store) GetProjectBySlug(ctx context.Context, slug string) (Project, error) {
	row, err := s.q.GetProjectBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, fmt.Errorf("project with wiki %q: %w", slug, ErrNotFound)
	}
	if err != nil {
		return Project{}, fmt.Errorf("get project with wiki %q: %w", slug, err)
	}
	return withMainRoom(toProject(row.Project), row.MainRoomID), nil
}

// SetProjectWikiThread records the project's wiki topic. A project that
// has one already keeps it, and false says so: another caller got there
// first, and its topic is the one to use.
func (s *Store) SetProjectWikiThread(ctx context.Context, projectID, threadID string) (bool, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return false, err
	}
	tid, err := parseUUID(threadID)
	if err != nil {
		return false, err
	}
	n, err := s.q.SetProjectWikiThread(ctx, db.SetProjectWikiThreadParams{ID: pid, ThreadID: tid})
	if err != nil {
		return false, mapPGError("set the wiki topic of project "+projectID, err)
	}
	return n == 1, nil
}

// wikiSlug makes a folder name from a project's name: its ASCII letters and
// digits, lowercased, the rest turned into single hyphens, cut at forty
// characters. A name with none of those, one in Chinese say, becomes
// "project".
func wikiSlug(name string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			hyphen = false
			b.WriteRune(r)
		default:
			hyphen = true
		}
	}
	slug := b.String()
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		return "project"
	}
	return slug
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
