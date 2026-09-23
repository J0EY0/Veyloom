package runtime

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

// TurnSpec describes one unit of work for a runtime: everything a machine
// needs to start the CLI. The hub builds it and it travels over the
// protocol, so it stays plain data.
type TurnSpec struct {
	// SystemPrompt is the standing instruction for the agent, its role card.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// Prompt is this turn's input: the brief the hub composed.
	Prompt string `json:"prompt"`
	// WorkDir is where the runtime runs, normally the project checkout.
	WorkDir string `json:"work_dir,omitempty"`
	// Model overrides the runtime's default model when set.
	Model string `json:"model,omitempty"`
	// Permission is the preset the runtime maps onto its own flags.
	Permission string `json:"permission,omitempty"`
	// Session is the conversation the turn belongs to.
	Session Session `json:"session,omitzero"`
	// Options are runtime-specific settings passed through from the agent.
	Options map[string]any `json:"options,omitempty"`
	// Host is what the turn may ask of the machine running it: the room
	// tools go through it. The machine sets it as the spec arrives; it is
	// no part of the spec on the wire. Nil leaves the turn without them.
	Host TurnHost `json:"-"`
	// Skills are the skill library's skills the turn is given, the ones
	// its runtime may load (docs/design.md 5.11). Nil gives none.
	Skills *SkillSet `json:"skills,omitempty"`
	// SkillDir is where the machine wrote Skills (see WriteSkills), for the
	// runtime to load them from. Like Host, the machine sets it.
	SkillDir string `json:"-"`
	// ExtraTools names the tools of Veyloom's the turn gets beyond every
	// turn's: MemoryToolNames while the person uses a memory, and
	// UpkeepToolNames for a wiki maintainer's upkeep turn.
	ExtraTools []string `json:"extra_tools,omitempty"`
}

// Session tells a runtime which conversation a turn belongs to. The hub
// keeps one per member and hands it over with every turn; the zero Session
// is a conversation of one turn.
type Session struct {
	// Key is the hub's name for the session, a UUID that stays the same for
	// the session's whole life. A runtime that can be told what to call its
	// session uses it: Claude Code as --session-id, Pi as the name of the
	// session file. That way a session whose first turn failed or was
	// cancelled is still there for the next one.
	Key string `json:"key,omitempty"`
	// Ref is the runtime's own reference as it last reported it with an
	// EventSession. Runtimes that name their sessions themselves, Codex
	// for one, need it to resume.
	Ref string `json:"ref,omitempty"`
	// Resume is true once the runtime has reported the session: the turn
	// continues it. False means the turn starts it.
	Resume bool `json:"resume,omitempty"`
}

// resumeID is the id to resume by: what the runtime last called the
// session, or failing that the name it was given.
func (s Session) resumeID() string {
	if s.Ref != "" {
		return s.Ref
	}
	return s.Key
}

// Permission presets, as carried in TurnSpec.Permission. Each runtime maps
// them onto its own flags; the names are the ones users pick when setting up an agent.
const (
	// PermissionReadOnly lets the agent read the repository and nothing else.
	PermissionReadOnly = "read_only"
	// PermissionEditWithApproval lets the agent edit files freely but asks
	// before running commands.
	PermissionEditWithApproval = "edit_with_approval"
	// PermissionFullAuto lets the agent edit and run without asking.
	PermissionFullAuto = "full_auto"
)

// EventKind classifies what happened during a turn.
type EventKind string

const (
	// EventStatus is a human-readable progress note in Text.
	EventStatus EventKind = "status"
	// EventText is a chunk of the agent's reply in Text. Chunks concatenate
	// to the final output.
	EventText EventKind = "text"
	// EventToolCall reports a tool the agent invoked: Tool and Input.
	EventToolCall EventKind = "tool_call"
	// EventToolResult reports what a tool returned: Tool and Text.
	EventToolResult EventKind = "tool_result"
	// EventFileChanged reports a file the agent wrote, in Path.
	EventFileChanged EventKind = "file_changed"
	// EventError reports a non-fatal problem in Text.
	EventError EventKind = "error"
	// EventSession reports the runtime's own reference to the turn's
	// session, in SessionRef, as soon as the runtime knows it. The hub
	// stores it at once, so a turn that fails or is cancelled later still
	// leaves a session the next turn can resume.
	EventSession EventKind = "session"
	// EventCompaction reports that the runtime is compacting the session:
	// summarising its older part to make room. Phase says where it stands.
	// Compacting is the runtime's own business and the hub never asks for
	// it; the hub only needs to know, because a session that has just
	// forgotten detail is shown the topic it works in once more.
	EventCompaction EventKind = "compaction"
	// EventApprovalRequest reports that the agent needs a person's answer:
	// permission to use Tool with Input or, as ApprovalKind says, answers
	// to questions, a form filled in or a link opened. ApprovalID identifies
	// the request; the turn waits until Turn.Answer settles it. With
	// Reviewer set the runtime has settled the request itself (Verdict, and
	// why in Text) and nothing waits: the event is there so people see it.
	EventApprovalRequest EventKind = "approval_request"
	// EventNotice is something the runtime wants people to see, in Text, at
	// a Level: a note, a warning, an error it carried on after.
	EventNotice EventKind = "notice"
	// EventApprovalWithdrawn reports that the runtime no longer waits for
	// the answer to the request ApprovalID, while the turn goes on: it took
	// the request back, as Claude Code does when one of its hooks settles a
	// permission first. People stop being asked.
	EventApprovalWithdrawn EventKind = "approval_withdrawn"
)

