package protocol

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/engine"
)

func TestMarshalRoundTrip(t *testing.T) {
	engines := []engine.Info{{Name: "claude", Binary: "claude", Version: "2.1.85", Status: engine.StatusReady}}

	messages := []Message{
		Hello{WorkerID: "w1", Name: "laptop", Token: "secret", Engines: engines},
		Heartbeat{},
		EnginesReport{Engines: engines},
		Welcome{WorkerID: "w1", HeartbeatInterval: Duration(15 * time.Second)},
		Probe{},
		StartTurn{TurnID: "t1", Engine: "fake", Spec: engine.TurnSpec{Prompt: "hi", Options: map[string]any{"reply": "yo"}}},
		CancelTurn{TurnID: "t1"},
		TurnEvent{TurnID: "t1", Event: engine.Event{Kind: engine.EventText, Text: "chunk", At: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)}},
		TurnDone{TurnID: "t1", Result: engine.Result{Output: "yo", SessionRef: "s1", Usage: map[string]any{"turns": float64(1)}}},
		TurnDone{TurnID: "t2", Error: "boom"},
		ApprovalRequest{TurnID: "t1", ApprovalID: "a1", Tool: "Bash", Input: `{"command":"make test"}`, At: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)},
		ApprovalDecision{TurnID: "t1", ApprovalID: "a1", Decision: engine.Decision{Allow: false, Message: "not now"}},
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
	for _, kind := range []Kind{KindHello, KindHeartbeat, KindEnginesReport, KindWelcome, KindProbe, KindStartTurn, KindCancelTurn, KindTurnEvent, KindTurnDone, KindApprovalRequest, KindApprovalDecision} {
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
	_, err := Unmarshal([]byte(`{"kind":"welcome","payload":{"worker_id":42}}`))
	if err == nil || !strings.Contains(err.Error(), "welcome") {
		t.Errorf("expected a payload error naming the kind, got %v", err)
	}
}

func TestDuration_WireFormat(t *testing.T) {
	data, err := Marshal(Welcome{WorkerID: "w1", HeartbeatInterval: Duration(90 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	// Humans and other languages should see "1m30s", not nanoseconds.
	if !strings.Contains(string(data), `"heartbeat_interval":"1m30s"`) {
		t.Errorf("unexpected wire format: %s", data)
	}
}
