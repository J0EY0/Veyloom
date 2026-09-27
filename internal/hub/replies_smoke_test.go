package hub

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Every turn leaves a word in its topic, on a real runtime (docs/design.md
// 5.24): a member answering with send_message alone has that as its reply,
// addressed; one told to write nothing at all is asked for its reply, and
// what it says then is its word. Nowhere is a reply made up for it.

func TestPiSmoke_Replies(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools"), RecordDir: os.Getenv("VEYLOOM_RECORD_DIR")})
	replyRounds(t, runners, store.NewAgent{Name: "Pi", Runtime: "pi", PermissionPreset: store.PermissionFullAuto})
}

func TestClaudeRealSmoke_Replies(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	replyRounds(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto})
}

func TestCodexSmoke_Replies(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	replyRounds(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionFullAuto})
}

// replyRounds has the member answer with send_message alone, then end a
// turn without a word.
func replyRounds(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "NOTES.md"), []byte("The build is green.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newSmokeRoom(t, runners, agent, dir)

	// Its answer, sent.
	r.say("Answer with your send_message tool: post the single word KIWI to this topic. "+
		"That message is your whole answer, so after the call end your turn without writing anything else.", "")
	sent := r.waitDone(1)
	said := r.saidIn(sent)
	if !strings.Contains(strings.ToUpper(strings.Join(r.bodies(said), "\n")), "KIWI") {
		t.Errorf("what it sent is in its topic, said %q\n%s", r.bodies(said), transcriptOf(t, sent))
	}
	r.addressed(sent, said)
	t.Logf("%s, answered by send_message: %q", agent.Runtime, r.bodies(said))

	// No word at all: asked for it, it answers.
	r.say("Read NOTES.md with a tool. Write no text at all in this turn, not even a word when you are done: "+
		"just read the file and end the turn.", "")
	quiet := r.waitDone(2)
	tx := transcriptOf(t, quiet)
	said = r.saidIn(quiet)
	if !strings.Contains(tx, `"reply_asked"`) {
		t.Logf("%s wrote something after all, so was not asked: %q", agent.Runtime, r.bodies(said))
	} else {
		t.Logf("%s said nothing and was asked; then: %q", agent.Runtime, r.bodies(said))
	}
	if len(said) == 0 {
		t.Errorf("asked for its reply, it gives one\n%s", tx)
	}
	r.addressed(quiet, said)
}

// saidIn is what a turn said in its topic, the head included.
func (r *smokeRoom) saidIn(turn store.Turn) []store.Message {
	r.t.Helper()
	thread, err := r.s.GetThread(r.ctx, turn.ThreadID)
	if err != nil {
		r.t.Fatal(err)
	}
	root, err := r.s.GetMessage(r.ctx, thread.RootMessageID)
	if err != nil {
		r.t.Fatal(err)
	}
	var said []store.Message
	for _, m := range append([]store.Message{root}, r.messagesOf(thread.ID)...) {
		if m.TurnID == turn.ID && m.SenderKind == store.SenderAgent && m.Body != "" {
			said = append(said, m)
		}
		if strings.Contains(m.Body, "(no reply)") {
			r.t.Errorf("a reply made up: %+v", m)
		}
	}
	return said
}

// addressed checks the turn's last word is its reply, addressed to the
// person who asked.
func (r *smokeRoom) addressed(turn store.Turn, said []store.Message) {
	r.t.Helper()
	if len(said) == 0 {
		return
	}
	last := said[len(said)-1]
	if turn.ReplyMessageID != last.ID || !slices.Contains(last.Mentions, store.Mention{Kind: store.MentionUser, ID: r.user.ID}) {
		r.t.Errorf("the last word, addressed: %+v (the turn's reply %s)", last, turn.ReplyMessageID)
	}
}

func (r *smokeRoom) bodies(msgs []store.Message) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Body
	}
	return out
}
