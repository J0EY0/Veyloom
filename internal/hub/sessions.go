package hub

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// sessionStore is the session access the hub uses.
type sessionStore interface {
	GetOpenSession(ctx context.Context, memberID string) (store.MemberSession, error)
	StartSession(ctx context.Context, n store.NewMemberSession) (store.MemberSession, error)
	SetSessionRef(ctx context.Context, id, ref string) error
	LatestSession(ctx context.Context, memberID string) (store.MemberSession, error)
	AdvanceSession(ctx context.Context, id string, position int64, threadID string) error
	NoteSessionCompactions(ctx context.Context, id string, count int) error
}

// sessionFor returns the session the member's next turn runs in: the one it
// has open when that still fits where the member runs now, else a new one
// that replaces it. A member keeps one conversation with its runtime across
// the room and every topic it works in; the hub's records are the truth and
// the session a cache of them, so opening a new one is always safe.
//
// newSession says what a turn's session follows on, when it is a new one
// that follows on another.
type newSession struct {
	// Reason is why the earlier session ended; empty when the session is
	// resumed or is the member's first. The agent's brief says so.
	Reason store.SessionEndReason
	// Announce is set when the hub replaced the session by itself, which
	// the room is told: nobody asked for it. A person who asked for a new
	// session needs no telling.
	Announce bool
}

func (m *TurnManager) sessionFor(ctx context.Context, member store.Member, agent store.Agent) (store.MemberSession, newSession, error) {
	open, err := m.store.GetOpenSession(ctx, member.ID)
	var replaces store.SessionEndReason
	var follows newSession
	switch {
	case err == nil:
		if replaces = staleReason(open, member, agent); replaces == "" {
			return open, newSession{}, nil
		}
		follows = newSession{Reason: replaces, Announce: true}
		m.logger.Info("session replaced", "member", member.ID, "session", open.ID, "reason", replaces)
	case errors.Is(err, store.ErrNotFound):
		// None open. Had the member one before, it ended between turns: a
		// person asked for a new session. The agent is told all the same.
		if last, err := m.store.LatestSession(ctx, member.ID); err == nil {
			follows = newSession{Reason: last.EndReason}
		} else if !errors.Is(err, store.ErrNotFound) {
			return store.MemberSession{}, newSession{}, fmt.Errorf("find session: %w", err)
		}
	default:
		return store.MemberSession{}, newSession{}, fmt.Errorf("find session: %w", err)
	}
	session, err := m.store.StartSession(ctx, store.NewMemberSession{
		MemberID:  member.ID,
		Runtime:   agent.Runtime,
		MachineID: member.MachineID,
		WorkDir:   member.RepoPath,
		Replaces:  replaces,
	})
	if err != nil {
		return store.MemberSession{}, newSession{}, fmt.Errorf("start session: %w", err)
	}
	return session, follows, nil
}

// staleReason says why a member's open session cannot carry its next turn,
// or "" when it can. A session belongs to the runtime, the machine and the
// directory it was opened on: no runtime resumes another's session, session
// files do not travel between machines, and resuming in another directory
// fails or stalls depending on the CLI.
func staleReason(open store.MemberSession, member store.Member, agent store.Agent) store.SessionEndReason {
	switch {
	case open.Runtime != agent.Runtime:
		return store.SessionRuntimeChanged
	case open.MachineID != member.MachineID:
		return store.SessionMachineChanged
	case !sameDir(open.WorkDir, member.RepoPath):
		return store.SessionDirChanged
	}
	return ""
}

// sameDir compares two directories as written. The paths belong to the
// member's machine, which may not be this one, so there is no looking at
// the file system: a path reached through a different symlink counts as
// changed, and the price is a fresh session.
func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// sessionSpec is what a turn in session tells its runtime.
func sessionSpec(session store.MemberSession) runtime.Session {
	return runtime.Session{Key: session.ID, Ref: session.Ref, Resume: session.Started()}
}

// noteSessionRef records the runtime's own reference to the turn's session
// as soon as the runtime reports it, so a turn that fails or is cancelled
// later still leaves a session the next turn resumes. The write runs on the
// turn's executor: the connection loop never waits on the database.
func (m *TurnManager) noteSessionRef(at *activeTurn, ref string) {
	at.mu.Lock()
	known := ref == "" || ref == at.sessionRef
	if !known {
		at.sessionRef = ref
	}
	// A session on trial has no row yet: the reference waits with it.
	trial := at.fresh != nil
	if trial {
		at.fresh.ref = ref
	}
	at.mu.Unlock()
	if known || trial {
		return
	}
	at.enqueue(func() { m.saveSessionRef(at, ref) })
}

