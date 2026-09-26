package store_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
)

func TestProjects_WikiMaintainer(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	project := f.room.ProjectID
	fresh, _ := f.s.GetProject(ctx, project)
	if fresh.WikiUpkeep || fresh.WikiMaintainerMemberID != "" || fresh.WikiMaintainerTrigger != store.UpkeepDaily {
		t.Fatalf("a new project keeps no wiki, and would do it daily: %+v", fresh)
	}

	// Turned on, the leader keeps it until someone else is chosen.
	on, weekly := true, store.UpkeepWeekly
	got, err := f.s.UpdateProject(ctx, project, store.ProjectPatch{WikiUpkeep: &on, WikiMaintainerTrigger: &weekly})
	if err != nil || !got.WikiUpkeep || got.WikiMaintainerMemberID != "" || got.LeaderID != f.member.ID || got.WikiMaintainerTrigger != store.UpkeepWeekly {
		t.Fatalf("turn upkeep on: %+v %v", got, err)
	}
	if listed, _ := f.s.ListMaintainedProjects(ctx); len(listed) != 1 || listed[0].ID != project || listed[0].MainRoomID != f.room.ID || listed[0].LeaderID != f.member.ID {
		t.Errorf("maintained projects %+v", listed)
	}
	member := f.member.ID
	if got, err := f.s.UpdateProject(ctx, project, store.ProjectPatch{WikiMaintainer: &member}); err != nil || got.WikiMaintainerMemberID != member || !got.WikiUpkeep {
		t.Fatalf("choose the maintainer: %+v %v", got, err)
	}
	// Renaming keeps it all.
	name := "renamed"
	if got, _ := f.s.UpdateProject(ctx, project, store.ProjectPatch{Name: &name}); got.WikiMaintainerMemberID != member || !got.WikiUpkeep {
		t.Errorf("a rename lost the maintainer: %+v", got)
	}

	_, otherRoom, _ := f.s.CreateProject(ctx, store.NewProject{Name: "elsewhere"})
	stranger, _ := f.s.CreateMember(ctx, store.NewMember{RoomID: otherRoom.ID, AgentID: f.agent.ID})
	if _, err := f.s.UpdateProject(ctx, project, store.ProjectPatch{WikiMaintainer: &stranger.ID}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("another project's member: %v", err)
	}
	bad := store.UpkeepTrigger("hourly")
	if _, err := f.s.UpdateProject(ctx, project, store.ProjectPatch{WikiMaintainerTrigger: &bad}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a trigger there is none of: %v", err)
	}

	none := ""
	if got, err := f.s.UpdateProject(ctx, project, store.ProjectPatch{WikiMaintainer: &none}); err != nil || got.WikiMaintainerMemberID != "" || !got.WikiUpkeep || got.WikiMaintainerTrigger != store.UpkeepWeekly {
		t.Errorf("back to the leader: %+v %v", got, err)
	}
	off := false
	if got, err := f.s.UpdateProject(ctx, project, store.ProjectPatch{WikiUpkeep: &off}); err != nil || got.WikiUpkeep {
		t.Errorf("turn upkeep off: %+v %v", got, err)
	}
	if listed, _ := f.s.ListMaintainedProjects(ctx); len(listed) != 0 {
		t.Errorf("a project whose upkeep is off is still kept: %+v", listed)
	}

	// A member taken out of the project keeps its wiki no longer.
	f.s.UpdateProject(ctx, project, store.ProjectPatch{WikiUpkeep: &on, WikiMaintainer: &member})
	if _, err := f.s.RemoveMember(ctx, member); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.s.GetProject(ctx, project); got.WikiMaintainerMemberID != "" || !got.WikiUpkeep || got.LeaderID != "" {
		t.Errorf("a removed member still keeps the wiki, or leads: %+v", got)
	}
	if _, err := f.s.UpdateProject(ctx, project, store.ProjectPatch{WikiMaintainer: &member}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a removed member: %v", err)
	}
}

