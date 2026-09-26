package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// The task board, a piece of work's page and the usage page read from the
// database: a person asks a member, which hands a part on to another
// member working in a worktree; that part asks a person once, writes the
// wiki and is merged.
func TestWork_TasksPageAndUsage(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	coder, err := f.s.CreateMember(ctx, store.NewMember{RoomID: f.room.ID, AgentID: f.agent.ID, DisplayName: "Coder"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SetMemberWorkspace(ctx, coder.ID, "/w/coder", "/w/coder", "veyloom/coder"); err != nil {
		t.Fatal(err)
	}

	ask, err := f.s.CreateMessage(ctx, store.NewMessage{
		RoomID: f.room.ID, SenderKind: store.SenderUser, UserID: f.user.ID, Body: "@Reviewer 给标签加个校验：长度不超过 20",
		Mentions: []store.Mention{{Kind: store.MentionAgent, ID: f.member.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	askThread, err := f.s.ThreadForMessage(ctx, ask.ID)
	if err != nil {
		t.Fatal(err)
	}
	asked, err := f.s.CreateTurn(ctx, store.NewTurn{
		MemberID: f.member.ID, RoomID: f.room.ID, ThreadID: askThread.ID, TriggerMessageID: ask.ID, MachineID: f.machineID, Runtime: "claude", ChainMessageID: ask.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	handOff, err := f.s.CreateMessage(ctx, store.NewMessage{
		RoomID: f.room.ID, SenderKind: store.SenderAgent, MemberID: f.member.ID, Body: "@Coder 请实现标签功能，细节如下……",
		Title: " 实现标签功能 ", Mentions: []store.Mention{{Kind: store.MentionAgent, ID: coder.ID}}, TurnID: asked.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := f.s.GetMessage(ctx, handOff.ID); got.Title != "实现标签功能" {
		t.Errorf("the hand-off's title: %q", got.Title)
	}
	partThread, err := f.s.ThreadForMessage(ctx, handOff.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.FinishTurn(ctx, asked.ID, store.TurnOutcome{Status: store.TurnDone, Usage: runtime.Usage{InputTokens: 100, OutputTokens: 10}}); err != nil {
		t.Fatal(err)
	}

	part, err := f.s.CreateTurn(ctx, store.NewTurn{
		MemberID: coder.ID, RoomID: f.room.ID, ThreadID: partThread.ID, TriggerMessageID: handOff.ID, MachineID: f.machineID, Runtime: "pi",
		ChainMessageID: ask.ID, WokenByTurnID: asked.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := f.s.CreateApproval(ctx, store.NewApproval{TurnID: part.ID, RoomID: f.room.ID, ThreadID: partThread.ID, MemberID: coder.ID, RequestID: "r1", Tool: "Bash", Input: `{"command":"go test"}`})
	if err != nil {
		t.Fatal(err)
	}
	// While the request waits, the part and the work it is part of are
	// under way, and the part waits for a person.
	tasks, err := f.s.ListRoomTasks(ctx, f.room.ID)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks: %+v, %v", tasks, err)
	}
	if tasks[0].State != store.TaskRunning || tasks[1].State != store.TaskRunning || !tasks[1].Waiting || tasks[0].Waiting {
		t.Errorf("under way: %+v / %+v", tasks[0], tasks[1])
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := f.s.DecideApproval(ctx, request.ID, store.ApprovalOutcome{Status: store.ApprovalAllowed, DecidedBy: f.user.ID}); err != nil {
		t.Fatal(err)
	}
	finished, err := f.s.FinishTurn(ctx, part.ID, store.TurnOutcome{
		Status: store.TurnDone, Usage: runtime.Usage{InputTokens: 1000, CacheReadTokens: 3000, OutputTokens: 50},
		FilesChanged: []string{"tags.go"}, WikiPages: []string{"/conventions/tags.md"}, Worked: true,
	})
	if err != nil || len(finished.WikiPages) != 1 || finished.WikiPages[0] != "/conventions/tags.md" {
		t.Fatalf("finished: %+v, %v", finished, err)
	}

	tasks, _ = f.s.ListRoomTasks(ctx, f.room.ID)
	if tasks[1].State != store.TaskToMerge || tasks[1].Title != "实现标签功能" || tasks[1].Work == nil || tasks[1].Work.Title != "给标签加个校验" || tasks[1].ThreadNumber != partThread.Number {
		t.Errorf("waiting to be merged: %+v", tasks[1])
	}
	if _, err := f.s.RecordBranchEvent(ctx, store.BranchEvent{MemberID: coder.ID, Kind: store.BranchMerged, Commit: "66930b5"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RecordBranchEvent(ctx, store.BranchEvent{MemberID: coder.ID, Kind: "sideways"}); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("an unknown kind: %v", err)
	}
	tasks, _ = f.s.ListRoomTasks(ctx, f.room.ID)
	if o := tasks[1].Outcome; tasks[1].State != store.TaskDone || o == nil || o.Kind != store.OutcomeMerged || o.Commit != "66930b5" {
		t.Errorf("merged: %+v (%+v)", tasks[1], o)
	}
	if p := tasks[0].Parts; tasks[0].State != store.TaskDone || p == nil || p.Done != 1 || p.Total != 1 || tasks[0].Outcome == nil || tasks[0].Outcome.Commit != "66930b5" {
		t.Errorf("the work asked for: %+v (%+v)", tasks[0], p)
	}

	work, err := f.s.GetWork(ctx, ask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if work.Title != "给标签加个校验" || work.Ask != "给标签加个校验：长度不超过 20" || work.AskedBy != f.user.ID || len(work.Turns) != 2 || work.Turns[0].Kind != store.WorkTurnSplit || work.Turns[1].Kind != store.WorkTurnTask {
		t.Errorf("work: %+v", work)
	}
	if work.Turns[1].WaitedMS < 20 || len(work.Turns[1].Waits) != 1 || len(work.Events) != 1 || work.Events[0].Commit != "66930b5" {
		t.Errorf("waits %+v, events %+v", work.Turns[1].Waits, work.Events)
	}
	if _, err := f.s.GetWork(ctx, handOff.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a message that began no work: %v", err)
	}

	usage, err := f.s.Usage(ctx, store.UsageQuery{Range: store.UsageToday, Now: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if usage.Turns != 2 || usage.Total.Total() != 4160 || len(usage.Points) != 2 || len(usage.Works) != 1 || usage.Works[0].Chain != ask.ID || usage.Works[0].Title != "给标签加个校验" {
		t.Errorf("usage: %+v", usage)
	}
	if len(usage.Members) != 2 || usage.Members[0].Name != "Coder" || usage.Members[0].Model != "claude-opus-5" || usage.Members[0].ProjectName != "p" {
		t.Errorf("members: %+v", usage.Members)
	}
	other, _, err := f.s.CreateProject(ctx, store.NewProject{Name: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if usage, _ := f.s.Usage(ctx, store.UsageQuery{Range: store.UsageToday, ProjectID: other.ID, Now: time.Now()}); usage.Turns != 0 {
		t.Errorf("another project's: %+v", usage)
	}
}
