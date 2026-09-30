package hub

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// otherProject is a second project with one fake-runtime member, whose
// turns this test waits for itself: loop's helpers look at its own room.
type otherProject struct {
	project store.Project
	room    store.Room
	member  store.Member
}

func (l *loop) otherProject(name, member string, options map[string]any) otherProject {
	l.t.Helper()
	project, room, err := l.s.CreateProject(l.ctx, store.NewProject{Name: name})
	if err != nil {
		l.t.Fatal(err)
	}
	agent, err := l.s.CreateAgent(l.ctx, store.NewAgent{
		Name: member + " agent", MachineID: l.machineID, Runtime: "fake", PermissionPreset: store.PermissionReadOnly, RoleCard: "Be brief.", RuntimeOptions: options,
	})
	if err != nil {
		l.t.Fatal(err)
	}
	m, err := l.s.CreateMember(l.ctx, store.NewMember{RoomID: room.ID, AgentID: agent.ID, DisplayName: member})
	if err != nil {
		l.t.Fatal(err)
	}
	return otherProject{project: project, room: room, member: m}
}

func (l *loop) askIn(o otherProject, body string, turns int) []store.Turn {
	l.t.Helper()
	_, err := l.h.PostUserMessage(l.ctx, store.NewMessage{RoomID: o.room.ID, UserID: l.user.ID, Body: body, Mentions: []store.Mention{{Kind: store.MentionAgent, ID: o.member.ID}}})
	if err != nil {
		l.t.Fatal(err)
	}
	var got []store.Turn
	eventually(l.t, func() bool {
		got, _ = l.s.ListRoomTurns(l.ctx, o.room.ID, 20)
		if len(got) != turns {
			return false
		}
		for _, turn := range got {
			if turn.Status == store.TurnRunning {
				return false
			}
		}
		return true
	}, "the other project's turn")
	return got
}

func skillArgs() map[string]any {
	return map[string]any{
		"scope": "library", "type": "Skill", "slug": "go-table-tests", "title": "Go table tests",
		"description": "Use when writing Go tests with several cases.", "body": "Write the cases as a table.\n\nSee [the pattern](/patterns/copy-paste-tests.md).",
		"sources": []any{map[string]any{"id": "p1", "resource": "/patterns/copy-paste-tests.md", "title": "Copy-pasted tests"}},
	}
}

// addSkill is a person adding a skill to the library from a folder of
// theirs, looked after by the team of project team, or by none.
func (l *loop) addSkill(name, description, body, team string) WikiPageView {
	l.t.Helper()
	dir := filepath.Join(l.t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		l.t.Fatal(err)
	}
	main := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(main), 0o644); err != nil {
		l.t.Fatal(err)
	}
	page, err := l.h.ImportSkill(l.ctx, dir, team, l.user.ID)
	if err != nil {
		l.t.Fatal(err)
	}
	return page
}

// install installs the library's skill name for a member's agent.
func (l *loop) install(name string, member store.Member) {
	l.t.Helper()
	if _, err := l.h.InstallSkill(l.ctx, name, member.AgentID, true); err != nil {
		l.t.Fatal(err)
	}
}

