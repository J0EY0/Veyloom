package hub

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// The skill library across teams, on a real runtime (docs/design.md 5.10,
// 5.12, 5.15): a skill one project's team owns is used in another
// project's chat, where a person then corrects what it said; the use is
// recorded, and the owning team's maintainer, in its upkeep, is told of the
// use and reads the turn with the correction after it. The skill is set
// right, on trial, by the member corrected, which has it installed, or by
// the maintainer. Same terms as the other smoke tests of each runtime.

func TestPiSmoke_CrossTeam(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	crossTeam(t, runners, store.NewAgent{Name: "Pi", Runtime: "pi", PermissionPreset: store.PermissionReadOnly, RoleCard: "You work on the shop with the team."})
}

func TestClaudeRealSmoke_CrossTeam(t *testing.T) {
	if os.Getenv("VEYLOOM_CLAUDE_REAL_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CLAUDE_REAL_SMOKE=1 to run against the real claude CLI and model")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skipf("claude is not installed: %v", err)
	}
	crossTeam(t, wikiSmokeRunners(t), store.NewAgent{Name: "Claude", Runtime: "claude", Model: "haiku", PermissionPreset: store.PermissionReadOnly, RoleCard: "You work on the shop with the team."})
}

func TestCodexSmoke_CrossTeam(t *testing.T) {
	if os.Getenv("VEYLOOM_CODEX_SMOKE") != "1" {
		t.Skip("set VEYLOOM_CODEX_SMOKE=1 to run against the real codex CLI")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex is not installed: %v", err)
	}
	crossTeam(t, wikiSmokeRunners(t), store.NewAgent{Name: "Codex", Runtime: "codex", PermissionPreset: store.PermissionEditWithApproval, RoleCard: "You work on the shop with the team."})
}

