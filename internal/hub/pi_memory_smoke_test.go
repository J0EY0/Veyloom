package hub

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// How the test has pi compact: a summary may take this many tokens of the
// window, and the compaction keeps this many of the newest verbatim, few
// enough that the start of the topic has to go into the summary.
const (
	piTestReserve    = 8192
	piTestKeepRecent = 1000
)

// TestPiSmoke_Compaction has pi compact its session for real, in the
// middle of a topic, and checks what the brief rests on (design.md 5.6):
// that the hub hears of the compaction in the turn that filled the window,
// and that the next brief tells the topic in full again, since the summary
// pi keeps in its place may have lost it.
//
// A window of a million tokens takes long to fill, so pi gets an agent
// directory of its own (PI_CODING_AGENT_DIR), with the person's settings
// and credentials, the credentials linked rather than copied. Once the
// first turn has shown how much of the window a turn takes, its models.json
// shrinks the window so that the second, which reads a long file, goes past
// pi's threshold (the window less what it reserves for the summary) but not
// past the window itself, which pi would take for an overflow and retry.
// Pi starts anew each turn, so it reads the smaller window from then on.
//
//	VEYLOOM_PI_SMOKE=1 VEYLOOM_TEST_DATABASE_URL=... go test -run TestPiSmoke_Compaction ./internal/hub/ -v
func TestPiSmoke_Compaction(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	agentDir, provider, model := piTestAgentDir(t)

	sessionDir := filepath.Join(t.TempDir(), "sessions")
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: sessionDir, ToolDir: filepath.Join(t.TempDir(), "tools")})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(longNotes(300)), 0o600); err != nil {
		t.Fatal(err)
	}
	r := newSmokeRoom(t, runners, store.NewAgent{
		Name: "Pi smoke", Runtime: "pi", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, dir)
	const word = "saffron"

	// Turn 1 opens the topic with the word. Asked to remember it, pi takes
	// that for a request to write it into the wiki, whose catalog would then
	// carry it into the briefs.
	r.say("The code word for this topic is "+word+". Reply with exactly: ok", "")
	first := r.waitDone(1)
	thread := mustThread(t, r, first)
	session, err := r.s.GetOpenSession(r.ctx, r.member.ID)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(sessionDir, session.ID+".jsonl")
	used := piContextTokens(t, file)
	window := used + piTestReserve + 1500
	writePiWindow(t, agentDir, provider, model, window)
	t.Logf("turn 1: pi said %q holding %d tokens; the window is now %d, compacting past %d", r.lastReply(thread.ID), used, window, window-piTestReserve)

	// Turn 2 reads the file, which fills the window: pi compacts once the
	// agent is done, and the turn is what reports it.
	r.say("Read notes.txt in full with your read tool, then reply with just the number of its last line.", thread.ID)
	second := r.waitDone(2)
	if prompt := promptOf(t, second); strings.Contains(prompt, word) {
		t.Errorf("the second brief should hold only what is new, not the word:\n%s", prompt)
	}
	phases := compactionPhases(t, second)
	after, err := r.s.GetOpenSession(r.ctx, r.member.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("turn 2: pi said %q holding %d tokens; compaction events %v; the session counts %d compactions",
		r.lastReply(thread.ID), piContextTokens(t, file), phases, after.Compactions)
	if !piCompacted(t, file) {
		t.Fatalf("pi should have compacted the session by the end of turn 2 (compaction events %v)", phases)
	}
	if !contains(phases, runtime.CompactionEnd) || after.Compactions != 1 {
		t.Fatalf("turn 2 should report the compaction pi made after it: events %v, session compactions %d", phases, after.Compactions)
	}

	// Turn 3: what the session read of the topic went into the summary, so
	// the brief tells the topic again, the word with it, and pi answers.
	r.say("What was the code word I gave you at the start of this topic? One word.", thread.ID)
	third := r.waitDone(3)
	if prompt := promptOf(t, third); !strings.Contains(prompt, word) {
		t.Errorf("after the compaction the brief should tell the topic in full again:\n%s", prompt)
	}
	if reply := r.lastReply(thread.ID); !strings.Contains(strings.ToLower(reply), word) {
		t.Errorf("pi should answer with the word, said %q", reply)
	}
	t.Logf("turn 3: pi said %q; compaction events %v", r.lastReply(thread.ID), compactionPhases(t, third))

	// Turn 4: what the file said went into the summary, if anywhere, and
	// the file is gone. What the second turn's read tool answered is in its
	// transcript, which read_turn tells, read_topic giving the turn's id.
	if err := os.Remove(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	r.say("notes.txt is gone now. Find your turn in this topic that read it with read_topic, read that turn with read_turn, "+
		"and reply with the words of the file's line 1, exactly as your read tool showed them.", thread.ID)
	fourth := r.waitDone(4)
	tx := transcriptOf(t, fourth)
	if !strings.Contains(tx, `"tool":"`+runtime.RoomToolReadTurn+`"`) || !strings.Contains(tx, second.ID) {
		t.Errorf("pi should have read turn 2 (%s) with read_turn:\n%s", second.ID, tx)
	}
	line1 := strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(longNotes(1), "\n", 2)[0], "Line 1: "), ".")
	if reply := r.lastReply(thread.ID); !strings.Contains(strings.ToLower(reply), line1) {
		t.Errorf("pi should give line 1 (%q) from the turn it read, said %q", line1, reply)
	}
	t.Logf("turn 4: pi said %q", r.lastReply(thread.ID))
}

