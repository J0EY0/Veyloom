package engine

import (
	"context"
	"time"
)

// TurnSpec describes one unit of work for an engine: everything a worker
// needs to start the CLI. The hub builds it and it travels over the
// protocol, so it stays plain data.
type TurnSpec struct {
	// SystemPrompt is the standing instruction for the agent, its role card.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// Prompt is this turn's input: the brief the hub composed.
	Prompt string `json:"prompt"`
	// WorkDir is where the engine runs, normally the project checkout.
	WorkDir string `json:"work_dir,omitempty"`
	// Model overrides the engine's default model when set.
	Model string `json:"model,omitempty"`
	// Permission is the preset the engine maps onto its own flags.
	Permission string `json:"permission,omitempty"`
	// SessionRef resumes an earlier session of the same agent when set.
	SessionRef string `json:"session_ref,omitempty"`
	// Options are engine-specific settings passed through from the template.
	Options map[string]any `json:"options,omitempty"`
}

// Permission presets, as carried in TurnSpec.Permission. Each engine maps
// them onto its own flags; the names are the ones users see in templates.
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
	// EventApprovalRequest reports that the agent wants to use Tool with
	// Input and may not without permission. ApprovalID identifies the
	// request; the turn waits until Turn.Answer settles it.
	EventApprovalRequest EventKind = "approval_request"
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
	// ApprovalID is set on EventApprovalRequest. Unlike tool_call, whose
	// Input may be elided for size, an approval request carries the full
	// input: it is what the person is asked to approve.
	ApprovalID string `json:"approval_id,omitempty"`
}

// Decision answers an approval request.
type Decision struct {
	Allow bool `json:"allow"`
	// Message is shown to the agent when the request is denied, so it can
	// explain itself or try something else.
	Message string `json:"message,omitempty"`
}

// Result is what a finished turn produced.
type Result struct {
	// Output is the agent's final reply.
	Output string `json:"output"`
	// SessionRef identifies the engine session to resume next time.
	SessionRef string `json:"session_ref,omitempty"`
	// Usage is engine-reported token and cost information, opaque to the hub.
	Usage map[string]any `json:"usage,omitempty"`
}

// Turn is a running engine invocation.
//
// Events streams what happens; the channel is closed once the turn is over,
// after which Result returns the outcome. Cancel stops the turn early: the
// event channel still closes and Result reports the cancellation.
//
// Answer settles an approval request the turn emitted. Engines without an
// approval mechanism never emit one, so Answer on them always reports
// ErrUnknownApproval.
type Turn interface {
	Events() <-chan Event
	Result() (Result, error)
	Cancel()
	Answer(approvalID string, d Decision) error
}

// Runner starts turns for one engine. Real engines wrap a CLI; Fake needs
// nothing and is used in tests and demos.
type Runner interface {
	Name() string
	StartTurn(ctx context.Context, spec TurnSpec) (Turn, error)
}