// crossTeam runs the round: the smoke room's project is the team owning
// the skill, a second project uses it.
func crossTeam(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent) {
	t.Helper()
	ownerDir, err := os.MkdirTemp("", "payments-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(ownerDir) })
	r := newSmokeRoom(t, runners, agent, ownerDir)
	name := "Payments"
	owner, err := r.s.UpdateProject(r.ctx, r.room.ProjectID, store.ProjectPatch{Name: &name})
	if err != nil {
		t.Fatal(err)
	}

	// The skill, owned by the Payments team.
	b, err := r.h.wikis.library(r.ctx)
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.Writer(okf.Human("smoke"))
	if err != nil {
		t.Fatal(err)
	}
	d := okf.New("Skill")
	d.SetString(okf.KeyName, "refund-window")
	d.SetString(okf.KeyTitle, "Refund window")
	d.SetString(okf.KeyDescription, "Use this skill whenever someone asks how long customers have to ask for a refund.")
	d.SetMetadata(wiki.TeamKey, owner.WikiSlug)
	d.SetBody("Customers may ask for a refund within 45 days of delivery.")
	if _, err := w.Create(wiki.SkillPath("refund-window"), d); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(r.ctx, "Smoke test"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.h.InstallSkill(r.ctx, "refund-window", r.agent.ID, true); err != nil {
		t.Fatal(err)
	}

	// Another project's member, of the same agent, uses it.
	storeDir, err := os.MkdirTemp("", "storefront-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(storeDir) })
	_, shop, err := r.s.CreateProject(r.ctx, store.NewProject{Name: "Storefront", RepoPath: storeDir})
	if err != nil {
		t.Fatal(err)
	}
	seller, err := r.s.CreateMember(r.ctx, store.NewMember{RoomID: shop.ID, AgentID: r.agent.ID, DisplayName: agent.Name, RepoPath: storeDir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.h.PostUserMessage(r.ctx, store.NewMessage{
		RoomID: shop.ID, UserID: r.user.ID, Body: "A customer asks how many days they have to ask for a refund. Use the skill that covers it, then reply in one short sentence.",
		Mentions: []store.Mention{{Kind: store.MentionAgent, ID: seller.ID}},
	}); err != nil {
		t.Fatal(err)
	}
	// What the shop's turns ask of a person, a person there allows.
	var asked []string
	used := r.waitTurnIn(shop.ID, 1, r.allowIn(shop.ID, &asked))
	if len(used.SkillsUsed) != 1 || used.SkillsUsed[0] != "refund-window" {
		tx, _ := os.ReadFile(used.TranscriptPath)
		t.Fatalf("the shop's turn should be recorded as using the skill, recorded %v:\n%s", used.SkillsUsed, excerpt(string(tx), 4000))
	}
	shopTopic, err := r.s.ThreadForMessage(r.ctx, used.ReplyMessageID)
	if err != nil {
		t.Fatal(err)
	}
	answer := r.lastReply(shopTopic.ID)
	if !strings.Contains(answer, "45") {
		t.Errorf("the shop's member should have answered from the skill, said %q", answer)
	}
	uses, err := r.h.SkillUses(r.ctx, "refund-window", 10)
	if err != nil || len(uses) != 1 || uses[0].TurnID != used.ID || uses[0].ProjectName != "Storefront" {
		t.Errorf("the skill's page lists the use: %+v %v", uses, err)
	}
	// A person there sets it right; the maintainer is to find this.
	if _, err := r.h.PostUserMessage(r.ctx, store.NewMessage{
		RoomID: shop.ID, ThreadID: shopTopic.ID, UserID: r.user.ID,
		Body:     "That is out of date: since September, items bought on sale can only be refunded within 30 days; full-price items keep the 45 days. Acknowledge in one short sentence.",
		Mentions: []store.Mention{{Kind: store.MentionAgent, ID: seller.ID}},
	}); err != nil {
		t.Fatal(err)
	}
	corrected := r.waitTurnIn(shop.ID, 2, r.allowIn(shop.ID, &asked))
	if len(asked) > 0 {
		t.Logf("the shop's turns asked a person: %s", strings.Join(asked, " | "))
	}

	// The owning team's maintainer is told of it and reads it.
	manual := store.UpkeepManual
	if _, err := r.s.UpdateProject(r.ctx, owner.ID, store.ProjectPatch{WikiMaintainer: &r.member.ID, WikiMaintainerTrigger: &manual}); err != nil {
		t.Fatal(err)
	}
	status, err := r.h.UpkeepStatus(r.ctx, owner.ID)
	if err != nil || status.Waiting.Uses == 0 {
		t.Fatalf("the use waits for the owners: %+v %v", status.Waiting, err)
	}
	if _, err := r.h.StartUpkeep(r.ctx, owner.ID); err != nil {
		t.Fatal(err)
	}
	upkeep := r.waitDone(1)
	if upkeep.Kind != store.TurnUpkeep {
		t.Fatalf("the owners' first turn should be the upkeep: %+v", upkeep)
	}
	brief := promptOf(t, upkeep)
	if !strings.Contains(brief, "Turns of other projects that used this team's skills (") || !strings.Contains(brief, used.ID+`: project "Storefront"`) {
		t.Errorf("the upkeep's brief should list the use:\n%s", brief)
	}
	// The brief is in the transcript too, so look at the calls alone.
	tx, _ := os.ReadFile(upkeep.TranscriptPath)
	var calls []string
	readIt := false
	for _, line := range strings.Split(string(tx), "\n") {
		if strings.Contains(line, `"kind":"tool_call"`) {
			calls = append(calls, excerpt(line, 300))
			readIt = readIt || strings.Contains(line, runtime.RoomToolReadTurn) && strings.Contains(line, used.ID)
		}
	}
	if !readIt {
		t.Errorf("the maintainer should have read the shop's turn with read_turn; it called:\n%s", strings.Join(calls, "\n"))
	}
	if status, _ := r.h.UpkeepStatus(r.ctx, owner.ID); status.Waiting.Uses != 0 {
		t.Errorf("the use should be gone over: %+v", status.Waiting)
	}
	// What came of the correction: the skill set right, at once, and on
	// trial for it (design.md 5.15). The agent it is installed for may do
	// that itself, in the turn a person corrected it, or else the owning
	// team's maintainer in its upkeep.
	body := ""
	if page, err := b.Page(wiki.SkillPath("refund-window")); err == nil {
		body = page.Doc.Body()
	}
	trial, trialErr := r.s.OpenSkillTrial(r.ctx, "refund-window")
	by := map[string]string{corrected.ID: "the shop's member, in the turn it was corrected", upkeep.ID: "the owners' maintainer, in its upkeep"}[trial.TurnID]
	if !strings.Contains(body, "30") || trialErr != nil || by == "" {
		var answered []string
		for _, line := range strings.Split(string(tx), "\n") {
			if strings.Contains(line, `"kind":"tool_result"`) {
				answered = append(answered, excerpt(line, 600))
			}
		}
		t.Errorf("the skill should have been set right, on trial, by the shop's member or the maintainer; the skill says %q, the trial %+v %v; the upkeep's tools answered:\n%s",
			excerpt(body, 600), trial, trialErr, strings.Join(answered, "\n"))
	}
	var patterns []string
	for _, s := range b.Pages() {
		if strings.HasPrefix(s.Path, "/patterns/") {
			patterns = append(patterns, s.Path)
		}
	}
	t.Logf("the shop's turn used %v and said %q", used.SkillsUsed, answer)
	t.Logf("the owners' upkeep called: %s", strings.Join(calls, " | "))
	t.Logf("the skill now says %q, set right by %s; the library has the patterns %v, and the upkeep ended saying %q", excerpt(body, 600), by, patterns, excerpt(r.lastReply(upkeep.ThreadID), 1500))
}
