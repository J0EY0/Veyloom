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

// A member reminds itself, on a real runtime (docs/design.md 5.23.4): asked
// to do something a little later, it sets a reminder with set_reminder,
// and the hub wakes it in the topic once it comes due, with its note, in
// the same piece of work.

func TestPiSmoke_Reminder(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	reminderRound(t, runners, store.NewAgent{Name: "Pi", Runtime: "pi", PermissionPreset: store.PermissionFullAuto})
}

func TestClaudeRealSmoke_Reminder(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	reminderRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto})
}

func TestCodexSmoke_Reminder(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	reminderRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionFullAuto})
}

// reminderRound asks the member to answer a little later, by a reminder.
func reminderRound(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := newSmokeRoom(t, runners, agent, dir)
	// A reminder may be seconds ahead, within the test's time.
	r.h.turns.reminders.min = 0
	asked := r.say("In 20 seconds, say the word PINEAPPLE in this topic. You cannot wait that long in this turn: "+
		"set a reminder for it with your set_reminder tool (in: 20s), then reply with the single word SET.", "")
	first := r.waitDone(1)
	thread, err := r.s.ThreadOfMessage(r.ctx, asked.ID)
	if err != nil {
		if thread, err = r.s.GetThread(r.ctx, first.ThreadID); err != nil {
			t.Fatal(err)
		}
	}
	reminders, err := r.s.ListThreadReminders(r.ctx, thread.ID)
	if err != nil || len(reminders) != 1 {
		t.Fatalf("one reminder set: %+v %v\n%s", reminders, err, transcriptOf(t, first))
	}
	if d := reminders[0].DueAt.Sub(reminders[0].CreatedAt); d < 10*time.Second || d > 40*time.Second {
		t.Errorf("due some 20s on: %v", d)
	}
	t.Logf("%s set %q, due %v on", agent.Runtime, reminders[0].Note, reminders[0].DueAt.Sub(reminders[0].CreatedAt).Round(time.Second))

	woken := r.waitDone(2)
	fired, err := r.s.GetReminder(r.ctx, reminders[0].ID)
	if err != nil || fired.Status != store.ReminderFired || woken.TriggerMessageID != fired.FiredMessageID {
		t.Fatalf("the reminder woke the member: %+v %v; turn %+v", fired, err, woken)
	}
	if woken.WokenByTurnID != first.ID || woken.ChainMessageID != first.ChainMessageID || woken.ThreadID != thread.ID {
		t.Errorf("in the same piece of work and topic: %+v", woken)
	}
	// What the woken turn said, in its reply or with send_message.
	var said []string
	for _, m := range r.messagesOf(thread.ID) {
		if m.TurnID == woken.ID && m.SenderKind == store.SenderAgent {
			said = append(said, m.Body)
		}
	}
	if !strings.Contains(strings.ToUpper(strings.Join(said, "\n")), "PINEAPPLE") {
		t.Errorf("the woken turn should do what the note said, said %q\n%s", said, transcriptOf(t, woken))
	}
	t.Logf("%s, woken: %q", agent.Runtime, said)
}
