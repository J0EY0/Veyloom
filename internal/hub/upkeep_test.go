package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// setOptions gives a member's agent new runtime options, for its next turn.
func (l *loop) setOptions(member store.Member, options map[string]any) {
	l.t.Helper()
	agent, err := l.s.GetAgent(l.ctx, member.AgentID)
	if err != nil {
		l.t.Fatal(err)
	}
	_, err = l.s.UpdateAgent(l.ctx, agent.ID, store.NewAgent{
		Name: agent.Name, MachineID: agent.MachineID, Runtime: agent.Runtime, Model: agent.Model,
		RoleCard: agent.RoleCard, PermissionPreset: agent.PermissionPreset, RuntimeOptions: options,
	})
	if err != nil {
		l.t.Fatal(err)
	}
}

// upkeepOn turns a project's wiki upkeep on, in a ProjectPatch.
var upkeepOn = true

// keep makes member the project's wiki maintainer, running on trigger.
func (l *loop) keep(member store.Member, trigger store.UpkeepTrigger) {
	l.t.Helper()
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{WikiUpkeep: &upkeepOn, WikiMaintainer: &member.ID, WikiMaintainerTrigger: &trigger}); err != nil {
		l.t.Fatal(err)
	}
}

// upkeeps are the room's upkeep turns, newest first, once none is running.
func (l *loop) upkeeps(n int) []store.Turn {
	l.t.Helper()
	var out []store.Turn
	eventually(l.t, func() bool {
		out = nil
		for _, turn := range l.turns() {
			if turn.Kind == store.TurnUpkeep {
				if turn.Status == store.TurnRunning {
					return false
				}
				out = append(out, turn)
			}
		}
		return len(out) == n
	}, "the upkeeps")
	return out
}

// upkeepReply is what the maintainer said in its upkeep.
func (l *loop) upkeepReply(turn store.Turn) string {
	l.t.Helper()
	var said []string
	for _, m := range l.replies(turn.ThreadID, store.SenderAgent) {
		if m.TurnID == turn.ID {
			said = append(said, m.Body)
		}
	}
	return strings.Join(said, "\n")
}

