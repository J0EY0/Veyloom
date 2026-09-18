package protocol

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
)

func TestMarshalRoundTrip(t *testing.T) {
	runtimes := []runtime.Info{{Name: "claude", Binary: "claude", Version: "2.1.85", Status: runtime.StatusReady}}

	messages := []Message{
		Hello{MachineID: "w1", Name: "laptop", Token: "secret", Runtimes: runtimes},
		Heartbeat{},
		RuntimesReport{Runtimes: runtimes},
		Welcome{MachineID: "w1", HeartbeatInterval: Duration(15 * time.Second)},
		Probe{},
		StartTurn{TurnID: "t1", Runtime: "fake", Spec: runtime.TurnSpec{Prompt: "hi", Options: map[string]any{"reply": "yo"}}},
		CancelTurn{TurnID: "t1"},
		TurnEvent{TurnID: "t1", Event: runtime.Event{Kind: runtime.EventText, Text: "chunk", At: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)}},
		TurnDone{TurnID: "t1", Result: runtime.Result{Output: "yo", SessionRef: "s1", Usage: runtime.Usage{InputTokens: 12, OutputTokens: 3}}},
		TurnDone{TurnID: "t2", Error: "boom"},
		ApprovalRequest{TurnID: "t1", ApprovalID: "a1", Tool: "Bash", Input: `{"command":"make test"}`, At: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)},
		ApprovalDecision{TurnID: "t1", ApprovalID: "a1", Decision: runtime.Decision{Allow: false, Message: "not now"}},
		RoomQuery{TurnID: "t1", QueryID: "q1", Query: runtime.RoomQuery{Tool: runtime.RoomToolReadTopic, Topic: 12, Before: 340, Limit: 20}},
		RoomResult{TurnID: "t1", QueryID: "q1", Text: "Topic #12 ..."},
		RoomResult{TurnID: "t1", QueryID: "q2", Error: "topic #99: not found"},
	}

	for _, want := range messages {
		t.Run(string(want.Kind()), func(t *testing.T) {
			data, err := Marshal(want)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			got, err := Unmarshal(data)
			if err != nil {
				t.Fatalf("Unmarshal(%s): %v", data, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("round trip changed the message:\n got %#v\nwant %#v", got, want)
			}
		})
	}
}

func TestMarshal_CoversEveryKind(t *testing.T) {
	// Every kind constant must have a decoder, otherwise a message could be
	// sent but never received.
	for _, kind := range []Kind{KindHello, KindHeartbeat, KindRuntimesReport, KindWelcome, KindProbe, KindStartTurn, KindCancelTurn, KindTurnEvent, KindTurnDone, KindApprovalRequest, KindApprovalDecision} {
		if _, ok := decoders[kind]; !ok {
			t.Errorf("no decoder registered for %q", kind)
		}
	}
}

func TestUnmarshal_UnknownKind(t *testing.T) {
	_, err := Unmarshal([]byte(`{"kind":"teleport","payload":{}}`))
	if err == nil || !strings.Contains(err.Error(), "teleport") {
		t.Errorf("expected an error naming the unknown kind, got %v", err)
	}
}

func TestUnmarshal_MalformedPayload(t *testing.T) {
	_, err := Unmarshal([]byte(`{"kind":"welcome","payload":{"machine_id":42}}`))
	if err == nil || !strings.Contains(err.Error(), "welcome") {
		t.Errorf("expected a payload error naming the kind, got %v", err)
	}
}

func TestDuration_WireFormat(t *testing.T) {
	data, err := Marshal(Welcome{MachineID: "w1", HeartbeatInterval: Duration(90 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	// Humans and other languages should see "1m30s", not nanoseconds.
	if !strings.Contains(string(data), `"heartbeat_interval":"1m30s"`) {
		t.Errorf("unexpected wire format: %s", data)
	}
}
