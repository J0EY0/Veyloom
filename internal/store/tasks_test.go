package store_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// A piece of work the way the third trial ran one: a person asks the lead,
// which hands parts on one after another and sums up; the parts' branches
// are merged in one commit. Beside it, one message asking two members at
// once, and a member that wrote the wiki.
var (
	at0     = time.Date(2026, 9, 25, 20, 41, 0, 0, time.UTC)
	lead    = store.Member{ID: "m-lead", DisplayName: "Lead"}
	coder   = store.Member{ID: "m-coder", DisplayName: "Coder", WorktreeDir: "/w/coder"}
	tester  = store.Member{ID: "m-tester", DisplayName: "Tester", WorktreeDir: "/w/tester"}
	writer  = store.Member{ID: "m-writer", DisplayName: "Writer", WorktreeDir: "/w/writer"}
	members = []store.Member{lead, coder, tester, writer}
)

func at(seconds int) time.Time { return at0.Add(time.Duration(seconds) * time.Second) }

func ended(seconds int) *time.Time {
	t := at(seconds)
	return &t
}

// turn is a finished turn of a piece of work.
func turn(id, chain, thread string, number int, member store.Member, start, end int, extra func(*store.TaskTurn)) store.TaskTurn {
	t := store.TaskTurn{
		Turn: store.Turn{
			ID: id, ChainMessageID: chain, ThreadID: thread, MemberID: member.ID, Status: store.TurnDone,
			StartedAt: at(start), EndedAt: ended(end), Usage: runtime.Usage{InputTokens: 100, OutputTokens: 10},
		},
		ThreadNumber: number, TriggerKind: string(store.SenderAgent),
	}
	if extra != nil {
		extra(&t)
	}
	return t
}

func trialTurns() []store.TaskTurn {
	return []store.TaskTurn{
		turn("l1", "c1", "t1", 1, lead, 0, 40, func(t *store.TaskTurn) {
			t.TriggerKind, t.TriggerBody = string(store.SenderUser), "@Lead 给 linkkeeper 加标签功能：add 可以带 --tag。请安排 Coder 实现。"
			t.Worked = true
		}),
		turn("c1", "c1", "t2", 2, coder, 41, 130, func(t *store.TaskTurn) {
			t.WokenByTurnID, t.TriggerTitle, t.TriggerBody = "l1", "实现标签功能", "@Coder 请实现标签功能，细节如下……"
			t.FilesChanged = []string{"tags.go"}
		}),
		turn("l2", "c1", "t1", 1, lead, 131, 140, func(t *store.TaskTurn) {
			t.TriggerBody = "@Lead 做完了。"
		}),
		turn("s1", "c1", "t3", 3, tester, 141, 200, func(t *store.TaskTurn) {
			t.WokenByTurnID, t.TriggerBody = "l2", "@Tester 补标签功能的测试。覆盖大小写。"
			t.FilesChanged = []string{"tags_test.go"}
		}),
		turn("l3", "c1", "t1", 1, lead, 201, 230, func(t *store.TaskTurn) {
			t.TriggerKind, t.TriggerBody = string(store.SenderSystem), "The work Lead handed on is done (Coder, Tester); back to Lead."
		}),
		// One message asking two members, each its own part.
		turn("c2", "c2", "t4", 4, coder, 300, 360, func(t *store.TaskTurn) {
			t.TriggerKind, t.TriggerBody = string(store.SenderUser), "@Coder 给 list 加一个 --json 参数 @Tester 把 README 里的例子跑一遍"
			t.FilesChanged = []string{"list.go"}
		}),
		turn("s2", "c2", "t4", 4, tester, 300, 0, func(t *store.TaskTurn) {
			t.TriggerKind, t.TriggerBody = string(store.SenderUser), "@Coder 给 list 加一个 --json 参数 @Tester 把 README 里的例子跑一遍"
			t.Status, t.EndedAt, t.Waiting = store.TurnRunning, nil, true
		}),
		turn("w1", "c3", "t5", 5, writer, 400, 430, func(t *store.TaskTurn) {
			t.TriggerKind, t.TriggerBody = string(store.SenderUser), "@Writer 把标签的规则记进 wiki。"
			t.WikiPages = []string{"/conventions/tag-matching.md"}
		}),
	}
}

