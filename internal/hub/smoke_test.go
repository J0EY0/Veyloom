package hub

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/machine"
	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/store/storetest"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// smokeTurnTimeout is how long a turn on a real CLI may take; an upkeep
// twice that.
const smokeTurnTimeout = 3 * time.Minute

// smokeRoom is what the tests against real CLIs share: a hub on a test
// database, one machine with the runners given, and a project whose room
// has one member on the runtime under test.
type smokeRoom struct {
	// files is where the files people send are kept.
	files  string
	t      *testing.T
	ctx    context.Context
	s      *store.Store
	h      *Hub
	room   store.Room
	user   store.User
	agent  store.Agent
	member store.Member
}

// newSmokeRoom connects a machine with runners to a new hub, made with
// opts, creates agent on that machine and makes it the one member of a
// project in dir.
func newSmokeRoom(t *testing.T, runners map[string]runtime.Runner, agent store.NewAgent, dir string, opts ...Option) *smokeRoom {
	t.Helper()
	s := storetest.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	files := t.TempDir()
	h := New(s, Config{TranscriptDir: t.TempDir(), WikiDir: t.TempDir(), AttachmentDir: files, HeartbeatInterval: time.Hour}, opts...)
	// Worktrees, for members other than the leader, go next to the project.
	worktrees := filepath.Join(filepath.Dir(dir), "worktrees")
	w := machine.New(machine.Config{Name: "laptop", ToolDir: t.TempDir(), WorktreeDir: worktrees}, machine.NewDiscovery(nil, time.Second), &machine.MemoryIdentity{}, runners)
	hubEnd, machineEnd := protocol.Pipe()
	go h.Serve(ctx, hubEnd)
	go w.Run(ctx, machineEnd)
	eventually(t, func() bool { return len(h.Machines()) == 1 }, "machine to connect")

	_, room, err := s.CreateProject(ctx, store.NewProject{Name: "smoke", RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	agent.MachineID = h.Machines()[0].ID
	if agent.Runtime == "codex" {
		agent = forCodexSmoke(agent)
	}
	created, err := s.CreateAgent(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateMember(ctx, store.NewMember{RoomID: room.ID, AgentID: created.ID, DisplayName: agent.Name, RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	return &smokeRoom{files: files, t: t, ctx: ctx, s: s, h: h, room: room, user: user, agent: created, member: member}
}

// withoutCodexMemories has codex leave its memories alone in a smoke test:
// it neither reads what it remembers of earlier runs, which biases it (it
// remembers the smoke tests' runs as checks wanting terse answers, and a
// maintainer so briefed writes nothing), nor remembers this one, which would
// bias it in a person's own Veyloom rooms. This is the tests' doing only: a
// person's codex keeps its memories.
func withoutCodexMemories(options map[string]any) map[string]any {
	out := maps.Clone(options)
	if out == nil {
		out = map[string]any{}
	}
	extra, _ := out["extra_args"].([]any)
	out["extra_args"] = append([]any{"--disable", "memories"}, extra...)
	return out
}

// forCodexSmoke readies a Codex agent for a smoke test: its memories left
// alone (withoutCodexMemories), and, when they are set, the model
// VEYLOOM_CODEX_SMOKE_MODEL names and the reasoning effort
// VEYLOOM_CODEX_SMOKE_EFFORT names in place of the person's defaults, so a
// smoke test can run on a model with quota left.
func forCodexSmoke(agent store.NewAgent) store.NewAgent {
	agent.RuntimeOptions = withoutCodexMemories(agent.RuntimeOptions)
	if model := os.Getenv("VEYLOOM_CODEX_SMOKE_MODEL"); model != "" {
		agent.Model = model
	}
	if effort := os.Getenv("VEYLOOM_CODEX_SMOKE_EFFORT"); effort != "" {
		extra, _ := agent.RuntimeOptions["extra_args"].([]any)
		agent.RuntimeOptions["extra_args"] = append(extra, "-c", `model_reasoning_effort="`+effort+`"`)
	}
	return agent
}

// say posts body from the person, mentioning the member, in the thread or,
// with threadID empty, in the room itself.
func (r *smokeRoom) say(body, threadID string) store.Message {
	r.t.Helper()
	msg, err := r.h.PostUserMessage(r.ctx, store.NewMessage{
		RoomID: r.room.ID, ThreadID: threadID, UserID: r.user.ID, Body: body,
		Mentions: []store.Mention{{Kind: store.MentionAgent, ID: r.member.ID}},
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return msg
}

// waitDone waits for the room's n-th turn to end and fails unless it is done.
func (r *smokeRoom) waitDone(n int) store.Turn {
	r.t.Helper()
	return r.waitTurn(n, nil)
}

// decideAll is waitDone for a turn that asks for approval: every approval
// it raises is decided as allow says. It fails if the turn asked for none.
func (r *smokeRoom) decideAll(n int, allow bool) (store.Turn, []store.Approval) {
	r.t.Helper()
	return r.decideAllWith(n, func(store.Approval) runtime.Decision { return runtime.Decision{Allow: allow} })
}

// decideAllWith is decideAll deciding each request as decide says, such as
// answering a question.
func (r *smokeRoom) decideAllWith(n int, decide func(store.Approval) runtime.Decision) (store.Turn, []store.Approval) {
	r.t.Helper()
	var decided []store.Approval
	turn := r.waitTurn(n, func() {
		pending, err := r.s.ListPendingRoomApprovals(r.ctx, r.room.ID)
		if err != nil {
			r.t.Fatal(err)
		}
		for _, a := range pending {
			d, err := r.h.DecideApproval(r.ctx, a.ID, r.user.ID, decide(a), "")
			if err != nil {
				r.t.Fatal(err)
			}
			decided = append(decided, d)
		}
	})
	if len(decided) == 0 {
		r.t.Fatalf("turn %d asked for no approval", n)
	}
	return turn, decided
}

// waitTurn polls until the room's n-th turn ends, calling poll (if any)
// each time while it runs, and fails unless the turn is done.
func (r *smokeRoom) waitTurn(n int, poll func()) store.Turn {
	r.t.Helper()
	return r.waitTurnIn(r.room.ID, n, poll)
}

// waitTurnIn is waitTurn for the n-th turn of another room. An upkeep,
// which goes over several turns and writes pages, gets twice the time.
func (r *smokeRoom) waitTurnIn(roomID string, n int, poll func()) store.Turn {
	r.t.Helper()
	start := time.Now()
	for {
		turns, err := r.s.ListRoomTurns(r.ctx, roomID, 50)
		if err != nil {
			r.t.Fatal(err)
		}
		limit := smokeTurnTimeout
		if len(turns) == n {
			if turns[0].Status != store.TurnRunning {
				if turns[0].Status != store.TurnDone {
					r.t.Fatalf("turn %d ended %s: %s", n, turns[0].Status, turns[0].Error)
				}
				return turns[0]
			}
			if turns[0].Kind == store.TurnUpkeep {
				limit *= 2
			}
		}
		if time.Since(start) > limit {
			r.t.Fatalf("turn %d did not finish in time%s", n, r.stuck(roomID))
		}
		if poll != nil {
			poll()
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// stuck says what the room's latest turn was doing, for one that did not
// finish in time: how its transcript ends.
func (r *smokeRoom) stuck(roomID string) string {
	turns, err := r.s.ListRoomTurns(r.ctx, roomID, 1)
	if err != nil || len(turns) == 0 {
		return ""
	}
	var out string
	// What waits for a person holds a turn up.
	if pending, err := r.s.ListPendingRoomApprovals(r.ctx, roomID); err == nil {
		for _, a := range pending {
			out += fmt.Sprintf("; it waits for a person: %s %s", a.Kind, excerpt(string(a.Input), 400))
		}
	}
	// A running turn's transcript is where the hub writes it; the turn
	// names it once it ends.
	path := turns[0].TranscriptPath
	if path == "" {
		path = filepath.Join(r.h.turns.transcripts, turns[0].ID+".jsonl")
	}
	tx, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(tx)), "\n")
	for i, line := range lines {
		lines[i] = excerpt(line, 400)
	}
	return out + "; its transcript ends:\n" + strings.Join(lines[max(0, len(lines)-10):], "\n")
}

// allowIn answers every request that waits for a person in a room by
// allowing it, as a person going along would, and keeps them for the log.
func (r *smokeRoom) allowIn(roomID string, asked *[]string) func() {
	return func() {
		pending, err := r.s.ListPendingRoomApprovals(r.ctx, roomID)
		if err != nil {
			r.t.Fatal(err)
		}
		for _, a := range pending {
			if _, err := r.h.DecideApproval(r.ctx, a.ID, r.user.ID, runtime.Decision{Allow: true}, ""); err != nil {
				r.t.Fatal(err)
			}
			*asked = append(*asked, string(a.Kind)+" "+excerpt(string(a.Input), 300))
		}
	}
}

// lastReply is the body of the latest agent message in the thread.
func (r *smokeRoom) lastReply(threadID string) string {
	r.t.Helper()
	msg, err := r.s.LastAgentMessageInThread(r.ctx, threadID)
	if err != nil {
		r.t.Fatal(err)
	}
	return msg.Body
}

// wikiRound has the member write a page to the project wiki and find it
// again, as the n-th turn, in the thread: the wiki tools, for real, through
// whichever way the runtime reaches Veyloom's tools. It returns the, and the page's history, whose author says how the runtime's writes
// are signed.
func (r *smokeRoom) wikiRound(n int, threadID string) (store.Turn, []wiki.Commit) {
	r.t.Helper()
	r.say("Use your write_wiki tool once, with type Fact, slug smoke-wiki-check, title Smoke check, "+
		"description A page the smoke test asked for., and body The smoke test wrote this page. "+
		"Then use your search_wiki tool with the query smoke check, and reply with just the path it lists.", threadID)
	turn := r.waitDone(n)
	if threadID == "" {
		// Asked in the room: the reply opened a topic.
		thread, err := r.s.ThreadForMessage(r.ctx, turn.ReplyMessageID)
		if err != nil {
			r.t.Fatal(err)
		}
		threadID = thread.ID
	}
	tx, _ := os.ReadFile(turn.TranscriptPath)
	for _, want := range []string{"write_wiki", "search_wiki", "Saved /facts/smoke-wiki-check.md"} {
		if !strings.Contains(string(tx), want) {
			r.t.Errorf("turn %d should show %q:\n%s", n, want, tx)
		}
	}
	// A runtime may make both calls at once, the search answered before the
	// page is written: then the pages sharing its words list it.
	if !strings.Contains(string(tx), `Pages matching \"smoke check\"`) && !strings.Contains(string(tx), `Pages sharing words with it, best first:\n/facts/smoke-wiki-check.md`) {
		r.t.Errorf("turn %d should find the page with search_wiki:\n%s", n, tx)
	}
	if reply := r.lastReply(threadID); !strings.Contains(reply, "/facts/smoke-wiki-check.md") {
		r.t.Errorf("the agent should have found the page, said %q", reply)
	}
	project, err := r.s.RoomProject(r.ctx, r.room.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	b, err := r.h.wikis.project(r.ctx, project)
	if err != nil {
		r.t.Fatal(err)
	}
	page, err := b.Page("/facts/smoke-wiki-check.md")
	if err != nil {
		r.t.Fatalf("the page should be in the wiki: %v", err)
	}
	commits, err := b.History(r.ctx, page.Path, 5)
	if err != nil || len(commits) != 1 {
		r.t.Fatalf("the turn should have committed the page once: %+v %v", commits, err)
	}
	r.t.Logf("turn %d wrote %s as %s, commit %q", n, page.Path, commits[0].Author, commits[0].Subject)
	return turn, commits
}

// briefRound follows wikiRound in the same topic: the wiki reaches the
// agent through its brief, no tool needed. A person's resident page comes
// in full, and the page wikiRound wrote is in the catalog, which the
// session has not been shown yet (the wiki was empty at its last brief).
func (r *smokeRoom) briefRound(n int, threadID string) store.Turn {
	r.t.Helper()
	project, err := r.s.RoomProject(r.ctx, r.room.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	b, err := r.h.wikis.project(r.ctx, project)
	if err != nil {
		r.t.Fatal(err)
	}
	w, err := b.Writer(okf.Human("smoke"))
	if err != nil {
		r.t.Fatal(err)
	}
	d := okf.New("Convention")
	d.SetString(okf.KeyTitle, "Code word")
	d.SetTags([]string{wiki.ResidentTag})
	d.SetBody("The team's code word is PERIWINKLE.")
	if _, err := w.Create("/conventions/code-word.md", d); err != nil {
		r.t.Fatal(err)
	}
	if _, err := w.Commit(r.ctx, "Smoke test"); err != nil {
		r.t.Fatal(err)
	}

	r.say("Answer from what you were given, without calling any tool: what is the team's code word, "+
		"and what is the title of the page the project wiki lists under /facts/? Reply as: word | title", threadID)
	turn := r.waitDone(n)
	prompt := promptOf(r.t, turn)
	for _, want := range []string{"--- /conventions/code-word.md: Code word\nThe team's code word is PERIWINKLE.", "   /facts/smoke-wiki-check.md: Smoke check (Fact)"} {
		if !strings.Contains(prompt, want) {
			r.t.Errorf("turn %d's brief should carry %q:\n%s", n, want, prompt)
		}
	}
	reply := r.lastReply(threadID)
	if !strings.Contains(strings.ToUpper(reply), "PERIWINKLE") || !strings.Contains(strings.ToLower(reply), "smoke check") {
		r.t.Errorf("the agent should have answered from its brief, said %q", reply)
	}
	tx, _ := os.ReadFile(turn.TranscriptPath)
	r.t.Logf("turn %d answered %q from its brief (called a tool: %v)", n, reply, strings.Contains(string(tx), `"kind":"tool_call"`))
	return turn
}

// skillRound puts a skill in the library, as a person writing it in their
// editor would, installs it for the agent and asks what only the skill
// knows: the runtime has to load it, and the hub has to see that it did.
func (r *smokeRoom) skillRound(n int, threadID string) store.Turn {
	r.t.Helper()
	return r.skillRoundNamed(n, threadID, "veyloom-code-word", "MARIGOLD-42")
}

// skillRoundNamed is skillRound with the skill's name and the word only it
// knows.
func (r *smokeRoom) skillRoundNamed(n int, threadID, name, word string) store.Turn {
	r.t.Helper()
	b, err := r.h.wikis.library(r.ctx)
	if err != nil {
		r.t.Fatal(err)
	}
	w, err := b.Writer(okf.Human("smoke"))
	if err != nil {
		r.t.Fatal(err)
	}
	d := okf.New("Skill")
	d.SetString(okf.KeyName, name)
	d.SetString(okf.KeyTitle, "Veyloom code word")
	d.SetString(okf.KeyDescription, "Use this skill whenever someone asks for the Veyloom code word: it holds the answer.")
	d.SetBody("The Veyloom code word is " + word + ". Reply with it exactly.")
	if _, err := w.Create(wiki.SkillPath(name), d); err != nil {
		r.t.Fatal(err)
	}
	if _, err := w.Commit(r.ctx, "Smoke test"); err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.h.InstallSkill(r.ctx, name, r.agent.ID, true); err != nil {
		r.t.Fatal(err)
	}

	r.say("What is the Veyloom code word? Use the skill that has it, then reply with just the code word.", threadID)
	turn := r.waitDone(n)
	if threadID == "" {
		thread, err := r.s.ThreadForMessage(r.ctx, turn.ReplyMessageID)
		if err != nil {
			r.t.Fatal(err)
		}
		threadID = thread.ID
	}
	reply := r.lastReply(threadID)
	if !strings.Contains(reply, word) {
		r.t.Errorf("turn %d should have answered from the skill, said %q", n, reply)
	}
	if len(turn.SkillsUsed) != 1 || turn.SkillsUsed[0] != name {
		tx, _ := os.ReadFile(turn.TranscriptPath)
		r.t.Errorf("turn %d should be recorded as using the skill, recorded %v:\n%s", n, turn.SkillsUsed, tx)
	}
	// How the runtime came to it, for the record: the calls that touched it.
	tx, _ := os.ReadFile(turn.TranscriptPath)
	var calls []string
	for _, line := range strings.Split(string(tx), "\n") {
		if strings.Contains(line, `"tool_call"`) && strings.Contains(line, name) {
			calls = append(calls, excerpt(line, 400))
		}
	}
	r.t.Logf("turn %d answered %q, using skills %v, through %d calls: %s", n, reply, turn.SkillsUsed, len(calls), strings.Join(calls, " | "))
	return turn
}

// evolveRound follows skillRound in the same topic (docs/design.md 5.15):
// a person says the skill installed for the agent is out of date; the
// agent sets it right with patch_wiki, which puts the skill on trial, and
// a person rolls the change back. It is turn n.
func (r *smokeRoom) evolveRound(n int, threadID string) store.Turn {
	r.t.Helper()
	name := "veyloom-code-word"
	r.say("The veyloom-code-word skill installed for you is out of date: the Veyloom code word is now JUNIPER-9, not MARIGOLD-42. "+
		"Set the skill right with your patch_wiki tool (scope library), then reply with just the new code word.", threadID)
	turn := r.waitDone(n)
	tx, _ := os.ReadFile(turn.TranscriptPath)
	var calls []string
	for _, line := range strings.Split(string(tx), "\n") {
		if strings.Contains(line, `"kind":"tool_call"`) && (strings.Contains(line, "patch_wiki") || strings.Contains(line, "read_wiki")) {
			calls = append(calls, excerpt(line, 400))
		}
	}
	b, err := r.h.wikis.library(r.ctx)
	if err != nil {
		r.t.Fatal(err)
	}
	body := func() string {
		page, err := b.Page(wiki.SkillPath(name))
		if err != nil {
			return ""
		}
		return page.Doc.Body()
	}
	if !strings.Contains(body(), "JUNIPER-9") {
		r.t.Errorf("turn %d should have set the skill right; the skill says %q; it called:\n%s", n, body(), strings.Join(calls, "\n"))
	}
	trial, err := r.s.OpenSkillTrial(r.ctx, name)
	if err != nil || trial.TurnID != turn.ID || trial.ChangedBy != r.member.DisplayName {
		r.t.Errorf("the skill should be on trial for turn %d: %+v %v", n, trial, err)
	}
	if _, err := r.h.RollbackSkill(r.ctx, name, r.user.ID, "smoke test"); err != nil {
		r.t.Errorf("rolling the change back: %v", err)
	}
	if got := body(); strings.Contains(got, "JUNIPER-9") || !strings.Contains(got, "MARIGOLD-42") {
		r.t.Errorf("rolled back, the skill should say what it said before: %q", got)
	}
	r.t.Logf("turn %d set the skill right through %d calls and said %q: %s", n, len(calls), r.lastReply(threadID), strings.Join(calls, " | "))
	return turn
}

// upkeepRound makes the member the project's wiki maintainer and has it go
// over two chat turns, the n-th and n+1-th: a fact told in the room, then
// set right in its topic. Nobody asks the members to record it, so they
// do not write the wiki, and what gets recorded is the upkeep's doing: it
// has to read the turns with its own tools and should record the corrected
// fact. The upkeep is turn n+2.
func (r *smokeRoom) upkeepRound(n int) store.Turn {
	r.t.Helper()
	r.say("Heads-up for the integration tests you will set up: our staging database listens on port 6543. Acknowledge in one short sentence.", "")
	told := r.waitDone(n)
	thread, err := r.s.ThreadForMessage(r.ctx, told.ReplyMessageID)
	if err != nil {
		r.t.Fatal(err)
	}
	r.say("Correction: the staging database moved to port 6544 last week, so use 6544. Acknowledge in one short sentence.", thread.ID)
	r.waitDone(n + 1)

	manual := store.UpkeepManual
	if _, err := r.s.UpdateProject(r.ctx, r.room.ProjectID, store.ProjectPatch{WikiUpkeep: &upkeepOn, WikiMaintainer: &r.member.ID, WikiMaintainerTrigger: &manual}); err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.h.StartUpkeep(r.ctx, r.room.ProjectID); err != nil {
		r.t.Fatal(err)
	}
	upkeep := r.waitDone(n + 2)
	if upkeep.Kind != store.TurnUpkeep {
		r.t.Fatalf("turn %d should be the upkeep: %+v", n+2, upkeep)
	}
	tx, _ := os.ReadFile(upkeep.TranscriptPath)
	var calls []string
	for _, line := range strings.Split(string(tx), "\n") {
		if strings.Contains(line, `"kind":"tool_call"`) {
			calls = append(calls, excerpt(line, 300))
		}
	}
	if !strings.Contains(string(tx), runtime.RoomToolReadTurn) && !strings.Contains(string(tx), runtime.UpkeepToolListTurns) {
		r.t.Errorf("the upkeep should have read the turns with its tools:\n%s", strings.Join(calls, "\n"))
	}
	// What it recorded: a page that has the corrected port.
	project, err := r.s.RoomProject(r.ctx, r.room.ID)
	if err != nil {
		r.t.Fatal(err)
	}
	b, err := r.h.wikis.project(r.ctx, project)
	if err != nil {
		r.t.Fatal(err)
	}
	var recorded []string
	for _, s := range b.Pages() {
		if page, err := b.Page(s.Path); err == nil && strings.Contains(page.Doc.Body()+page.Title+page.Description, "6544") {
			recorded = append(recorded, s.Path)
		}
	}
	if len(recorded) == 0 {
		r.t.Errorf("the upkeep should have recorded the corrected port; it called:\n%s", strings.Join(calls, "\n"))
		if dump := os.Getenv("VEYLOOM_SMOKE_DUMP"); dump != "" {
			os.WriteFile(dump, tx, 0o600)
		}
	}
	if status, _ := r.h.UpkeepStatus(r.ctx, r.room.ProjectID); status.Waiting.Own != 0 {
		r.t.Errorf("the upkeep should have gone over both turns: %+v", status.Waiting)
	}
	r.t.Logf("turn %d, the upkeep, recorded %v through %d calls: %s", n+2, recorded, len(calls), strings.Join(calls, " | "))
	r.t.Logf("turn %d, the upkeep, ended saying: %q", n+2, r.lastReply(upkeep.ThreadID))
	return upkeep
}

// mountRound mounts a copy of OKF's sample bundle, a retailer's data
// catalog, in the project's wiki and asks what only the catalog says: the
// agent has to find the page with search_wiki and read it with read_wiki at
// its /@ path, and the bundle stays as it was.
func (r *smokeRoom) mountRound(n int, threadID string) store.Turn {
	r.t.Helper()
	dir := filepath.Join(r.t.TempDir(), "acme-retail")
	if err := os.CopyFS(dir, os.DirFS(acmeRetail)); err != nil {
		r.t.Fatal(err)
	}
	before := treeOf(r.t, dir)
	dirs := []string{dir}
	if _, err := r.s.UpdateProject(r.ctx, r.room.ProjectID, store.ProjectPatch{WikiExternalBundles: &dirs}); err != nil {
		r.t.Fatal(err)
	}
	r.say("Finance keeps its data catalog in a bundle the project wiki mounts. With your search_wiki and read_wiki tools, find how the catalog defines gross margin, "+
		"then reply with the path of the page you read and what full COGS is made of, in one sentence.", threadID)
	turn := r.waitDone(n)
	if threadID == "" {
		threadID = topicOf(r.t, r, turn)
	}
	if brief := promptOf(r.t, turn); !strings.Contains(brief, "mounts bundles from elsewhere, read-only: acme-retail (9 pages)") {
		r.t.Errorf("turn %d's brief should name the mount:\n%s", n, brief)
	}
	tx, _ := os.ReadFile(turn.TranscriptPath)
	var calls []string
	read := false
	for _, line := range strings.Split(string(tx), "\n") {
		if strings.Contains(line, `"kind":"tool_call"`) {
			calls = append(calls, excerpt(line, 300))
			read = read || strings.Contains(line, "read_wiki") && strings.Contains(line, "/@acme-retail/")
		}
	}
	if !read {
		r.t.Errorf("turn %d should have read a page of the mount with read_wiki; it called:\n%s", n, strings.Join(calls, "\n"))
	}
	reply := r.lastReply(threadID)
	// The metric's page, or the policy it rests on: both say what full COGS
	// is made of.
	if !strings.Contains(reply, "/@acme-retail/") || !strings.Contains(strings.ToLower(reply), "shipping") {
		r.t.Errorf("turn %d should have answered from the catalog's pages, said %q", n, reply)
	}
	if after := treeOf(r.t, dir); !maps.Equal(after, before) {
		r.t.Errorf("turn %d changed the mounted bundle", n)
	}
	r.t.Logf("turn %d answered %q from the mount, through %d calls: %s", n, reply, len(calls), strings.Join(calls, " | "))
	return turn
}

// treeOf is every file under dir by its path there, with what it holds.
func treeOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		rel, _ := filepath.Rel(dir, p)
		files[rel] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