// saveSessionRef stores ref on the turn's session. Runs on the executor.
func (m *TurnManager) saveSessionRef(at *activeTurn, ref string) {
	if at.turn.SessionID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()
	if err := m.store.SetSessionRef(ctx, at.turn.SessionID, ref); err != nil {
		m.logger.Error("save session ref", "member", at.member.ID, "session", at.turn.SessionID, "err", err)
	}
}

// sessionEndPhrase says why a session ended in words that fit a sentence,
// for the agent's brief and the note in the room.
func sessionEndPhrase(reason store.SessionEndReason) string {
	switch reason {
	case store.SessionRuntimeChanged:
		return "the agent now runs on a different runtime"
	case store.SessionMachineChanged:
		return "the agent now runs on a different machine"
	case store.SessionDirChanged:
		return "its working directory changed"
	case store.SessionNotFound:
		return "the runtime no longer has it"
	case store.SessionContextOverflow:
		return "it outgrew the model's context window"
	case store.SessionManual:
		return "a person asked for a new one"
	default:
		return "resuming it failed"
	}
}

// newSessionNote tells the room that a member starts over, so nobody
// wonders why it has forgotten what it did an hour ago.
func newSessionNote(name string, reason store.SessionEndReason) string {
	return fmt.Sprintf("%s started a new session (%s) and no longer remembers its earlier turns; the chat history is still here for it.", name, sessionEndPhrase(reason))
}

// freshSession is a session on trial: the one a turn is run again in after
// its own would not resume. It becomes the member's session only once that
// second run has succeeded. Until then it has no row, and the session that
// failed stays the member's: a failure that was never about the session
// (credentials, the network) then costs the member nothing.
type freshSession struct {
	id     string
	reason store.SessionEndReason
	// ref is what the runtime calls it, kept here until there is a row.
	ref string
}

// endReasonOf is why a session that would not resume ends, from what the
// runtime said of the failure.
func endReasonOf(kind runtime.FailureKind) store.SessionEndReason {
	switch kind {
	case runtime.FailureSessionNotFound:
		return store.SessionNotFound
	case runtime.FailureContextOverflow:
		return store.SessionContextOverflow
	default:
		return store.SessionResumeFailed
	}
}

// retryFresh decides whether a turn that just failed is run again in a new
// session, and if so sets that going and reports true: the turn is not over.
//
// The test is how the turn behaved, not what its error says: it resumed a
// session and failed before the agent said or did anything. Then nothing
// is lost by starting over, and nothing is done twice. A turn gets one
// such second run. Failures the hub produced itself (the machine is gone,
// the brief could not be built) are not retried: a new session changes
// nothing about them.
func (m *TurnManager) retryFresh(at *activeTurn, done protocol.TurnDone) bool {
	if done.Error == "" || done.Cancelled {
		return false
	}
	at.mu.Lock()
	acted := at.output.Len() > 0 || at.toolActivity || at.segments > 0
	ok := at.resumed && !acted && !at.retried && !at.noRetry && !at.closed
	if ok {
		at.retried = true
	}
	at.mu.Unlock()
	if !ok {
		return false
	}
	return at.enqueue(func() { m.rerun(at, done) })
}