// A skill's settings for runtimes are the person's (docs/design.md 5.15):
// an agent improving a skill installed for it changes its text, not what
// a runtime lets the skill do or keeps it from.
func TestLoop_SkillSettingsArePeoples(t *testing.T) {
	l, dir := wikiLoop(t)
	home := l.project()
	src := skillFolder(t, "release-notes", map[string]string{
		"SKILL.md": "---\nname: release-notes\ndescription: Use when writing release notes.\nallowed-tools: Bash(git log:*)\ndisable-model-invocation: true\n---\n\nGroup the changes by kind.\n",
	})
	if _, err := l.h.ImportSkill(l.ctx, src, home.ID, l.user.ID); err != nil {
		t.Fatal(err)
	}
	patch := func(target, content string) map[string]any {
		return call(runtime.WikiToolPatch, map[string]any{"scope": "library", "path": "/skills/release-notes/SKILL.md", "reason": "better",
			"edits": []any{map[string]any{"op": "replace", "target": target, "content": content}}})
	}
	editor := l.member("Editor", map[string]any{"tool_calls": []any{
		patch("disable-model-invocation: true", "disable-model-invocation: false"),
		patch("allowed-tools: Bash(git log:*)", "allowed-tools: Bash(*)"),
		patch("disable-model-invocation: true", "disable-model-invocation: true\nhooks:\n  Stop: []"),
		patch("disable-model-invocation: true", "disable-model-invocation: true\nstatus: deprecated"),
		patch("disable-model-invocation: true", "disable-model-invocation: true\ntags: [runtime-claude]"),
		patch("Group the changes by kind.", "Group the changes by kind, newest first."),
	}})
	l.install("release-notes", editor)
	asked := l.say("@Editor improve the release notes skill", "", editor)
	l.waitTurns(1, store.TurnDone, "Editor's turn")
	answer := l.root(l.topic(asked)).Body
	if n := strings.Count(answer, "are set by people"); n != 5 {
		t.Errorf("the five changes to its settings are refused, %d were:\n%s", n, answer)
	}
	if !strings.Contains(answer, "Changed the skill release-notes") {
		t.Errorf("the change to its text goes through:\n%s", answer)
	}
	data, err := os.ReadFile(filepath.Join(dir, "library", "skills", "release-notes", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"disable-model-invocation: true", "allowed-tools: Bash(git log:*)", "newest first"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("SKILL.md lacks %q:\n%s", want, data)
		}
	}
	for _, gone := range []string{"hooks", "status:", "tags:"} {
		if strings.Contains(string(data), gone) {
			t.Errorf("no %s came in:\n%s", gone, data)
		}
	}
	// It still goes to the agents it is installed for.
	agent, _ := l.s.GetAgent(l.ctx, editor.AgentID)
	if skills := l.h.turns.librarySkills(l.ctx, agent); len(skills) != 1 {
		t.Errorf("the skill still goes to its agents: %d", len(skills))
	}
}