// A project's wiki upkeep is turned on as it is created, or offered in its
// chat, once, unless a person said no (design.md 5.16).
func TestProjects_WikiMaintainerOffer(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	project := f.room.ProjectID
	offerable := func(id string) bool {
		t.Helper()
		listed, err := f.s.ListUnmaintainedProjects(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range listed {
			if p.ID == id {
				return p.MainRoomID != ""
			}
		}
		return false
	}
	if !offerable(project) {
		t.Fatal("a project whose upkeep is off may be offered it")
	}
	note, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderSystem, Body: "offer"})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := f.s.SetProjectWikiOffer(ctx, project, note.ID); !ok || err != nil {
		t.Fatalf("record the offer: %v %v", ok, err)
	}
	if ok, _ := f.s.SetProjectWikiOffer(ctx, project, note.ID); ok {
		t.Error("offered twice")
	}
	if got, _ := f.s.GetProject(ctx, project); got.WikiOfferMessageID != note.ID || offerable(project) {
		t.Errorf("the offer %+v", got)
	}

	// A person says no: never offered.
	declined, room, _ := f.s.CreateProject(ctx, store.NewProject{Name: "declined"})
	got, err := f.s.UpdateProject(ctx, declined.ID, store.ProjectPatch{DeclineWikiOffer: true})
	if err != nil || got.WikiOfferDeclinedAt == nil {
		t.Fatalf("decline: %+v %v", got, err)
	}
	again, _ := f.s.UpdateProject(ctx, declined.ID, store.ProjectPatch{DeclineWikiOffer: true})
	if again.WikiOfferDeclinedAt == nil || !again.WikiOfferDeclinedAt.Equal(*got.WikiOfferDeclinedAt) {
		t.Errorf("declining again keeps the first time: %v then %v", got.WikiOfferDeclinedAt, again.WikiOfferDeclinedAt)
	}
	other, _ := f.s.CreateMessage(ctx, store.NewMessage{RoomID: room.ID, SenderKind: store.SenderSystem, Body: "offer"})
	if ok, _ := f.s.SetProjectWikiOffer(ctx, declined.ID, other.ID); ok || offerable(declined.ID) {
		t.Error("a declined project was offered a maintainer")
	}

	// Turned on as the project is created: daily unless said otherwise, by
	// the leader unless someone else is chosen.
	led, ledRoom, err := f.s.CreateProject(ctx, store.NewProject{Name: "led", AgentIDs: []string{f.agent.ID}, WikiUpkeep: true})
	if err != nil || !led.WikiUpkeep || led.WikiMaintainerMemberID != "" || led.WikiMaintainerTrigger != store.UpkeepDaily || offerable(led.ID) {
		t.Fatalf("created with upkeep on: %+v %v", led, err)
	}
	if members, _ := f.s.ListRoomMembers(ctx, ledRoom.ID); len(members) != 1 || members[0].ID != led.LeaderID || led.MainRoomID != ledRoom.ID {
		t.Errorf("the leader is the agent's member: %+v %+v", members, led)
	}
	kept, keptRoom, err := f.s.CreateProject(ctx, store.NewProject{Name: "kept", AgentIDs: []string{f.agent.ID}, WikiUpkeep: true, WikiMaintainerAgentID: f.agent.ID})
	if err != nil || kept.WikiMaintainerMemberID == "" || !kept.WikiUpkeep {
		t.Fatalf("created with a maintainer: %+v %v", kept, err)
	}
	if members, _ := f.s.ListRoomMembers(ctx, keptRoom.ID); len(members) != 1 || members[0].ID != kept.WikiMaintainerMemberID {
		t.Errorf("the maintainer is the agent's member: %+v", members)
	}
	weekly, _, err := f.s.CreateProject(ctx, store.NewProject{Name: "weekly", AgentIDs: []string{f.agent.ID}, WikiUpkeep: true, WikiMaintainerTrigger: store.UpkeepWeekly})
	if err != nil || weekly.WikiMaintainerTrigger != store.UpkeepWeekly {
		t.Errorf("created weekly: %+v %v", weekly, err)
	}
	if _, _, err := f.s.CreateProject(ctx, store.NewProject{Name: "nobody", WikiUpkeep: true, WikiMaintainerAgentID: f.agent.ID}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a maintainer the project does not start with: %v", err)
	}
	if _, _, err := f.s.CreateProject(ctx, store.NewProject{Name: "off", AgentIDs: []string{f.agent.ID}, WikiMaintainerAgentID: f.agent.ID}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a maintainer with upkeep off: %v", err)
	}
	if _, _, err := f.s.CreateProject(ctx, store.NewProject{Name: "hourly", WikiMaintainerTrigger: "hourly"}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a trigger there is none of: %v", err)
	}
}

