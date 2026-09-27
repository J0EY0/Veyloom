// Package protocol defines the messages exchanged between the hub and its
// machines, and the Conn abstraction they travel over.
//
// The same message types are used whether the two sides share a process
// (see Pipe) or talk over the network (a WebSocket transport, later). Byte
// transports use Marshal and Unmarshal; the in-process pipe hands Message
// values across directly. Messages are always passed by value.
package protocol

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/worktree"
)

// Kind identifies a message type on the wire.
type Kind string

// Messages sent by a machine to the hub.
const (
	// KindHello opens a connection. It must be the first message a machine
	// sends and carries its identity plus the result of its initial runtime
	// discovery.
	KindHello Kind = "hello"
	// KindHeartbeat tells the hub the machine is still alive.
	KindHeartbeat Kind = "heartbeat"
	// KindRuntimesReport carries fresh discovery results, normally in answer
	// to a Probe.
	KindRuntimesReport Kind = "runtimes_report"
	// KindTurnEvent streams one event of a running turn.
	KindTurnEvent Kind = "turn_event"
	// KindTurnDone reports that a turn finished, with its result or error.
	KindTurnDone Kind = "turn_done"
	// KindApprovalRequest asks, on behalf of a running turn, for permission
	// to use a tool. The turn waits for an ApprovalDecision.
	KindApprovalRequest Kind = "approval_request"
	// KindRoomQuery asks, on behalf of a running turn, to read something of
	// the turn's room: a call of one of the agent's room tools. The call
	// waits for a RoomResult.
	KindRoomQuery Kind = "room_query"
	// KindWorkspaceResult answers a WorkspaceRequest.
	KindWorkspaceResult Kind = "workspace_result"
)

// Messages sent by the hub to a machine.
const (
	// KindWelcome acknowledges a Hello and assigns the machine its ID.
	KindWelcome Kind = "welcome"
	// KindProbe asks the machine to re-run runtime discovery.
	KindProbe Kind = "probe"
	// KindStartTurn asks the machine to run a turn on one of its runtimes.
	KindStartTurn Kind = "start_turn"
	// KindCancelTurn asks the machine to stop a running turn.
	KindCancelTurn Kind = "cancel_turn"
	// KindSteerTurn passes what people said to a running turn.
	KindSteerTurn Kind = "steer_turn"
	// KindApprovalDecision answers an ApprovalRequest.
	KindApprovalDecision Kind = "approval_decision"
	// KindRoomResult answers a RoomQuery.
	KindRoomResult Kind = "room_result"
	// KindWorkspaceRequest asks the machine to work on a project's checkout
	// or a member's worktree; the machine answers with a WorkspaceResult.
	KindWorkspaceRequest Kind = "workspace_request"
)

// Message is implemented by every payload type.
type Message interface {
	Kind() Kind
}

// Hello is the first message on a connection, machine to hub.
type Hello struct {
	// MachineID is the ID the hub assigned in an earlier Welcome, or empty on
	// a machine's very first connection. The hub uses it to recognise a
	// returning machine; the name below is only a label.
	MachineID string `json:"machine_id,omitempty"`
	// Name is a human-readable label for the machine, typically its hostname.
	Name string `json:"name"`
	// Token authenticates remote machines. In-process machines leave it empty.
	Token string `json:"token,omitempty"`
	// Runtimes is the result of the machine's initial discovery.
	Runtimes []runtime.Info `json:"runtimes"`
}

// Kind implements Message.
func (Hello) Kind() Kind { return KindHello }

// Welcome is the hub's reply to Hello.
type Welcome struct {
	// MachineID is the identifier the hub will use for this machine.
	MachineID string `json:"machine_id"`
	// HeartbeatInterval is how often the hub expects a Heartbeat.
	HeartbeatInterval Duration `json:"heartbeat_interval"`
}

// Kind implements Message.
func (Welcome) Kind() Kind { return KindWelcome }

// Heartbeat is a periodic liveness signal, machine to hub.
type Heartbeat struct{}

// Kind implements Message.
func (Heartbeat) Kind() Kind { return KindHeartbeat }

// RuntimesReport carries fresh runtime discovery results, machine to hub.
type RuntimesReport struct {
	Runtimes []runtime.Info `json:"runtimes"`
}

// Kind implements Message.
func (RuntimesReport) Kind() Kind { return KindRuntimesReport }

// Probe asks a machine to re-run discovery and answer with a RuntimesReport.
type Probe struct{}

// Kind implements Message.
func (Probe) Kind() Kind { return KindProbe }

