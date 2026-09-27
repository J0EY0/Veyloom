package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The replay tests (docs/design.md 5.23.9): what each CLI printed in real
// turns, recorded with RunnerOptions.RecordDir while the smoke tests ran,
// is played by the fakes to the runners as the CLI's output again, and
// the events and the result the runner makes of it are kept in a golden
// file beside it. A CLI release that prints otherwise shows as a
// difference once a turn of it is recorded anew. To take one in:
//
//	VEYLOOM_RECORD_DIR=/tmp/rec VEYLOOM_CODEX_SMOKE=1 go test ./internal/hub -run TestCodexSmoke_Replies
//	VEYLOOM_REPLAY_IMPORT=codex/read-file=/tmp/rec/codex-123.jsonl go test ./internal/runtime -run TestReplay -update
//
// Taking one in drops the paths of the machine it was recorded on, and a
// pi recording's answers to the runner's own commands, which the fake
// gives itself.

// updateReplay rewrites the replay tests' golden files from what the
// runners make of the recordings now.
var updateReplay = flag.Bool("update", false, "rewrite the golden files of the replay tests")

func TestReplay(t *testing.T) {
	importRecordings(t)
	paths, err := filepath.Glob(filepath.Join("testdata", "replay", "*", "*.jsonl"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no recordings under testdata/replay: %v", err)
	}
	for _, path := range paths {
		runtimeName := filepath.Base(filepath.Dir(path))
		name := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		t.Run(runtimeName+"/"+name, func(t *testing.T) {
			events, res, err := replayRecording(t, runtimeName, path)
			replayGolden(t, strings.TrimSuffix(path, ".jsonl")+".golden", describeRun(events, res, err))
		})
	}
}

// replayRecording plays the recording at path to the runner of
// runtimeName, as it is set up on a machine.
func replayRecording(t *testing.T, runtimeName, path string) ([]Event, Result, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	spec := TurnSpec{Prompt: "replayed", WorkDir: t.TempDir()}
	switch runtimeName {
	case "claude":
		fakeClaudeCLI(t, string(data), 0, "")
		return runClaude(t, DefaultClaudeConfig(), spec)
	case "pi":
		fakePiCLI(t, string(data), 0, "")
		return runPi(t, DefaultPiConfig(), spec)
	case "codex":
		h := newCodexHarness(t)
		// The fake runs in the turn's directory.
		abs, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("VEYLOOM_FAKE_CODEX_OUTPUT", abs)
		cfg := DefaultCodexConfig()
		cfg.Binary = h.runner.cfg.Binary
		turn, err := NewCodexRunner(cfg).StartTurn(context.Background(), spec)
		if err != nil {
			t.Fatalf("StartTurn: %v", err)
		}
		events := drain(t, turn)
		res, err := turn.Result()
		return events, res, err
	}
	t.Fatalf("no runner for %s", runtimeName)
	return nil, Result{}, nil
}

// describeRun writes what a runner made of a turn, one event a line, then
// the result: what the golden files hold. When things happened and their
// numbers are the machine's and the hub's, not the CLI's, and are left out.
func describeRun(events []Event, res Result, err error) string {
	var b strings.Builder
	for _, ev := range events {
		// A quota's reset is a moment, written in UTC wherever the test runs.
		if ev.Quota != nil {
			q := *ev.Quota
			q.ResetsAt = q.ResetsAt.UTC()
			ev.Quota = &q
		}
		var fields map[string]any
		data, _ := json.Marshal(ev)
		_ = json.Unmarshal(data, &fields)
		delete(fields, "at")
		delete(fields, "seq")
		line, _ := json.Marshal(fields)
		b.Write(line)
		b.WriteByte('\n')
	}
	result, _ := json.Marshal(struct {
		Output     string      `json:"output"`
		SessionRef string      `json:"session_ref,omitempty"`
		Usage      Usage       `json:"usage"`
		Failure    FailureKind `json:"failure,omitempty"`
	}{res.Output, res.SessionRef, res.Usage, res.Failure})
	fmt.Fprintf(&b, "result %s\n", result)
	if err != nil {
		fmt.Fprintf(&b, "error %v\n", err)
	}
	return b.String()
}

// replayGolden compares got with the golden file at path, or writes it
// there under -update.
func replayGolden(t *testing.T, path, got string) {
	t.Helper()
	if *updateReplay {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (go test -run TestReplay -update writes it)", path, err)
	}
	if got != string(want) {
		wl, gl := strings.Split(string(want), "\n"), strings.Split(got, "\n")
		for i := 0; i < max(len(wl), len(gl)); i++ {
			var w, g string
			if i < len(wl) {
				w = wl[i]
			}
			if i < len(gl) {
				g = gl[i]
			}
			if w != g {
				t.Fatalf("%s no longer matches at line %d; if the change is meant, rewrite it with -update:\n- %s\n+ %s", path, i+1, w, g)
			}
		}
	}
}

// importRecordings takes in the recordings VEYLOOM_REPLAY_IMPORT names, as
// runtime/name=path, comma-separated.
func importRecordings(t *testing.T) {
	t.Helper()
	spec := os.Getenv("VEYLOOM_REPLAY_IMPORT")
	if spec == "" {
		return
	}
	for _, item := range strings.Split(spec, ",") {
		target, from, ok := strings.Cut(item, "=")
		runtimeName, name, named := strings.Cut(target, "/")
		if !ok || !named || runtimeName == "" || name == "" {
			t.Fatalf("VEYLOOM_REPLAY_IMPORT: %q is not runtime/name=path", item)
		}
		data, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join("testdata", "replay", runtimeName)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".jsonl"), sanitizeRecording(runtimeName, data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// scratchPath is a directory a test or a turn ran in on the machine that
// recorded, under the system's temporary directory.
var scratchPath = regexp.MustCompile(`(?:/private)?/var/folders/[^/"\\]+/[^/"\\]+/T/[^/"\\]+(?:/[0-9]+)?`)

// sanitizeRecording takes out of a recording what belongs to the machine
// it was made on: its temporary directories and its home directory. From
// a pi recording it drops pi's answers to the runner's own commands, which
// the fake gives when it plays the rest.
func sanitizeRecording(runtimeName string, data []byte) []byte {
	out := scratchPath.ReplaceAll(data, []byte("/tmp/work"))
	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/" {
		out = bytes.ReplaceAll(out, []byte(home), []byte("/home/user"))
	}
	if runtimeName != "pi" {
		return out
	}
	var kept [][]byte
	for _, line := range bytes.SplitAfter(out, []byte("\n")) {
		var ev struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(line, &ev) == nil && ev.Type == "response" {
			continue
		}
		kept = append(kept, line)
	}
	return bytes.Join(kept, nil)
}
