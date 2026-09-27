package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/J0EY0/veyloom/internal/runtime"
)

// A piece of work as its page shows it (docs/webui.md 4.20): what the
// person asked, its turns one after another with what each did, spent and
// waited for a person, and what became of the branches it changed.

// Work is a piece of work: the turns a person's message set going, in
// whichever topics.
type Work struct {
	// Chain is the message the person began it with, in RoomID.
	Chain  string `json:"chain"`
	RoomID string `json:"room_id"`
	// ThreadID and ThreadNumber are the topic it began in.
	ThreadID     string `json:"thread_id"`
	ThreadNumber int    `json:"thread_number"`
	// Title is what the person asked, in a line; Ask the whole of it,
	// without the @s it began with.
	Title string `json:"title"`
	Ask   string `json:"ask"`
	// AskedBy is the person who asked; Asked the members they asked.
	AskedBy string   `json:"asked_by,omitempty"`
	Asked   []string `json:"asked"`
	Running bool     `json:"running"`
	// StartedAt is when its first turn began; EndedAt when its last one
	// ended, once none runs.
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Turns     []WorkTurn `json:"turns"`
	// Usage is what all its turns spent; WaitedMS how long they waited
	// for people, in all.
	Usage    runtime.Usage `json:"usage"`
	WaitedMS int64         `json:"waited_ms"`
	// Events are what became of the branches it changed, since.
	Events []WorkEvent `json:"events"`
}

// What a turn of a piece of work did, as its row says it.
const (
	// WorkTurnTask: it began its member's task, Title.
	WorkTurnTask = "task"
	// WorkTurnSplit: it began the task a person asked for, and handed
	// parts of it on to Woke.
	WorkTurnSplit = "split"
	// WorkTurnHandOff: a later turn of its task handed work on to Woke.
	WorkTurnHandOff = "handoff"
	// WorkTurnSumUp: it summed up what came of the work its task handed on.
	WorkTurnSumUp = "sumup"
	// WorkTurnContinue: a later turn of its task, woken again, by Title.
	WorkTurnContinue = "continue"
)

// WorkTurn is one turn of a piece of work.
type WorkTurn struct {
	ID           string        `json:"id"`
	MemberID     string        `json:"member_id"`
	ThreadID     string        `json:"thread_id"`
	ThreadNumber int           `json:"thread_number"`
	Status       TurnStatus    `json:"status"`
	Error        string        `json:"error,omitempty"`
	StartedAt    time.Time     `json:"started_at"`
	EndedAt      *time.Time    `json:"ended_at,omitempty"`
	Usage        runtime.Usage `json:"usage"`
	// Kind is what the turn did, one of the WorkTurn constants; Title the
	// task it began or what woke it again; Woke the members it handed work
	// on to.
	Kind  string   `json:"kind"`
	Title string   `json:"title,omitempty"`
	Woke  []string `json:"woke,omitempty"`
	// Relay says it only handed work on: the quieter row.
	Relay bool `json:"relay,omitempty"`
	// Waits are the stretches it waited for a person to decide, merged
	// where they overlap; WaitedMS their length in all.
	Waits    []WorkWait `json:"waits,omitempty"`
	WaitedMS int64      `json:"waited_ms"`
	Files    int        `json:"files"`
}

// WorkWait is a stretch a turn waited for a person; To is empty while it
// still does.
type WorkWait struct {
	From time.Time  `json:"from"`
	To   *time.Time `json:"to,omitempty"`
}

// WorkEvent is what became of branches the work changed: merged onto the
// main line in Commit, or reset with their work archived under Ref, with
// whose work it was.
type WorkEvent struct {
	Kind    BranchEventKind `json:"kind"`
	Commit  string          `json:"commit,omitempty"`
	Ref     string          `json:"ref,omitempty"`
	Members []string        `json:"members"`
	At      time.Time       `json:"at"`
}

// ChainWait is a request a person was asked in a piece of work: raised and,
// once they decided or it went unanswered, settled.
type ChainWait struct {
	TurnID    string
	CreatedAt time.Time
	DecidedAt *time.Time
}

