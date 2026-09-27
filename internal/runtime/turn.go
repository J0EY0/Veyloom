package runtime

import (
	"context"
	"encoding/json"
	"errors"
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
	// Env is added to the environment the runtime runs in, and with it the
	// commands it runs. Like Host, the machine sets it.
	Env []string `json:"-"`
	// ExtraTools names the tools of Veyloom's the turn gets beyond every
	// turn's: MemoryToolNames while the person uses a memory,
	// UpkeepToolNames for a wiki maintainer's upkeep turn, and
	// SetupToolNames for the project leader's turns (design.md 5.21).
	ExtraTools []string `json:"extra_tools,omitempty"`
	// AllowedRules are what people allowed the member always, in the
	// runtime's own terms (docs/design.md 4.6): permission rules Claude
	// Code matches itself, command prefixes as JSON arrays the Codex runner
	// matches. Only presets that ask people carry them.
	AllowedRules []string `json:"allowed_rules,omitempty"`
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
	// PermissionAutoReview is edit_with_approval with the runtime's own
	// reviewer deciding what it would ask, and a person asked only when the
	// reviewer cannot tell: Claude Code's auto mode, Codex's automatic
	// review.
	PermissionAutoReview = "auto_review"
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
	// EventSteer reports that the runtime took in, while the turn ran, what
	// Turn.Steer passed it as SteerID: Text is what it was given
	// (design.md 5.23.2).
	EventSteer EventKind = "steer"
	// EventSteerDropped reports that what Turn.Steer passed as SteerID will
	// not reach the agent in this turn: it was taken, but the turn ended
	// before the agent got to it (or, said by the machine, it was refused).
	EventSteerDropped EventKind = "steer_dropped"
	// EventQuota reports where the runtime's account stands against its
	// usage limits (Event.Quota), whenever the runtime tells of it
	// (design.md 5.23.3).
	EventQuota EventKind = "quota"
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
	// CallID is set on EventToolCall and EventToolResult when the runtime
	// names its calls: a result carries its call's, so calls made at once,
	// as when Claude Code reads two files, are told apart.
	CallID string `json:"call_id,omitempty"`
	// Seq numbers the turn's events from 1 in the order the hub takes them
	// in; the hub sets it, a runtime leaves it alone. The transcript and
	// the room's stream carry the same numbers, so a reader can lay what
	// it heard live over what it read of the transcript.
	Seq int64 `json:"seq,omitempty"`
	// SteerID is set on EventSteer and EventSteerDropped: the id Turn.Steer
	// was given.
	SteerID string `json:"steer_id,omitempty"`
	// Quota is set on EventQuota.
	Quota *Quota `json:"quota,omitempty"`
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
	// Similar is set on an EventApprovalRequest whose runtime can take in,
	// with an allow, the like of the request for the rest of the turn.
	Similar *Similar `json:"similar,omitempty"`
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
	// Similar has an allow take in, for the rest of the turn, what the
	// request's Similar says: a person need not be asked again for it.
	Similar bool `json:"similar,omitempty"`
	// Message is shown to the agent when the request is denied, so it can
	// explain itself or try something else.
	Message string `json:"message,omitempty"`
	// Answer is what the person gave beyond yes or no, as JSON: for a
	// question {"answers": {"<question id>": ["..."]}}, for a form
	// {"content": {...}}.
	Answer json.RawMessage `json:"answer,omitempty"`
}

// Similar is what allowing a request can take in besides, for the rest of
// the turn, as its runtime offers: the permission rules Claude Code
// suggests, such as Bash(go test *), a permission mode it would switch to,
// or folders it would open up; or, for Codex, the same request again, and
// commands starting with the words it proposes, such as go test.
//
// Rules and Prefix can also be kept for the member, for every turn after
// (docs/design.md 4.6): see TurnSpec.AllowedRules and Standing.
type Similar struct {
	Rules  []string `json:"rules,omitempty"`
	Mode   string   `json:"mode,omitempty"`
	Dirs   []string `json:"dirs,omitempty"`
	Same   bool     `json:"same,omitempty"`
	Prefix []string `json:"prefix,omitempty"`
}

// Standing is what of the offer can be kept for the member, as the rules
// TurnSpec.AllowedRules carries: Claude Code's permission rules, and a
// Codex command prefix as a JSON array. Nil when nothing can.
func (s *Similar) Standing() []string {
	if s == nil {
		return nil
	}
	rules := append([]string(nil), s.Rules...)
	if len(s.Prefix) > 0 {
		if raw, err := json.Marshal(s.Prefix); err == nil {
			rules = append(rules, string(raw))
		}
	}
	return rules
}

// ReviewerRule names, on a request a runtime settled itself, a rule of the
// member's it matched: one in TurnSpec.AllowedRules, or one a person
// allowed for the rest of the turn.
const ReviewerRule = "rule"

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
	// RetryAt is, for a failure of the account's (FailureKind.Account),
	// when it is expected to pass, when the runtime said: the reset of the
	// usage limit that was reached.
	RetryAt time.Time `json:"retry_at,omitzero"`
}

// Quota is where a runtime's account stands against its usage limits, as
// the runtime reported it: the limit nearest to being reached, or the one
// reached.
type Quota struct {
	// Limited says the account takes no more turns until the limit resets,
	// or, without a reset time, until a person sees to it.
	Limited bool `json:"limited,omitempty"`
	// Window names the limit by its span: "5h", "7d", "7d opus", or the
	// runtime's own word for it; empty when the runtime did not say.
	Window string `json:"window,omitempty"`
	// UsedPercent is how much of it is used, 0 to 100; nil when not known.
	UsedPercent *int `json:"used_percent,omitempty"`
	// ResetsAt is when it resets; zero when not known.
	ResetsAt time.Time `json:"resets_at,omitzero"`
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
	// Steer passes text to the agent while the turn runs, the runtime's own
	// way (design.md 5.23.2), naming it id: an EventSteer tells when the
	// runtime takes it in, an EventSteerDropped that the turn ended before
	// the agent got to it. ErrSteerRefused when the turn cannot take it
	// now, being about to end say, or the runtime takes nothing mid-turn.
	Steer(id, text string) error
}

// ErrSteerRefused is what Turn.Steer says of text the turn cannot take.
var ErrSteerRefused = errors.New("the turn takes no more input now")

// Runner starts turns for one runtime. Real runtimes wrap a CLI; Fake needs
// nothing and is used in tests and demos.
type Runner interface {
	Name() string
	StartTurn(ctx context.Context, spec TurnSpec) (Turn, error)
}
