package hub

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// TestClaudeSmoke_Pauses drives the real Claude Code CLI against
// fakeAnthropic answering with errors (TestClaudeSmoke): what the CLI
// names of the failed call pauses the account for the right reason
// (docs/design.md 5.23.3), and what is asked meanwhile waits.
func TestClaudeSmoke_Pauses(t *testing.T) {
	cli := claudeSmokeCLI(t)
	// Retrying a 429 at the CLI's own pace would take minutes.
	t.Setenv("CLAUDE_CODE_MAX_RETRIES", "0")
	for _, c := range []struct {
		name, directive string
		want            store.PauseReason
	}{
		{"auth", "API_ERROR 401 authentication_error invalid x-api-key", store.PauseAuth},
		{"rate limit", "API_ERROR 429 rate_limit_error Number of requests has exceeded your rate limit", store.PauseRateLimit},
		{"billing", "API_ERROR 400 invalid_request_error Your credit balance is too low to access the Anthropic API.", store.PauseQuota},
	} {
		t.Run(c.name, func(t *testing.T) {
			runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{ToolDir: filepath.Join(t.TempDir(), "tools"), ProxyBinary: cli.proxy})
			runners["claude"] = runtime.NewClaudeRunner(runtime.ClaudeConfig{Binary: cli.bin, StreamPartials: true, ProxyBinary: cli.proxy})
			r := newSmokeRoom(t, runners, store.NewAgent{Name: "Claude smoke", Runtime: "claude", PermissionPreset: store.PermissionFullAuto, Model: "claude-sonnet-4-5"}, t.TempDir())
			r.say(c.directive, "")
			turn := r.waitEnded(1)
			if turn.Status != store.TurnFailed {
				t.Fatalf("the turn: %+v", turn)
			}
			pauses := r.h.Pauses(r.ctx)
			if len(pauses) != 1 || pauses[0].Reason != c.want || pauses[0].Runtime != "claude" {
				t.Fatalf("pauses = %+v, want one of %s; the turn failed with %q", pauses, c.want, turn.Error)
			}
			r.say("and then this", "")
			time.Sleep(time.Second)
			if n := len(r.turnsOf()); n != 1 {
				t.Errorf("nothing starts under the pause: %d turns", n)
			}
			t.Logf("%s: paused for %s, the CLI said %q", c.name, pauses[0].Reason, turn.Error)
		})
	}
}

// The runtimes that tell how their account stands do: Codex as it goes,
// Claude (on claude.ai) when it changes. Pi tells nothing.
func TestCodexSmoke_Quota(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionFullAuto}, t.TempDir())
	r.say("Reply with the single word OK.", "")
	r.waitDone(1)
	q, ok := r.h.Machines()[0].Quotas["codex"]
	if !ok || q.UsedPercent == nil || q.Window == "" {
		t.Fatalf("quotas = %+v", r.h.Machines()[0].Quotas)
	}
	t.Logf("codex: %s window %d%% used, resets %s, limited %v", q.Window, *q.UsedPercent, q.ResetsAt.Local().Format(time.RFC3339), q.Limited)
}

func TestClaudeRealSmoke_Quota(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	r := newSmokeRoom(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto}, t.TempDir())
	r.say("Reply with the single word OK.", "")
	turn := r.waitDone(1)
	q, ok := r.h.Machines()[0].Quotas["claude"]
	// Told only when it changes: a turn may well pass without it.
	used := -1
	if q.UsedPercent != nil {
		used = *q.UsedPercent
	}
	t.Logf("claude reported a quota: %v; %s window %d%% used, resets %s, limited %v", ok, q.Window, used, q.ResetsAt.Local().Format(time.RFC3339), q.Limited)
	if ok && q.Limited {
		t.Errorf("the turn went well on an account out of quota: %+v", turn)
	}
}

// waitEnded waits for the room's n-th turn to end, however it ends.
func (r *smokeRoom) waitEnded(n int) store.Turn {
	r.t.Helper()
	var ended store.Turn
	eventuallyWithin(r.t, smokeTurnTimeout, func() bool {
		turns := r.turnsOf()
		if len(turns) == n && turns[0].Status != store.TurnRunning {
			ended = turns[0]
			return true
		}
		return false
	}, "the turn to end")
	return ended
}