func TestLoop_TheSkillLibrary(t *testing.T) {
	l, dir := wikiLoop(t)
	home := l.project()
	scribe := l.member("Scribe", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, map[string]any{
			"scope": "library", "type": "Pattern", "slug": "copy-paste-tests", "title": "Copy-pasted tests drift apart",
			"description": "Tests copied case by case stop matching.", "body": "Symptom, root cause, fix: a table.",
		}),
		call(runtime.WikiToolWrite, skillArgs()),
		call(runtime.WikiToolWrite, map[string]any{"scope": "library", "type": "Fact", "slug": "x", "title": "x", "description": "x", "body": "x"}),
	}})
	asked := l.say("@Scribe keep what we learned", "", scribe)
	l.waitTurns(1, store.TurnDone, "Scribe's turn")
	answer := l.root(l.topic(asked)).Body
	for _, want := range []string{"Saved /patterns/copy-paste-tests.md in the skill library", "skills are added by people", "a Fact goes in the project's wiki"} {
		if !strings.Contains(answer, want) {
			t.Errorf("Scribe should hear %q:\n%s", want, answer)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "library", "patterns", "copy-paste-tests.md")); err != nil {
		t.Fatalf("the pattern is saved at once: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "library", "skills", "go-table-tests")); !os.IsNotExist(err) {
		t.Errorf("an agent adds no skill: %v", err)
	}

	// A person adds it, looked after by this project's team.
	skill := l.addSkill("go-table-tests", "Use when writing Go tests with several cases.", "Write the cases as a table.", home.ID)
	if skill.Team != home.WikiSlug || skill.GeneratedBy != "human:alice" || len(skill.Installed) != 0 {
		t.Fatalf("the skill added %+v", skill.WikiPageInfo)
	}
	catalog, _ := l.h.LibraryCatalog(l.ctx)
	if len(catalog.Pages) != 2 || len(catalog.Teams) != 1 || catalog.Teams[0].ProjectID != home.ID || catalog.Dirs[0].Type != "Skill" {
		t.Errorf("library catalog %+v", catalog)
	}

	// Another project's member does not change it until it is installed
	// for its agent; then it does, at once, on trial, and nothing waits.
	patch := call(runtime.WikiToolPatch, map[string]any{"scope": "library", "path": "/skills/go-table-tests/SKILL.md", "reason": "subtests read better",
		"edits": []any{map[string]any{"op": "append", "content": "Name each case with t.Run."}}})
	other := l.otherProject("docs site", "Other", map[string]any{"tool_calls": []any{patch}})
	turns := l.askIn(other, "@Other improve the table tests skill", 1)
	if answer := l.root(l.topicOf(turns[0])).Body; !strings.Contains(answer, "not installed for you") {
		t.Errorf("Other should hear the skill is not theirs:\n%s", answer)
	}
	l.install("go-table-tests", other.member)
	l.askIn(other, "@Other try again", 2)
	if trial := l.trialOf("go-table-tests", store.TrialOpen); trial.ChangedBy != "Other" || trial.ProjectName != "docs site" {
		t.Errorf("on trial %+v", trial)
	}
	if skill, _ := l.h.LibraryPage(l.ctx, wiki.SkillPath("go-table-tests")); !strings.Contains(skill.Body, "Name each case with t.Run.") || skill.Team != home.WikiSlug {
		t.Errorf("changed at once, still the team's: %+v", skill.WikiPageInfo)
	}

	// Nor does a team take a skill over by editing whose it is.
	grab := call(runtime.WikiToolPatch, map[string]any{"scope": "library", "path": "/skills/go-table-tests/SKILL.md", "reason": "ours now",
		"edits": []any{map[string]any{"op": "replace", "target": "veyloom-team: " + home.WikiSlug, "content": "veyloom-team: third"}}})
	taker := l.otherProject("third", "Taker", map[string]any{"tool_calls": []any{grab}})
	l.install("go-table-tests", taker.member)
	taken := l.askIn(taker, "@Taker take the skill over", 1)
	if answer := l.root(l.topicOf(taken[0])).Body; !strings.Contains(answer, "a person hands a skill over to another team") {
		t.Errorf("a skill's team is not patched:\n%s", answer)
	}

	// A person hands it to the other team; when that project goes, the
	// skill is nobody's but stays.
	handed, err := l.h.TransferSkill(l.ctx, "go-table-tests", other.project.ID, l.user.ID)
	if err != nil || handed.Team != other.project.WikiSlug {
		t.Fatalf("handed %+v %v", handed.WikiPageInfo, err)
	}
	if err := l.h.ArchiveProjectWiki(other.project.ID, other.project.WikiSlug); err != nil {
		t.Fatal(err)
	}
	orphan, _ := l.h.LibraryPage(l.ctx, wiki.SkillPath("go-table-tests"))
	if orphan.Team != "" || orphan.Status != "stable" {
		t.Errorf("an orphaned skill %+v", orphan.WikiPageInfo)
	}
	history, _ := l.h.LibraryHistory(l.ctx, "", 20)
	var subjects []string
	for _, c := range history {
		subjects = append(subjects, c.Subject)
	}
	if !strings.Contains(strings.Join(subjects, "|"), "Scribe of "+l.project().WikiSlug+" in topic #1") || history[len(history)-2].ProjectName != "p" {
		t.Errorf("library history %q, first turn's project %q", subjects, history[len(history)-2].ProjectName)
	}
}

// topicOf is the topic a turn ran in.
func (l *loop) topicOf(turn store.Turn) store.Thread {
	l.t.Helper()
	thread, err := l.s.GetThread(l.ctx, turn.ThreadID)
	if err != nil {
		l.t.Fatal(err)
	}
	return thread
}