// TestPiSmoke_AtWork asks pi while another member waits on a person: the
// brief says who is at work, in which topic, what it has changed and what
// it waits for (design.md 5.2), and pi answers from it. The other member
// runs on the fake runtime, which changes a file and asks leave to run a
// command, and so holds its turn open until the test decides.
//
//	VEYLOOM_PI_SMOKE=1 VEYLOOM_TEST_DATABASE_URL=... go test -run TestPiSmoke_AtWork ./internal/hub/ -v
func TestPiSmoke_AtWork(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	dir := t.TempDir()
	r := newSmokeRoom(t, runners, store.NewAgent{
		Name: "Pi smoke", Runtime: "pi", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "Answer in as few words as you can.",
	}, dir)
	agent, err := r.s.CreateAgent(r.ctx, store.NewAgent{
		Name: "Worker", Runtime: "fake", MachineID: r.agent.MachineID, PermissionPreset: store.PermissionEditWithApproval,
		RuntimeOptions: map[string]any{"changes": []any{"auth/token.go"}, "approval": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := r.s.CreateMember(r.ctx, store.NewMember{RoomID: r.room.ID, AgentID: agent.ID, DisplayName: "Worker", RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.h.PostUserMessage(r.ctx, store.NewMessage{
		RoomID: r.room.ID, UserID: r.user.ID, Body: "@Worker do the tokens",
		Mentions: []store.Mention{{Kind: store.MentionAgent, ID: worker.ID}},
	}); err != nil {
		t.Fatal(err)
	}
	var asked []store.Approval
	eventually(t, func() bool {
		asked, err = r.s.ListPendingRoomApprovals(r.ctx, r.room.ID)
		return err == nil && len(asked) == 1
	}, "Worker to wait on a person")

	r.say("Is another member of this chat at work right now? If so, say who, in which topic, which file it has changed and what it waits for, in one line.", "")
	turn := r.waitDone(2)
	reply := r.lastReply(mustThread(t, r, turn).ID)
	if prompt := promptOf(t, turn); !strings.Contains(prompt, "At work right now:") {
		t.Errorf("the brief should say who is at work:\n%s", prompt)
	}
	for _, want := range []string{"worker", "#1", "auth/token.go", "make test"} {
		if !strings.Contains(strings.ToLower(reply), want) {
			t.Errorf("pi's answer lacks %q: %q", want, reply)
		}
	}
	t.Logf("pi said %q", reply)

	if _, err := r.h.DecideApproval(r.ctx, asked[0].ID, r.user.ID, runtime.Decision{Allow: true}, ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		turns, err := r.s.ListRoomTurns(r.ctx, r.room.ID, 10)
		return err == nil && !slices.ContainsFunc(turns, func(t store.Turn) bool { return t.Status == store.TurnRunning })
	}, "Worker's turn to end")
}

// TestPiSmoke_UpkeepHearsPeople has pi keep the wiki after people spoke
// without asking any agent (design.md 5.16): a decision said in the room,
// and a checklist sent as a file. The brief only says where and how much;
// pi reads the room itself, opens the file at its path, records what both
// say, and keeps the file in the wiki whole, as the person asked.
//
//	VEYLOOM_PI_SMOKE=1 VEYLOOM_TEST_DATABASE_URL=... go test -run TestPiSmoke_UpkeepHearsPeople ./internal/hub/ -v
func TestPiSmoke_UpkeepHearsPeople(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	r := newSmokeRoom(t, runners, store.NewAgent{
		Name: "Keeper", Runtime: "pi", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "You keep the team's wiki in order.",
	}, t.TempDir())
	manual := store.UpkeepManual
	if _, err := r.s.UpdateProject(r.ctx, r.room.ProjectID, store.ProjectPatch{WikiUpkeep: &upkeepOn, WikiMaintainer: &r.member.ID, WikiMaintainerTrigger: &manual}); err != nil {
		t.Fatal(err)
	}
	// With the maintainer alone in the chat, what people say without an @
	// would go to it as a chat turn; with another member, it goes to no one
	// and waits for the upkeep.
	coder, err := r.s.CreateAgent(r.ctx, store.NewAgent{Name: "Coder", Runtime: "fake", MachineID: r.agent.MachineID, PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.CreateMember(r.ctx, store.NewMember{RoomID: r.room.ID, AgentID: coder.ID, DisplayName: "Coder"}); err != nil {
		t.Fatal(err)
	}
	post := func(body string, attachmentIDs ...string) {
		t.Helper()
		if _, err := r.h.PostUserMessage(r.ctx, store.NewMessage{RoomID: r.room.ID, UserID: r.user.ID, Body: body, AttachmentIDs: attachmentIDs}); err != nil {
			t.Fatal(err)
		}
	}
	post("定了：staging 的数据库端口从 6543 改成 6544，以后都用 6544。")
	checklist := "# 发版清单\n\n1. 先跑 make test-db。\n2. 再跑 make web-e2e。\n3. 发版代号用 OSPREY-7。\n"
	id := store.NewID()
	rel := r.room.ID + "/" + id
	if err := os.MkdirAll(filepath.Join(r.files, r.room.ID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.files, filepath.FromSlash(rel)), []byte(checklist), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.CreateAttachment(r.ctx, store.NewAttachment{ID: id, RoomID: r.room.ID, Filename: "release-checklist.md", MediaType: "text/markdown", Size: int64(len(checklist)), Path: rel}); err != nil {
		t.Fatal(err)
	}
	post("这是发版清单，整理进 wiki，原文件也存一份。", id)

	if turns, _ := r.s.ListRoomTurns(r.ctx, r.room.ID, 10); len(turns) != 0 {
		t.Fatalf("what people said started turns: %+v", turns)
	}
	if _, err := r.h.StartUpkeep(r.ctx, r.room.ProjectID); err != nil {
		t.Fatal(err)
	}
	upkeep := r.waitDone(1)
	if upkeep.Kind != store.TurnUpkeep {
		t.Fatalf("the turn that ran is no upkeep: %+v", upkeep)
	}
	tx := transcriptOf(t, upkeep)
	found := func(query string) []WikiHit {
		t.Helper()
		hits, err := r.h.SearchWiki(r.ctx, r.room.ProjectID, query, 10)
		if err != nil {
			t.Fatal(err)
		}
		return hits
	}
	if len(found("6544")) == 0 {
		t.Errorf("the decision said in the room is in no page; the upkeep went:\n%s", tx)
	}
	if len(found("OSPREY-7")) == 0 {
		t.Errorf("what the file says is in no page, so pi did not open it; the upkeep went:\n%s", tx)
	}
	project, err := r.s.GetProject(r.ctx, r.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	kept, _ := filepath.Glob(filepath.Join(r.h.cfg.WikiDir, "projects", project.WikiSlug, "files", "*", "*"))
	if len(kept) == 0 {
		t.Errorf("the file was not kept in the wiki, though the person asked; the upkeep went:\n%s", tx)
	} else if data, _ := os.ReadFile(kept[0]); string(data) != checklist {
		t.Errorf("the file kept is not the one sent: %q", data)
	}
	if p, _ := r.s.GetProject(r.ctx, r.room.ProjectID); p.WikiSeenSeq == 0 {
		t.Error("the upkeep did not move how far it went over the chat")
	}
	t.Logf("pages: 6544 in %v, OSPREY-7 in %v; kept %v; pi said %q", hitPaths(found("6544")), hitPaths(found("OSPREY-7")), kept, r.lastReply(upkeep.ThreadID))
}

// TestPiSmoke_ChecksPagesAgain has pi check the pages due at an upkeep
// (design.md 5.16): a fact that a later turn made wrong, by changing the
// file it names, and an old convention that still holds. The fact is to be
// set right or deprecated, the convention confirmed; both leave the pages
// due.
func TestPiSmoke_ChecksPagesAgain(t *testing.T) {
	if os.Getenv("VEYLOOM_PI_SMOKE") != "1" {
		t.Skip("set VEYLOOM_PI_SMOKE=1 to run against the real pi CLI")
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skipf("pi is not installed: %v", err)
	}
	runners := runtime.BuiltinRunnersWith(runtime.RunnerOptions{SessionDir: filepath.Join(t.TempDir(), "sessions"), ToolDir: filepath.Join(t.TempDir(), "tools")})
	dir := t.TempDir()
	r := newSmokeRoom(t, runners, store.NewAgent{
		Name: "Keeper", Runtime: "pi", PermissionPreset: store.PermissionReadOnly,
		RoleCard: "You keep the team's wiki in order.",
	}, dir)
	manual := store.UpkeepManual
	if _, err := r.s.UpdateProject(r.ctx, r.room.ProjectID, store.ProjectPatch{WikiUpkeep: &upkeepOn, WikiMaintainer: &r.member.ID, WikiMaintainerTrigger: &manual}); err != nil {
		t.Fatal(err)
	}
	project, err := r.s.GetProject(r.ctx, r.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	// The repository as it is now, and two pages written long ago.
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config", "staging.env"), []byte("DB_HOST=staging-db\nDB_PORT=6544\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.h.WikiCatalog(r.ctx, project.ID); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(r.h.cfg.WikiDir, "projects", project.WikiSlug)
	for p, text := range map[string]string{
		"facts/staging-db-port.md":       "---\ntype: Fact\ntitle: Staging database port\ndescription: The port the staging database listens on.\ngenerated:\n  by: codex/default\n  at: 2020-01-02T03:04:05Z\n---\n\nThe staging database listens on port 6543, as config/staging.env sets it.\n",
		"conventions/english-commits.md": "---\ntype: Convention\ntitle: Commit messages in English\ndescription: How commit messages are written.\ngenerated:\n  by: codex/default\n  at: 2020-01-02T03:04:05Z\n---\n\nCommit messages are written in English, whatever language the chat is in.\n",
	} {
		if err := os.WriteFile(filepath.Join(folder, filepath.FromSlash(p)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A turn changes the file the fact names.
	coder, err := r.s.CreateAgent(r.ctx, store.NewAgent{
		Name: "Coder", Runtime: "fake", MachineID: r.agent.MachineID, PermissionPreset: store.PermissionReadOnly,
		RuntimeOptions: map[string]any{"changes": []any{"config/staging.env"}, "reply": "端口改成 6544 了。"},
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := r.s.CreateMember(r.ctx, store.NewMember{RoomID: r.room.ID, AgentID: coder.ID, DisplayName: "Coder", RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.h.PostUserMessage(r.ctx, store.NewMessage{
		RoomID: r.room.ID, UserID: r.user.ID, Body: "@Coder 把 staging 的数据库端口改成 6544",
		Mentions: []store.Mention{{Kind: store.MentionAgent, ID: member.ID}},
	}); err != nil {
		t.Fatal(err)
	}
	r.waitDone(1)

	if _, err := r.h.StartUpkeep(r.ctx, r.room.ProjectID); err != nil {
		t.Fatal(err)
	}
	upkeep := r.waitDone(2)
	if upkeep.Kind != store.TurnUpkeep {
		t.Fatalf("the turn that ran is no upkeep: %+v", upkeep)
	}
	tx := transcriptOf(t, upkeep)
	if prompt := promptOf(t, upkeep); !strings.Contains(prompt, "Pages to check again (2, in this order):\n- /facts/staging-db-port.md") {
		t.Errorf("the brief lists the pages to check, the changed one first:\n%s", prompt)
	}
	fact, err := r.h.WikiPage(r.ctx, project.ID, "/facts/staging-db-port.md")
	if err != nil {
		t.Fatal(err)
	}
	if fact.Status != "deprecated" && !strings.Contains(fact.Body, "6544") {
		t.Errorf("the fact is still wrong: %q; the upkeep went:\n%s", fact.Body, tx)
	}
	convention, err := r.h.WikiPage(r.ctx, project.ID, "/conventions/english-commits.md")
	if err != nil {
		t.Fatal(err)
	}
	if convention.Review != nil {
		t.Errorf("the convention was left due: %+v; the upkeep went:\n%s", convention.Review, tx)
	}
	if fact.Review != nil {
		t.Errorf("the fact was left due: %+v", fact.Review)
	}
	t.Logf("fact %s (%s): %q; convention %s, verified %+v; pi said %q", fact.Status, fact.Tier, fact.Body, convention.Tier, convention.Verified, r.lastReply(upkeep.ThreadID))
}

func hitPaths(hits []WikiHit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Path)
	}
	return out
}

// piTestAgentDir makes pi's agent directory for the test and points pi at
// it: the person's settings, with the compaction settings the test needs,
// and their credentials, linked. It returns the directory and the provider
// and model pi uses by default, the one whose window the test shrinks.
func piTestAgentDir(t *testing.T) (dir, provider, model string) {
	t.Helper()
	src := os.Getenv("PI_CODING_AGENT_DIR")
	if src == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip(err)
		}
		src = filepath.Join(home, ".pi", "agent")
	}
	auth := filepath.Join(src, "auth.json")
	if _, err := os.Stat(auth); err != nil {
		t.Skipf("pi is not signed in: %v", err)
	}
	settings := map[string]any{}
	if data, err := os.ReadFile(filepath.Join(src, "settings.json")); err == nil {
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("pi's settings: %v", err)
		}
	}
	provider, _ = settings["defaultProvider"].(string)
	model, _ = settings["defaultModel"].(string)
	if provider == "" || model == "" {
		t.Skip("pi has no default provider and model to shrink the window of")
	}
	settings["compaction"] = map[string]any{"enabled": true, "reserveTokens": piTestReserve, "keepRecentTokens": piTestKeepRecent}

	dir = t.TempDir()
	if err := os.Symlink(auth, filepath.Join(dir, "auth.json")); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(dir, "settings.json"), settings)
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	return dir, provider, model
}

// writePiWindow overrides the window of the model pi uses.
func writePiWindow(t *testing.T, dir, provider, model string, window int) {
	t.Helper()
	writeJSON(t, filepath.Join(dir, "models.json"), map[string]any{
		"providers": map[string]any{provider: map[string]any{
			"modelOverrides": map[string]any{model: map[string]any{"contextWindow": window}},
		}},
	})
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// piTestUsage is what an answer took of the window, as pi's session has it.
type piTestUsage struct {
	Input       float64 `json:"input"`
	Output      float64 `json:"output"`
	CacheRead   float64 `json:"cacheRead"`
	CacheWrite  float64 `json:"cacheWrite"`
	TotalTokens float64 `json:"totalTokens"`
}

// piSessionEntries reads pi's session file: the usage of its answers and
// whether it holds a compaction.
func piSessionEntries(t *testing.T, file string) (usages []piTestUsage, compacted bool) {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var entry struct {
			Type    string `json:"type"`
			Message struct {
				Role  string       `json:"role"`
				Usage *piTestUsage `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		switch {
		case entry.Type == "compaction":
			compacted = true
		case entry.Type == "message" && entry.Message.Role == "assistant" && entry.Message.Usage != nil:
			usages = append(usages, *entry.Message.Usage)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return usages, compacted
}

// piContextTokens is how much of the window the session took at its last
// answer, counted as pi counts it.
func piContextTokens(t *testing.T, file string) int {
	t.Helper()
	usages, _ := piSessionEntries(t, file)
	if len(usages) == 0 {
		t.Fatalf("pi's session %s has no answer with usage", file)
	}
	u := usages[len(usages)-1]
	if u.TotalTokens > 0 {
		return int(u.TotalTokens)
	}
	return int(u.Input + u.Output + u.CacheRead + u.CacheWrite)
}

// piCompacted reports whether pi wrote a compaction into the session.
func piCompacted(t *testing.T, file string) bool {
	t.Helper()
	_, compacted := piSessionEntries(t, file)
	return compacted
}

// compactionPhases lists the compaction events a turn's transcript holds.
func compactionPhases(t *testing.T, turn store.Turn) []string {
	t.Helper()
	var phases []string
	for _, line := range strings.Split(transcriptOf(t, turn), "\n") {
		var rec struct {
			Event *runtime.Event `json:"event"`
		}
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Event != nil && rec.Event.Kind == runtime.EventCompaction {
			phases = append(phases, rec.Event.Phase)
		}
	}
	return phases
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// longNotes is a file long enough to fill a good part of a small window:
// n numbered lines of words in an order no two lines share.
func longNotes(n int) string {
	words := []string{"harbor", "lantern", "meadow", "copper", "willow", "granite", "orchard", "falcon", "ember", "quartz", "thistle", "juniper", "cobalt", "maple", "sparrow", "tundra"}
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "Line %d:", i)
		for j := 0; j < 10; j++ {
			fmt.Fprintf(&b, " %s", words[(i*7+j*j*3+j)%len(words)])
		}
		b.WriteString(".\n")
	}
	return b.String()
}
