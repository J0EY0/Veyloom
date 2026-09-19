package hub

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// TestPiSmoke_SessionLifecycle drives the real pi CLI through the hub: it
// spends a few model calls, so it runs only when VEYLOOM_PI_SMOKE=1 (and a
// test database is configured, and pi is installed and signed in):
//
//	VEYLOOM_PI_SMOKE=1 VEYLOOM_TEST_DATABASE_URL=... go test -run TestPiSmoke ./internal/hub/ -v
//
// What it proves is what no fake can: that pi starts a session in the file
// the hub names, remembers across turns through it, that moving the
// member's directory gets a fresh session instead of the prompt pi shows
// ("Fork this session into current directory?") when it is resumed by id
// from somewhere else, which no one is there to answer, that a session
// whose file is gone is replaced within the turn that finds out, and that
// pi, under the read-only preset, which has no shell, reads the room through
// the tools the extension gives it.
func TestPiSmoke_SessionLifecycle(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}

	sessionDir := filepath.Join(t.TempDir(), "sessions")
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: sessionDir, ToolDir: filepath.Join(t.TempDir(), "tools")})
	firstDir, secondDir := t.TempDir(), t.TempDir()
	r := newSmokeRoom(t, runners, store.NewAgent{
		Name: "Pi smoke", Runtime: "pi", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, firstDir)
	ctx, s, member := r.ctx, r.s, r.member
	say, waitDone, lastReply := r.say, r.waitDone, r.lastReply

	// Turn 1 starts the session in the file the hub named.
	asked := say("Remember the word banana. Reply with exactly: ok", "")
	first := waitDone(1)
	thread, err := s.ThreadForMessage(ctx, first.ReplyMessageID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.GetOpenSession(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.SessionID != session.ID || !session.Started() || session.WorkDir != firstDir {
		t.Fatalf("session after the first turn = %+v (turn ran in %s)", session, first.SessionID)
	}
	file := filepath.Join(sessionDir, session.ID+".jsonl")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("pi should keep the session where the hub said: %v", err)
	}
	t.Logf("turn 1: asked %q, pi said %q; session %s, pi calls it %s", asked.Body, lastReply(thread.ID), session.ID, session.Ref)

	// Turn 2 continues it: pi remembers what only the first turn told it.
	// The second brief holds what is new and no more, so the word is not in
	// it: the answer can only come from the session.
	before, _ := os.Stat(file)
	say("Which word did I ask you to remember? One word.", thread.ID)
	second := waitDone(2)
	after, _ := os.Stat(file)
	if second.SessionID != session.ID {
		t.Fatalf("the second turn ran in session %s, want %s", second.SessionID, session.ID)
	}
	if after.Size() <= before.Size() {
		t.Errorf("the session file should grow with the second turn: %d then %d bytes", before.Size(), after.Size())
	}
	if files, _ := os.ReadDir(sessionDir); len(files) != 1 {
		t.Errorf("session directory holds %d files, want the one session", len(files))
	}
	if prompt := promptOf(t, second); strings.Contains(strings.ToLower(prompt), "banana") {
		t.Errorf("the second brief should not repeat what the session has read:\n%s", prompt)
	}
	if reply := lastReply(thread.ID); !strings.Contains(strings.ToLower(reply), "banana") {
		t.Errorf("pi should remember the word, said %q", reply)
	}

	// The member moves: resuming would stall on pi's question, so the hub
	// opens a new session and the turn goes through.
	if _, err := s.UpdateMember(ctx, member.ID, store.MemberPatch{RepoPath: &secondDir}); err != nil {
		t.Fatal(err)
	}
	say("Reply with exactly: moved", thread.ID)
	third := waitDone(3)
	moved, err := s.GetOpenSession(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if moved.ID == session.ID || third.SessionID != moved.ID || moved.WorkDir != secondDir {
		t.Fatalf("session after the move = %+v, want a new one in %s", moved, secondDir)
	}
	old, err := s.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if old.EndReason != store.SessionDirChanged {
		t.Errorf("the session left behind ended as %q, want dir_changed", old.EndReason)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, moved.ID+".jsonl")); err != nil {
		t.Errorf("the new session should have a file of its own: %v", err)
	}
	t.Logf("turn 3 after the move: pi said %q in session %s", lastReply(thread.ID), moved.ID)

	// The session file disappears (a cleanup, a reinstall). The turn that
	// finds out starts over in a new session by itself and still answers.
	if err := os.Remove(filepath.Join(sessionDir, moved.ID+".jsonl")); err != nil {
		t.Fatal(err)
	}
	say("Reply with exactly: back", thread.ID)
	fourth := waitDone(4)
	recovered, err := s.GetOpenSession(ctx, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ID == moved.ID || fourth.SessionID != recovered.ID || !recovered.Started() {
		t.Fatalf("session after the file was lost = %+v, want a new, started one carrying turn %s", recovered, fourth.SessionID)
	}
	if lost, _ := s.GetSession(ctx, moved.ID); lost.EndReason != store.SessionNotFound {
		t.Errorf("the lost session ended as %q, want not_found", lost.EndReason)
	}
	if reply := lastReply(thread.ID); !strings.Contains(strings.ToLower(reply), "back") {
		t.Errorf("pi should have answered in the new session, said %q", reply)
	}
	data, _ := os.ReadFile(fourth.TranscriptPath)
	if !strings.Contains(string(data), `"kind":"restart"`) || !strings.Contains(string(data), "This is a new session") {
		t.Errorf("turn 4 should record its second run and tell pi it starts over:\n%s", data)
	}
	t.Logf("turn 4 after the session file was removed: pi said %q in session %s", lastReply(thread.ID), recovered.ID)

	// The room tools, for real: read-only pi has no shell, so the only way
	// to the answer is the tool the extension registered, which goes to the
	// turn's endpoint, to the machine, over the protocol to the hub and back.
	say("Call your list_topics tool, then tell me how many topics this chat has. Reply with just the number.", thread.ID)
	fifth := waitDone(5)
	tx, _ := os.ReadFile(fifth.TranscriptPath)
	if !strings.Contains(string(tx), `"kind":"tool_call"`) || !strings.Contains(string(tx), `"tool":"list_topics"`) {
		t.Fatalf("pi should have called list_topics:\n%s", tx)
	}
	if !strings.Contains(string(tx), "Topics, most recently active first") {
		t.Errorf("the tool should have answered with the directory:\n%s", tx)
	}
	if reply := lastReply(thread.ID); !strings.Contains(reply, "1") {
		t.Errorf("the chat has one topic, pi said %q", reply)
	}
	t.Logf("turn 5 with the room tools: pi said %q", lastReply(thread.ID))
}

// TestPiSmoke_ExtensionDialogs drives the real pi through the hub with an
// extension whose tool asks the person things (testdata/piask): pi's RPC
// mode hands its dialogs to the runner, which puts them on cards, and the
// answers given there are what the extension gets. Same terms as
// TestPiSmoke_SessionLifecycle.
func TestPiSmoke_ExtensionDialogs(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	ask, err := filepath.Abs("testdata/piask/ask.ts")
	if err != nil {
		t.Fatal(err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	r := newSmokeRoom(t, runners, store.NewAgent{
		Name: "Pi dialogs", Runtime: "pi", PermissionPreset: store.PermissionFullAuto,
		RoleCard:       "Answer in as few words as you can.",
		RuntimeOptions: map[string]any{"extra_args": []any{"-e", ask}},
	}, t.TempDir())

	r.say("Call your ask_person tool once, then reply with exactly the text it returned.", "")
	turn, asked := r.decideAllWith(1, func(a store.Approval) runtime.Decision {
		switch a.Tool {
		case "select":
			return runtime.Decision{Allow: true, Answer: json.RawMessage(`{"answers":{"1":["production"]}}`)}
		case "input":
			return runtime.Decision{Allow: true, Answer: json.RawMessage(`{"answers":{"1":["v9"]}}`)}
		case "editor":
			return runtime.Decision{Allow: true, Answer: json.RawMessage(`{"answers":{"1":["- fixed things\n- and more\n"]}}`)}
		}
		return runtime.Decision{Allow: true}
	})
	var got []string
	for _, a := range asked {
		got = append(got, string(a.Kind)+" "+a.Tool)
	}
	if strings.Join(got, ", ") != "tool_use confirm, question select, question input, question editor" {
		t.Errorf("requests = %q, want the confirmation, the choice, the line and the text", got)
	}
	var editor runtime.QuestionSet
	if json.Unmarshal(asked[3].Input, &editor) != nil || len(editor.Questions) != 1 || !editor.Questions[0].Multiline || editor.Questions[0].Default != "- fixed things\n" {
		t.Errorf("the editor = %s, want several lines starting from its prefill", asked[3].Input)
	}
	results := toolResults(t, turn, "ask_person")
	if len(results) != 1 || results[0] != `{"confirmed":true,"environment":"production","name":"v9","notes":"- fixed things\n- and more\n"}` {
		t.Errorf("the extension got %q, want the answers given on the cards", results)
	}
	if notices := noticesOf(t, turn); len(notices) != 1 || notices[0] != "Asked the person four things" {
		t.Errorf("notices = %q, want the extension's notification", notices)
	}
	t.Logf("pi asked %q; the extension got %q; pi said %q", got, results, r.lastReply(mustThread(t, r, turn).ID))
}

// mustThread is the thread a turn replied in.
func mustThread(t *testing.T, r *smokeRoom, turn store.Turn) store.Thread {
	t.Helper()
	thread, err := r.s.ThreadForMessage(r.ctx, turn.ReplyMessageID)
	if err != nil {
		t.Fatal(err)
	}
	return thread
}