// GetWork returns a piece of work by the message a person began it with;
// ErrNotFound for a message that set no turn going.
func (s *Store) GetWork(ctx context.Context, chain string) (Work, error) {
	cid, err := parseUUID(chain)
	if err != nil {
		return Work{}, err
	}
	ask, err := s.GetMessage(ctx, chain)
	if err != nil {
		return Work{}, err
	}
	rows, err := s.q.ListChainTurns(ctx, cid)
	if err != nil {
		return Work{}, fmt.Errorf("turns of work %s: %w", chain, err)
	}
	if len(rows) == 0 {
		return Work{}, fmt.Errorf("work %s: %w", chain, ErrNotFound)
	}
	turns := make([]TaskTurn, len(rows))
	for i, row := range rows {
		turns[i] = TaskTurn{Turn: toTurn(row.Turn), ThreadNumber: int(row.ThreadNumber), TriggerBody: row.TriggerBody, TriggerTitle: row.TriggerTitle, TriggerKind: row.TriggerKind}
	}
	waitRows, err := s.q.ListChainWaits(ctx, cid)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Work{}, fmt.Errorf("waits of work %s: %w", chain, err)
	}
	waits := make([]ChainWait, len(waitRows))
	for i, w := range waitRows {
		waits[i] = ChainWait{TurnID: uuidString(w.TurnID), CreatedAt: w.CreatedAt.Time}
		if w.DecidedAt.Valid {
			decided := w.DecidedAt.Time
			waits[i].DecidedAt = &decided
		}
	}
	members, err := s.ListRoomMembers(ctx, ask.Room)
	if err != nil {
		return Work{}, err
	}
	events, err := s.ListRoomBranchEvents(ctx, ask.Room, turns[0].StartedAt)
	if err != nil {
		return Work{}, err
	}
	return BuildWork(ask, turns, waits, members, events, time.Now()), nil
}

// BuildWork puts a piece of work together from the message that began it,
// its turns oldest first, the requests people were asked, the room's
// members and what became of their branches since it began. now ends the
// stretches still waiting.
func BuildWork(ask Message, turns []TaskTurn, waits []ChainWait, members []Member, events []BranchEvent, now time.Time) Work {
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m.DisplayName
	}
	tasks := BuildTasks(turns, members, nil)
	taskOf := func(t TaskTurn) *Task {
		for i := range tasks {
			if k := &tasks[i]; k.Chain == t.ChainMessageID && k.ThreadID == t.ThreadID && k.MemberID == t.MemberID {
				return k
			}
		}
		return nil
	}
	woke := map[string][]string{}
	firsts := map[taskKey]string{}
	for _, t := range turns {
		k := taskKey{t.ChainMessageID, t.ThreadID, t.MemberID}
		if _, seen := firsts[k]; !seen {
			firsts[k] = t.ID
			if t.WokenByTurnID != "" && !slices.Contains(woke[t.WokenByTurnID], t.MemberID) {
				woke[t.WokenByTurnID] = append(woke[t.WokenByTurnID], t.MemberID)
			}
		}
	}

	w := Work{
		Chain: ask.ID, RoomID: ask.Room, Title: AskLine(ask.Body, names), Ask: StripAsk(ask.Body, names), AskedBy: ask.UserID,
		Asked: []string{}, StartedAt: turns[0].StartedAt, Turns: make([]WorkTurn, 0, len(turns)), Events: []WorkEvent{},
	}
	lastChange := map[string]time.Time{}
	for _, t := range turns {
		task := taskOf(t)
		root := task != nil && task.Work == nil
		first := firsts[taskKey{t.ChainMessageID, t.ThreadID, t.MemberID}] == t.ID
		if w.ThreadID == "" && root {
			w.ThreadID, w.ThreadNumber = t.ThreadID, t.ThreadNumber
		}
		if root && first && !slices.Contains(w.Asked, t.MemberID) {
			w.Asked = append(w.Asked, t.MemberID)
		}
		wt := WorkTurn{
			ID: t.ID, MemberID: t.MemberID, ThreadID: t.ThreadID, ThreadNumber: t.ThreadNumber, Status: t.Status, Error: t.Error,
			StartedAt: t.StartedAt, EndedAt: t.EndedAt, Usage: t.Usage, Woke: woke[t.ID], Files: len(t.FilesChanged),
		}
		switch {
		case first && root && len(wt.Woke) > 0:
			wt.Kind = WorkTurnSplit
		case first:
			wt.Kind = WorkTurnTask
			if task != nil {
				wt.Title = task.Title
			}
		case t.TriggerKind == string(SenderSystem) && root && task.Parts != nil:
			wt.Kind = WorkTurnSumUp
		case len(wt.Woke) > 0:
			wt.Kind, wt.Relay = WorkTurnHandOff, !t.Worked
		default:
			wt.Kind = WorkTurnContinue
			if t.TriggerTitle != "" {
				wt.Title = t.TriggerTitle
			} else if t.TriggerKind != string(SenderSystem) {
				wt.Title = AskLine(t.TriggerBody, names)
			}
		}
		wt.Waits, wt.WaitedMS = waitsOf(t.ID, waits, now)
		w.Usage = w.Usage.Plus(t.Usage)
		w.WaitedMS += wt.WaitedMS
		if t.Status == TurnRunning {
			w.Running = true
		}
		if t.EndedAt != nil {
			if w.EndedAt == nil || t.EndedAt.After(*w.EndedAt) {
				ended := *t.EndedAt
				w.EndedAt = &ended
			}
			if len(t.FilesChanged) > 0 && t.EndedAt.After(lastChange[t.MemberID]) {
				lastChange[t.MemberID] = *t.EndedAt
			}
		}
		w.Turns = append(w.Turns, wt)
	}
	if w.Running {
		w.EndedAt = nil
	}
	if w.ThreadID == "" {
		w.ThreadID, w.ThreadNumber = turns[0].ThreadID, turns[0].ThreadNumber
	}
	w.Events = workEvents(lastChange, members, events)
	return w
}