// StartTurn asks a machine to run a turn, hub to machine.
type StartTurn struct {
	// TurnID is assigned by the hub and echoed in every event.
	TurnID string `json:"turn_id"`
	// Runtime names the runner to use, e.g. "claude" or "fake".
	Runtime string           `json:"runtime"`
	Spec    runtime.TurnSpec `json:"spec"`
}

// Kind implements Message.
func (StartTurn) Kind() Kind { return KindStartTurn }

// CancelTurn asks a machine to stop a turn, hub to machine. Cancelling an
// unknown or finished turn is not an error.
type CancelTurn struct {
	TurnID string `json:"turn_id"`
}

// Kind implements Message.
func (CancelTurn) Kind() Kind { return KindCancelTurn }

// SteerTurn passes text to a running turn, hub to machine: what people
// said in its topic while it ran (docs/design.md 5.23.2). The turn's
// events tell what became of it, under SteerID: runtime.EventSteer once
// the agent took it in, runtime.EventSteerDropped when it will not reach
// the agent in this turn, which is also what a turn not running here, or
// one that cannot take it now, reports at once.
type SteerTurn struct {
	TurnID  string `json:"turn_id"`
	SteerID string `json:"steer_id"`
	Text    string `json:"text"`
}

// Kind implements Message.
func (SteerTurn) Kind() Kind { return KindSteerTurn }

// TurnEvent carries one event of a running turn, machine to hub.
type TurnEvent struct {
	TurnID string        `json:"turn_id"`
	Event  runtime.Event `json:"event"`
}

// Kind implements Message.
func (TurnEvent) Kind() Kind { return KindTurnEvent }

// TurnDone reports the end of a turn, machine to hub. Error is empty on
// success; otherwise Result holds only the tokens spent before the end.
type TurnDone struct {
	TurnID string         `json:"turn_id"`
	Result runtime.Result `json:"result"`
	Error  string         `json:"error,omitempty"`
	// Cancelled marks an Error that was caused by CancelTurn or shutdown
	// rather than by the runtime failing.
	Cancelled bool `json:"cancelled,omitempty"`
}

// Kind implements Message.
func (TurnDone) Kind() Kind { return KindTurnDone }

// ApprovalRequest asks a person, on behalf of a turn, machine to hub: for
// permission to use a tool, or for answers to questions, a form or a link
// (ApprovalKind). Input is in full, never elided: it is what the person
// decides on. With Reviewer set the runtime has settled the request itself
// and the hub only records it; nothing waits for a decision.
type ApprovalRequest struct {
	TurnID     string `json:"turn_id"`
	ApprovalID string `json:"approval_id"`
	// ApprovalKind is one of the runtime.Approval constants; empty is
	// tool_use.
	ApprovalKind string    `json:"approval_kind,omitempty"`
	Tool         string    `json:"tool"`
	Input        string    `json:"input"`
	At           time.Time `json:"at"`
	// Similar is what an allow can take in besides, for the rest of the
	// turn, as the runtime offers.
	Similar *runtime.Similar `json:"similar,omitempty"`
	// Reviewer, Verdict, Why and Detail describe a request the runtime
	// settled: who decided, the verdict (a runtime.Verdict constant), why,
	// and the reviewer's findings as JSON.
	Reviewer string          `json:"reviewer,omitempty"`
	Verdict  string          `json:"verdict,omitempty"`
	Why      string          `json:"why,omitempty"`
	Detail   json.RawMessage `json:"detail,omitempty"`
}

// Kind implements Message.
func (ApprovalRequest) Kind() Kind { return KindApprovalRequest }

// ApprovalDecision settles an ApprovalRequest, hub to machine. A decision
// for a turn or request that is no longer pending is ignored.
type ApprovalDecision struct {
	TurnID     string           `json:"turn_id"`
	ApprovalID string           `json:"approval_id"`
	Decision   runtime.Decision `json:"decision"`
}

// Kind implements Message.
func (ApprovalDecision) Kind() Kind { return KindApprovalDecision }

// RoomQuery is a call of one of an agent's room tools, machine to hub. The
// hub answers for the room of the turn that asks, and for no other: a turn
// cannot name a room.
type RoomQuery struct {
	TurnID string `json:"turn_id"`
	// QueryID pairs the result with the call; unique within the turn.
	QueryID string            `json:"query_id"`
	Query   runtime.RoomQuery `json:"query"`
}

// Kind implements Message.
func (RoomQuery) Kind() Kind { return KindRoomQuery }

