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

// SessionEndReason says why a member's session is over.
type SessionEndReason string

// The reasons a session ends. The first three are found when a turn starts
// and the member no longer matches what the session was opened on; the next
// three come from a turn that could not resume it; the last two are people's
// doing.
const (
	SessionRuntimeChanged  SessionEndReason = "runtime_changed"
	SessionMachineChanged  SessionEndReason = "machine_changed"
	SessionDirChanged      SessionEndReason = "dir_changed"
	SessionNotFound        SessionEndReason = "not_found"
	SessionContextOverflow SessionEndReason = "context_overflow"
	SessionResumeFailed    SessionEndReason = "resume_failed"
	SessionManual          SessionEndReason = "manual"
	SessionMemberRemoved   SessionEndReason = "member_removed"
)

// MemberSession is one conversation of a member with its runtime. A member
// has at most one open at a time and every turn resumes it. The hub's
// records are the truth and a session is a cache of them: ending one and
// opening the next loses nothing that cannot be rebuilt.
type MemberSession struct {
	// ID is also the name a runtime that can be told its session id is
	// given: Claude Code's --session-id, the name of Pi's session file.
	ID       string `json:"id"`
	MemberID string `json:"member_id"`
	// Runtime, MachineID and WorkDir are what the session was opened on. A
	// turn whose member no longer matches all three opens a new session.
	Runtime   string `json:"runtime"`
	MachineID string `json:"machine_id"`
	WorkDir   string `json:"work_dir"`
	// Ref is the runtime's own reference, stored as soon as the runtime
	// reports it. Empty means the session has not run a turn yet.
	Ref string `json:"session_ref,omitempty"`
	// RoomSeen and ThreadSeen are how far the session has read, as
	// messages.seq: where the room stood when its last brief was put
	// together, and the same per topic it was briefed in. A brief tells a
	// session only what came after; a topic missing from ThreadSeen is
	// shown in full. A new session has read nothing, which makes its first
	// brief the whole story.
	RoomSeen   int64            `json:"room_seen"`
	ThreadSeen map[string]int64 `json:"thread_seen,omitempty"`
	// WikiSeen is where the project wiki stood when the session was last
	// shown its catalog (see Reading.Wiki): the next brief lists only the
	// pages changed since. Nil until the first, and again after a
	// compaction, so that the next brief lists them all.
	WikiSeen *time.Time `json:"wiki_seen,omitempty"`
	// Compactions counts the times the runtime compacted the session.
	Compactions int              `json:"compactions"`
	StartedAt   time.Time        `json:"started_at"`
	EndedAt     *time.Time       `json:"ended_at,omitempty"`
	EndReason   SessionEndReason `json:"end_reason,omitempty"`
}

// Started reports whether the runtime has acknowledged the session, which
// is what makes a turn resume it rather than create it.
func (s MemberSession) Started() bool { return s.Ref != "" }

// NewMemberSession is the input to StartSession.
type NewMemberSession struct {
	// ID names the session; empty lets the store pick. The hub picks when
	// it has already handed the id to a runtime, before the row exists.
	ID        string
	MemberID  string
	Runtime   string
	MachineID string
	WorkDir   string
	// Ref is set when the runtime reported its reference before the row
	// existed.
	Ref string
	// Replaces says why the member's open session, if it has one, ends
	// here. Empty means the member is expected to have none open; when it
	// does, StartSession is ErrConflict.
	Replaces SessionEndReason
}

// GetOpenSession returns the session a member's next turn resumes, or
// ErrNotFound when it has none.
func (s *Store) GetOpenSession(ctx context.Context, memberID string) (MemberSession, error) {
	mid, err := parseUUID(memberID)
	if err != nil {
		return MemberSession{}, err
	}
	row, err := s.q.GetOpenSession(ctx, mid)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberSession{}, fmt.Errorf("open session of member %s: %w", memberID, ErrNotFound)
	}
	if err != nil {
		return MemberSession{}, fmt.Errorf("open session of member %s: %w", memberID, err)
	}
	return toMemberSession(row), nil
}

