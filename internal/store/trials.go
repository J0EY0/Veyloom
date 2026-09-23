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

// SkillTrialStatus is where a skill's trial stands.
type SkillTrialStatus string

const (
	TrialOpen       SkillTrialStatus = "open"
	TrialKept       SkillTrialStatus = "kept"
	TrialRolledBack SkillTrialStatus = "rolled_back"
)

// SkillTrial is a skill of the library on trial after an agent changed it
// (docs/design.md 5.15): kept once enough turns used it since and ended
// well, or rolled back to the version before.
type SkillTrial struct {
	ID    string `json:"id"`
	Skill string `json:"skill"`
	// BaseSHA is the library's commit before the first change, what a
	// rollback goes back to.
	BaseSHA   string    `json:"base_sha"`
	StartedAt time.Time `json:"started_at"`
	// The last change: when, in which turn, by whom and of which project,
	// by their names then; and how many changes the trial has had.
	ChangedAt   time.Time        `json:"changed_at"`
	TurnID      string           `json:"turn_id,omitempty"`
	ChangedBy   string           `json:"changed_by"`
	ProjectName string           `json:"project_name"`
	Changes     int              `json:"changes"`
	Status      SkillTrialStatus `json:"status"`
	// EndedBy is who ended it, an OKF actor; Reason why, when they said.
	EndedBy string     `json:"ended_by,omitempty"`
	EndedAt *time.Time `json:"ended_at,omitempty"`
	Reason  string     `json:"reason,omitempty"`
}

// NewSkillTrial is a change to a skill that opens a trial, or starts an
// open one's count again.
type NewSkillTrial struct {
	Skill string
	// BaseSHA is the library's commit before the change; an open trial
	// keeps its own.
	BaseSHA     string
	TurnID      string
	ChangedBy   string
	ProjectName string
}

// StartSkillTrial records a change to a skill: it opens the skill's trial,
// or starts the count of its open one again from the same version to go
// back to.
func (s *Store) StartSkillTrial(ctx context.Context, in NewSkillTrial) (SkillTrial, error) {
	turn, err := optionalUUID(in.TurnID)
	if err != nil {
		return SkillTrial{}, err
	}
	row, err := s.q.StartSkillTrial(ctx, db.StartSkillTrialParams{
		Skill: in.Skill, BaseSha: in.BaseSHA, TurnID: turn, ChangedBy: in.ChangedBy, ProjectName: in.ProjectName,
	})
	if err != nil {
		return SkillTrial{}, fmt.Errorf("start the trial of skill %s: %w", in.Skill, err)
	}
	return toSkillTrial(row), nil
}

// OpenSkillTrial returns the skill's open trial; ErrNotFound when it has
// none.
func (s *Store) OpenSkillTrial(ctx context.Context, skill string) (SkillTrial, error) {
	row, err := s.q.OpenSkillTrial(ctx, skill)
	if errors.Is(err, pgx.ErrNoRows) {
		return SkillTrial{}, fmt.Errorf("skill %s is on no trial: %w", skill, ErrNotFound)
	}
	if err != nil {
		return SkillTrial{}, fmt.Errorf("the trial of skill %s: %w", skill, err)
	}
	return toSkillTrial(row), nil
}

// LatestSkillTrial returns the skill's open trial, or else the one that
// ended last; ErrNotFound when it never had one.
func (s *Store) LatestSkillTrial(ctx context.Context, skill string) (SkillTrial, error) {
	row, err := s.q.LatestSkillTrial(ctx, skill)
	if errors.Is(err, pgx.ErrNoRows) {
		return SkillTrial{}, fmt.Errorf("skill %s was never on trial: %w", skill, ErrNotFound)
	}
	if err != nil {
		return SkillTrial{}, fmt.Errorf("the trial of skill %s: %w", skill, err)
	}
	return toSkillTrial(row), nil
}

// EndSkillTrial ends an open trial as kept or rolled back, by an OKF actor
// and for a reason that may be empty. A trial that has ended already is a
// conflict: someone else ended it first.
func (s *Store) EndSkillTrial(ctx context.Context, id string, status SkillTrialStatus, endedBy, reason string) (SkillTrial, error) {
	if status != TrialKept && status != TrialRolledBack {
		return SkillTrial{}, fmt.Errorf("%w: a trial ends kept or rolled back, not %q", ErrInvalidInput, status)
	}
	uid, err := parseUUID(id)
	if err != nil {
		return SkillTrial{}, err
	}
	row, err := s.q.EndSkillTrial(ctx, db.EndSkillTrialParams{ID: uid, Status: string(status), EndedBy: endedBy, Reason: reason})
	if errors.Is(err, pgx.ErrNoRows) {
		return SkillTrial{}, Conflicting("trialEnded", nil, "the trial has ended already")
	}
	if err != nil {
		return SkillTrial{}, fmt.Errorf("end trial %s: %w", id, err)
	}
	return toSkillTrial(row), nil
}

// SkillTrialUses counts the turns that used a skill since it last changed
// by how they ended: done, and failed.
func (s *Store) SkillTrialUses(ctx context.Context, skill string, since time.Time) (done, failed int, err error) {
	row, err := s.q.SkillTrialUses(ctx, db.SkillTrialUsesParams{Skill: skill, Since: pgtype.Timestamptz{Time: since, Valid: true}})
	if err != nil {
		return 0, 0, fmt.Errorf("count the uses of skill %s: %w", skill, err)
	}
	return int(row.Done), int(row.Failed), nil
}

// ListOpenSkillTrials returns the open trials of the skills named, oldest
// first.
func (s *Store) ListOpenSkillTrials(ctx context.Context, skills []string) ([]SkillTrial, error) {
	if len(skills) == 0 {
		return nil, nil
	}
	rows, err := s.q.ListOpenSkillTrials(ctx, skills)
	if err != nil {
		return nil, fmt.Errorf("list open skill trials: %w", err)
	}
	out := make([]SkillTrial, 0, len(rows))
	for _, row := range rows {
		out = append(out, toSkillTrial(row))
	}
	return out, nil
}

func toSkillTrial(row db.SkillTrial) SkillTrial {
	t := SkillTrial{
		ID: uuidString(row.ID), Skill: row.Skill, BaseSHA: row.BaseSha, StartedAt: row.StartedAt.Time, ChangedAt: row.ChangedAt.Time,
		TurnID: uuidString(row.TurnID), ChangedBy: row.ChangedBy, ProjectName: row.ProjectName, Changes: int(row.Changes),
		Status: SkillTrialStatus(row.Status), EndedBy: row.EndedBy, Reason: row.Reason,
	}
	if row.EndedAt.Valid {
		at := row.EndedAt.Time
		t.EndedAt = &at
	}
	return t
}