// RoomResult answers a RoomQuery, hub to machine: text for the agent to
// read, or why there is none.
type RoomResult struct {
	TurnID  string `json:"turn_id"`
	QueryID string `json:"query_id"`
	Text    string `json:"text,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Kind implements Message.
func (RoomResult) Kind() Kind { return KindRoomResult }

// WorkspaceOp names what a WorkspaceRequest asks for.
type WorkspaceOp string

// The operations on checkouts and worktrees (docs/design.md 5.21).
const (
	// WorkspaceInspect: how the checkout stands.
	WorkspaceInspect WorkspaceOp = "inspect"
	// WorkspaceCreate: a new worktree for a member, on a branch of its own.
	WorkspaceCreate WorkspaceOp = "create"
	// WorkspacePrepare: the steps the project's leader wrote down, run in a
	// new worktree.
	WorkspacePrepare WorkspaceOp = "prepare"
	// WorkspaceStatus: how a worktree stands against the main line.
	WorkspaceStatus WorkspaceOp = "status"
	// WorkspaceDiff: the patch of what a worktree changed.
	WorkspaceDiff WorkspaceOp = "diff"
	// WorkspaceSquash: a worktree's work put on the main line as one commit.
	WorkspaceSquash WorkspaceOp = "squash"
	// WorkspaceSync: the main line brought into a worktree.
	WorkspaceSync WorkspaceOp = "sync"
	// WorkspaceRemove: a worktree taken away, its branch kept.
	WorkspaceRemove WorkspaceOp = "remove"
	// WorkspaceConclude: a merge a member left under way in its worktree
	// committed once its conflicts are settled, named while not.
	WorkspaceConclude WorkspaceOp = "conclude"
	// WorkspaceAbortMerge: a merge under way in a worktree given up.
	WorkspaceAbortMerge WorkspaceOp = "abort_merge"
	// WorkspaceCheckoutDiff: the patch of what the checkout changed and did
	// not commit.
	WorkspaceCheckoutDiff WorkspaceOp = "checkout_diff"
	// WorkspaceCheckoutCommit: the changes to some files of the checkout,
	// not committed, committed on its branch.
	WorkspaceCheckoutCommit WorkspaceOp = "checkout_commit"
	// WorkspaceSetAside: what a worktree has kept aside in git, and the
	// worktree started over from the main line.
	WorkspaceSetAside WorkspaceOp = "set_aside"
	// WorkspaceOverlap: the files two worktrees' work both changes, each
	// its own way.
	WorkspaceOverlap WorkspaceOp = "overlap"
)

// WorkspaceRequest asks the machine to work on a project's checkout and its
// members' worktrees there (docs/design.md 5.21), hub to machine. Every
// request is answered by a WorkspaceResult with its RequestID.
type WorkspaceRequest struct {
	RequestID string      `json:"request_id"`
	Op        WorkspaceOp `json:"op"`
	// Checkout is the project's checkout: the main line is its branch.
	Checkout string `json:"checkout"`
	// Dir is the member's worktree; WorkDir where in it the member works,
	// which prepare works in.
	Dir     string `json:"dir,omitempty"`
	WorkDir string `json:"work_dir,omitempty"`
	// Name and Branch are what create calls the worktree, under the
	// machine's folder for them, and its branch.
	Name   string `json:"name,omitempty"`
	Branch string `json:"branch,omitempty"`
	// Copy and Run are prepare's steps.
	Copy []string `json:"copy,omitempty"`
	Run  string   `json:"run,omitempty"`
	// Message is squash's commit message, checkout_commit's, what remove
	// commits the work left uncommitted with, or what set_aside keeps the
	// work with.
	Message string `json:"message,omitempty"`
	// Leave are the new files squash leaves out, from the top of the
	// repository: they stay in the worktree.
	Leave []string `json:"leave,omitempty"`
	// Sides are where the two worktrees overlap sets side by side stand,
	// as their statuses said.
	Sides []worktree.Side `json:"sides,omitempty"`
	// Paths are the files checkout_commit commits, from the top of the
	// repository.
	Paths []string `json:"paths,omitempty"`
	// FastForwardOnly has sync leave alone a worktree with work of its own.
	FastForwardOnly bool `json:"fast_forward_only,omitempty"`
	// MaxBytes caps diff's patch; zero leaves it whole.
	MaxBytes int `json:"max_bytes,omitempty"`
}

// Kind implements Message.
func (WorkspaceRequest) Kind() Kind { return KindWorkspaceRequest }

// Workspace failures the hub tells apart, in WorkspaceResult.Code.
const (
	WorkspaceNotRepo       = "not_repo"
	WorkspaceNoCommits     = "no_commits"
	WorkspaceDetached      = "detached"
	WorkspaceNoChanges     = "no_changes"
	WorkspaceMainLineMoved = "main_line_moved"
	WorkspaceNoFolder      = "no_folder"
	// WorkspaceGone: the worktree is not there any more.
	WorkspaceGone = "gone"
	// WorkspaceMergeUnderway: a merge is under way in the worktree.
	WorkspaceMergeUnderway = "merge_underway"
	// WorkspaceCheckoutChanged: the merge changes files that have changes
	// in the checkout not committed; Files names them.
	WorkspaceCheckoutChanged = "checkout_changed"
	// WorkspaceNotNew: squash was asked to leave out files that are not
	// new, which Files names.
	WorkspaceNotNew = "not_new"
)

// WorkspaceResult answers a WorkspaceRequest, machine to hub: what the
// operation came to, or why it failed. Error is empty on success; Code
// names the failures the hub tells apart.
type WorkspaceResult struct {
	RequestID string `json:"request_id"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`

	Repo      *worktree.Repo           `json:"repo,omitempty"`
	Workspace *worktree.Workspace      `json:"workspace,omitempty"`
	Status    *worktree.Status         `json:"status,omitempty"`
	Merge     *worktree.MergeResult    `json:"merge,omitempty"`
	Sync      *worktree.SyncResult     `json:"sync,omitempty"`
	Conclude  *worktree.ConcludeResult `json:"conclude,omitempty"`
	// Text is what prepare did and its command said, or diff's patch, cut
	// when Cut says so. Prepare's text comes with a failure too.
	Text string `json:"text,omitempty"`
	Cut  bool   `json:"cut,omitempty"`
	// Commit is checkout_commit's new commit.
	Commit string `json:"commit,omitempty"`
	// Ref is where set_aside kept the work.
	Ref string `json:"ref,omitempty"`
	// Files are overlap's files, or those a checkout_changed or not_new
	// failure names.
	Files []string `json:"files,omitempty"`
}

// Kind implements Message.
func (WorkspaceResult) Kind() Kind { return KindWorkspaceResult }

// Duration is a time.Duration that marshals as a human-readable string such
// as "15s", which keeps the wire format legible and language-neutral.
type Duration time.Duration

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(v)
	return nil
}

// envelope is the wire form of a message: its kind plus the payload as raw
// JSON, so the kind can be read before the payload type is known.
type envelope struct {
	Kind    Kind            `json:"kind"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Marshal encodes m into its wire form.
func Marshal(m Message) ([]byte, error) {
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode %s payload: %w", m.Kind(), err)
	}
	return json.Marshal(envelope{Kind: m.Kind(), Payload: payload})
}

