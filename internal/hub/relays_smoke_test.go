package hub

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Members waking one another, on a real runtime (docs/design.md 5.22). The
// lead, asked by a person, hands a task to a helper with send_message; the
// helper is woken at once and does it, in the same piece of work. With the
// project's limit at one wake, the lead hands on two tasks in two messages:
// the second waits for the person, who lets it go on. Both are in the
// full-auto preset, working in a plain directory, so no worktrees.

func TestPiSmoke_Relays(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	relays(t, runners, store.NewAgent{Name: "Pi", Runtime: "pi", PermissionPreset: store.PermissionFullAuto})
}

func TestClaudeRealSmoke_Relays(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	relays(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionFullAuto})
}

func TestCodexSmoke_Relays(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	relays(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionFullAuto})
}

// relays runs the rounds with a lead and a helper made like agent.
func relays(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lead := agent
	lead.Name = agent.Name + " lead"
	lead.RoleCard = "You lead the project's members: you hand the work on to them rather than doing it yourself."
	r := newSmokeRoom(t, runners, lead, dir)
	helper := agent
	helper.Name = agent.Name + " helper"
	helper.RoleCard = "You help the lead. Do what you are asked, then say in a line what you did."
	helper.MachineID = r.h.Machines()[0].ID
	if helper.Runtime == "codex" {
		helper = forCodexSmoke(helper)
	}
	created, err := r.s.CreateAgent(r.ctx, helper)
	if err != nil {
		t.Fatal(err)
	}
	aide, err := r.s.CreateMember(r.ctx, store.NewMember{RoomID: r.room.ID, AgentID: created.ID, DisplayName: helper.Name, RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	file := func(name string) string {
		body, _ := os.ReadFile(filepath.Join(dir, name))
		return string(body)
	}

	// The lead hands the task on; the helper, woken by the lead's turn in
	// the same piece of work, does it.
	asked := r.sayTo(r.member, fmt.Sprintf("用 send_message 工具发一条消息（to 填 room），@%s 请它在项目根目录新建 notes.txt，内容是一行 RELAY-OK。你自己不要建这个文件，发完消息就结束。", aide.DisplayName), "")
	// The lead, the helper, and the lead summing up.
	turns := r.waitQuiet(3)
	handed := firstTurnOf(turns, r.member)
	work, ok := turnOf(turns, aide, store.TurnChat)
	if !ok || work.WokenByTurnID != handed.ID || work.ChainMessageID != asked.ID || handed.ChainMessageID != asked.ID {
		t.Fatalf("the helper's turn %+v, after the lead's %+v\n%s", work, handed, transcriptOf(t, handed))
	}
	summed, _ := turnOf(turns, r.member, store.TurnChat)
	trigger, err := r.s.GetMessage(r.ctx, summed.TriggerMessageID)
	if err != nil || summed.ID == handed.ID || !strings.Contains(trigger.Body, "handed on is done") || summed.ThreadID != handed.ThreadID {
		t.Fatalf("the lead did not sum up: %+v, answering %+v (%v)", summed, trigger, err)
	}
	if said, err := r.s.GetMessage(r.ctx, summed.ReplyMessageID); err != nil || !strings.HasPrefix(said.Body, "@alice") || !slices.Contains(said.Mentions, store.Mention{Kind: store.MentionUser, ID: r.user.ID}) {
		t.Errorf("the summing up is not addressed to the person: %+v %v", said, err)
	} else {
		t.Logf("the lead summed up: %q", said.Body)
	}
	sends := talkCalls(t, handed)
	if len(sends) == 0 {
		t.Fatalf("the lead called no send_message\n%s", transcriptOf(t, handed))
	}
	if !strings.Contains(file("notes.txt"), "RELAY-OK") {
		t.Errorf("notes.txt has %q\n%s", file("notes.txt"), transcriptOf(t, work))
	}
	if !work.Worked {
		t.Errorf("the helper's turn wrote a file, yet did no work\n%s", transcriptOf(t, work))
	}
	if calls := toolsCalled(t, handed); !slices.ContainsFunc(calls, workTool) && handed.Worked {
		t.Errorf("the lead only talked, calling %v, yet its turn did work", calls)
	} else {
		t.Logf("the lead called %v; its turn did work: %v", calls, handed.Worked)
	}

	// One wake at most: the lead's second message waits for the person,
	// who lets it go on.
	one := 1
	if _, err := r.s.UpdateProject(r.ctx, r.room.ProjectID, store.ProjectPatch{RelayLimit: &one}); err != nil {
		t.Fatal(err)
	}
	asked = r.sayTo(r.member, fmt.Sprintf("用 send_message 工具分两条消息派活（to 都填 room），每条都 @%s：第一条请它新建 a.txt，内容是一行 A；看到第一条的工具回话以后，再发第二条，请它新建 b.txt，内容是一行 B。你自己不要建文件，两条都发完就结束。", aide.DisplayName), "")
	before := len(turns)
	turns = r.waitQuiet(before + 2)
	for _, turn := range turns {
		if turn.TriggerMessageID == asked.ID {
			handed = turn
		}
	}
	if n := len(talkCalls(t, handed)); n < 2 {
		t.Fatalf("the lead sent %d messages\n%s", n, transcriptOf(t, handed))
	}
	holds, err := r.s.ListThreadRelayHolds(r.ctx, handed.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(holds) != 1 || holds[0].Reason != store.HoldLimit || holds[0].MemberID != aide.ID {
		t.Fatalf("the held wakes: %+v\n%s", holds, transcriptOf(t, handed))
	}
	note, err := r.s.GetMessage(r.ctx, holds[0].MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(note.Body, "@alice ") || len(note.Mentions) != 1 || note.Mentions[0].ID != r.user.ID {
		t.Errorf("the note of the held wake: %+v", note)
	}
	// Held back, the work is left to the person: nothing is summed up.
	if len(turns) != before+2 {
		t.Errorf("want %d turns, the work held back not summed up, got %d", before+2, len(turns))
	}
	if err := r.h.ContinueRelay(r.ctx, note.ID); err != nil {
		t.Fatal(err)
	}
	turns = r.waitQuiet(before + 3)
	if turns[0].MemberID != aide.ID || turns[0].ChainMessageID != note.ID || turns[0].WokenByTurnID != "" {
		t.Errorf("the turn let go on: %+v", turns[0])
	}
	if !strings.Contains(file("a.txt"), "A") || !strings.Contains(file("b.txt"), "B") {
		t.Errorf("a.txt has %q, b.txt %q", file("a.txt"), file("b.txt"))
	}
}

// waitQuiet waits until the room has n turns at least, none running and
// none new for a few seconds, newest first; it fails unless every one is
// done. A real model may do a little more than asked, such as naming a
// member once more.
func (r *smokeRoom) waitQuiet(n int) []store.Turn {
	r.t.Helper()
	start, since, seen := time.Now(), time.Now(), 0
	for {
		turns, err := r.s.ListRoomTurns(r.ctx, r.room.ID, 100)
		if err != nil {
			r.t.Fatal(err)
		}
		running := slices.ContainsFunc(turns, func(t store.Turn) bool { return t.Status == store.TurnRunning })
		if len(turns) != seen || running {
			seen, since = len(turns), time.Now()
		}
		if len(turns) >= n && time.Since(since) > 3*time.Second {
			for _, turn := range turns {
				if turn.Status != store.TurnDone {
					r.t.Fatalf("a %s turn ended %s: %s", turn.Kind, turn.Status, turn.Error)
				}
			}
			return turns
		}
		if time.Since(start) > 2*smokeTurnTimeout {
			r.t.Fatalf("the turns did not finish in time%s", r.stuck(r.room.ID))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// toolsCalled are the tools turn called, by the names its runtime gave
// them, in order.
func toolsCalled(t *testing.T, turn store.Turn) []string {
	t.Helper()
	var calls []string
	for _, line := range strings.Split(transcriptOf(t, turn), "\n") {
		var entry struct {
			Event runtime.Event `json:"event"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Event.Kind == runtime.EventToolCall {
			calls = append(calls, entry.Event.Tool)
		}
	}
	return calls
}

// talkCalls are the calls of send_message in turn's transcript, by the name
// its runtime gave the tool; each must be one talkTool knows, so a turn
// that only talks does no work.
func talkCalls(t *testing.T, turn store.Turn) []string {
	t.Helper()
	var calls []string
	for _, tool := range toolsCalled(t, turn) {
		if !strings.Contains(tool, runtime.MessageToolSend) {
			continue
		}
		if !talkTool(tool) {
			t.Errorf("%s is not known as a tool to talk with", tool)
		}
		calls = append(calls, tool)
	}
	return calls
}
