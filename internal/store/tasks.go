package store

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// The project's task board (docs/webui.md 4.20): a card a task, a task
// being one member's part of a piece of work in one topic. A person asking
// a member is its task; a member it hands work on to has a task of its
// own, part of the first one's work, which counts how many of its parts
// are done.

// TaskState is where a task stands on the board.
type TaskState string

const (
	// TaskRunning: one of its turns is under way, or for a task that
	// handed work on, one of the parts'.
	TaskRunning TaskState = "running"
	// TaskToMerge: it changed files on its member's branch, and they are
	// neither merged nor set aside yet.
	TaskToMerge TaskState = "merge"
	// TaskDone: nothing of it is under way or waits to be merged.
	TaskDone TaskState = "done"
)

// What came of a task, for the corner of its card.
const (
	// OutcomeMerged: its changes went on the main line in Commit.
	OutcomeMerged = "merged"
	// OutcomeArchived: its member's branch was reset, its changes archived
	// under Ref.
	OutcomeArchived = "archived"
	// OutcomeWiki: it wrote Page in the project's wiki.
	OutcomeWiki = "wiki"
)

// Task is one member's part of a piece of work in one topic.
type Task struct {
	// Chain is the piece of work: the message a person began it with.
	Chain        string `json:"chain"`
	ThreadID     string `json:"thread_id"`
	ThreadNumber int    `json:"thread_number"`
	MemberID     string `json:"member_id"`
	// Title is what the member was asked, in a line: what the member that
	// handed it on called it, or the words it was asked with, its own part
	// of them when they asked several members. Empty when neither says
	// anything.
	Title string `json:"title"`
	// Work is the task this one is part of, when it was handed on: the
	// task a person asked for.
	Work *TaskRef `json:"work,omitempty"`
	// Parts counts the tasks handed on from this one, and how many are
	// done with; nil when it handed none on.
	Parts *TaskParts `json:"parts,omitempty"`
	State TaskState  `json:"state"`
	// Waiting says a request of one of its turns waits for a person.
	Waiting   bool       `json:"waiting,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	// Outcome is what came of it, when something did.
	Outcome *TaskOutcome `json:"outcome,omitempty"`
	Turns   int          `json:"turns"`
}

// TaskRef names the task another one is part of.
type TaskRef struct {
	Chain        string `json:"chain"`
	ThreadNumber int    `json:"thread_number"`
	MemberID     string `json:"member_id"`
	Title        string `json:"title"`
}

// TaskParts counts the tasks one handed on.
type TaskParts struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// TaskOutcome is what came of a task: one of the Outcome constants, with
// the commit, the ref or the wiki page.
type TaskOutcome struct {
	Kind   string `json:"kind"`
	Commit string `json:"commit,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Page   string `json:"page,omitempty"`
}

// TaskTurn is a turn with what the board needs of it besides: its topic's
// number, the message that set it off, and whether a request of it waits
// for a person.
type TaskTurn struct {
	Turn
	ThreadNumber int
	TriggerBody  string
	TriggerTitle string
	// TriggerKind is who wrote the message that set it off, a SenderKind;
	// empty when the message is gone.
	TriggerKind string
	Waiting     bool
}

// TaskChains is how many of a room's latest pieces of work the board
// reads.
const TaskChains = 100