func TestUpkeep_WhatTheMaintainerGoesOver(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	project := f.room.ProjectID
	finish := func(in store.NewTurn, skills ...string) store.Turn {
		t.Helper()
		turn, err := f.s.CreateTurn(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		done, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: store.TurnDone, SkillsUsed: skills})
		if err != nil {
			t.Fatal(err)
		}
		return done
	}
	first := finish(f.newTurn())
	second := finish(f.newTurn(), "go-table-tests")
	running, _ := f.s.CreateTurn(ctx, f.newTurn())
	upkeepTurn := f.newTurn()
	upkeepTurn.Kind = store.TurnUpkeep
	upkeep := finish(upkeepTurn)
	if upkeep.Kind != store.TurnUpkeep || first.Kind != store.TurnChat {
		t.Fatalf("kinds %q %q", upkeep.Kind, first.Kind)
	}

	// Another project, one of whose turns used the team's skill.
	_, otherRoom, _ := f.s.CreateProject(ctx, store.NewProject{Name: "docs site"})
	otherMember, _ := f.s.CreateMember(ctx, store.NewMember{RoomID: otherRoom.ID, AgentID: f.agent.ID, DisplayName: "Other"})
	root, _ := f.s.CreateMessage(ctx, store.NewMessage{RoomID: otherRoom.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "@Other go"})
	otherThread, _ := f.s.ThreadForMessage(ctx, root.ID)
	elsewhere := store.NewTurn{MemberID: otherMember.ID, RoomID: otherRoom.ID, ThreadID: otherThread.ID, MachineID: f.machineID, Runtime: "fake"}
	use := finish(elsewhere, "go-table-tests")
	finish(elsewhere, "release-notes")
	owned := []string{"go-table-tests"}

	ids := func(turns []store.UpkeepTurn) []string {
		var out []string
		for _, turn := range turns {
			out = append(out, turn.ID)
		}
		return out
	}
	own, err := f.s.ListUpkeepTurns(ctx, store.UpkeepQuery{ProjectID: project, Unreviewed: true, OldestFirst: true, Limit: 10})
	if err != nil || !slices.Equal(ids(own), []string{first.ID, second.ID}) {
		t.Fatalf("the project's own, oldest first, neither running nor the maintainer's: %v %v", ids(own), err)
	}
	if o := own[1]; o.ThreadID != f.thread.ID || o.TopicNumber != f.thread.Number || o.RootBody != "@agent go" || o.MemberName != f.member.DisplayName ||
		o.Status != store.TurnDone || !slices.Equal(o.SkillsUsed, owned) || o.ProjectID != project || o.Reviewed || o.EndedAt == nil {
		t.Errorf("a turn as the maintainer sees it: %+v", o)
	}
	uses, err := f.s.ListUpkeepTurns(ctx, store.UpkeepQuery{ProjectID: project, Skills: true, Owned: owned, Unreviewed: true, Limit: 10})
	if err != nil || !slices.Equal(ids(uses), []string{use.ID}) || uses[0].ProjectName != "docs site" || uses[0].MemberName != "Other" {
		t.Fatalf("other projects' turns that used the team's skills: %+v %v", uses, err)
	}

	later, earlier := time.Now().Add(time.Minute), time.Now().Add(-time.Hour)
	waiting, err := f.s.CountUpkeepWaiting(ctx, project, owned, later)
	// The turn still running keeps its topic from being settled.
	if err != nil || waiting != (store.UpkeepWaiting{Own: 2, Uses: 1, Settled: 1, People: 1, PeopleSettled: 1}) || waiting.Total() != 3 {
		t.Fatalf("waiting %+v %v", waiting, err)
	}
	f.s.FinishTurn(ctx, running.ID, store.TurnOutcome{Status: store.TurnFailed, Error: "boom"})
	if waiting, _ := f.s.CountUpkeepWaiting(ctx, project, owned, later); waiting != (store.UpkeepWaiting{Own: 3, Uses: 1, Settled: 4, People: 1, PeopleSettled: 1}) {
		t.Errorf("once nothing runs, every topic is quiet by then: %+v", waiting)
	}
	if waiting, _ := f.s.CountUpkeepWaiting(ctx, project, owned, earlier); waiting.Settled != 0 {
		t.Errorf("nothing had ended an hour ago: %+v", waiting)
	}
	// A word in the topic unsettles it again: quiet since the other
	// project's turn ended, only that one's topic is.
	f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "one more thing"})
	if waiting, _ := f.s.CountUpkeepWaiting(ctx, project, owned, *use.EndedAt); waiting.Settled != 1 {
		t.Errorf("only the other project's topic is quiet: %+v", waiting)
	}
	settledBy := *use.EndedAt
	if quiet, _ := f.s.ListUpkeepTurns(ctx, store.UpkeepQuery{ProjectID: project, Unreviewed: true, SettledBy: &settledBy, Limit: 10}); len(quiet) != 0 {
		t.Errorf("none of the project's own topics is quiet: %v", ids(quiet))
	}
	if quiet, _ := f.s.ListUpkeepTurns(ctx, store.UpkeepQuery{ProjectID: project, Skills: true, Owned: owned, Unreviewed: true, SettledBy: &settledBy, Limit: 10}); !slices.Equal(ids(quiet), []string{use.ID}) {
		t.Errorf("the other project's quiet topic: %v", ids(quiet))
	}

	if err := f.s.RecordWikiReviews(ctx, project, upkeep.ID, []string{first.ID, use.ID}); err != nil {
		t.Fatal(err)
	}
	// Recording again changes nothing.
	if err := f.s.RecordWikiReviews(ctx, project, upkeep.ID, []string{first.ID}); err != nil {
		t.Fatal(err)
	}
	if own, _ := f.s.ListUpkeepTurns(ctx, store.UpkeepQuery{ProjectID: project, Unreviewed: true, OldestFirst: true, Limit: 10}); !slices.Equal(ids(own), []string{second.ID, running.ID}) {
		t.Errorf("left to go over %v", ids(own))
	}
	all, _ := f.s.ListUpkeepTurns(ctx, store.UpkeepQuery{ProjectID: project, Limit: 10})
	if !slices.Equal(ids(all), []string{running.ID, second.ID, first.ID}) || !all[2].Reviewed || all[1].Reviewed {
		t.Errorf("newest first, marked: %+v", all)
	}
	if older, _ := f.s.ListUpkeepTurns(ctx, store.UpkeepQuery{ProjectID: project, Before: &second.StartedAt, Limit: 10}); !slices.Equal(ids(older), []string{first.ID}) {
		t.Errorf("before the second: %v", ids(older))
	}
	if uses, _ := f.s.ListUpkeepTurns(ctx, store.UpkeepQuery{ProjectID: project, Skills: true, Owned: owned, Unreviewed: true, Limit: 10}); len(uses) != 0 {
		t.Errorf("the use was gone over: %+v", uses)
	}

	// One topic only.
	another, _ := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "@agent again"})
	anotherThread, _ := f.s.ThreadForMessage(ctx, another.ID)
	there := f.newTurn()
	there.ThreadID = anotherThread.ID
	inThere := finish(there)
	if topic, _ := f.s.ListUpkeepTurns(ctx, store.UpkeepQuery{ProjectID: project, Topic: anotherThread.Number, Limit: 10}); !slices.Equal(ids(topic), []string{inThere.ID}) {
		t.Errorf("topic #%d: %v", anotherThread.Number, ids(topic))
	}

	if last, err := f.s.LastUpkeep(ctx, project); err != nil || last.ID != upkeep.ID {
		t.Errorf("last upkeep %+v %v", last, err)
	}
	if _, err := f.s.LastUpkeep(ctx, otherRoom.ProjectID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a project whose maintainer never ran: %v", err)
	}
}

