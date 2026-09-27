package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// DraftKind is what a draft has a person do (docs/design.md 5.23.5).
type DraftKind string

const (
	// DraftMerge puts a member's work on the main line.
	DraftMerge DraftKind = "merge"
	// DraftSetAside gives a member's work up, archived.
	DraftSetAside DraftKind = "set_aside"
	// DraftInstallSkill installs a skill of the library for a member.
	DraftInstallSkill DraftKind = "install_skill"
	// DraftSetupSteps adopts the steps new worktrees are got ready with.
	DraftSetupSteps DraftKind = "setup_steps"
)

// DraftStatus is where a draft stands.
type DraftStatus string

const (
	DraftPending DraftStatus = "pending"
	// DraftRunning: a person runs it now.
	DraftRunning DraftStatus = "running"
	DraftDone    DraftStatus = "done"
	// DraftConflicted: a merge that met conflicts, nothing changed.
	DraftConflicted DraftStatus = "conflicted"
	DraftDeclined   DraftStatus = "declined"
	// DraftSuperseded: the member drafted the same thing anew.
	DraftSuperseded DraftStatus = "superseded"
)

// Settled says a draft is over: nothing more is done with it.
func (s DraftStatus) Settled() bool {
	return s != DraftPending && s != DraftRunning
}

// DraftParams are what a draft's kind needs.
type DraftParams struct {
	// Message is a merge's commit message.
	Message string `json:"message,omitempty"`
	// Reason says why work is given up.
	Reason string `json:"reason,omitempty"`
	// Skill is the skill to install.
	Skill string `json:"skill,omitempty"`
	// Steps are the setup steps to adopt.
	Steps *WorkspaceSteps `json:"steps,omitempty"`
}

// DraftResult is what came of running a draft.
type DraftResult struct {
	// Commit is the merge's on the main line, and Unsettled why the
	// worktree did not start over after it; Ref is where work given up is
	// kept; Conflicts the files a merge met conflicts in.
	Commit    string   `json:"commit,omitempty"`
	Unsettled string   `json:"unsettled,omitempty"`
	Ref       string   `json:"ref,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`
}