func TestLoop_TheMaintainerGoesOverWhatTheChatDid(t *testing.T) {
	l, dir := wikiLoop(t)
	coder := l.member("Coder", map[string]any{"reply": "The staging database listens on 6543."})
	keeper := l.member("Keeper", nil)

	asked := l.say("@Coder where does the staging database listen?", "", coder)
	l.waitTurns(1, store.TurnDone, "Coder's answer")
	topic := l.topic(asked)
	// The person sets it right in the topic, and Coder answers that too.
	l.say("No, it moved to 6544 last week.", topic.ID)
	chat := l.waitTurns(2, store.TurnDone, "Coder's second answer")
	first := chat[1]

	if _, err := l.h.StartUpkeep(l.ctx, l.room.ProjectID); !errors.Is(err, store.ErrInvalidInput) {
		t.Fatalf("an upkeep without a maintainer: %v", err)
	}
	l.keep(keeper, store.UpkeepManual)
	status, err := l.h.UpkeepStatus(l.ctx, l.room.ProjectID)
	if err != nil || status.MemberID != keeper.ID || status.MemberName != "Keeper" || status.Trigger != store.UpkeepManual ||
		status.Waiting.Own != 2 || status.Last != nil || status.ThreadID != "" || status.IdleMinutes != 30 {
		t.Fatalf("status before the first upkeep %+v %v", status, err)
	}

	l.setOptions(keeper, map[string]any{"tool_calls": []any{
		call(runtime.UpkeepToolListTurns, map[string]any{}),
		call(runtime.RoomToolReadTurn, map[string]any{"turn": first.ID}),
		call(runtime.UpkeepToolListTurns, map[string]any{"skills": true}),
		call(runtime.WikiToolWrite, map[string]any{
			"type": "Fact", "slug": "staging-db-port", "title": "Staging database port",
			"description": "The staging database listens on 6544.", "body": "Port 6544, since it moved.", "topics": []any{topic.Number},
		}),
	}})
	if _, err := l.h.StartUpkeep(l.ctx, l.room.ProjectID); err != nil {
		t.Fatal(err)
	}
	upkeep := l.upkeeps(1)[0]
	if upkeep.Status != store.TurnDone || upkeep.MemberID != keeper.ID || upkeep.SessionID != "" {
		t.Fatalf("the upkeep turn %+v", upkeep)
	}
	project := l.project()
	if project.WikiThreadID == "" || upkeep.ThreadID != project.WikiThreadID {
		t.Fatalf("the upkeep runs in the wiki topic %q: %+v", project.WikiThreadID, upkeep)
	}
	if notes := l.notes(upkeep.ThreadID); countContaining(notes, "Wiki upkeep by Keeper (asked by a person): 2 turns of this chat, 0 turns of other projects using this team's skills, 2 messages from people.") != 1 {
		t.Errorf("the wiki topic says the upkeep started: %q", notes)
	}

	brief := promptOf(t, upkeep)
	for _, want := range []string{
		`You are "Keeper", the wiki maintainer of the project "p"`, "Why now: asked by a person.",
		"Turns of this chat to go over (2, oldest first):", "- " + first.ID + `: topic #1 "The staging database listens on 6543.", Coder, done`,
		"The project wiki (0 pages", "Health check:\nNothing found.", "list_turns, rollback_skill", "read_topic, read_turn,",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lacks %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "other projects that used") {
		t.Errorf("a team that owns no skills hears of no other project:\n%s", brief)
	}

	said := l.upkeepReply(upkeep)
	for _, want := range []string{
		"Turns of this chat, newest first:", first.ID,
		"What it was asked, by alice", "where does the staging database listen?",
		"Said: The staging database listens on 6543.",
		"What a person said next in the topic, alice", "No, it moved to 6544 last week.",
		"owns no skills of the library",
		"Saved /facts/staging-db-port.md",
	} {
		if !strings.Contains(said, want) {
			t.Errorf("the maintainer lacks %q:\n%s", want, said)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "projects", project.WikiSlug, "facts", "staging-db-port.md")); err != nil {
		t.Errorf("the page it wrote: %v", err)
	}

	// Gone over: nothing waits any more.
	status, _ = l.h.UpkeepStatus(l.ctx, l.room.ProjectID)
	if status.Waiting.Total() != 0 || status.Last == nil || status.Last.ID != upkeep.ID || status.ThreadID != project.WikiThreadID || status.Queued {
		t.Errorf("status after the upkeep %+v", status)
	}

	// The spec it ran with: the memory tools and the maintainer's, a
	// session of its own.
	var start struct {
		Spec runtime.TurnSpec `json:"spec"`
	}
	json.Unmarshal([]byte(strings.SplitN(transcriptOf(t, upkeep), "\n", 2)[0]), &start)
	if !equalStrings(start.Spec.ExtraTools, append(append([]string{}, runtime.MemoryToolNames...), runtime.UpkeepToolNames...)) || start.Spec.Session.Key != upkeep.ID || start.Spec.Session.Resume {
		t.Errorf("the upkeep's spec %+v", start.Spec)
	}

	// Asked again with nothing new, it looks over the wiki as a whole.
	l.setOptions(keeper, map[string]any{"reply": "All in order."})
	if _, err := l.h.StartUpkeep(l.ctx, l.room.ProjectID); err != nil {
		t.Fatal(err)
	}
	again := l.upkeeps(2)[0]
	if brief := promptOf(t, again); !strings.Contains(brief, "None: nothing new since the last upkeep") || !strings.Contains(brief, "/facts/staging-db-port.md (Fact): Staging database port") {
		t.Errorf("the second brief:\n%s", brief)
	}
	if got := len(l.turns()); got != 4 {
		t.Errorf("the upkeeps are not gone over themselves: %d turns", got)
	}
}

func equalStrings(a, b []string) bool {
	return strings.Join(a, "\x00") == strings.Join(b, "\x00")
}

