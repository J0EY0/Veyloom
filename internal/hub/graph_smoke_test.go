package hub

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// The wiki's relations on a real runtime (docs/design.md 5.17): before
// changing a file a member looks through the wiki around it. A decision
// names the file, and a pitfall that only links to the decision holds what
// to mind; walking out from the file finds the pitfall. Same terms as the
// other smoke tests of each runtime.

func TestPiSmoke_RelatedWiki(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	relatedWiki(t, runners, store.NewAgent{Name: "Pi smoke", Runtime: "pi", PermissionPreset: store.PermissionReadOnly, RoleCard: "Answer in as few words as you can."})
}

func TestClaudeRealSmoke_RelatedWiki(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	relatedWiki(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionReadOnly, RoleCard: "Answer in as few words as you can."})
}

func TestCodexSmoke_RelatedWiki(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	relatedWiki(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionReadOnly, RoleCard: "Answer in as few words as you can."})
}

// relatedWiki runs the round with agent the one member of a project whose
// wiki holds the three pages.
func relatedWiki(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	r := newSmokeRoom(t, runners, agent, t.TempDir())
	project, err := r.s.GetProject(r.ctx, r.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.h.WikiCatalog(r.ctx, project.ID); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(r.h.cfg.WikiDir, "projects", project.WikiSlug)
	for p, text := range map[string]string{
		"decisions/member-list.md": "---\ntype: Decision\ntitle: Member list first\ndescription: Where the member list goes in a prompt.\n---\n\n" +
			"internal/hub/brief.go writes the member list right after the project's description.\n",
		"pitfalls/empty-room.md": "---\ntype: Pitfall\ntitle: Empty room\ndescription: A room with no members.\n---\n\n" +
			"A room with no members once broke [that decision](/decisions/member-list.md): return before writing the list when nobody is there. Code word: HERON-5.\n",
		"facts/port.md": "---\ntype: Fact\ntitle: Port\ndescription: The port the hub listens on.\n---\n\nThe hub listens on 7788.\n",
	} {
		if err := os.WriteFile(filepath.Join(folder, filepath.FromSlash(p)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r.say("我准备改 internal/hub/brief.go。改之前先看看 wiki 里关于它有哪些决定和坑要注意，列出来，里面有暗号的把暗号也写上。", "")
	turn := r.waitDone(1)
	tx := transcriptOf(t, turn)
	reply := r.lastReply(mustThread(t, r, turn).ID)
	t.Logf("%s said %q", agent.Name, reply)
	// Claude and Codex name it with their MCP server's prefix.
	if !regexp.MustCompile(`"tool":"[^"]*related_wiki"`).MatchString(tx) {
		t.Errorf("%s should look at the relations of the file:\n%s", agent.Name, tx)
	}
	if !strings.Contains(reply, "HERON-5") {
		t.Errorf("%s should find the pitfall linked to the decision about the file: %q", agent.Name, reply)
	}
}