// Draft is something a member drafted for a person to do with one press
// (docs/design.md 5.23.5); the card in its topic is drawn from it.
type Draft struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	RoomID    string `json:"room_id"`
	ThreadID  string `json:"thread_id"`
	// MemberID drafted it, in TurnID.
	MemberID string    `json:"member_id"`
	TurnID   string    `json:"turn_id,omitempty"`
	Kind     DraftKind `json:"kind"`
	// TargetID is the member whose work or agent it acts on; empty for
	// setup steps.
	TargetID string      `json:"target_id,omitempty"`
	Subject  string      `json:"subject"`
	Params   DraftParams `json:"params"`
	// Then is what the member does once it is done: set, it is woken then.
	Then   string      `json:"then,omitempty"`
	Status DraftStatus `json:"status"`
	Result DraftResult `json:"result"`
	// MessageID is the card; ResultMessageID told the member what came of
	// it.
	MessageID       string `json:"message_id,omitempty"`
	ResultMessageID string `json:"result_message_id,omitempty"`
	// DecidedBy is the person who ran it or turned it down.
	DecidedBy string     `json:"decided_by,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	SettledAt *time.Time `json:"settled_at,omitempty"`
}

// NewDraft is what CreateDraft records.
type NewDraft struct {
	ProjectID, RoomID, ThreadID, MemberID, TurnID string
	Kind                                          DraftKind
	TargetID                                      string
	Subject                                       string
	Params                                        DraftParams
	Then                                          string
}

// CreateDraft records a pending draft, and has the project's pending ones
// about the same subject give way to it, which it returns besides.
func (s *Store) CreateDraft(ctx context.Context, d NewDraft) (Draft, []Draft, error) {
	project, err := parseUUID(d.ProjectID)
	if err != nil {
		return Draft{}, nil, err
	}
	room, err := parseUUID(d.RoomID)
	if err != nil {
		return Draft{}, nil, err
	}
	thread, err := parseUUID(d.ThreadID)
	if err != nil {
		return Draft{}, nil, err
	}
	member, err := parseUUID(d.MemberID)
	if err != nil {
		return Draft{}, nil, err
	}
	turn, err := optionalUUID(d.TurnID)
	if err != nil {
		return Draft{}, nil, err
	}
	target, err := optionalUUID(d.TargetID)
	if err != nil {
		return Draft{}, nil, err
	}
	params, err := json.Marshal(d.Params)
	if err != nil {
		return Draft{}, nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Draft{}, nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	q := s.q.WithTx(tx)
	// Drafted at once by two members, one waits for the other, then has
	// it give way, as the database keeps one pending (drafts_one_pending).
	if err := q.LockDraftSubject(ctx, db.LockDraftSubjectParams{ProjectID: d.ProjectID, Subject: d.Subject}); err != nil {
		return Draft{}, nil, fmt.Errorf("hold the drafts about %s: %w", d.Subject, err)
	}
	gone, err := q.SupersedeDrafts(ctx, db.SupersedeDraftsParams{ProjectID: project, Subject: d.Subject})
	if err != nil {
		return Draft{}, nil, fmt.Errorf("supersede drafts: %w", err)
	}
	row, err := q.CreateDraft(ctx, db.CreateDraftParams{
		ProjectID: project, RoomID: room, ThreadID: thread, MemberID: member, TurnID: turn,
		Kind: string(d.Kind), TargetID: target, Subject: d.Subject, Params: params, ThenNote: d.Then,
	})
	if err != nil {
		return Draft{}, nil, fmt.Errorf("create a draft: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Draft{}, nil, err
	}
	return draftFromRow(row), draftsFromRows(gone), nil
}

// OpenDraft reads the project's draft about subject that is not settled
// yet; ErrNotFound when there is none.
func (s *Store) OpenDraft(ctx context.Context, projectID, subject string) (Draft, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return Draft{}, err
	}
	return s.draftRow(s.q.OpenDraft(ctx, db.OpenDraftParams{ProjectID: pid, Subject: subject}))
}

// SupersedeDrafts has the project's pending drafts about subject give way:
// what they were about was settled otherwise. It returns them.
func (s *Store) SupersedeDrafts(ctx context.Context, projectID, subject string) ([]Draft, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.SupersedeDrafts(ctx, db.SupersedeDraftsParams{ProjectID: pid, Subject: subject})
	if err != nil {
		return nil, fmt.Errorf("supersede drafts: %w", err)
	}
	return draftsFromRows(rows), nil
}

// SetDraftMessage records the card that shows a draft.
func (s *Store) SetDraftMessage(ctx context.Context, id, messageID string) error {
	did, mid, err := draftLink(id, messageID)
	if err != nil {
		return err
	}
	if err := s.q.SetDraftMessage(ctx, db.SetDraftMessageParams{ID: did, MessageID: mid}); err != nil {
		return fmt.Errorf("draft %s: %w", id, err)
	}
	return nil
}

// SetDraftResultMessage records the message that told the member what came
// of its draft.
func (s *Store) SetDraftResultMessage(ctx context.Context, id, messageID string) error {
	did, mid, err := draftLink(id, messageID)
	if err != nil {
		return err
	}
	if err := s.q.SetDraftResultMessage(ctx, db.SetDraftResultMessageParams{ID: did, MessageID: mid}); err != nil {
		return fmt.Errorf("draft %s: %w", id, err)
	}
	return nil
}

// draftLink parses a draft's id and a message's.
func draftLink(id, messageID string) (pgtype.UUID, pgtype.UUID, error) {
	did, err := parseUUID(id)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	mid, err := parseUUID(messageID)
	return did, mid, err
}

// GetDraft reads a draft.
func (s *Store) GetDraft(ctx context.Context, id string) (Draft, error) {
	did, err := parseUUID(id)
	if err != nil {
		return Draft{}, err
	}
	return s.draftRow(s.q.GetDraft(ctx, did))
}

// GetDraftByMessage reads the draft a message is the card or the outcome
// of; ErrNotFound when it is neither.
func (s *Store) GetDraftByMessage(ctx context.Context, messageID string) (Draft, error) {
	mid, err := parseUUID(messageID)
	if err != nil {
		return Draft{}, err
	}
	return s.draftRow(s.q.GetDraftByMessage(ctx, mid))
}

// ListThreadDrafts lists the drafts of a topic, in the order drafted.
func (s *Store) ListThreadDrafts(ctx context.Context, threadID string) ([]Draft, error) {
	tid, err := parseUUID(threadID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListThreadDrafts(ctx, tid)
	if err != nil {
		return nil, fmt.Errorf("list the drafts of topic %s: %w", threadID, err)
	}
	return draftsFromRows(rows), nil
}

// CountTurnDrafts counts the drafts a turn made.
func (s *Store) CountTurnDrafts(ctx context.Context, turnID string) (int, error) {
	tid, err := parseUUID(turnID)
	if err != nil {
		return 0, err
	}
	n, err := s.q.CountTurnDrafts(ctx, tid)
	if err != nil {
		return 0, fmt.Errorf("count the drafts of turn %s: %w", turnID, err)
	}
	return int(n), nil
}

// ClaimDraft has a person's run of a pending draft begin: one at a time.
// ErrNotFound when it is not pending.
func (s *Store) ClaimDraft(ctx context.Context, id string) (Draft, error) {
	did, err := parseUUID(id)
	if err != nil {
		return Draft{}, err
	}
	return s.draftRow(s.q.ClaimDraft(ctx, did))
}

// ReleaseDraft has a draft whose run could not be done for now wait
// again. ErrNotFound when it is not running.
func (s *Store) ReleaseDraft(ctx context.Context, id string) (Draft, error) {
	did, err := parseUUID(id)
	if err != nil {
		return Draft{}, err
	}
	return s.draftRow(s.q.ReleaseDraft(ctx, did))
}

// ReleaseRunningDrafts has the runs a stopped hub left under way wait
// again. The hub calls it as it starts.
func (s *Store) ReleaseRunningDrafts(ctx context.Context) ([]Draft, error) {
	rows, err := s.q.ReleaseRunningDrafts(ctx)
	if err != nil {
		return nil, fmt.Errorf("release running drafts: %w", err)
	}
	return draftsFromRows(rows), nil
}

// SettleDraft records what came of a run, by the person userID: done, or
// conflicted. ErrNotFound when it is not running.
func (s *Store) SettleDraft(ctx context.Context, id string, status DraftStatus, result DraftResult, userID string) (Draft, error) {
	did, err := parseUUID(id)
	if err != nil {
		return Draft{}, err
	}
	by, err := optionalUUID(userID)
	if err != nil {
		return Draft{}, err
	}
	res, err := json.Marshal(result)
	if err != nil {
		return Draft{}, err
	}
	return s.draftRow(s.q.SettleDraft(ctx, db.SettleDraftParams{ID: did, Status: string(status), Result: res, DecidedBy: by}))
}

// DeclineDraft turns a pending draft down, by the person userID.
// ErrNotFound when it is not pending.
func (s *Store) DeclineDraft(ctx context.Context, id, userID string) (Draft, error) {
	did, err := parseUUID(id)
	if err != nil {
		return Draft{}, err
	}
	by, err := optionalUUID(userID)
	if err != nil {
		return Draft{}, err
	}
	return s.draftRow(s.q.DeclineDraft(ctx, db.DeclineDraftParams{ID: did, DecidedBy: by}))
}

// draftRow turns a query's one row into a draft, no row into ErrNotFound.
func (s *Store) draftRow(row db.Draft, err error) (Draft, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return Draft{}, fmt.Errorf("draft: %w", ErrNotFound)
	}
	if err != nil {
		return Draft{}, fmt.Errorf("draft: %w", err)
	}
	return draftFromRow(row), nil
}

func draftsFromRows(rows []db.Draft) []Draft {
	out := make([]Draft, len(rows))
	for i, row := range rows {
		out[i] = draftFromRow(row)
	}
	return out
}

func draftFromRow(row db.Draft) Draft {
	d := Draft{
		ID: uuidString(row.ID), ProjectID: uuidString(row.ProjectID), RoomID: uuidString(row.RoomID), ThreadID: uuidString(row.ThreadID),
		MemberID: uuidString(row.MemberID), TurnID: uuidString(row.TurnID), Kind: DraftKind(row.Kind), TargetID: uuidString(row.TargetID),
		Subject: row.Subject, Then: row.ThenNote, Status: DraftStatus(row.Status),
		MessageID: uuidString(row.MessageID), ResultMessageID: uuidString(row.ResultMessageID), DecidedBy: uuidString(row.DecidedBy),
		CreatedAt: row.CreatedAt.Time,
	}
	// Written by CreateDraft and SettleDraft alone, from these very types.
	_ = json.Unmarshal(row.Params, &d.Params)
	_ = json.Unmarshal(row.Result, &d.Result)
	if row.SettledAt.Valid {
		at := row.SettledAt.Time
		d.SettledAt = &at
	}
	return d
}
