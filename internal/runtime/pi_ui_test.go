package runtime

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// runScriptedPi runs a turn on the scripted fake pi, prompt naming its
// dialogs. Each request to people is answered as decide says, or left
// alone when it returns nil. It returns the events and what the fake was
// answered, by keyword.
func runScriptedPi(t *testing.T, prompt string, decide func(Event) *Decision) ([]Event, map[string]json.RawMessage) {
	t.Helper()
	scriptedPiCLI(t)
	runner := NewPiRunner(PiConfig{})
	t.Cleanup(func() { runner.Close() })
	turn, err := runner.StartTurn(context.Background(), TurnSpec{Prompt: prompt})
	if err != nil {
		t.Fatal(err)
	}
	var events []Event
	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev, ok := <-turn.Events():
			if !ok {
				res, err := turn.Result()
				if err != nil {
					t.Fatalf("turn failed: %v", err)
				}
				answers := map[string]json.RawMessage{}
				if err := json.Unmarshal([]byte(res.Output), &answers); err != nil {
					t.Fatalf("output %q is not the fake's answers: %v", res.Output, err)
				}
				return events, answers
			}
			events = append(events, ev)
			if ev.Kind == EventApprovalRequest && decide != nil {
				if d := decide(ev); d != nil {
					if err := turn.Answer(ev.ApprovalID, *d); err != nil {
						t.Errorf("answer %s: %v", ev.Tool, err)
					}
				}
			}
		case <-timeout:
			t.Fatal("turn did not finish")
		}
	}
}

func questionOf(t *testing.T, ev Event) Question {
	t.Helper()
	var set QuestionSet
	if err := json.Unmarshal([]byte(ev.Input), &set); err != nil || len(set.Questions) != 1 {
		t.Fatalf("question input %q: %v", ev.Input, err)
	}
	return set.Questions[0]
}

// The prompt goes in as the RPC prompt command, after asking for the
// session's id.
func TestPi_CommandsOverStdin(t *testing.T) {
	_, stdinPath := scriptedPiCLI(t)
	events, res, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "line 1\nline 2 with 'quotes'"})
	if err != nil {
		t.Fatal(err)
	}
	if res.SessionRef != "pi-sess-1" || events[0].Kind != EventSession || events[0].SessionRef != "pi-sess-1" {
		t.Errorf("session: result %q, first event %+v", res.SessionRef, events[0])
	}
	raw, _ := os.ReadFile(stdinPath)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var state, prompt map[string]any
	if len(lines) != 2 || json.Unmarshal([]byte(lines[0]), &state) != nil || json.Unmarshal([]byte(lines[1]), &prompt) != nil {
		t.Fatalf("stdin = %q, want two commands", raw)
	}
	if state["type"] != "get_state" || prompt["type"] != "prompt" || prompt["message"] != "line 1\nline 2 with 'quotes'" {
		t.Errorf("commands = %v then %v", state, prompt)
	}
}

// Every dialog an extension opens reaches people, and pi gets their
// answer in the shape the dialog takes.
func TestPi_ExtensionDialogs(t *testing.T) {
	var asked []Event
	events, answers := runScriptedPi(t, "[confirm] [select] [input] [editor]", func(ev Event) *Decision {
		asked = append(asked, ev)
		switch ev.Tool {
		case "confirm":
			return &Decision{Allow: true}
		case "select":
			return &Decision{Allow: true, Answer: json.RawMessage(`{"answers":{"1":["production"]}}`)}
		case "input":
			return &Decision{Allow: true, Answer: json.RawMessage(`{"answers":{"1":["v2.0.0"]}}`)}
		}
		return &Decision{Allow: true, Answer: json.RawMessage(`{"answers":{"1":["- fixed more\n"]}}`)}
	})
	if len(asked) != 4 {
		t.Fatalf("requests = %+v", eventsOf(events, EventApprovalRequest))
	}
	if asked[0].ApprovalKind != "" || asked[0].Input != `{"message":"Allow rm -rf build?","title":"Dangerous command"}` {
		t.Errorf("confirm = %+v, want an approval with its title and message", asked[0])
	}
	for i, want := range []Question{
		{ID: "1", Question: "Which environment?", Options: []QuestionOption{{Label: "staging"}, {Label: "production"}}},
		{ID: "1", Question: "Release name?", Placeholder: "v1.2.3"},
		{ID: "1", Question: "Edit the changelog", Multiline: true, Default: "- fixed things\n"},
	} {
		ev := asked[i+1]
		if ev.ApprovalKind != ApprovalQuestion {
			t.Errorf("request %d = %+v, want a question", i+1, ev)
			continue
		}
		if got := questionOf(t, ev); !reflect.DeepEqual(got, want) {
			t.Errorf("question %d = %+v\n        want %+v", i+1, got, want)
		}
	}
	for keyword, want := range map[string]string{
		"[confirm]": `{"confirmed":true}`,
		"[select]":  `{"value":"production"}`,
		"[input]":   `{"value":"v2.0.0"}`,
		"[editor]":  `{"value":"- fixed more\n"}`,
	} {
		answerIs(t, answers, keyword, want)
	}

	// Declined, a dialog is cancelled, and a confirmation refused.
	_, answers = runScriptedPi(t, "[confirm] [select]", func(Event) *Decision { return &Decision{} })
	answerIs(t, answers, "[confirm]", `{"confirmed":false}`)
	answerIs(t, answers, "[select]", `{"cancelled":true}`)
}

