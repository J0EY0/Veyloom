// Package protocol defines the messages exchanged between the hub and its
// workers, and the Conn abstraction they travel over.
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

	"github.com/J0EY0/veyloom/internal/engine"
)

// Kind identifies a message type on the wire.
type Kind string

// Messages sent by a worker to the hub.
const (
	// KindHello opens a connection. It must be the first message a worker
	// sends and carries its identity plus the result of its initial engine
	// discovery.
	KindHello Kind = "hello"
	// KindHeartbeat tells the hub the worker is still alive.
	KindHeartbeat Kind = "heartbeat"
	// KindEnginesReport carries fresh discovery results, normally in answer
	// to a Probe.
	KindEnginesReport Kind = "engines_report"
	// KindTurnEvent streams one event of a running turn.
	KindTurnEvent Kind = "turn_event"
	// KindTurnDone reports that a turn finished, with its result or error.
	KindTurnDone Kind = "turn_done"
	// KindApprovalRequest asks, on behalf of a running turn, for permission
	// to use a tool. The turn waits for an ApprovalDecision.
	KindApprovalRequest Kind = "approval_request"
)

// Messages sent by the hub to a worker.
const (
	// KindWelcome acknowledges a Hello and assigns the worker its ID.
	KindWelcome Kind = "welcome"
	// KindProbe asks the worker to re-run engine discovery.
	KindProbe Kind = "probe"
	// KindStartTurn asks the worker to run a turn on one of its engines.
	KindStartTurn Kind = "start_turn"
	// KindCancelTurn asks the worker to stop a running turn.
	KindCancelTurn Kind = "cancel_turn"
	// KindApprovalDecision answers an ApprovalRequest.
	KindApprovalDecision Kind = "approval_decision"
)

// Message is implemented by every payload type.
type Message interface {
	Kind() Kind
}

// Hello is the first message on a connection, worker to hub.
type Hello struct {
	// WorkerID is the ID the hub assigned in an earlier Welcome, or empty on
	// a worker's very first connection. The hub uses it to recognise a
	// returning worker; the name below is only a label.
	WorkerID string `json:"worker_id,omitempty"`
	// Name is a human-readable label for the machine, typically its hostname.
	Name string `json:"name"`
	// Token authenticates remote workers. In-process workers leave it empty.
	Token string `json:"token,omitempty"`
	// Engines is the result of the worker's initial discovery.
	Engines []engine.Info `json:"engines"`
}

// Kind implements Message.
func (Hello) Kind() Kind { return KindHello }

// Welcome is the hub's reply to Hello.
type Welcome struct {
	// WorkerID is the identifier the hub will use for this worker.
	WorkerID string `json:"worker_id"`
	// HeartbeatInterval is how often the hub expects a Heartbeat.
	HeartbeatInterval Duration `json:"heartbeat_interval"`
}

// Kind implements Message.
func (Welcome) Kind() Kind { return KindWelcome }

// Heartbeat is a periodic liveness signal, worker to hub.
type Heartbeat struct{}

// Kind implements Message.
func (Heartbeat) Kind() Kind { return KindHeartbeat }

// EnginesReport carries fresh engine discovery results, worker to hub.
type EnginesReport struct {
	Engines []engine.Info `json:"engines"`
}

// Kind implements Message.
func (EnginesReport) Kind() Kind { return KindEnginesReport }

// Probe asks a worker to re-run discovery and answer with an EnginesReport.
type Probe struct{}

// Kind implements Message.
func (Probe) Kind() Kind { return KindProbe }

// StartTurn asks a worker to run a turn, hub to worker.
type StartTurn struct {
	// TurnID is assigned by the hub and echoed in every event.
	TurnID string `json:"turn_id"`
	// Engine names the runner to use, e.g. "claude" or "fake".
	Engine string          `json:"engine"`
	Spec   engine.TurnSpec `json:"spec"`
}

// Kind implements Message.
func (StartTurn) Kind() Kind { return KindStartTurn }

// CancelTurn asks a worker to stop a turn, hub to worker. Cancelling an
// unknown or finished turn is not an error.
type CancelTurn struct {
	TurnID string `json:"turn_id"`
}

// Kind implements Message.
func (CancelTurn) Kind() Kind { return KindCancelTurn }

// TurnEvent carries one event of a running turn, worker to hub.
type TurnEvent struct {
	TurnID string       `json:"turn_id"`
	Event  engine.Event `json:"event"`
}

// Kind implements Message.
func (TurnEvent) Kind() Kind { return KindTurnEvent }

// TurnDone reports the end of a turn, worker to hub. Error is empty on
// success; otherwise Result is meaningless.
type TurnDone struct {
	TurnID string        `json:"turn_id"`
	Result engine.Result `json:"result"`
	Error  string        `json:"error,omitempty"`
	// Cancelled marks an Error that was caused by CancelTurn or shutdown
	// rather than by the engine failing.
	Cancelled bool `json:"cancelled,omitempty"`
}

// Kind implements Message.
func (TurnDone) Kind() Kind { return KindTurnDone }

// ApprovalRequest asks for permission on behalf of a turn, worker to hub.
// Input is the tool's full input as JSON: it is what the person decides on,
// so unlike a tool_call event it is never elided.
type ApprovalRequest struct {
	TurnID     string    `json:"turn_id"`
	ApprovalID string    `json:"approval_id"`
	Tool       string    `json:"tool"`
	Input      string    `json:"input"`
	At         time.Time `json:"at"`
}

// Kind implements Message.
func (ApprovalRequest) Kind() Kind { return KindApprovalRequest }

// ApprovalDecision settles an ApprovalRequest, hub to worker. A decision
// for a turn or request that is no longer pending is ignored.
type ApprovalDecision struct {
	TurnID     string          `json:"turn_id"`
	ApprovalID string          `json:"approval_id"`
	Decision   engine.Decision `json:"decision"`
}

// Kind implements Message.
func (ApprovalDecision) Kind() Kind { return KindApprovalDecision }

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
	KindHello:         decodeAs[Hello],
	KindHeartbeat:     decodeAs[Heartbeat],
	KindEnginesReport: decodeAs[EnginesReport],
	KindWelcome:       decodeAs[Welcome],
	KindProbe:         decodeAs[Probe],
	KindStartTurn:     decodeAs[StartTurn],
	KindCancelTurn:    decodeAs[CancelTurn],
	KindTurnEvent:     decodeAs[TurnEvent],
	KindTurnDone:      decodeAs[TurnDone],

	KindApprovalRequest:  decodeAs[ApprovalRequest],
	KindApprovalDecision: decodeAs[ApprovalDecision],
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
