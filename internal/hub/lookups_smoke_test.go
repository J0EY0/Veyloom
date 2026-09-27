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

// A search the chat's turns made in vain reaches the maintainer, on a real
// runtime (docs/design.md 5.23.7): Coder searched 审批 超时, found nothing
// as written, and read the English page on approval timeouts next; the
// upkeep puts the words on that page, and the same search finds it after.

func TestPiSmoke_Lookups(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	lookupsRound(t, runners, store.NewAgent{Name: "Keeper", Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
}

func TestClaudeRealSmoke_Lookups(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	lookupsRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Keeper", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionReadOnly})
}

func TestCodexSmoke_Lookups(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	lookupsRound(t, wikiSmokeRunners(t), store.NewAgent{Name: "Keeper", Runtime: "codex", PermissionPreset: store.PermissionReadOnly})
}

// lookupsRound has a scripted Coder search in vain and read the page it
// wanted, then the real maintainer go over it.
func lookupsRound(t *testing.T, runners map[string]runtime.Runner, keeper store.NewAgent) {
	keeper.RoleCard = "You keep the team's wiki in order."
	r := newSmokeRoom(t, runners, keeper, t.TempDir())
	manual := store.UpkeepManual
	if _, err := r.s.UpdateProject(r.ctx, r.room.ProjectID, store.ProjectPatch{WikiUpkeep: &upkeepOn, WikiMaintainer: &r.member.ID, WikiMaintainerTrigger: &manual}); err != nil {
		t.Fatal(err)
	}
	options := func(calls ...any) map[string]any { return map[string]any{"reply": "Done.", "tool_calls": calls} }
	coderAgent, err := r.s.CreateAgent(r.ctx, store.NewAgent{Name: "Coder", Runtime: "fake", MachineID: r.agent.MachineID, PermissionPreset: store.PermissionReadOnly,
		RuntimeOptions: options(call(runtime.WikiToolWrite, map[string]any{
			"type": "Fact", "slug": "approval-timeouts", "title": "Approval timeouts",
			"description": "How long an approval waits before it times out.", "body": "An approval waits ten minutes; after that the turn goes on without it.",
		}))})
	if err != nil {
		t.Fatal(err)
	}
	coder, err := r.s.CreateMember(r.ctx, store.NewMember{RoomID: r.room.ID, AgentID: coderAgent.ID, DisplayName: "Coder"})
	if err != nil {
		t.Fatal(err)
	}
	ask := func(body string) {
		t.Helper()
		if _, err := r.h.PostUserMessage(r.ctx, store.NewMessage{RoomID: r.room.ID, UserID: r.user.ID, Body: "@Coder " + body,
			Mentions: []store.Mention{{Kind: store.MentionAgent, ID: coder.ID}}}); err != nil {
			t.Fatal(err)
		}
	}
	ask("write down how long an approval waits")
	r.waitDone(1)
	if _, err := r.s.UpdateAgent(r.ctx, coderAgent.ID, store.NewAgent{Name: "Coder", Runtime: "fake", MachineID: coderAgent.MachineID, PermissionPreset: store.PermissionReadOnly,
		RuntimeOptions: options(
			call(runtime.WikiToolSearch, map[string]any{"query": "审批 超时"}),
			call(runtime.WikiToolRead, map[string]any{"path": "/facts/approval-timeouts.md"}),
		)}); err != nil {
		t.Fatal(err)
	}
	ask("审批超时是多久？")
	r.waitDone(2)
	if hits, _ := r.h.SearchWiki(r.ctx, r.room.ProjectID, "审批 超时", 10); len(hits) != 0 {
		t.Fatalf("the search should find nothing yet: %v", hitPaths(hits))
	}

	if _, err := r.h.StartUpkeep(r.ctx, r.room.ProjectID); err != nil {
		t.Fatal(err)
	}
	upkeep := r.waitDone(3)
	if upkeep.Kind != store.TurnUpkeep || !strings.Contains(specOf(t, upkeep).Prompt, `"审批 超时" in the project wiki, by turn `) {
		t.Fatalf("the upkeep is not told of the search: %+v", upkeep)
	}
	hits, err := r.h.SearchWiki(r.ctx, r.room.ProjectID, "审批 超时", 10)
	if err != nil {
		t.Fatal(err)
	}
	// The page that answers it is found; the maintainer may well have
	// written another about the topic besides.
	if paths := hitPaths(hits); !slices.Contains(paths, "/facts/approval-timeouts.md") {
		t.Errorf("after the upkeep the search finds %v; it went:\n%s", paths, transcriptOf(t, upkeep))
	}
	page, err := r.h.WikiPage(r.ctx, r.room.ProjectID, "/facts/approval-timeouts.md")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: the page now says %q, tags %v; it said %q", keeper.Runtime, page.Description, page.Tags, r.lastReply(upkeep.ThreadID))
}