func TestLoop_OnlyTheUpkeepHasTheMaintainersTools(t *testing.T) {
	l, _ := wikiLoop(t)
	nosy := l.member("Nosy", map[string]any{"tool_calls": []any{call(runtime.UpkeepToolListTurns, map[string]any{})}})
	l.say("@Nosy what did everyone do?", "", nosy)
	l.waitTurns(1, store.TurnDone, "Nosy's turn")
	if answer := l.root(l.topic(l.topLevel()[0])).Body; !strings.Contains(answer, "this turn has no tool list_turns") {
		t.Errorf("a chat turn is given the maintainer's tools:\n%s", answer)
	}
	// Nor would the hub answer it.
	if _, err := l.h.turns.answerUpkeep(l.ctx, &activeTurn{}, runtime.RoomQuery{Tool: runtime.UpkeepToolListTurns}); err == nil {
		t.Error("the hub answers a chat turn's list_turns")
	}
}

func TestUpkeep_ReadsOnlyWhatIsTheTeams(t *testing.T) {
	l, _ := wikiLoop(t)
	coder := l.member("Coder", nil)
	l.say("@Coder go", "", coder)
	own := l.waitTurns(1, store.TurnDone, "Coder's turn")[0]

	// Another project, one turn using the team's skill, one not.
	other := l.otherProject("docs site", "Other", nil)
	finish := func(skills ...string) store.Turn {
		t.Helper()
		root, _ := l.s.CreateMessage(l.ctx, store.NewMessage{RoomID: other.room.ID, SenderKind: store.SenderUser, UserID: l.user.ID, Body: "@Other go"})
		thread, _ := l.s.ThreadForMessage(l.ctx, root.ID)
		turn, err := l.s.CreateTurn(l.ctx, store.NewTurn{MemberID: other.member.ID, RoomID: other.room.ID, ThreadID: thread.ID, MachineID: l.machineID, Runtime: "fake"})
		if err != nil {
			t.Fatal(err)
		}
		done, _ := l.s.FinishTurn(l.ctx, turn.ID, store.TurnOutcome{Status: store.TurnFailed, Error: "the tests broke", SkillsUsed: skills})
		return done
	}
	used := finish("go-table-tests")
	unrelated := finish("release-notes")

	up := &upkeep{project: l.project(), owned: []string{"go-table-tests"}}
	m := l.h.turns
	if text, err := m.readTurn(l.ctx, up.project, up.owned, own.ID); err != nil || !strings.Contains(text, `of project "p": done`) {
		t.Errorf("its own project's turn: %v\n%s", err, text)
	}
	text, err := m.readTurn(l.ctx, up.project, up.owned, used.ID)
	if err != nil || !strings.Contains(text, `of project "docs site": failed`) || !strings.Contains(text, "It ended with: the tests broke") ||
		!strings.Contains(text, "Skills of the library it used: go-table-tests") || !strings.Contains(text, "(its transcript is gone)") {
		t.Errorf("another project's use of the team's skill: %v\n%s", err, text)
	}
	if _, err := m.readTurn(l.ctx, up.project, up.owned, unrelated.ID); err == nil || !strings.Contains(err.Error(), "not yours to read") {
		t.Errorf("another project's turn with none of the team's skills: %v", err)
	}
	if _, err := m.readTurn(l.ctx, up.project, up.owned, "not-a-turn"); err == nil {
		t.Error("a turn there is not")
	}
	// A member in a chat turn reads its own project's turns, and no other
	// project's, whatever skills they used.
	if text, err := m.readTurn(l.ctx, up.project, nil, own.ID); err != nil || !strings.Contains(text, `of project "p": done`) {
		t.Errorf("a member reading its project's turn: %v\n%s", err, text)
	}
	if _, err := m.readTurn(l.ctx, up.project, nil, used.ID); err == nil || !strings.Contains(err.Error(), "is another project's: it is not yours to read") {
		t.Errorf("a member reading another project's turn: %v", err)
	}
	list, err := m.listTurns(l.ctx, up, upkeepArgs{Skills: true})
	if err != nil || !strings.Contains(list, used.ID) || strings.Contains(list, unrelated.ID) || !strings.Contains(list, `project "docs site"`) {
		t.Errorf("the team's skill uses: %v\n%s", err, list)
	}
	if _, err := m.listTurns(l.ctx, up, upkeepArgs{Skills: true, Topic: 1}); err == nil {
		t.Error("a topic of other projects")
	}
	if list, _ := m.listTurns(l.ctx, up, upkeepArgs{Before: own.ID}); list != "No older turns." {
		t.Errorf("before the first turn: %q", list)
	}
}