// ListRoomTasks returns the tasks of a room's latest pieces of work, in the
// order they began.
func (s *Store) ListRoomTasks(ctx context.Context, roomID string) ([]Task, error) {
	rid, err := parseUUID(roomID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListRoomTaskTurns(ctx, db.ListRoomTaskTurnsParams{RoomID: rid, Chains: TaskChains})
	if err != nil {
		return nil, fmt.Errorf("list tasks of room %s: %w", roomID, err)
	}
	if len(rows) == 0 {
		return []Task{}, nil
	}
	turns := make([]TaskTurn, len(rows))
	for i, row := range rows {
		turns[i] = TaskTurn{
			Turn: toTurn(row.Turn), ThreadNumber: int(row.ThreadNumber),
			TriggerBody: row.TriggerBody, TriggerTitle: row.TriggerTitle, TriggerKind: row.TriggerKind, Waiting: row.Waiting,
		}
	}
	members, err := s.ListRoomMembers(ctx, roomID)
	if err != nil {
		return nil, err
	}
	events, err := s.ListRoomBranchEvents(ctx, roomID, turns[0].StartedAt)
	if err != nil {
		return nil, err
	}
	return BuildTasks(turns, members, events), nil
}

// taskKey is what makes a task: a piece of work, a topic, a member.
type taskKey struct{ chain, thread, member string }

// taskBuild is a task as it is put together.
type taskBuild struct {
	task Task
	// root is the task of the piece of work a person asked for: itself
	// for such a task, else the one its first turn was handed on from, in
	// the end.
	root *taskBuild
	// changed is when the last of its turns that changed files ended;
	// zero when none did.
	changed time.Time
	wiki    []string
	running bool
}

// BuildTasks puts turns, oldest first, together into tasks: what their
// members were asked, whose work each is part of, where each stands, and
// what came of it by what became of its member's branch since.
func BuildTasks(turns []TaskTurn, members []Member, events []BranchEvent) []Task {
	byID := make(map[string]Member, len(members))
	names := make([]string, 0, len(members))
	for _, m := range members {
		byID[m.ID] = m
		names = append(names, m.DisplayName)
	}
	index := map[taskKey]*taskBuild{}
	byTurn := map[string]*taskBuild{}
	var order []*taskBuild
	for _, tt := range turns {
		k := taskKey{tt.ChainMessageID, tt.ThreadID, tt.MemberID}
		b := index[k]
		if b == nil {
			b = &taskBuild{task: Task{
				Chain: tt.ChainMessageID, ThreadID: tt.ThreadID, ThreadNumber: tt.ThreadNumber, MemberID: tt.MemberID,
				Title: taskTitle(tt, names, byID[tt.MemberID].DisplayName), StartedAt: tt.StartedAt,
			}}
			if waker := byTurn[tt.WokenByTurnID]; waker != nil && tt.WokenByTurnID != "" {
				b.root = waker.root
			}
			if b.root == nil {
				b.root = b
			}
			index[k] = b
			order = append(order, b)
		}
		byTurn[tt.ID] = b
		b.task.Turns++
		b.task.Waiting = b.task.Waiting || tt.Waiting
		if tt.Status == TurnRunning {
			b.running = true
		}
		if tt.EndedAt != nil {
			if b.task.EndedAt == nil || tt.EndedAt.After(*b.task.EndedAt) {
				ended := *tt.EndedAt
				b.task.EndedAt = &ended
			}
			if len(tt.FilesChanged) > 0 && tt.EndedAt.After(b.changed) {
				b.changed = *tt.EndedAt
			}
		}
		for _, p := range tt.WikiPages {
			if !slices.Contains(b.wiki, p) {
				b.wiki = append(b.wiki, p)
			}
		}
	}

	for _, b := range order {
		if b.task.Title == "" && b.root != b {
			b.task.Title = b.root.task.Title
		}
		settle(b, byID[b.task.MemberID], events)
	}
	// What each part is part of; a piece of work under way while any of
	// it is.
	for _, b := range order {
		root := b.root
		if root == b {
			continue
		}
		b.task.Work = &TaskRef{Chain: root.task.Chain, ThreadNumber: root.task.ThreadNumber, MemberID: root.task.MemberID, Title: root.task.Title}
		if root.task.Parts == nil {
			root.task.Parts = &TaskParts{}
		}
		root.task.Parts.Total++
		if b.task.State == TaskRunning {
			root.task.State, root.task.EndedAt = TaskRunning, nil
		} else {
			root.task.Parts.Done++
		}
		// The merge its parts went on the main line in, the latest.
		if o := b.task.Outcome; o != nil && o.Kind == OutcomeMerged && (root.task.Outcome == nil || root.task.Outcome.Kind != OutcomeArchived) {
			root.task.Outcome = o
		}
	}
	out := make([]Task, len(order))
	for i, b := range order {
		out[i] = b.task
	}
	return out
}

// settle says where a task stands by its own turns, and what came of it:
// changes on a member's own branch wait there until the first merge or
// reset after them says what became of them.
func settle(b *taskBuild, member Member, events []BranchEvent) {
	switch {
	case b.running:
		b.task.State, b.task.EndedAt = TaskRunning, nil
	case !b.changed.IsZero() && member.WorktreeDir != "":
		b.task.State = TaskToMerge
		if e := firstBranchEvent(events, member.ID, b.changed); e != nil {
			b.task.State = TaskDone
			if e.Kind == BranchMerged {
				b.task.Outcome = &TaskOutcome{Kind: OutcomeMerged, Commit: e.Commit}
			} else {
				b.task.Outcome = &TaskOutcome{Kind: OutcomeArchived, Ref: e.Ref}
			}
		}
	default:
		b.task.State = TaskDone
	}
	if b.task.Outcome == nil && len(b.wiki) > 0 {
		b.task.Outcome = &TaskOutcome{Kind: OutcomeWiki, Page: b.wiki[0]}
	}
}

// firstBranchEvent is what first became of a member's branch after a
// moment; nil when nothing has yet.
func firstBranchEvent(events []BranchEvent, memberID string, after time.Time) *BranchEvent {
	for i := range events {
		if e := &events[i]; e.MemberID == memberID && e.At.After(after) {
			return e
		}
	}
	return nil
}

// taskTitle is what a task's member was asked, in a line: what the member
// that handed it on called it; else, from the words of the message that
// set its first turn off, the part addressed to the member when they ask
// several, up to where its first sentence ends; nothing for a note of the
// hub's.
func taskTitle(first TaskTurn, names []string, member string) string {
	if first.TriggerTitle != "" {
		return first.TriggerTitle
	}
	if first.TriggerKind == string(SenderSystem) || first.TriggerKind == "" {
		return ""
	}
	if part := partFor(first.TriggerBody, names, member); part != "" {
		return AskLine(part, names)
	}
	return AskLine(first.TriggerBody, names)
}

// partFor is the part of a message addressed to one member of several it
// names: from @member up to the next @ of another, empty when the message
// does not name it or says nothing to it there. An @ of a longer name that
// begins with the member's is that other's.
func partFor(body string, names []string, member string) string {
	if member == "" {
		return ""
	}
	at := "@" + member
	var longer []string
	for _, n := range names {
		if len(n) > len(member) && strings.HasPrefix(n, member) {
			longer = append(longer, "@"+n)
		}
	}
	start := -1
	for from := 0; start < 0; {
		i := strings.Index(body[from:], at)
		if i < 0 {
			return ""
		}
		pos := from + i
		if !slices.ContainsFunc(longer, func(n string) bool { return strings.HasPrefix(body[pos:], n) }) {
			start = pos
		}
		from = pos + len(at)
	}
	rest := body[start+len(at):]
	end := len(rest)
	for _, n := range names {
		if n == "" || n == member {
			continue
		}
		if i := strings.Index(rest, "@"+n); i >= 0 && i < end {
			end = i
		}
	}
	return strings.TrimFunc(rest[:end], func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("，,、；;", r) })
}