// Unmarshal decodes a message from its wire form. Unknown kinds are an error
// so that a transport can surface protocol mismatches instead of silently
// dropping traffic.
func Unmarshal(data []byte) (Message, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("decode envelope: %w", err)
	}
	decode, ok := decoders[env.Kind]
	if !ok {
		return nil, fmt.Errorf("unknown message kind %q", env.Kind)
	}
	m, err := decode(env.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode %s payload: %w", env.Kind, err)
	}
	return m, nil
}

// decoders maps each kind to a function that decodes its payload. Adding a
// message type means adding a line here; Unmarshal's tests cover every entry.
var decoders = map[Kind]func(json.RawMessage) (Message, error){
	KindHello:          decodeAs[Hello],
	KindHeartbeat:      decodeAs[Heartbeat],
	KindRuntimesReport: decodeAs[RuntimesReport],
	KindWelcome:        decodeAs[Welcome],
	KindProbe:          decodeAs[Probe],
	KindStartTurn:      decodeAs[StartTurn],
	KindCancelTurn:     decodeAs[CancelTurn],
	KindSteerTurn:      decodeAs[SteerTurn],
	KindTurnEvent:      decodeAs[TurnEvent],
	KindTurnDone:       decodeAs[TurnDone],

	KindApprovalRequest:  decodeAs[ApprovalRequest],
	KindApprovalDecision: decodeAs[ApprovalDecision],

	KindRoomQuery:  decodeAs[RoomQuery],
	KindRoomResult: decodeAs[RoomResult],

	KindWorkspaceRequest: decodeAs[WorkspaceRequest],
	KindWorkspaceResult:  decodeAs[WorkspaceResult],
}

// decodeAs decodes payload into a value of type T. An empty payload yields the
// zero value, which is how bodiless messages like Heartbeat travel.
func decodeAs[T Message](payload json.RawMessage) (Message, error) {
	var v T
	if len(payload) == 0 {
		return v, nil
	}
	if err := json.Unmarshal(payload, &v); err != nil {
		return nil, err
	}
	return v, nil
}