// rerun runs the turn again in a new session, with a brief that tells the
// agent so. Runs on the turn's executor. When the second run cannot even be
// started the turn ends with the failure of the first.
func (m *TurnManager) rerun(at *activeTurn, first protocol.TurnDone) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()
	giveUp := func(why string, err error) {
		m.logger.Error("retry in a new session", "turn", at.turn.ID, "step", why, "err", err)
		m.OnDone(at.turn.ID, first)
	}

	fresh := &freshSession{id: store.NewID(), reason: endReasonOf(first.Result.Failure)}
	m.logger.Info("session would not resume; running the turn again in a new one",
		"member", at.member.ID, "turn", at.turn.ID, "session", at.turn.SessionID, "reason", fresh.reason, "err", first.Error)

	if fresh.reason != store.SessionResumeFailed {
		// The runtime itself declared the session beyond use: it ends now,
		// whatever becomes of the second run.
		if err := m.adopt(ctx, at, fresh); err != nil {
			giveUp("replace session", err)
			return
		}
		m.postSystem(ctx, at.thread, at.turn.ID, newSessionNote(at.member.DisplayName, fresh.reason))
		fresh = nil
	}

	// A session that has read nothing: the brief is the whole story.
	b, err := m.brief.Build(ctx, briefInput{Member: at.member, Thread: at.thread, Triggers: at.triggers, NewSession: endReasonOf(first.Result.Failure)})
	if err != nil {
		giveUp("brief", err)
		return
	}
	spec := at.spec
	spec.Prompt = b.Prompt
	spec.Session = runtime.Session{Key: at.turn.SessionID}
	if fresh != nil {
		spec.Session = runtime.Session{Key: fresh.id}
	}

	at.mu.Lock()
	at.fresh = fresh
	at.resumed = false
	at.sessionRef = ""
	at.position = b.Position
	at.spent = at.spent.Plus(first.Result.Usage)
	if at.transcript != nil {
		if err := at.transcript.write(transcriptLine{Kind: "restart", TurnID: at.turn.ID, Runtime: at.agent.Runtime, Spec: &spec, Error: first.Error}); err != nil {
			m.logger.Warn("transcript", "turn", at.turn.ID, "err", err)
		}
	}
	at.mu.Unlock()

	note := runtime.Event{Kind: runtime.EventStatus, At: time.Now(), Text: "could not resume the session; starting a new one"}
	m.publish(Event{Kind: EventTurnEvent, RoomID: at.thread.RoomID, At: note.At, TurnID: at.turn.ID, TurnEvent: &note})

	conn, ok := m.connFor(at.member.MachineID)
	if !ok {
		giveUp("dispatch", errors.New("machine is offline"))
		return
	}
	if err := conn.Send(ctx, protocol.StartTurn{TurnID: at.turn.ID, Runtime: at.agent.Runtime, Spec: spec}); err != nil {
		giveUp("dispatch", err)
	}
}

// adopt makes a session on trial the member's session: the one that would
// not resume ends, for the reason found, and the turn moves to the new one.
func (m *TurnManager) adopt(ctx context.Context, at *activeTurn, fresh *freshSession) error {
	session, err := m.store.StartSession(ctx, store.NewMemberSession{
		ID:        fresh.id,
		MemberID:  at.member.ID,
		Runtime:   at.agent.Runtime,
		MachineID: at.member.MachineID,
		WorkDir:   at.member.RepoPath,
		Ref:       fresh.ref,
		Replaces:  fresh.reason,
	})
	if err != nil {
		return err
	}
	if err := m.store.SetTurnSession(ctx, at.turn.ID, session.ID); err != nil {
		// The session is the member's all the same; only the turn's
		// pointer to it is stale.
		m.logger.Error("move turn to its new session", "turn", at.turn.ID, "session", session.ID, "err", err)
	}
	at.mu.Lock()
	at.turn.SessionID = session.ID
	at.mu.Unlock()
	return nil
}

// settleReading records, once a turn is over, how far its session has read
// and what the runtime did to it. Runs on the turn's executor.
//
// The positions move up to where the room stood when the brief was put
// together, not to where it stands now: what others said while the turn ran
// is still news next time. They move only when the turn said or did
// something, which is how the hub knows the session took the brief in; a
// turn that failed before that leaves them, and the next brief tells it
// all again. Compactions the runtime reported are counted either way.
func (m *TurnManager) settleReading(ctx context.Context, at *activeTurn) {
	at.mu.Lock()
	sessionID, position := at.turn.SessionID, at.position
	acted := at.output.Len() > 0 || at.toolActivity || at.segments > 0
	compactions := at.compactions
	at.mu.Unlock()
	if sessionID == "" {
		return
	}
	if acted && position > 0 {
		if err := m.store.AdvanceSession(ctx, sessionID, position, at.thread.ID); err != nil {
			m.logger.Error("advance session", "session", sessionID, "err", err)
		}
	}
	// After the positions: a compaction forgets what was read of each
	// topic, this one included, so the topic is shown in full next time.
	if err := m.store.NoteSessionCompactions(ctx, sessionID, compactions); err != nil {
		m.logger.Error("note compactions", "session", sessionID, "err", err)
	}
}