func TestUpkeep_NextPersonMessage(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	turn, _ := f.s.CreateTurn(ctx, f.newTurn())
	done, _ := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: store.TurnDone})
	if _, err := f.s.NextPersonMessage(ctx, f.thread.ID, *done.EndedAt); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("nobody spoke yet: %v", err)
	}
	f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "anything else?"})
	said, _ := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "no, that was wrong"})
	f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: f.thread.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "and later"})
	if got, err := f.s.NextPersonMessage(ctx, f.thread.ID, *done.EndedAt); err != nil || got.ID != said.ID {
		t.Errorf("next word %+v %v", got, err)
	}
}

func TestUpkeep_WhatPeopleSaid(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	project := f.room.ProjectID
	seen := f.post(t, "gone over at the last upkeep", "")
	said := f.post(t, "releases go out on Fridays", "")
	root := f.post(t, "@agent the checklist", "")
	thread, err := f.s.ThreadForMessage(ctx, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := store.NewID()
	upload, err := f.s.CreateAttachment(ctx, store.NewAttachment{ID: id, RoomID: f.room.ID, Filename: "checklist.md", MediaType: "text/markdown", Size: 9, Path: f.room.ID + "/" + id + ".md"})
	if err != nil {
		t.Fatal(err)
	}
	withFile, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: f.room.ID, ThreadID: thread.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "here it is", AttachmentIDs: []string{upload.ID}})
	if err != nil {
		t.Fatal(err)
	}
	// Another room of the project is not the chat the maintainer reads,
	// nor keeps files from.
	side, err := f.s.CreateRoom(ctx, project, "side")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateMessage(ctx, store.NewMessage{RoomID: side.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "elsewhere"}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetProjectWikiSeen(ctx, project, seen.Seq); err != nil {
		t.Fatal(err)
	}
	upto, err := f.s.RoomPosition(ctx, f.room.ID)
	if err != nil || upto != withFile.Seq {
		t.Fatalf("the room stands at %d, want %d: %v", upto, withFile.Seq, err)
	}

	later := time.Now().Add(time.Minute)
	if waiting, err := f.s.CountUpkeepWaiting(ctx, project, nil, later); err != nil || waiting.People != 3 || waiting.PeopleSettled != 3 {
		t.Fatalf("three messages since the position, all quiet by then: %+v %v", waiting, err)
	}
	news, err := f.s.ListPeopleNews(ctx, project, seen.Seq, upto, 10)
	if err != nil || len(news) != 2 {
		t.Fatalf("the room itself and one topic: %+v %v", news, err)
	}
	// A topic's first message is said in the room itself.
	if n := news[0]; n.Topic != 0 || n.ThreadID != "" || n.Messages != 2 || n.Files != 0 {
		t.Errorf("the room itself: %+v", n)
	}
	if n := news[1]; n.Topic != thread.Number || n.ThreadID != thread.ID || n.RootBody != root.Body || n.Messages != 1 || n.Files != 1 {
		t.Errorf("the topic: %+v", n)
	}
	if news, _ := f.s.ListPeopleNews(ctx, project, seen.Seq, said.Seq, 10); len(news) != 1 || news[0].Messages != 1 {
		t.Errorf("up to a position, no further: %+v", news)
	}
	files, err := f.s.ListPeopleFiles(ctx, project, seen.Seq, upto, 10)
	if err != nil || len(files) != 1 {
		t.Fatalf("one file: %+v %v", files, err)
	}
	if got := files[0]; got.ID != upload.ID || got.MessageID != withFile.ID || got.Topic != thread.Number || got.Sender != f.user.Name || got.Filename != "checklist.md" {
		t.Errorf("the file, where and by whom: %+v", got)
	}

	// The position moves forward only; the side room never counted.
	if err := f.s.SetProjectWikiSeen(ctx, project, upto); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetProjectWikiSeen(ctx, project, seen.Seq); err != nil {
		t.Fatal(err)
	}
	if p, err := f.s.GetProject(ctx, project); err != nil || p.WikiSeenSeq != upto {
		t.Errorf("the position is %d, want %d: %v", p.WikiSeenSeq, upto, err)
	}
	if waiting, _ := f.s.CountUpkeepWaiting(ctx, project, nil, later); waiting.People != 0 {
		t.Errorf("nothing left once gone over: %+v", waiting)
	}
}

