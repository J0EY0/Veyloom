package hub

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// A person adds to what they asked while the member's turn runs, on a real
// runtime, and the turn takes it in its runtime's own way (docs/design.md
// 5.23.2): one turn answers both.

func TestPiSmoke_Steer(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	steerRound(t, runners, store.NewAgent{Name: "Pi", Runtime: "pi", PermissionPreset: store.PermissionFullAuto})
}

func TestClaudeRealSmoke_Steer(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	steerRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto})
}

func TestCodexSmoke_Steer(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	steerRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionFullAuto})
}

// steerRound asks for a slow command and, while it runs, adds to the ask
// in its topic.
func steerRound(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := newSmokeRoom(t, runners, agent, dir)
	asked := r.say("Run the shell command `sleep 20` and wait for it to finish. Then reply with the single word DONE.", "")
	turn, thread := r.running(asked)
	r.untilTranscript(turn.ID, `"kind":"tool_call"`)
	r.say("While that runs: when you reply, put the word BANANA after DONE.", thread.ID)

	done := r.waitDone(1)
	if n := len(r.turnsOf()); n != 1 {
		t.Errorf("one turn answers both, not %d", n)
	}
	tx, _ := os.ReadFile(done.TranscriptPath)
	if !strings.Contains(string(tx), `"kind":"steer"`) {
		t.Errorf("the turn should have taken the message in:\n%s", tx)
	}
	reply := r.lastReply(thread.ID)
	if !strings.Contains(strings.ToUpper(reply), "BANANA") {
		t.Errorf("the reply should answer what was added, said %q", reply)
	}
	if q, err := r.s.ListQueuedWakes(r.ctx, r.agent.MachineID); err != nil || len(q) != 0 {
		t.Errorf("answered, nothing waits: %+v %v", q, err)
	}
	t.Logf("%s answered %q", agent.Runtime, reply)
}

// TestClaudeSmoke_Steer drives the real Claude Code CLI with the model
// played by fakeAnthropic (TestClaudeSmoke): a message passed while a
// command runs reaches the model with the command's result; one passed as
// the model answers with no tool call left is answered as a turn of its
// own before the CLI ends. Each time the hub has one turn answer both.
func TestClaudeSmoke_Steer(t *testing.T) {
	cli := claudeSmokeCLI(t)
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{ToolDir: filepath.Join(t.TempDir(), "tools"), ProxyBinary: cli.proxy})
	runners["claude"] = runtime.NewClaudeRunner(runtime.ClaudeConfig{Binary: cli.bin, StreamPartials: true, ProxyBinary: cli.proxy})
	r := newSmokeRoom(t, runners, store.NewAgent{Name: "Claude smoke", Runtime: "claude", PermissionPreset: store.PermissionFullAuto, Model: "claude-sonnet-4-5"}, t.TempDir())

	// With a command running.
	asked := r.say(`USE_TOOL Bash {"command":"sleep 4; echo slept","description":"wait a little"}`, "")
	turn, thread := r.running(asked)
	r.untilTranscript(turn.ID, `"kind":"tool_call"`)
	r.say("while it runs: note the word BANANA", thread.ID)
	done := r.waitDone(1)
	reply := r.lastReply(thread.ID)
	// The model is handed it inside the command's result, which the fake
	// model repeats.
	if !strings.Contains(reply, "while you were at work on it") || !strings.Contains(reply, "note the word BANANA") {
		t.Errorf("the model should have been handed the message with the result, said %q", reply)
	}
	if n := len(r.turnsOf()); n != 1 || !strings.Contains(transcriptOf(t, done), `"kind":"steer"`) {
		t.Errorf("one turn, which took it in: %d turns\n%s", n, transcriptOf(t, done))
	}

	// With the model answering and no tool call left.
	second := r.say("WAIT_MS 4000 and then answer", thread.ID)
	turn, _ = r.running(second)
	eventuallyWithin(t, time.Minute, func() bool { return cli.api.heldBack() == 1 }, "the model to be thinking")
	r.say("and one more thing: CHERRY", thread.ID)
	done = r.waitDone(2)
	if n := len(r.turnsOf()); n != 2 || !strings.Contains(transcriptOf(t, done), `"kind":"steer"`) {
		t.Errorf("one turn more, which took it in: %d turns\n%s", n, transcriptOf(t, done))
	}
	if !cli.api.asked("CHERRY") {
		t.Error("the CLI should have asked the model about the message, after its answer")
	}
	var said []string
	for _, m := range r.messagesOf(thread.ID) {
		if m.TurnID == done.ID {
			said = append(said, m.Body)
		}
	}
	// Its answer to the ask, then its answer to what was added, addressed.
	if len(said) != 2 || said[0] != "ok" || said[1] != "@alice ok" {
		t.Errorf("the turn's messages: %q", said)
	}
}

// running waits for the turn msg asked for to reach its machine, and
// returns it and its topic.
func (r *smokeRoom) running(msg store.Message) (store.Turn, store.Thread) {
	r.t.Helper()
	var turn store.Turn
	eventuallyWithin(r.t, time.Minute, func() bool {
		for _, t := range r.turnsOf() {
			if t.TriggerMessageID == msg.ID {
				turn = t
				r.h.turns.mu.Lock()
				at := r.h.turns.active[t.ID]
				r.h.turns.mu.Unlock()
				if at == nil {
					return false
				}
				at.mu.Lock()
				defer at.mu.Unlock()
				return at.dispatched
			}
		}
		return false
	}, "the turn for "+msg.Body)
	thread, err := r.s.GetThread(r.ctx, turn.ThreadID)
	if err != nil {
		r.t.Fatal(err)
	}
	return turn, thread
}

// untilTranscript waits for a running turn's transcript to hold part.
func (r *smokeRoom) untilTranscript(turnID, part string) {
	r.t.Helper()
	path := filepath.Join(r.h.turns.transcripts, turnID+".jsonl")
	eventuallyWithin(r.t, smokeTurnTimeout, func() bool {
		r.h.turns.TranscriptSoFar(turnID)
		data, _ := os.ReadFile(path)
		return strings.Contains(string(data), part)
	}, "the transcript to show "+part)
}

// turnsOf is the room's turns, newest first.
func (r *smokeRoom) turnsOf() []store.Turn {
	r.t.Helper()
	turns, err := r.s.ListRoomTurns(r.ctx, r.room.ID, 50)
	if err != nil {
		r.t.Fatal(err)
	}
	return turns
}

// messagesOf is a topic's messages, oldest first.
func (r *smokeRoom) messagesOf(threadID string) []store.Message {
	r.t.Helper()
	msgs, err := r.s.ListThreadMessages(r.ctx, threadID, 0, 200)
	if err != nil {
		r.t.Fatal(err)
	}
	return msgs
}