func TestHub_WhenAnUpkeepIsDue(t *testing.T) {
	// The hub's clock runs ahead by as much as the test says.
	var ahead atomic.Int64
	l := newLoopWith(t, Config{WikiDir: t.TempDir()}, WithClock(func() time.Time { return time.Now().Add(time.Duration(ahead.Load())) }))
	coder := l.member("Coder", nil)
	keeper := l.member("Keeper", map[string]any{"reply": "Kept."})
	ctx := context.Background()
	at := func(d time.Duration) { ahead.Store(int64(d)) }
	due := func() (upkeepReason, bool) {
		t.Helper()
		project, _ := l.s.GetProject(ctx, l.room.ProjectID)
		reason, ok, err := l.h.upkeepDue(ctx, project)
		if err != nil {
			t.Fatal(err)
		}
		return reason, ok
	}

	l.say("@Coder go", "", coder)
	l.waitTurns(1, store.TurnDone, "Coder's turn")
	l.keep(keeper, store.UpkeepIdle)
	at(0)
	if _, ok := due(); ok {
		t.Error("the topic has only just gone quiet")
	}
	at(2 * time.Hour)
	if reason, ok := due(); !ok || reason != upkeepQuiet {
		t.Fatalf("quiet for long enough: %q %v", reason, ok)
	}
	// Queued, it is not due again.
	l.h.turns.reserveUpkeep(l.room.ProjectID)
	if _, ok := due(); ok {
		t.Error("an upkeep is queued already")
	}
	l.h.turns.upkeepStarted(l.room.ProjectID)

	l.h.checkUpkeep(ctx)
	first := l.upkeeps(1)[0]
	if notes := l.notes(first.ThreadID); countContaining(notes, "Wiki upkeep by Keeper (topics went quiet): 1 turn of this chat") != 1 {
		t.Errorf("notes %q", notes)
	}
	if _, ok := due(); ok {
		t.Error("everything was gone over")
	}

	// Something new: due again once quiet.
	l.say("@Coder more", "", coder)
	eventually(t, func() bool {
		w, _ := l.s.CountUpkeepWaiting(ctx, l.room.ProjectID, nil, time.Now().Add(time.Hour))
		return w.Own == 1
	}, "the new turn")
	at(4 * time.Hour)
	if _, ok := due(); !ok {
		t.Error("quiet again, and long after the last upkeep")
	}

	// Daily: once a day, when there is something new.
	l.keep(keeper, store.UpkeepDaily)
	if _, ok := due(); ok {
		t.Error("the last upkeep was not a day ago")
	}
	at(25 * time.Hour)
	if reason, ok := due(); !ok || reason != upkeepDaily {
		t.Errorf("a day later: %q %v", reason, ok)
	}

	// Every three days, and weekly (the longest, design.md 5.16).
	l.keep(keeper, store.UpkeepEvery3Days)
	if _, ok := due(); ok {
		t.Error("the last upkeep was not three days ago")
	}
	at(73 * time.Hour)
	if reason, ok := due(); !ok || reason != upkeepEvery3Days {
		t.Errorf("three days later: %q %v", reason, ok)
	}
	l.keep(keeper, store.UpkeepWeekly)
	if _, ok := due(); ok {
		t.Error("the last upkeep was not a week ago")
	}
	at(169 * time.Hour)
	if reason, ok := due(); !ok || reason != upkeepWeekly {
		t.Errorf("a week later: %q %v", reason, ok)
	}

	// Manual: never of itself.
	l.keep(keeper, store.UpkeepManual)
	if _, ok := due(); ok {
		t.Error("a manual maintainer runs only when asked")
	}
}

