package hub

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// A turn that may be stuck, on a real runtime (docs/design.md 5.23.8): the
// member runs a command that sleeps, and says nothing while it does; the
// hub finds the turn quiet, a person cancels it with a new session, the
// runtime stops, and the member's next turn starts a new session.

func TestPiSmoke_Quiet(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	quietRound(t, runners, store.NewAgent{Name: "Pi", Runtime: "pi", PermissionPreset: store.PermissionFullAuto})
}

func TestClaudeRealSmoke_Quiet(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	quietRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto})
}

func TestCodexSmoke_Quiet(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	quietRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionFullAuto})
}

// quietRound has the member sleep in a command, finds the turn quiet,
// cancels it with a new session and asks again.
func quietRound(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := newSmokeRoom(t, runners, agent, dir)
	sub := r.h.Subscribe(r.room.ID)
	defer sub.Close()
	r.say("Run the shell command `sleep 90` and wait for it to finish, then reply with the single word DONE.", "")

	// Asleep in the command: the runtime says nothing until it ends.
	var turn store.Turn
	deadline := time.After(3 * time.Minute)
	for turn.ID == "" {
		select {
		case ev := <-sub.Events():
			if ev.Kind == EventTurnEvent && ev.TurnEvent.Kind == runtime.EventToolCall && strings.Contains(ev.TurnEvent.Input, "sleep") {
				turns, err := r.s.ListRoomTurns(r.ctx, r.room.ID, 1)
				if err != nil || len(turns) != 1 {
					t.Fatalf("the turn: %+v %v", turns, err)
				}
				turn = turns[0]
			}
		case <-deadline:
			t.Fatal("no sleep started within 3 minutes")
		}
	}
	time.Sleep(2 * time.Second)
	r.h.turns.checkQuiet(time.Now().Add(11 * time.Minute))
	since, quiet := r.h.QuietSince([]string{turn.ID})[turn.ID]
	if !quiet {
		t.Fatalf("asleep in its command, the turn is not quiet\n%s", transcriptOf(t, turn))
	}

	// Cancelled with a new session: the runtime stops, the session ends.
	start := time.Now()
	if err := r.h.CancelTurn(r.ctx, turn.ID, true); err != nil {
		t.Fatal(err)
	}
	cancelled := r.waitEnded(1)
	took := time.Since(start)
	if cancelled.Status != store.TurnCancelled || took > time.Minute {
		t.Fatalf("cancelled in %v: %+v", took, cancelled)
	}
	// The session ends as the turn's end is wound up, before the member's
	// next turn may start.
	eventuallyWithin(t, 10*time.Second, func() bool {
		old, err := r.s.GetSession(r.ctx, cancelled.SessionID)
		return err == nil && old.EndReason == store.SessionCancelled
	}, "the session to end as a person asked")

	r.say("Reply with the single word KIWI.", "")
	next := r.waitDone(2)
	spec := specOf(t, next)
	if next.SessionID == cancelled.SessionID || spec.Session.Resume || !strings.Contains(spec.Prompt, "a person cancelled your last turn") {
		t.Errorf("the next turn, in a new session: %s then %s, %+v", cancelled.SessionID, next.SessionID, spec.Session)
	}
	// Told its last turn was stopped on purpose, it answers the new ask
	// alone rather than sleeping again.
	said := strings.Join(r.bodies(r.saidIn(next)), "\n")
	if !strings.Contains(strings.ToUpper(said), "KIWI") || strings.Contains(strings.ToUpper(said), "DONE") || ranSleep(t, next) {
		t.Errorf("the next turn said %q\n%s", said, transcriptOf(t, next))
	}
	t.Logf("%s: quiet since %s, cancelled in %v, then %q", agent.Runtime, since.Format(time.TimeOnly), took.Round(time.Second), r.bodies(r.saidIn(next)))
}

// ranSleep says the turn called a tool to sleep: what its transcript
// records of its tool calls, the brief it began with aside.
func ranSleep(t *testing.T, turn store.Turn) bool {
	t.Helper()
	for _, line := range strings.Split(transcriptOf(t, turn), "\n") {
		var rec struct {
			Kind  string         `json:"kind"`
			Event *runtime.Event `json:"event"`
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Kind == "event" && rec.Event != nil &&
			rec.Event.Kind == runtime.EventToolCall && strings.Contains(rec.Event.Input, "sleep") {
			return true
		}
	}
	return false
}