// GetSession returns one session, or ErrNotFound.
func (s *Store) GetSession(ctx context.Context, id string) (MemberSession, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return MemberSession{}, err
	}
	row, err := s.q.GetSession(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return MemberSession{}, fmt.Errorf("session %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return MemberSession{}, fmt.Errorf("get session %s: %w", id, err)
	}
	return toMemberSession(row), nil
}

// StartSession opens a session for a member. The one it replaces, if any,
// ends in the same transaction, so the member never has two open nor, to a
// reader in between, none.
func (s *Store) StartSession(ctx context.Context, n NewMemberSession) (MemberSession, error) {
	if n.ID == "" {
		n.ID = NewID()
	}
	id, err := parseUUID(n.ID)
	if err != nil {
		return MemberSession{}, err
	}
	memberID, err := parseUUID(n.MemberID)
	if err != nil {
		return MemberSession{}, err
	}
	machineID, err := parseUUID(n.MachineID)
	if err != nil {
		return MemberSession{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MemberSession{}, fmt.Errorf("start session: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	q := s.q.WithTx(tx)

	if n.Replaces != "" {
		if _, err := q.EndOpenSession(ctx, db.EndOpenSessionParams{MemberID: memberID, EndReason: string(n.Replaces)}); err != nil {
			return MemberSession{}, mapPGError("end session", err)
		}
	}
	row, err := q.CreateSession(ctx, db.CreateSessionParams{
		ID:         id,
		MemberID:   memberID,
		Runtime:    n.Runtime,
		MachineID:  machineID,
		WorkDir:    n.WorkDir,
		SessionRef: n.Ref,
	})
	if err != nil {
		return MemberSession{}, mapPGError("start session", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return MemberSession{}, fmt.Errorf("start session: %w", err)
	}
	return toMemberSession(row), nil
}

// EndOpenSession ends the member's open session and says why. A member
// with none open is left alone: there is nothing to end.
func (s *Store) EndOpenSession(ctx context.Context, memberID string, reason SessionEndReason) error {
	mid, err := parseUUID(memberID)
	if err != nil {
		return err
	}
	if _, err := s.q.EndOpenSession(ctx, db.EndOpenSessionParams{MemberID: mid, EndReason: string(reason)}); err != nil {
		return mapPGError("end session", err)
	}
	return nil
}

// SetSessionRef stores the runtime's own reference to a session.
func (s *Store) SetSessionRef(ctx context.Context, id, ref string) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	n, err := s.q.SetSessionRef(ctx, db.SetSessionRefParams{ID: uid, SessionRef: ref})
	if err != nil {
		return fmt.Errorf("set ref of session %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("session %s: %w", id, ErrNotFound)
	}
	return nil
}

// ListMemberSessions returns every session a member has had, newest first.
func (s *Store) ListMemberSessions(ctx context.Context, memberID string) ([]MemberSession, error) {
	mid, err := parseUUID(memberID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListMemberSessions(ctx, mid)
	if err != nil {
		return nil, fmt.Errorf("list sessions of member %s: %w", memberID, err)
	}
	out := make([]MemberSession, 0, len(rows))
	for _, row := range rows {
		out = append(out, toMemberSession(row))
	}
	return out, nil
}

// Reading is how far a turn's brief took its session, recorded once the
// session has taken the brief in.
type Reading struct {
	// Position is where the room stood when the brief was put together, as
	// messages.seq; the room's position and that of the topic ThreadID,
	// the one the turn was in, move up to it.
	Position int64
	ThreadID string
	// Wiki is where the project wiki stood: its newest change the brief
	// knew of. Zero when the brief showed no wiki, which leaves the
	// session's position in it as it was.
	Wiki time.Time
}

// AdvanceSession moves a session's reading positions forward to where
// things stood when the brief of a turn was put together. Positions never
// move back.
func (s *Store) AdvanceSession(ctx context.Context, id string, r Reading) error {
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	if _, err := parseUUID(r.ThreadID); err != nil {
		return err
	}
	n, err := s.q.AdvanceSession(ctx, db.AdvanceSessionParams{
		ID: uid, RoomSeen: r.Position, ThreadID: r.ThreadID, ThreadSeen: r.Position,
		WikiSeen: pgtype.Timestamptz{Time: r.Wiki, Valid: !r.Wiki.IsZero()},
	})
	if err != nil {
		return fmt.Errorf("advance session %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("session %s: %w", id, ErrNotFound)
	}
	return nil
}

// NoteSessionCompactions counts compactions the runtime reported. A
// compaction forgets detail, so what the session had read of each topic is
// forgotten with it: topics are shown in full again the next time the
// session is briefed in them, and so is the wiki's catalog. Its place in
// the room stays.
func (s *Store) NoteSessionCompactions(ctx context.Context, id string, count int) error {
	if count <= 0 {
		return nil
	}
	uid, err := parseUUID(id)
	if err != nil {
		return err
	}
	n, err := s.q.NoteSessionCompactions(ctx, db.NoteSessionCompactionsParams{ID: uid, Count: int32(count)})
	if err != nil {
		return fmt.Errorf("note compactions of session %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("session %s: %w", id, ErrNotFound)
	}
	return nil
}

// SessionTurns returns how many turns have run in a session.
func (s *Store) SessionTurns(ctx context.Context, id string) (int, error) {
	uid, err := parseUUID(id)
	if err != nil {
		return 0, err
	}
	n, err := s.q.CountSessionTurns(ctx, uid)
	if err != nil {
		return 0, fmt.Errorf("count turns of session %s: %w", id, err)
	}
	return int(n), nil
}

// LatestSession returns the member's most recent session, open or ended, or
// ErrNotFound when it never had one.
func (s *Store) LatestSession(ctx context.Context, memberID string) (MemberSession, error) {
	all, err := s.ListMemberSessions(ctx, memberID)
	if err != nil {
		return MemberSession{}, err
	}
	if len(all) == 0 {
		return MemberSession{}, fmt.Errorf("sessions of member %s: %w", memberID, ErrNotFound)
	}
	return all[0], nil
}

// ResetSession ends the member's open session because a person asked for a
// new one: its next turn starts over, told what the hub has on record. It
// is ErrConflict while one of the member's turns is running, which would
// go on in a session that is no longer the member's; with no session open
// there is nothing to do, and that is fine.
func (s *Store) ResetSession(ctx context.Context, memberID string) error {
	mid, err := parseUUID(memberID)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("reset session: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // a no-op after Commit
	q := s.q.WithTx(tx)

	busy, err := q.MemberHasRunningTurn(ctx, mid)
	if err != nil {
		return fmt.Errorf("reset session of member %s: %w", memberID, err)
	}
	if busy {
		return fmt.Errorf("member %s: %w", memberID, stillRunning())
	}
	if _, err := q.EndOpenSession(ctx, db.EndOpenSessionParams{MemberID: mid, EndReason: string(SessionManual)}); err != nil {
		return mapPGError("reset session", err)
	}
	return tx.Commit(ctx)
}

func toMemberSession(row db.MemberSession) MemberSession {
	out := MemberSession{
		ID:          uuidString(row.ID),
		MemberID:    uuidString(row.MemberID),
		Runtime:     row.Runtime,
		MachineID:   uuidString(row.MachineID),
		WorkDir:     row.WorkDir,
		Ref:         row.SessionRef,
		RoomSeen:    row.RoomSeen,
		Compactions: int(row.Compactions),
		StartedAt:   row.StartedAt.Time,
		EndReason:   SessionEndReason(row.EndReason),
	}
	// The column is written by this package alone; should it ever hold
	// something else, the session has simply read no topic.
	_ = json.Unmarshal(row.ThreadSeen, &out.ThreadSeen)
	if row.WikiSeen.Valid {
		seen := row.WikiSeen.Time
		out.WikiSeen = &seen
	}
	if row.EndedAt.Valid {
		ended := row.EndedAt.Time
		out.EndedAt = &ended
	}
	return out
}