// waitsOf is the stretches a turn waited for people, merged where they
// overlap, and their length in all; one still open lasts until now.
func waitsOf(turnID string, waits []ChainWait, now time.Time) ([]WorkWait, int64) {
	var out []WorkWait
	var total time.Duration
	for _, w := range waits {
		if w.TurnID != turnID {
			continue
		}
		end := now
		if w.DecidedAt != nil {
			end = *w.DecidedAt
		}
		if n := len(out); n > 0 {
			last := &out[n-1]
			lastEnd := now
			if last.To != nil {
				lastEnd = *last.To
			}
			if !w.CreatedAt.After(lastEnd) {
				if end.After(lastEnd) {
					total += end.Sub(lastEnd)
					last.To = w.DecidedAt
				}
				continue
			}
		}
		out = append(out, WorkWait{From: w.CreatedAt, To: w.DecidedAt})
		total += end.Sub(w.CreatedAt)
	}
	return out, total.Milliseconds()
}

// workEvents are what first became of each member's branch after the last
// of the work's changes on it, a merge or a reset carrying several
// members' work said once.
func workEvents(lastChange map[string]time.Time, members []Member, events []BranchEvent) []WorkEvent {
	worktree := map[string]bool{}
	for _, m := range members {
		worktree[m.ID] = m.WorktreeDir != ""
	}
	byKey := map[string]*WorkEvent{}
	var out []*WorkEvent
	for memberID, changed := range lastChange {
		if !worktree[memberID] {
			continue
		}
		e := firstBranchEvent(events, memberID, changed)
		if e == nil {
			continue
		}
		key := string(e.Kind) + e.Commit + e.Ref
		we := byKey[key]
		if we == nil {
			we = &WorkEvent{Kind: e.Kind, Commit: e.Commit, Ref: e.Ref, At: e.At}
			byKey[key] = we
			out = append(out, we)
		}
		we.Members = append(we.Members, memberID)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	res := make([]WorkEvent, len(out))
	for i, we := range out {
		sort.Strings(we.Members)
		res[i] = *we
	}
	return res
}

// StripAsk is what a message asks, whole, without the @s of the names it
// began with, the longest name taken first.
func StripAsk(body string, names []string) string {
	text := strings.TrimSpace(body)
	names = slices.Clone(names)
	slices.SortFunc(names, func(a, b string) int { return len(b) - len(a) })
	for {
		i := slices.IndexFunc(names, func(n string) bool { return n != "" && strings.HasPrefix(text, "@"+n) })
		if i < 0 {
			return text
		}
		text = strings.TrimSpace(text[len(names[i])+1:])
	}
}
