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
	// KindApprovalDecision answers an ApprovalRequest.
	KindApprovalDecision Kind = "approval_decision"
	// KindRoomResult answers a RoomQuery.
	KindRoomResult Kind = "room_result"
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
	KindTurnEvent:      decodeAs[TurnEvent],
	KindTurnDone:       decodeAs[TurnDone],

	KindApprovalRequest:  decodeAs[ApprovalRequest],
	KindApprovalDecision: decodeAs[ApprovalDecision],

	KindRoomQuery:  decodeAs[RoomQuery],
	KindRoomResult: decodeAs[RoomResult],
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
