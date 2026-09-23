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

// The wiki maintainer's bookkeeping (docs/design.md 5.12): the turns a
// project's maintainer looks at, the ones it has gone over, when it ran.

// UpkeepTurn is a finished turn as a wiki maintainer looks at it: where it
// ran, who ran it, how it went, what it touched.
type UpkeepTurn struct {
	ID       string `json:"id"`
	ThreadID string `json:"thread_id"`
	RoomID   string `json:"room_id"`
	// TopicNumber is the number of its topic in its chat, the 12 of #12.
	TopicNumber int `json:"topic_number"`
	// RootBody is the text heading the topic.
	RootBody     string     `json:"root_body"`
	MemberName   string     `json:"member_name"`
	Runtime      string     `json:"runtime"`
	Status       TurnStatus `json:"status"`
	Error        string     `json:"error,omitempty"`
	FilesChanged []string   `json:"files_changed,omitempty"`
	SkillsUsed   []string   `json:"skills_used,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	ProjectID    string     `json:"project_id"`
	ProjectName  string     `json:"project_name"`
	// Reviewed says the maintainer that asked has gone over it.
	Reviewed bool `json:"reviewed"`
}

// UpkeepQuery picks the turns of ListUpkeepTurns.
type UpkeepQuery struct {
	// ProjectID is the project whose maintainer looks.
	ProjectID string
	// Skills looks at other projects' turns that used one of Owned, the
	// skills the project's team owns; false at the project's own.
	Skills bool
	Owned  []string
	// Unreviewed keeps the turns the maintainer has not gone over.
	Unreviewed bool
	// Topic keeps one topic of the project's own chat; zero keeps all.
	Topic int
	// SettledBy keeps the turns in topics settled by then: the turn had
	// ended, nothing was said in its topic after, and nothing runs there.
	// Nil keeps all.
	SettledBy *time.Time
	// Before keeps the turns started before it; nil keeps all.
	Before *time.Time
	// OldestFirst lists the turns in the order they ran, not the newest
	// first.
	OldestFirst bool
	Limit       int
}

// ListUpkeepTurns lists finished turns a project's wiki maintainer may
// look at. The maintainer's own turns are never among them.
func (s *Store) ListUpkeepTurns(ctx context.Context, q UpkeepQuery) ([]UpkeepTurn, error) {
	pid, err := parseUUID(q.ProjectID)
	if err != nil {
		return nil, err
	}
	var before, settled pgtype.Timestamptz
	if q.Before != nil {
		before = pgtype.Timestamptz{Time: *q.Before, Valid: true}
	}
	if q.SettledBy != nil {
		settled = pgtype.Timestamptz{Time: *q.SettledBy, Valid: true}
	}
	rows, err := s.q.ListUpkeepTurns(ctx, db.ListUpkeepTurnsParams{
		ProjectID: pid, Skills: q.Skills, Owned: nonNil(q.Owned), Topic: int32(q.Topic), Unreviewed: q.Unreviewed,
		SettledBy: settled, Before: before, OldestFirst: q.OldestFirst, Lim: clampLimit(q.Limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list turns for the wiki maintainer of %s: %w", q.ProjectID, err)
	}
	out := make([]UpkeepTurn, 0, len(rows))
	for _, row := range rows {
		t := UpkeepTurn{
			ID: uuidString(row.ID), ThreadID: uuidString(row.ThreadID), RoomID: uuidString(row.RoomID),
			TopicNumber: int(row.TopicNumber), RootBody: row.RootBody, MemberName: row.MemberName, Runtime: row.Runtime,
			Status: TurnStatus(row.Status), Error: row.Error, FilesChanged: row.FilesChanged, SkillsUsed: row.SkillsUsed,
			StartedAt: row.StartedAt.Time, ProjectID: uuidString(row.ProjectID), ProjectName: row.ProjectName, Reviewed: row.Reviewed,
		}
		if row.EndedAt.Valid {
			ended := row.EndedAt.Time
			t.EndedAt = &ended
		}
		out = append(out, t)
	}
	return out, nil
}

// UpkeepWaiting is what waits for a project's wiki maintainer.
type UpkeepWaiting struct {
	// Own counts the project's finished turns it has not gone over.
	Own int `json:"own"`
	// Uses counts other projects' finished turns that used a skill of its
	// team and that it has not gone over.
	Uses int `json:"uses"`
	// Settled counts those of all of them whose topic has been quiet since
	// the moment asked about.
	Settled int `json:"settled"`
	// People counts what people said in the chat since the maintainer last
	// went over it (docs/design.md 5.16), and PeopleSettled those said in
	// the room itself, or in a topic that has been quiet, by the moment
	// asked about.
	People        int `json:"people"`
	PeopleSettled int `json:"people_settled"`
}

// Total is every turn that waits.
func (w UpkeepWaiting) Total() int { return w.Own + w.Uses }

// Anything reports whether anything at all waits: a turn, or something a
// person said.
func (w UpkeepWaiting) Anything() bool { return w.Total()+w.People > 0 }

// CountUpkeepWaiting counts what waits for a project's maintainer: owned
// are the skills its team owns, quietSince the moment a topic counts as
// settled by.
func (s *Store) CountUpkeepWaiting(ctx context.Context, projectID string, owned []string, quietSince time.Time) (UpkeepWaiting, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return UpkeepWaiting{}, err
	}
	row, err := s.q.CountUpkeepWaiting(ctx, db.CountUpkeepWaitingParams{
		ProjectID: pid, Owned: nonNil(owned), QuietSince: pgtype.Timestamptz{Time: quietSince, Valid: true},
	})
	if err != nil {
		return UpkeepWaiting{}, fmt.Errorf("count what waits for the wiki maintainer of %s: %w", projectID, err)
	}
	people, err := s.q.CountPeopleWaiting(ctx, db.CountPeopleWaitingParams{ProjectID: pid, QuietSince: pgtype.Timestamptz{Time: quietSince, Valid: true}})
	if err != nil {
		return UpkeepWaiting{}, fmt.Errorf("count what people said for the wiki maintainer of %s: %w", projectID, err)
	}
	return UpkeepWaiting{Own: int(row.Own), Uses: int(row.Uses), Settled: int(row.Settled), People: int(people.People), PeopleSettled: int(people.Settled)}, nil
}

// PeopleNews is what people said in one topic of a project's chat, or in
// the room itself (Topic zero), within a stretch of it.
type PeopleNews struct {
	ThreadID string
	Topic    int
	RootBody string
	Messages int
	Files    int
	LastAt   time.Time
}

// PeopleFile is a file a person sent in a project's chat: where and by whom.
type PeopleFile struct {
	Attachment
	Topic  int
	Sender string
}

// ListPeopleNews lists, by topic, what people said in the project's chat
// after one position up to another (messages.seq), oldest first.
func (s *Store) ListPeopleNews(ctx context.Context, projectID string, after, upto int64, limit int) ([]PeopleNews, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListPeopleNews(ctx, db.ListPeopleNewsParams{ProjectID: pid, After: after, Upto: upto, Lim: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("list what people said in project %s: %w", projectID, err)
	}
	out := make([]PeopleNews, 0, len(rows))
	for _, row := range rows {
		out = append(out, PeopleNews{
			ThreadID: uuidString(row.ThreadID), Topic: int(row.TopicNumber), RootBody: row.RootBody,
			Messages: int(row.Messages), Files: int(row.Files), LastAt: row.LastAt.Time,
		})
	}
	return out, nil
}

// ListPeopleFiles lists the files people sent in the project's chat after
// one position up to another, oldest first.
func (s *Store) ListPeopleFiles(ctx context.Context, projectID string, after, upto int64, limit int) ([]PeopleFile, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListPeopleFiles(ctx, db.ListPeopleFilesParams{ProjectID: pid, After: after, Upto: upto, Lim: int32(limit)})
	if err != nil {
		return nil, fmt.Errorf("list the files people sent in project %s: %w", projectID, err)
	}
	out := make([]PeopleFile, 0, len(rows))
	for _, row := range rows {
		out = append(out, PeopleFile{
			Attachment: Attachment{
				ID: uuidString(row.ID), RoomID: uuidString(row.RoomID), MessageID: uuidString(row.MessageID),
				Filename: row.Filename, MediaType: row.MediaType, Size: row.Size, Path: row.Path, CreatedAt: row.CreatedAt.Time,
			},
			Topic: int(row.TopicNumber), Sender: row.Sender,
		})
	}
	return out, nil
}

// SetProjectWikiSeen moves how far the project's maintainer has gone over
// its chat, as messages.seq; it never moves back.
func (s *Store) SetProjectWikiSeen(ctx context.Context, projectID string, seq int64) error {
	pid, err := parseUUID(projectID)
	if err != nil {
		return err
	}
	if err := s.q.SetProjectWikiSeen(ctx, db.SetProjectWikiSeenParams{ID: pid, Seq: seq}); err != nil {
		return fmt.Errorf("move the wiki position of project %s: %w", projectID, err)
	}
	return nil
}

// CountSettledTopicsWaiting counts the topics of the project's own chat
// that went quiet by quietSince with turns its maintainer has not gone
// over: what a maintainer is offered on (docs/design.md 5.16).
func (s *Store) CountSettledTopicsWaiting(ctx context.Context, projectID string, quietSince time.Time) (int, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return 0, err
	}
	n, err := s.q.CountSettledTopicsWaiting(ctx, db.CountSettledTopicsWaitingParams{ProjectID: pid, QuietSince: pgtype.Timestamptz{Time: quietSince, Valid: true}})
	if err != nil {
		return 0, fmt.Errorf("count the quiet topics waiting in project %s: %w", projectID, err)
	}
	return int(n), nil
}

// CountUpkeepsSince counts the upkeeps of the project's wiki started since.
func (s *Store) CountUpkeepsSince(ctx context.Context, projectID string, since time.Time) (int, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return 0, err
	}
	n, err := s.q.CountUpkeepsSince(ctx, db.CountUpkeepsSinceParams{ProjectID: pid, Since: pgtype.Timestamptz{Time: since, Valid: true}})
	if err != nil {
		return 0, fmt.Errorf("count the upkeeps of project %s: %w", projectID, err)
	}
	return int(n), nil
}

// CountUpkeepReviews counts the turns an upkeep went over.
func (s *Store) CountUpkeepReviews(ctx context.Context, upkeepTurnID string) (int, error) {
	id, err := parseUUID(upkeepTurnID)
	if err != nil {
		return 0, err
	}
	n, err := s.q.CountUpkeepReviews(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("count what upkeep %s went over: %w", upkeepTurnID, err)
	}
	return int(n), nil
}

// LastUpkeep returns the latest turn of a project's wiki maintainer,
// running or not; ErrNotFound when it never ran.
func (s *Store) LastUpkeep(ctx context.Context, projectID string) (Turn, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return Turn{}, err
	}
	row, err := s.q.LastUpkeep(ctx, pid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Turn{}, fmt.Errorf("upkeep of project %s: %w", projectID, ErrNotFound)
	}
	if err != nil {
		return Turn{}, fmt.Errorf("last upkeep of project %s: %w", projectID, err)
	}
	return toTurn(row), nil
}

// RecordWikiReviews marks turns as gone over by a project's maintainer in
// its turn upkeepTurnID. A turn gone over before keeps its first record;
// one that no longer exists is skipped.
func (s *Store) RecordWikiReviews(ctx context.Context, projectID, upkeepTurnID string, turnIDs []string) error {
	if len(turnIDs) == 0 {
		return nil
	}
	pid, err := parseUUID(projectID)
	if err != nil {
		return err
	}
	upkeep, err := parseUUID(upkeepTurnID)
	if err != nil {
		return err
	}
	ids := make([]pgtype.UUID, 0, len(turnIDs))
	for _, id := range turnIDs {
		uid, err := parseUUID(id)
		if err != nil {
			return err
		}
		ids = append(ids, uid)
	}
	if err := s.q.RecordWikiReviews(ctx, db.RecordWikiReviewsParams{ProjectID: pid, UpkeepTurnID: upkeep, TurnIds: ids}); err != nil {
		return fmt.Errorf("record what the wiki maintainer of %s went over: %w", projectID, err)
	}
	return nil
}

// NextPersonMessage returns the first thing a person said in a thread
// after a moment; ErrNotFound when nobody has.
func (s *Store) NextPersonMessage(ctx context.Context, threadID string, after time.Time) (Message, error) {
	tid, err := parseUUID(threadID)
	if err != nil {
		return Message{}, err
	}
	row, err := s.q.NextPersonMessage(ctx, db.NextPersonMessageParams{ThreadID: tid, CreatedAt: pgtype.Timestamptz{Time: after, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return Message{}, fmt.Errorf("a person's message in thread %s: %w", threadID, ErrNotFound)
	}
	if err != nil {
		return Message{}, fmt.Errorf("next message of a person in thread %s: %w", threadID, err)
	}
	return toMessage(row)
}

// ChangedFile is a file the turns of a project's chat changed, with the
// last of them to start: where, and when it started.
type ChangedFile struct {
	Path        string    `json:"path"`
	TurnID      string    `json:"turn_id"`
	RoomID      string    `json:"room_id"`
	ThreadID    string    `json:"thread_id"`
	TopicNumber int       `json:"topic_number"`
	At          time.Time `json:"at"`
}

// ListChangedFiles lists the files changed by the project's turns that
// started after since, each once with the last of those turns, up to
// limit, by path.
func (s *Store) ListChangedFiles(ctx context.Context, projectID string, since time.Time, limit int) ([]ChangedFile, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListChangedFiles(ctx, db.ListChangedFilesParams{
		ProjectID: pid, Since: pgtype.Timestamptz{Time: since, Valid: true}, Lim: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list the files changed in project %s: %w", projectID, err)
	}
	out := make([]ChangedFile, 0, len(rows))
	for _, row := range rows {
		out = append(out, ChangedFile{
			Path: row.Path, TurnID: uuidString(row.TurnID), RoomID: uuidString(row.RoomID), ThreadID: uuidString(row.ThreadID),
			TopicNumber: int(row.TopicNumber), At: row.StartedAt.Time,
		})
	}
	return out, nil
}