func TestSkillsIn(t *testing.T) {
	set := &runtime.SkillSet{Hash: "0123456789abcdef", Skills: []runtime.Skill{{Name: "go"}, {Name: "go-table-tests"}}}
	for _, tc := range []struct {
		name string
		ev   runtime.Event
		want []string
	}{
		{"Claude's Skill tool", runtime.Event{Kind: runtime.EventToolCall, Tool: "Skill", Input: `{"skill":"veyloom:go-table-tests"}`}, []string{"go-table-tests"}},
		{"a read of SKILL.md", runtime.Event{Kind: runtime.EventToolCall, Tool: "read", Input: `{"path":"/tools/skills/0123456789abcdef/skills/go/SKILL.md"}`}, []string{"go"}},
		{"a shell reading a script", runtime.Event{Kind: runtime.EventToolCall, Tool: "commandExecution", Input: "sh /t/skills/0123456789abcdef/skills/go-table-tests/scripts/run.sh"}, []string{"go-table-tests"}},
		{"another set's file", runtime.Event{Kind: runtime.EventToolCall, Tool: "read", Input: `{"path":"/tools/skills/ffffffffffffffff/skills/go/SKILL.md"}`}, nil},
		{"the person's own skill", runtime.Event{Kind: runtime.EventToolCall, Tool: "Skill", Input: `{"skill":"pdf"}`}, nil},
		{"a result, not a call", runtime.Event{Kind: runtime.EventToolResult, Tool: "Skill", Input: `{"skill":"veyloom:go"}`}, nil},
		// Renamed where a person's own skill has the name: the set's
		// directory carries more hex, the skill its alias.
		{"a renamed skill read", runtime.Event{Kind: runtime.EventToolCall, Tool: "read", Input: `{"path":"/tools/skills/0123456789abcdef9a8b7c6d/skills/veyloom-go/SKILL.md"}`}, []string{"go"}},
		{"a renamed skill through Claude", runtime.Event{Kind: runtime.EventToolCall, Tool: "Skill", Input: `{"skill":"veyloom:veyloom-go-table-tests"}`}, []string{"go-table-tests"}},
		{"a longer name that only starts the same", runtime.Event{Kind: runtime.EventToolCall, Tool: "read", Input: `{"path":"/tools/skills/0123456789abcdef/skills/go-other/SKILL.md"}`}, nil},
	} {
		if got := skillsIn(tc.ev, set); strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if skillsIn(runtime.Event{Kind: runtime.EventToolCall, Tool: "Skill", Input: `{"skill":"veyloom:go"}`}, nil) != nil {
		t.Error("a turn given no skills uses none")
	}
}

// A call reaching for a skill counts once the runtime carried it out:
// Claude Code refuses a skill kept from the model, and says so in the
// call's result.
func TestNoteSkillUse(t *testing.T) {
	m := &TurnManager{}
	at := &activeTurn{spec: runtime.TurnSpec{Skills: &runtime.SkillSet{Hash: "0123456789abcdef", Skills: []runtime.Skill{{Name: "gate-word"}, {Name: "go"}}}}}
	events := []runtime.Event{
		{Kind: runtime.EventToolCall, Tool: "Skill", CallID: "c1", Input: `{"skill":"gate-word"}`},
		{Kind: runtime.EventToolCall, Tool: "read", CallID: "c2", Input: `{"path":"/t/skills/0123456789abcdef/skills/go/SKILL.md"}`},
	}
	for _, ev := range events {
		m.noteSkillUse(at, ev)
	}
	if len(at.skillsUsed) != 0 {
		t.Errorf("nothing is used before the results: %v", at.skillsUsed)
	}
	m.noteSkillUse(at, runtime.Event{Kind: runtime.EventToolResult, Tool: "Skill", CallID: "c1", Failed: true, Text: "cannot be used with Skill tool due to disable-model-invocation"})
	m.noteSkillUse(at, runtime.Event{Kind: runtime.EventToolResult, Tool: "read", CallID: "c2", Text: "The go skill"})
	if !slices.Equal(at.skillsUsed, []string{"go"}) || len(at.skillCalls) != 0 {
		t.Errorf("used %v, waiting %v; want only the call that went through", at.skillsUsed, at.skillCalls)
	}
	// A runtime that does not name its calls has them count at once.
	m.noteSkillUse(at, runtime.Event{Kind: runtime.EventToolCall, Tool: "Skill", Input: `{"skill":"veyloom:gate-word"}`})
	if !slices.Equal(at.skillsUsed, []string{"go", "gate-word"}) {
		t.Errorf("used %v", at.skillsUsed)
	}
}

func TestLoop_TurnsGetTheSkillsAndSayWhichTheyUsed(t *testing.T) {
	l, _ := wikiLoop(t)
	scribe := l.member("Scribe", nil)
	l.say("@Scribe hello", "", scribe)
	l.waitTurns(1, store.TurnDone, "Scribe's turn")
	l.addSkill("go-table-tests", "Use when writing Go tests with several cases.", "Write the cases as a table.", l.project().ID)

	// Only an agent it is installed for is given it.
	user := l.member("User", map[string]any{"use_skill": "go-table-tests"})
	before := l.say("@User write the tests", "", user)
	l.waitTurns(2, store.TurnDone, "User's turn before the install")
	if reply := l.root(l.topic(before)).Body; reply != "@alice no such skill: go-table-tests" {
		t.Errorf("not installed, the skill is not there: %q", reply)
	}
	l.install("go-table-tests", user)
	if page, _ := l.h.LibraryPage(l.ctx, wiki.SkillPath("go-table-tests")); len(page.Installed) != 1 || page.Installed[0].Name != "User agent" {
		t.Errorf("the skill's page names whom it is installed for: %+v", page.Installed)
	}
	asked := l.say("@User write the tests", "", user)
	turns := l.waitTurns(3, store.TurnDone, "User's turn")
	if got := turns[0].SkillsUsed; len(got) != 1 || got[0] != "go-table-tests" {
		t.Errorf("the turn used %v", got)
	}
	// The runtime read the skill as the library has it, in Agent Skills form.
	if reply := l.root(l.topic(asked)).Body; reply != "@alice Write the cases as a table." {
		t.Errorf("the skill as the runtime read it: %q", reply)
	}
	// The transcript names the skills; their text is the library's.
	tx := transcriptOf(t, turns[0])
	if !strings.Contains(tx, `"name":"go-table-tests"`) || strings.Contains(tx, "Write the cases as a table.\\n") || strings.Contains(tx, `"files"`) {
		t.Errorf("transcript start:\n%s", strings.SplitN(tx, "\n", 2)[0])
	}
	uses, err := l.h.SkillUses(l.ctx, "go-table-tests", 10)
	if err != nil || len(uses) != 1 || uses[0].TurnID != turns[0].ID || uses[0].MemberName != "User" {
		t.Errorf("uses %+v %v", uses, err)
	}
	// A turn that reads nothing of it used nothing.
	if turns[2].SkillsUsed != nil && len(turns[2].SkillsUsed) != 0 {
		t.Errorf("Scribe's turn used %v", turns[2].SkillsUsed)
	}
	// Taken off, the skill is not given any more.
	if installed, err := l.h.InstallSkill(l.ctx, "go-table-tests", user.AgentID, false); err != nil || len(installed) != 0 {
		t.Errorf("taken off: %+v %v", installed, err)
	}
	after := l.say("@User write the tests", "", user)
	l.waitTurns(4, store.TurnDone, "User's turn after")
	if reply := l.root(l.topic(after)).Body; reply != "@alice no such skill: go-table-tests" {
		t.Errorf("taken off, the skill is not there: %q", reply)
	}
}