// A dialog pi settles itself once its timeout passes is taken back from
// people, and gets no answer after that.
func TestPi_TimedOutDialogIsTakenBack(t *testing.T) {
	events, answers := runScriptedPi(t, "[timeout]", func(Event) *Decision { return nil })
	asked := eventsOf(events, EventApprovalRequest)
	withdrawn := eventsOf(events, EventApprovalWithdrawn)
	if len(asked) != 1 || len(withdrawn) != 1 || withdrawn[0].ApprovalID != asked[0].ApprovalID {
		t.Fatalf("requests %+v, withdrawn %+v", asked, withdrawn)
	}
	if string(answers["[timeout]"]) != `"no answer"` || len(answers) != 1 {
		t.Errorf("answers = %v, want none", answers)
	}
}

// What pi and its extensions say for people to see becomes notices; what
// only dresses pi's own screen does not.
func TestPi_NoticesFromExtensionsAndRetries(t *testing.T) {
	events, _ := runScriptedPi(t, "[notify] [status] [retry] [exterr]", nil)
	var notices []string
	for _, ev := range eventsOf(events, EventNotice) {
		notices = append(notices, ev.Level+": "+ev.Text)
	}
	want := []string{
		"warning: Command blocked by the permission gate",
		"warning: Pi: 529 overloaded; retrying in 2s, attempt 1 of 3",
		"error: Pi gave up after 3 attempts: 529 overloaded_error: Overloaded",
		"error: Pi extension gate.ts failed in tool_call: boom",
	}
	if strings.Join(notices, "\n") != strings.Join(want, "\n") {
		t.Errorf("notices:\n%s\nwant:\n%s", strings.Join(notices, "\n"), strings.Join(want, "\n"))
	}
}

// A prompt pi turns down ends the turn with pi's reason.
func TestPi_RejectedPrompt(t *testing.T) {
	scriptedPiCLI(t)
	_, _, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "[reject]"})
	if err == nil || err.Error() != "pi: Agent is busy" {
		t.Errorf("err = %v, want pi's reason", err)
	}
}

// An edit that failed changed nothing.
func TestPi_FailedEditChangesNoFile(t *testing.T) {
	fakePiCLI(t, `{"type":"tool_execution_start","toolCallId":"tc1","toolName":"write","args":{"path":"notes.md","content":"hi"}}
{"type":"tool_execution_end","toolCallId":"tc1","toolName":"write","result":"EACCES","isError":true}
{"type":"tool_execution_start","toolCallId":"tc2","toolName":"edit","args":{"path":"main.go"}}
{"type":"tool_execution_end","toolCallId":"tc2","toolName":"edit","result":"ok","isError":false}
{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"done"}],"stopReason":"stop"}}
{"type":"agent_end","messages":[]}
`, 0, "")
	events, _, err := runPi(t, PiConfig{}, TurnSpec{Prompt: "x"})
	if err != nil {
		t.Fatal(err)
	}
	var changed []string
	for _, ev := range eventsOf(events, EventFileChanged) {
		changed = append(changed, ev.Path)
	}
	if strings.Join(changed, ",") != "main.go" {
		t.Errorf("files changed = %q, want only the edit that went through", changed)
	}
}