func trialEvents() []store.BranchEvent {
	return []store.BranchEvent{
		{MemberID: tester.ID, Kind: store.BranchMerged, Commit: "66930b5", At: at(250)},
		{MemberID: coder.ID, Kind: store.BranchMerged, Commit: "66930b5", ViaMemberID: tester.ID, At: at(250)},
	}
}

func TestBuildTasks(t *testing.T) {
	tasks := store.BuildTasks(trialTurns(), members, trialEvents())
	type card struct {
		member, title string
		state         store.TaskState
		waiting       bool
		outcome       string
		work          string
		parts         string
	}
	var got []card
	for _, task := range tasks {
		c := card{member: task.MemberID, title: task.Title, state: task.State, waiting: task.Waiting}
		if task.Outcome != nil {
			c.outcome = task.Outcome.Kind + ":" + task.Outcome.Commit + task.Outcome.Page
		}
		if task.Work != nil {
			c.work = task.Work.Title
		}
		if task.Parts != nil {
			c.parts = string(rune('0'+task.Parts.Done)) + "/" + string(rune('0'+task.Parts.Total))
		}
		got = append(got, c)
	}
	want := []card{
		{member: lead.ID, title: "给 linkkeeper 加标签功能", state: store.TaskDone, outcome: "merged:66930b5", parts: "2/2"},
		{member: coder.ID, title: "实现标签功能", state: store.TaskDone, outcome: "merged:66930b5", work: "给 linkkeeper 加标签功能"},
		{member: tester.ID, title: "补标签功能的测试", state: store.TaskDone, outcome: "merged:66930b5", work: "给 linkkeeper 加标签功能"},
		{member: coder.ID, title: "给 list 加一个 --json 参数", state: store.TaskToMerge},
		{member: tester.ID, title: "把 README 里的例子跑一遍", state: store.TaskRunning, waiting: true},
		{member: writer.ID, title: "把标签的规则记进 wiki", state: store.TaskDone, outcome: "wiki:/conventions/tag-matching.md"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tasks:\n got %+v\nwant %+v", got, want)
	}
	if tasks[0].Turns != 3 || tasks[0].EndedAt == nil || !tasks[0].EndedAt.Equal(at(230)) || tasks[4].EndedAt != nil {
		t.Errorf("the lead's task: %+v; the running one: %+v", tasks[0], tasks[4])
	}
}

// A piece of work is under way while any of its parts is; a part reset
// is archived; a member without a worktree has nothing to merge.
func TestBuildTasksStates(t *testing.T) {
	turns := trialTurns()[:4]
	turns[3].Status, turns[3].EndedAt = store.TurnRunning, nil
	tasks := store.BuildTasks(turns, members, nil)
	if tasks[0].State != store.TaskRunning || tasks[0].EndedAt != nil || tasks[0].Parts.Done != 1 || tasks[0].Parts.Total != 2 {
		t.Errorf("the lead's task while a part runs: %+v (parts %+v)", tasks[0], tasks[0].Parts)
	}
	if tasks[1].State != store.TaskToMerge {
		t.Errorf("the coder's part, not merged: %+v", tasks[1])
	}

	reset := []store.BranchEvent{{MemberID: coder.ID, Kind: store.BranchReset, Ref: "refs/veyloom/set-aside/veyloom/coder/1", At: at(135)}}
	tasks = store.BuildTasks(trialTurns()[:2], members, reset)
	if o := tasks[1].Outcome; tasks[1].State != store.TaskDone || o == nil || o.Kind != store.OutcomeArchived || o.Ref != reset[0].Ref {
		t.Errorf("the coder's part set aside: %+v (%+v)", tasks[1], o)
	}
	// What was merged before the change is no answer to it.
	early := []store.BranchEvent{{MemberID: coder.ID, Kind: store.BranchMerged, Commit: "old", At: at(100)}}
	if tasks = store.BuildTasks(trialTurns()[:2], members, early); tasks[1].State != store.TaskToMerge {
		t.Errorf("merged before it ended: %+v", tasks[1])
	}
	solo := []store.Member{{ID: coder.ID, DisplayName: "Coder"}, lead}
	if tasks = store.BuildTasks(trialTurns()[:2], solo, nil); tasks[1].State != store.TaskDone {
		t.Errorf("a member in the checkout itself: %+v", tasks[1])
	}
}

// A task is named by the part of a message addressed to its member, even
// where another member's name begins with its own.
func TestTaskTitles(t *testing.T) {
	code := store.Member{ID: "m-code", DisplayName: "Code"}
	people := []store.Member{coder, code}
	for body, want := range map[string]string{
		"@Coder 实现它，@Code 看一下": "实现它",
		"@Code 看一下 @Coder 实现它": "实现它",
		"@Coder @Code 一起看看这个问题": "一起看看这个问题",
		"请 @Coder 修一下 README 的错字。然后提交": "修一下 README 的错字",
	} {
		turn := turn("x", "c", "t", 1, coder, 0, 1, func(t *store.TaskTurn) { t.TriggerKind, t.TriggerBody = string(store.SenderUser), body })
		if got := store.BuildTasks([]store.TaskTurn{turn}, people, nil)[0].Title; got != want {
			t.Errorf("%q: title %q, want %q", body, got, want)
		}
	}
}

func TestBuildWork(t *testing.T) {
	turns := trialTurns()[:5]
	turns[1].Status, turns[1].Worked = store.TurnDone, true
	waits := []store.ChainWait{
		{TurnID: "c1", CreatedAt: at(50), DecidedAt: ended(70)},
		{TurnID: "c1", CreatedAt: at(60), DecidedAt: ended(80)},
		{TurnID: "s1", CreatedAt: at(150), DecidedAt: ended(155)},
	}
	ask := store.Message{ID: "c1", Room: "r1", UserID: "u1", Body: "@Lead 给 linkkeeper 加标签功能：add 可以带 --tag。请安排 Coder 实现。"}
	work := store.BuildWork(ask, turns, waits, members, trialEvents(), at(1000))

	if work.Title != "给 linkkeeper 加标签功能" || work.Ask != "给 linkkeeper 加标签功能：add 可以带 --tag。请安排 Coder 实现。" || work.AskedBy != "u1" {
		t.Errorf("the ask: %q / %q by %q", work.Title, work.Ask, work.AskedBy)
	}
	if work.ThreadNumber != 1 || !reflect.DeepEqual(work.Asked, []string{lead.ID}) || work.Running || work.EndedAt == nil || !work.EndedAt.Equal(at(230)) {
		t.Errorf("the work: %+v", work)
	}
	type row struct {
		kind, title string
		woke        []string
		relay       bool
		waited      int64
	}
	var got []row
	for _, turn := range work.Turns {
		got = append(got, row{turn.Kind, turn.Title, turn.Woke, turn.Relay, turn.WaitedMS})
	}
	want := []row{
		{store.WorkTurnSplit, "", []string{coder.ID}, false, 0},
		{store.WorkTurnTask, "实现标签功能", nil, false, 30_000},
		{store.WorkTurnHandOff, "", []string{tester.ID}, true, 0},
		{store.WorkTurnTask, "补标签功能的测试", nil, false, 5_000},
		{store.WorkTurnSumUp, "", nil, false, 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("turns:\n got %+v\nwant %+v", got, want)
	}
	if len(work.Turns[1].Waits) != 1 || !work.Turns[1].Waits[0].From.Equal(at(50)) || !work.Turns[1].Waits[0].To.Equal(at(80)) {
		t.Errorf("overlapping waits are one stretch: %+v", work.Turns[1].Waits)
	}
	if work.WaitedMS != 35_000 || work.Usage.Total() != 5*110 {
		t.Errorf("waited %d ms, spent %d", work.WaitedMS, work.Usage.Total())
	}
	if len(work.Events) != 1 || work.Events[0].Commit != "66930b5" || !reflect.DeepEqual(work.Events[0].Members, []string{coder.ID, tester.ID}) {
		t.Errorf("events: %+v", work.Events)
	}

	// A request still open waits until now.
	open := []store.ChainWait{{TurnID: "c1", CreatedAt: at(900)}}
	turns[1].Status, turns[1].EndedAt = store.TurnRunning, nil
	work = store.BuildWork(ask, turns, open, members, nil, at(1000))
	if !work.Running || work.EndedAt != nil || work.Turns[1].WaitedMS != 100_000 || work.Turns[1].Waits[0].To != nil {
		t.Errorf("under way: running %v, waited %d, waits %+v", work.Running, work.Turns[1].WaitedMS, work.Turns[1].Waits)
	}
}

func TestSummarizeUsage(t *testing.T) {
	shanghai := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 26, 15, 0, 0, 0, shanghai)
	day := func(d, h int) time.Time { return time.Date(2026, 9, 26+d, h, 0, 0, 0, shanghai) }
	use := func(in, out int64) runtime.Usage { return runtime.Usage{InputTokens: in, CacheReadTokens: in * 3, OutputTokens: out} }
	end := func(t time.Time, minutes int) *time.Time { e := t.Add(time.Duration(minutes) * time.Minute); return &e }
	turns := []store.UsageTurn{
		{ID: "a", MemberID: "m1", Member: "Lead", RoomID: "r1", ThreadNumber: 1, Runtime: "claude", Kind: store.TurnChat, Chain: "c1",
			StartedAt: day(-3, 9), EndedAt: end(day(-3, 9), 2), Usage: use(100, 10), ProjectName: "Tags", ChainBody: "@Lead 给标签加个校验：长度不超过 20"},
		{ID: "b", MemberID: "m2", Member: "Coder", RoomID: "r1", ThreadNumber: 2, Runtime: "pi", Kind: store.TurnChat, Chain: "c1",
			StartedAt: day(0, 10), EndedAt: end(day(0, 10), 5), Usage: use(1000, 50), ProjectName: "Tags"},
		{ID: "c", MemberID: "m1", Member: "Lead", RoomID: "r1", ThreadNumber: 3, Runtime: "claude", Kind: store.TurnSetup,
			StartedAt: day(0, 11), EndedAt: end(day(0, 11), 1), Usage: use(10, 1), ProjectName: "Tags"},
		{ID: "d", MemberID: "m1", Member: "Lead", RoomID: "r1", ThreadNumber: 1, Runtime: "claude", Kind: store.TurnChat, Chain: "c2",
			StartedAt: day(0, 14), Usage: use(50, 5), ProjectName: "Tags", ChainBody: "@Lead search 也能按标签搜吗？"},
	}

	week := store.SummarizeUsage(turns, store.UsageQuery{Range: store.UsageWeek, Location: shanghai, Now: now})
	if week.Step != store.UsageStepDay || len(week.Points) != 7 || week.Turns != 4 {
		t.Fatalf("week: step %s, %d points, %d turns", week.Step, len(week.Points), week.Turns)
	}
	if p := week.Points[3]; p.Turns != 1 || p.Tokens != 410 || !p.At.Equal(day(-3, 0)) {
		t.Errorf("three days ago: %+v", p)
	}
	if p := week.Points[6]; p.Turns != 3 || p.Tokens != 4050+41+205 || p.DurationMS != (5+1+60)*60_000 {
		t.Errorf("today: %+v", p)
	}
	if !reflect.DeepEqual(week.Runtimes, []store.UsageRuntime{{Runtime: "pi", Tokens: 4050, Turns: 1}, {Runtime: "claude", Tokens: 410 + 41 + 205, Turns: 3}}) {
		t.Errorf("runtimes: %+v", week.Runtimes)
	}
	if len(week.Members) != 2 || week.Members[0].Name != "Coder" || week.Members[1].Turns != 3 {
		t.Errorf("members: %+v", week.Members)
	}
	if len(week.Works) != 3 || week.Works[0].Chain != "c1" || week.Works[0].Tokens != 4050+410 || week.Works[0].Title != "给标签加个校验" || week.Works[0].ThreadNumber != 1 {
		t.Errorf("works: %+v", week.Works)
	}
	if w := week.Works[1]; w.Chain != "c2" || w.Title != "search 也能按标签搜吗" {
		t.Errorf("the second work: %+v", w)
	}
	if w := week.Works[2]; w.Kind != store.TurnSetup || w.Chain != "" || w.Tokens != 41 {
		t.Errorf("the setup: %+v", w)
	}

	today := store.SummarizeUsage(turns, store.UsageQuery{Range: store.UsageToday, Location: shanghai, Now: now})
	if today.Step != store.UsageStepTurn || len(today.Points) != 3 || today.Turns != 3 {
		t.Fatalf("today: step %s, %d points, %d turns", today.Step, len(today.Points), today.Turns)
	}
	if p := today.Points[0]; p.TurnID != "b" || p.Member != "Coder" || p.ThreadNumber != 2 || p.Tokens != 4050 || p.DurationMS != 5*60_000 {
		t.Errorf("the first turn today: %+v", p)
	}
	if p := today.Points[2]; p.DurationMS != 60*60_000 {
		t.Errorf("a turn under way counts up to now: %+v", p)
	}
}