// Approval kinds, as carried in Event.ApprovalKind. Empty means tool_use.
const (
	ApprovalToolUse  = "tool_use"
	ApprovalQuestion = "question"
	ApprovalForm     = "form"
	ApprovalLink     = "link"
)

// Verdicts of a request a runtime settled without a person, as carried in
// Event.Verdict; they are the settled statuses of an approval.
const (
	VerdictAllowed   = "allowed"
	VerdictDenied    = "denied"
	VerdictExpired   = "expired"
	VerdictCancelled = "cancelled"
)

// Levels of an EventNotice.
const (
	NoticeInfo    = "info"
	NoticeWarning = "warning"
	NoticeError   = "error"
)

// Event is one thing that happened during a turn. Fields other than Kind
// and At are set according to the kind.
type Event struct {
	Kind  EventKind `json:"kind"`
	At    time.Time `json:"at"`
	Text  string    `json:"text,omitempty"`
	Tool  string    `json:"tool,omitempty"`
	Input string    `json:"input,omitempty"`
	Path  string    `json:"path,omitempty"`
	// SessionRef is set on EventSession.
	SessionRef string `json:"session_ref,omitempty"`
	// Phase is set on EventCompaction: one of the Compaction constants.
	Phase string `json:"phase,omitempty"`
	// ApprovalID is set on EventApprovalRequest. Unlike tool_call, whose
	// Input may be elided for size, an approval request carries the full
	// input: it is what the person is asked to approve.
	ApprovalID string `json:"approval_id,omitempty"`
	// ApprovalKind is set on an EventApprovalRequest that asks for more
	// than permission: one of the Approval constants other than tool_use.
	ApprovalKind string `json:"approval_kind,omitempty"`
	// Reviewer, Verdict and Detail are set on an EventApprovalRequest the
	// runtime settled itself: who decided, one of the Verdict constants, and
	// the reviewer's findings as JSON, such as the risk it saw.
	Reviewer string          `json:"reviewer,omitempty"`
	Verdict  string          `json:"verdict,omitempty"`
	Detail   json.RawMessage `json:"detail,omitempty"`
	// Level is set on EventNotice: one of the Notice constants.
	Level string `json:"level,omitempty"`
}

// The phases of an EventCompaction. Only a compaction that ended counts as
// one: a failed one left the session as it was.
const (
	CompactionStart  = "start"
	CompactionEnd    = "end"
	CompactionFailed = "failed"
)

// Decision answers an approval request.
type Decision struct {
	Allow bool `json:"allow"`
	// Message is shown to the agent when the request is denied, so it can
	// explain itself or try something else.
	Message string `json:"message,omitempty"`
	// Answer is what the person gave beyond yes or no, as JSON: for a
	// question {"answers": {"<question id>": ["..."]}}, for a form
	// {"content": {...}}.
	Answer json.RawMessage `json:"answer,omitempty"`
}

// Result is what a finished turn produced. A turn that failed or was
// cancelled has only its Usage, what it had spent by then, and for a
// failure the runtime could name, its Failure.
type Result struct {
	// Output is the agent's final reply.
	Output string `json:"output"`
	// SessionRef identifies the runtime session to resume next time.
	SessionRef string `json:"session_ref,omitempty"`
	// Usage is the tokens the turn spent, as the runtime reported them.
	Usage Usage `json:"usage"`
	// Failure names why the turn failed, when the runtime could tell.
	Failure FailureKind `json:"failure,omitempty"`
}

// Usage is the tokens a turn spent, in parts that do not overlap: fresh
// input, input read from a prompt cache, input written to one, and output
// (reasoning included). Runtimes split these differently; each runner maps
// its own report onto this shape. Cost is left out on purpose: a runtime's
// own figure is a list-price estimate, wrong behind a third-party API.
type Usage struct {
	InputTokens      int64 `json:"input_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
}

// Total is every token the turn spent.
func (u Usage) Total() int64 {
	return u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens + u.OutputTokens
}

// Plus is the two usages added up, part by part.
func (u Usage) Plus(v Usage) Usage {
	return Usage{
		InputTokens:      u.InputTokens + v.InputTokens,
		CacheReadTokens:  u.CacheReadTokens + v.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens + v.CacheWriteTokens,
		OutputTokens:     u.OutputTokens + v.OutputTokens,
	}
}

// tokens turns a count read as a JSON number into a whole, non-negative
// number of tokens. Runners decode counts as float64 so that an odd
// decimal point cannot fail the line that carries the reply.
func tokens(n float64) int64 {
	return max(int64(math.Round(n)), 0)
}

// Turn is a running runtime invocation.
//
// Events streams what happens; the channel is closed once the turn is over,
// after which Result returns the outcome. Cancel stops the turn early: the
// event channel still closes and Result reports the cancellation.
//
// Answer settles an approval request the turn emitted. Runtimes without an
// approval mechanism never emit one, so Answer on them always reports
// ErrUnknownApproval.
type Turn interface {
	Events() <-chan Event
	Result() (Result, error)
	Cancel()
	Answer(approvalID string, d Decision) error
}

// Runner starts turns for one runtime. Real runtimes wrap a CLI; Fake needs
// nothing and is used in tests and demos.
type Runner interface {
	Name() string
	StartTurn(ctx context.Context, spec TurnSpec) (Turn, error)
}
