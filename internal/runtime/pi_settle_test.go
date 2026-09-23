package runtime

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Once the agent's run is over, pi compacts the session: the turn waits
// for the compaction and reports it, instead of ending pi halfway through.
func TestPi_WaitsForTheCompactionAfterTheRun(t *testing.T) {
	_, stdinPath := scriptedPiCLI(t)
	events, res, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "[compact]"})
	if err != nil {
		t.Fatal(err)
	}
	if got := compactionPhases(events); got != "start,end" {
		t.Errorf("compaction phases = %q, want start,end", got)
	}
	if res.Output != "done" {
		t.Errorf("output = %q, want the answer before the compaction", res.Output)
	}
	// Asked at the end of the run, when pi was compacting, and again once
	// the compaction was over.
	if n := settleAsks(t, stdinPath); n != 2 {
		t.Errorf("the runner asked pi for its state %d times after the run, want 2", n)
	}
}

// An answer that failed in a way worth another try pi tries again, after a
// pause: the turn waits through it and ends with the second answer.
func TestPi_WaitsForPiToTryAgain(t *testing.T) {
	scriptedPiCLI(t)
	events, res, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "[retry-ok]"})
	if err != nil {
		t.Fatalf("the turn should end with what pi answered on its second try: %v", err)
	}
	if res.Output != "answered on the second try" {
		t.Errorf("output = %q", res.Output)
	}
	var retried bool
	for _, ev := range events {
		retried = retried || (ev.Kind == EventNotice && strings.Contains(ev.Text, "retrying"))
	}
	if !retried {
		t.Errorf("the turn should say pi is retrying: %+v", events)
	}
}

// A prompt that overflows the window pi compacts the session for, then
// runs the agent again on what is left.
func TestPi_WaitsForTheRunAfterAnOverflow(t *testing.T) {
	scriptedPiCLI(t)
	events, res, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "[overflow]"})
	if err != nil {
		t.Fatalf("the turn should end with the answer after the compaction: %v", err)
	}
	if got := compactionPhases(events); got != "start,end" {
		t.Errorf("compaction phases = %q, want start,end", got)
	}
	if res.Output != "answered after the compaction" {
		t.Errorf("output = %q", res.Output)
	}
}

// settleAsks counts the times the runner asked pi whether it was done.
func settleAsks(t *testing.T, stdinPath string) int {
	t.Helper()
	raw, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var cmd struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(line), &cmd) == nil && cmd.ID == piSettleID && cmd.Type == "get_state" {
			n++
		}
	}
	return n
}

func TestPiSettle(t *testing.T) {
	const (
		idle       = `{"id":"settle","type":"response","command":"get_state","success":true,"data":{"isStreaming":false,"isCompacting":false}}`
		compacting = `{"id":"settle","type":"response","command":"get_state","success":true,"data":{"isStreaming":false,"isCompacting":true}}`
		streaming  = `{"id":"settle","type":"response","command":"get_state","success":true,"data":{"isStreaming":true,"isCompacting":false}}`
		start      = `{"type":"agent_start"}`
		end        = `{"type":"agent_end","messages":[]}`
	)
	cases := []struct {
		name    string
		records []string
		// asks and done are how often the runner asked for pi's state and
		// whether it closed the input, after all the records.
		asks int
		done bool
	}{
		{"idle after the run", []string{start, end, idle}, 1, true},
		{"the prompt refused", []string{`{"id":"prompt","type":"response","command":"prompt","success":false,"error":"busy"}`}, 0, true},
		{"the first get_state is no answer", []string{`{"id":"state","type":"response","command":"get_state","success":true,"data":{}}`, start}, 0, false},
		{"pi says it runs still", []string{start, end, streaming}, 1, false},
		{"compacting after the run", []string{start, end, `{"type":"compaction_start","reason":"threshold"}`, compacting}, 1, false},
		{"the compaction over", []string{start, end, `{"type":"compaction_start","reason":"threshold"}`, compacting,
			`{"type":"compaction_end","reason":"threshold","result":{"summary":"s"},"willRetry":false}`, idle}, 2, true},
		{"a retry pending", []string{start, end, `{"type":"auto_retry_start","attempt":1,"maxAttempts":3,"delayMs":2000}`, idle}, 1, false},
		{"the retry done", []string{start, end, `{"type":"auto_retry_start","attempt":1}`, idle, start, end, idle}, 2, true},
		{"the retries given up", []string{start, end, `{"type":"auto_retry_start","attempt":3}`, idle, start, end,
			`{"type":"auto_retry_end","success":false,"attempt":3}`, idle}, 2, true},
		{"an overflow, compacted, to run again", []string{start, end, `{"type":"compaction_start","reason":"overflow"}`, compacting,
			`{"type":"compaction_end","reason":"overflow","result":{"summary":"s"},"willRetry":true}`, idle}, 2, false},
		{"a failed compaction runs nothing again", []string{start, end, `{"type":"compaction_start","reason":"overflow"}`, compacting,
			`{"type":"compaction_end","reason":"overflow","willRetry":true,"errorMessage":"Context overflow recovery failed"}`, idle}, 2, true},
		{"the run after the overflow", []string{start, end, `{"type":"compaction_start","reason":"overflow"}`, compacting,
			`{"type":"compaction_end","reason":"overflow","result":{"summary":"s"},"willRetry":true}`, idle, start, end, idle}, 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			asks, done := 0, false
			s := &piSettle{ask: func() { asks++ }, done: func() { done = true }}
			for _, rec := range tc.records {
				var ev piEvent
				if err := json.Unmarshal([]byte(rec), &ev); err != nil {
					t.Fatalf("%s: %v", rec, err)
				}
				if done {
					t.Fatalf("the input closed before %s", rec)
				}
				s.handle(ev)
			}
			if asks != tc.asks || done != tc.done {
				t.Errorf("asked %d times, done %v; want %d, %v", asks, done, tc.asks, tc.done)
			}
		})
	}
}