func TestUpkeep_ListChangedFiles(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()
	finish := func(files ...string) store.Turn {
		t.Helper()
		turn, err := f.s.CreateTurn(ctx, f.newTurn())
		if err != nil {
			t.Fatal(err)
		}
		done, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: store.TurnDone, FilesChanged: files})
		if err != nil {
			t.Fatal(err)
		}
		return done
	}
	first := finish("internal/hub/brief.go", "go.mod")
	second := finish("/repo/internal/hub/brief.go", "internal/hub/brief.go")
	project := f.room.ProjectID

	files, err := f.s.ListChangedFiles(ctx, project, first.StartedAt.Add(-time.Minute), 10)
	if err != nil {
		t.Fatal(err)
	}
	// Each file once, by path, with the last turn to start that changed it.
	got := map[string]string{}
	for _, file := range files {
		got[file.Path] = file.TurnID
		if file.ThreadID != f.thread.ID || file.TopicNumber != f.thread.Number || file.At.IsZero() {
			t.Errorf("where and when %+v", file)
		}
	}
	want := map[string]string{"internal/hub/brief.go": second.ID, "go.mod": first.ID, "/repo/internal/hub/brief.go": second.ID}
	if len(files) != 3 || fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if later, _ := f.s.ListChangedFiles(ctx, project, first.StartedAt, 10); len(later) != 2 {
		t.Errorf("only what turns that started after the first changed: %+v", later)
	}
	if capped, _ := f.s.ListChangedFiles(ctx, project, first.StartedAt.Add(-time.Minute), 1); len(capped) != 1 {
		t.Errorf("up to the limit: %+v", capped)
	}
}