// What people said and sent since the last upkeep is news to the next one,
// though no agent was asked (design.md 5.16): it makes an upkeep due, the
// brief says where and how much, not what, and lists the files with the ids
// that keep them and the paths that open them. Gone over, it is news no
// more.
func TestLoop_TheMaintainerHearsWhatPeopleSaid(t *testing.T) {
	files := t.TempDir()
	l := newLoopWith(t, Config{WikiDir: t.TempDir(), AttachmentDir: files})
	keeper := l.member("Keeper", map[string]any{"reply": "Kept."})
	l.keep(keeper, store.UpkeepDaily)
	due := func() bool {
		t.Helper()
		_, ok, err := l.h.upkeepDue(l.ctx, l.project())
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if due() {
		t.Fatal("nothing said yet")
	}

	checklist := l.attach(files, l.room.ID, "release-checklist.md", "text/markdown", "- run make test-db first\n")
	said, err := l.h.PostUserMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, UserID: l.user.ID, Body: "定了：发版前先跑一遍端到端。", AttachmentIDs: []string{checklist.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := l.h.UpkeepStatus(l.ctx, l.room.ProjectID); status.Waiting.People != 1 || status.Waiting.Total() != 0 {
		t.Fatalf("a person's word waits, and no turn: %+v", status.Waiting)
	}
	if !due() {
		t.Fatal("what a person said is due for the daily upkeep")
	}
	l.h.checkUpkeep(l.ctx)
	upkeep := l.upkeeps(1)[0]
	brief := promptOf(t, upkeep)
	for _, want := range []string{
		"Go over what people said and sent since the last upkeep",
		"What people said since the last upkeep (1 message; read them with read_room and read_topic):",
		"- the room itself: 1 message, 1 file, the last ",
		"- release-checklist.md (text/markdown, 25 bytes), in the room by alice, ",
		"file " + checklist.ID + " at " + filepath.Join(files, filepath.FromSlash(checklist.Path)),
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the brief lacks %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "发版前先跑一遍端到端") {
		t.Errorf("the brief says where and how much, not what:\n%s", brief)
	}
	if notes := l.notes(upkeep.ThreadID); countContaining(notes, "0 turns of this chat, 0 turns of other projects using this team's skills, 1 message from people.") != 1 {
		t.Errorf("notes %q", notes)
	}
	if p := l.project(); p.WikiSeenSeq < said.Seq {
		t.Errorf("gone over up to %d, said at %d", p.WikiSeenSeq, said.Seq)
	}
	if status, _ := l.h.UpkeepStatus(l.ctx, l.room.ProjectID); status.Waiting.People != 0 {
		t.Errorf("gone over, it is news no more: %+v", status.Waiting)
	}
}

// An upkeep that went over as many turns as it may leaves the rest to the
// next, which starts at once rather than a day later, up to the day's runs.
func TestHub_ABacklogIsGoneOverAtOnce(t *testing.T) {
	l := newLoopWith(t, Config{WikiDir: t.TempDir(), UpkeepTurns: 1, UpkeepRunsPerDay: 2})
	coder := l.member("Coder", nil)
	keeper := l.member("Keeper", map[string]any{"reply": "Kept."})
	for i := range 3 {
		l.say(fmt.Sprintf("@Coder go %d", i), "", coder)
		l.waitTurns(i+1, store.TurnDone, "Coder's turn")
	}
	l.keep(keeper, store.UpkeepDaily)
	due := func() (upkeepReason, bool) {
		t.Helper()
		reason, ok, err := l.h.upkeepDue(l.ctx, l.project())
		if err != nil {
			t.Fatal(err)
		}
		return reason, ok
	}
	if reason, ok := due(); !ok || reason != upkeepDaily {
		t.Fatalf("never gone over: %q %v", reason, ok)
	}
	l.h.checkUpkeep(l.ctx)
	l.upkeeps(1)
	if reason, ok := due(); !ok || reason != upkeepBacklog {
		t.Fatalf("one of three turns gone over, the rest wait: %q %v", reason, ok)
	}
	l.h.checkUpkeep(l.ctx)
	l.upkeeps(2)
	if w, _ := l.s.CountUpkeepWaiting(l.ctx, l.room.ProjectID, nil, time.Now()); w.Own != 1 {
		t.Fatalf("one turn left: %+v", w)
	}
	if _, ok := due(); ok {
		t.Error("the day's two runs are used up, though a turn still waits")
	}
}

// A project without a maintainer is offered one in its chat once enough of
// its topics wait, and only once (design.md 5.16).
func TestHub_OffersAMaintainerOnce(t *testing.T) {
	var ahead atomic.Int64
	l := newLoopWith(t, Config{WikiDir: t.TempDir(), UpkeepOfferTopics: 2}, WithClock(func() time.Time { return time.Now().Add(time.Duration(ahead.Load())) }))
	coder := l.member("Coder", nil)
	systemNotes := func() []store.Message {
		t.Helper()
		msgs, err := l.s.ListRoomMessagesBefore(l.ctx, l.room.ID, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		var out []store.Message
		for _, m := range msgs {
			if m.SenderKind == store.SenderSystem {
				out = append(out, m)
			}
		}
		return out
	}

	l.say("@Coder one", "", coder)
	l.waitTurns(1, store.TurnDone, "the first turn")
	ahead.Store(int64(time.Hour))
	l.h.checkUpkeep(l.ctx)
	if p := l.project(); p.WikiOfferMessageID != "" {
		t.Fatalf("one quiet topic is not enough: %+v", p)
	}
	l.say("@Coder two", "", coder)
	l.waitTurns(2, store.TurnDone, "the second turn")
	// Not yet quiet for long enough.
	ahead.Store(0)
	l.h.checkUpkeep(l.ctx)
	if p := l.project(); p.WikiOfferMessageID != "" {
		t.Fatalf("the second topic has only just gone quiet: %+v", p)
	}
	ahead.Store(int64(time.Hour))
	l.h.checkUpkeep(l.ctx)
	p := l.project()
	notes := systemNotes()
	if p.WikiOfferMessageID == "" || len(notes) != 1 || notes[0].ID != p.WikiOfferMessageID || notes[0].ThreadID != "" ||
		!strings.Contains(notes[0].Body, "2 topics of this chat have turns nobody has gone over") {
		t.Fatalf("the offer: %+v %+v", p, notes)
	}
	l.h.checkUpkeep(l.ctx)
	if again := systemNotes(); len(again) != 1 {
		t.Errorf("offered again: %+v", again)
	}
}

// Nothing is offered where a person said no, or when the offer is off.
func TestHub_NoOfferWhereSaidNoOrOff(t *testing.T) {
	for _, tc := range []struct {
		name   string
		topics int
		no     bool
	}{{"said no", 1, true}, {"off", -1, false}} {
		t.Run(tc.name, func(t *testing.T) {
			var ahead atomic.Int64
			l := newLoopWith(t, Config{WikiDir: t.TempDir(), UpkeepOfferTopics: tc.topics}, WithClock(func() time.Time { return time.Now().Add(time.Duration(ahead.Load())) }))
			coder := l.member("Coder", nil)
			if tc.no {
				if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{DeclineWikiOffer: true}); err != nil {
					t.Fatal(err)
				}
			}
			l.say("@Coder one", "", coder)
			l.waitTurns(1, store.TurnDone, "the turn")
			ahead.Store(int64(time.Hour))
			l.h.checkUpkeep(l.ctx)
			if p := l.project(); p.WikiOfferMessageID != "" {
				t.Errorf("offered: %+v", p)
			}
		})
	}
}

func TestLoop_AFailedUpkeepLeavesTheTurnsForTheNext(t *testing.T) {
	l, _ := wikiLoop(t)
	coder := l.member("Coder", nil)
	keeper := l.member("Keeper", map[string]any{"fail": true})
	l.say("@Coder go", "", coder)
	l.waitTurns(1, store.TurnDone, "Coder's turn")
	l.keep(keeper, store.UpkeepManual)
	if _, err := l.h.StartUpkeep(l.ctx, l.room.ProjectID); err != nil {
		t.Fatal(err)
	}
	if failed := l.upkeeps(1)[0]; failed.Status != store.TurnFailed {
		t.Fatalf("the upkeep %+v", failed)
	}
	if status, _ := l.h.UpkeepStatus(l.ctx, l.room.ProjectID); status.Waiting.Own != 1 || status.Last.Status != store.TurnFailed {
		t.Errorf("the turn still waits: %+v", status)
	}
}

// An upkeep that failed leaves its turns waiting, but is not tried again
// sooner than the idle time after it started: a maintainer that keeps
// failing does not run every minute.
func TestHub_AFailingUpkeepIsNotRetriedAtOnce(t *testing.T) {
	idle := 400 * time.Millisecond
	l := newLoopWith(t, Config{WikiDir: t.TempDir(), UpkeepIdle: idle})
	coder := l.member("Coder", nil)
	keeper := l.member("Keeper", map[string]any{"fail": true})
	l.say("@Coder go", "", coder)
	l.waitTurns(1, store.TurnDone, "Coder's turn")
	l.keep(keeper, store.UpkeepIdle)
	project := l.project()
	due := func() bool {
		t.Helper()
		_, ok, err := l.h.upkeepDue(l.ctx, project)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	eventually(t, due, "the topic to go quiet")
	l.h.checkUpkeep(l.ctx)
	if failed := l.upkeeps(1)[0]; failed.Status != store.TurnFailed {
		t.Fatalf("the upkeep %+v", failed)
	}
	if due() {
		t.Error("tried again at once")
	}
	time.Sleep(idle + 100*time.Millisecond)
	if !due() {
		t.Error("the turn still waits, and the idle time has passed")
	}
}

// A maintainer changes a skill its team owns at once, on trial, installed
// for it or not; another team's skill is not its to change.
func TestLoop_AMaintainerChangesItsTeamsSkills(t *testing.T) {
	l, _ := wikiLoop(t)
	keeper := l.member("Keeper", nil)
	l.addSkill("check-before-merge", "Use before merging a branch.", "Run the tests first.", l.project().ID)
	l.addSkill("someone-elses", "Use for other things.", "Do the other thing.", "")
	l.keep(keeper, store.UpkeepManual)
	l.setOptions(keeper, map[string]any{"tool_calls": []any{
		call(runtime.WikiToolPatch, map[string]any{
			"scope": "library", "path": "/skills/check-before-merge/SKILL.md", "reason": "the lint too",
			"edits": []any{map[string]any{"op": "append", "content": "Run the linter too."}},
		}),
		call(runtime.WikiToolPatch, map[string]any{
			"scope": "library", "path": "/skills/someone-elses/SKILL.md", "reason": "mine now",
			"edits": []any{map[string]any{"op": "append", "content": "Mine."}},
		}),
	}})
	if _, err := l.h.StartUpkeep(l.ctx, l.room.ProjectID); err != nil {
		t.Fatal(err)
	}
	upkeep := l.upkeeps(1)[0]
	if trial := l.trialOf("check-before-merge", store.TrialOpen); trial.ChangedBy != "Keeper" || trial.TurnID != upkeep.ID {
		t.Errorf("the team's skill, on trial: %+v", trial)
	}
	if reply := l.upkeepReply(upkeep); !strings.Contains(reply, "not installed for you") {
		t.Errorf("another team's skill is not the maintainer's:\n%s", reply)
	}
	if _, err := l.s.OpenSkillTrial(l.ctx, "someone-elses"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unchanged, not on trial: %v", err)
	}
}

func TestHub_AnUpkeepAskedOfAMaintainerSwitchedOff(t *testing.T) {
	l, _ := wikiLoop(t)
	keeper := l.member("Keeper", nil)
	l.keep(keeper, store.UpkeepManual)
	off := false
	if _, err := l.s.UpdateMember(l.ctx, keeper.ID, store.MemberPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	_, err := l.h.StartUpkeep(l.ctx, l.room.ProjectID)
	var p *store.Problem
	if !errors.Is(err, store.ErrConflict) || !errors.As(err, &p) || p.Code != "maintainerOff" || p.Params["name"] != "Keeper" {
		t.Errorf("a maintainer switched off: %v", err)
	}
	if _, err := l.h.StartUpkeep(l.ctx, l.room.ProjectID); err == nil || store.Reason(err) != "the wiki maintainer Keeper is switched off or out of the project: a person chooses another in the project's settings" {
		t.Errorf("told as %q", store.Reason(err))
	}
}
