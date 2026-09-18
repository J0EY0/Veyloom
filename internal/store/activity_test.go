package store_test

import (
	"context"
	"strings"
	"testing"
	"time"
	// Zones by name without depending on the host's zone files.
	_ "time/tzdata"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

func TestListMachineMembers(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	// A second project with a member on the same machine, one taken out,
	// and one on another machine.
	_, docs, err := f.s.CreateProject(ctx, store.NewProject{Name: "docs"})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := f.s.CreateMember(ctx, store.NewMember{RoomID: docs.ID, AgentID: f.agent.ID, DisplayName: "Writer"})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := f.s.CreateMember(ctx, store.NewMember{RoomID: docs.ID, AgentID: f.agent.ID, DisplayName: "Gone"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RemoveMember(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	elsewhere, err := f.s.RegisterMachine(ctx, "", "build-box", nil)
	if err != nil {
		t.Fatal(err)
	}
	remote, err := f.s.CreateAgent(ctx, store.NewAgent{Name: "Remote", MachineID: elsewhere, Runtime: "pi", PermissionPreset: store.PermissionReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateMember(ctx, store.NewMember{RoomID: docs.ID, AgentID: remote.ID}); err != nil {
		t.Fatal(err)
	}

	// The fixture's member is mid-turn and asking a person; Writer is idle.
	turn, err := f.s.CreateTurn(ctx, f.newTurn())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateApproval(ctx, store.NewApproval{TurnID: turn.ID, RoomID: f.room.ID, ThreadID: f.thread.ID, MemberID: f.member.ID, RequestID: "r1", Tool: "Bash", Input: `{"command":"make test"}`}); err != nil {
		t.Fatal(err)
	}

	members, err := f.s.ListMachineMembers(ctx, f.machineID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("want the 2 current members on this machine, got %+v", members)
	}
	// By project name: "docs" before "p".
	if members[0].Member.ID != writer.ID || members[0].ProjectName != "docs" || members[0].Turn != nil || members[0].Approval != nil {
		t.Errorf("first member: %+v", members[0])
	}
	busy := members[1]
	if busy.Member.ID != f.member.ID || busy.ProjectName != "p" || busy.ProjectID == "" {
		t.Errorf("second member: %+v", busy)
	}
	if busy.Approval == nil || busy.Approval.Tool != "Bash" || !strings.Contains(string(busy.Approval.Input), "make test") {
		t.Errorf("its request waiting for a person: %+v", busy.Approval)
	}
	if busy.Turn == nil || busy.Turn.ID != turn.ID || busy.Turn.ThreadID != f.thread.ID || busy.Turn.StartedAt.IsZero() {
		t.Errorf("its turn in flight: %+v", busy.Turn)
	}

	// Once the turn is over it has none.
	if _, err := f.s.FinishTurn(ctx, turn.ID, store.TurnOutcome{Status: store.TurnDone}); err != nil {
		t.Fatal(err)
	}
	members, _ = f.s.ListMachineMembers(ctx, f.machineID)
	if members[1].Turn != nil {
		t.Errorf("finished turn still in flight: %+v", members[1].Turn)
	}

	if none, err := f.s.ListMachineMembers(ctx, "00000000-0000-0000-0000-000000000000"); err != nil || len(none) != 0 {
		t.Errorf("unknown machine: %+v, %v", none, err)
	}
}

func TestMachineActivity(t *testing.T) {
	f := newTurnFixture(t)
	ctx := context.Background()

	start := func(runtimeName string) store.Turn {
		t.Helper()
		spec := f.newTurn()
		spec.Runtime = runtimeName
		turn, err := f.s.CreateTurn(ctx, spec)
		if err != nil {
			t.Fatal(err)
		}
		return turn
	}
	done := start("pi")
	if _, err := f.s.FinishTurn(ctx, done.ID, store.TurnOutcome{Status: store.TurnDone, Usage: runtime.Usage{InputTokens: 100, CacheReadTokens: 900, OutputTokens: 20}}); err != nil {
		t.Fatal(err)
	}
	failed := start("claude")
	if _, err := f.s.FinishTurn(ctx, failed.ID, store.TurnOutcome{Status: store.TurnFailed, Error: "boom", Usage: runtime.Usage{InputTokens: 30, CacheWriteTokens: 5}}); err != nil {
		t.Fatal(err)
	}
	sum := func(a store.MachineActivity) (turns, failed int, tokens int64) {
		for _, bucket := range a.Buckets {
			turns, failed, tokens = turns+bucket.Turns, failed+bucket.Failed, tokens+bucket.Tokens
		}
		return turns, failed, tokens
	}

	now := time.Now().Add(time.Minute)
	activity, err := f.s.MachineActivity(ctx, f.machineID, store.ActivityQuery{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if turns, failedTurns, tokens := sum(activity); len(activity.Buckets) != 24 || activity.Turns != 2 || activity.Failed != 1 || turns != 2 || failedTurns != 1 || tokens != 1055 {
		t.Errorf("activity: %+v", activity)
	}
	// What the failed turn spent counts too.
	if want := (runtime.Usage{InputTokens: 130, CacheReadTokens: 900, CacheWriteTokens: 5, OutputTokens: 20}); activity.Usage != want {
		t.Errorf("usage = %+v, want %+v", activity.Usage, want)
	}

	// One runtime's turns only.
	pi, err := f.s.MachineActivity(ctx, f.machineID, store.ActivityQuery{Runtime: "pi", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if pi.Turns != 1 || pi.Failed != 0 || pi.Usage != (runtime.Usage{InputTokens: 100, CacheReadTokens: 900, OutputTokens: 20}) {
		t.Errorf("pi only: %+v", pi)
	}

	// A month, day by day in a zone.
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	month, err := f.s.MachineActivity(ctx, f.machineID, store.ActivityQuery{Range: store.ActivityMonth, Location: shanghai, Now: now})
	if turns, _, _ := sum(month); err != nil || month.Step != store.StepDay || len(month.Buckets) != 30 || turns != 2 {
		t.Errorf("a month: %+v, %v", month, err)
	}

	// Two days later both have left the last 24 hours, not the last week;
	// another machine never had any.
	later := now.Add(48 * time.Hour)
	if gone, _ := f.s.MachineActivity(ctx, f.machineID, store.ActivityQuery{Now: later}); gone.Turns != 0 {
		t.Errorf("two days later: %+v", gone)
	}
	if week, _ := f.s.MachineActivity(ctx, f.machineID, store.ActivityQuery{Range: store.ActivityWeek, Now: later}); week.Turns != 2 {
		t.Errorf("the week two days later: %+v", week)
	}
	other, err := f.s.RegisterMachine(ctx, "", "build-box", nil)
	if err != nil {
		t.Fatal(err)
	}
	if idle, _ := f.s.MachineActivity(ctx, other, store.ActivityQuery{Now: now}); idle.Turns != 0 || len(idle.Buckets) != 24 {
		t.Errorf("another machine: %+v", idle)
	}
}

func TestActivityBuckets(t *testing.T) {
	zone := func(name string) *time.Location {
		loc, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		return loc
	}
	shanghai, kolkata, newYork := zone("Asia/Shanghai"), zone("Asia/Kolkata"), zone("America/New_York")
	// 12:47 in Shanghai, 10:17 in Kolkata.
	now := time.Date(2026, 9, 16, 4, 47, 12, 0, time.UTC)

	step, hours := store.ActivityBuckets(store.ActivityDay, now, shanghai)
	if step != store.StepHour || len(hours) != 24 || !hours[23].Equal(time.Date(2026, 9, 16, 12, 0, 0, 0, shanghai)) || !hours[0].Equal(time.Date(2026, 9, 15, 13, 0, 0, 0, shanghai)) {
		t.Errorf("24h in Shanghai: %s, first %v, last %v", step, hours[0], hours[len(hours)-1])
	}
	// Half an hour off UTC, the hours are still whole hours there.
	if _, hours := store.ActivityBuckets(store.ActivityDay, now, kolkata); !hours[23].Equal(time.Date(2026, 9, 16, 10, 0, 0, 0, kolkata)) {
		t.Errorf("last hour in Kolkata: %v", hours[23])
	}
	if step, week := store.ActivityBuckets(store.ActivityWeek, now, shanghai); step != store.StepDay || len(week) != 7 || !week[6].Equal(time.Date(2026, 9, 16, 0, 0, 0, 0, shanghai)) || !week[0].Equal(time.Date(2026, 9, 10, 0, 0, 0, 0, shanghai)) {
		t.Errorf("7d: %s, %d buckets from %v", step, len(week), week[0])
	}
	step, days := store.ActivityBuckets(store.ActivityMonth, now, shanghai)
	if step != store.StepDay || len(days) != 30 || !days[29].Equal(time.Date(2026, 9, 16, 0, 0, 0, 0, shanghai)) || !days[0].Equal(time.Date(2026, 8, 18, 0, 0, 0, 0, shanghai)) {
		t.Errorf("30d in Shanghai: %s, first %v, last %v", step, days[0], days[len(days)-1])
	}
	// The day daylight saving ends is 25 hours long and still starts at
	// midnight, as does the next.
	_, days = store.ActivityBuckets(store.ActivityMonth, time.Date(2026, 11, 2, 15, 0, 0, 0, newYork), newYork)
	if !days[29].Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, newYork)) || days[29].Sub(days[28]) != 25*time.Hour {
		t.Errorf("around the end of daylight saving: %v, %v", days[28], days[29])
	}
	// No zone is UTC.
	if _, hours := store.ActivityBuckets(store.ActivityDay, now, nil); !hours[23].Equal(time.Date(2026, 9, 16, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("no zone: %v", hours[23])
	}
}

func TestSummarizeActivity(t *testing.T) {
	now := time.Date(2026, 9, 16, 12, 30, 0, 0, time.UTC)
	ended := func(start time.Time, took time.Duration) *time.Time {
		end := start.Add(took)
		return &end
	}
	at := func(ago time.Duration) time.Time { return now.Add(-ago) }
	turns := []store.TurnSpan{
		{Status: store.TurnDone, StartedAt: at(10 * time.Minute), EndedAt: ended(at(10*time.Minute), 30*time.Second), Usage: runtime.Usage{InputTokens: 50, CacheReadTokens: 400, OutputTokens: 10}},
		// Right on the start of its hour.
		{Status: store.TurnFailed, StartedAt: at(90 * time.Minute), EndedAt: ended(at(90*time.Minute), time.Minute), Usage: runtime.Usage{InputTokens: 7}},
		// In the first hour, which began 13:00 the day before.
		{Status: store.TurnDone, StartedAt: at(23*time.Hour + 30*time.Minute), EndedAt: ended(at(23*time.Hour+30*time.Minute), 10*time.Second), Usage: runtime.Usage{CacheWriteTokens: 3, OutputTokens: 2}},
		// Still running, stamped a moment after now by a skewed clock.
		{Status: store.TurnRunning, StartedAt: now.Add(time.Minute)},
		// A minute before the first hour, and older: left out.
		{Status: store.TurnDone, StartedAt: at(23*time.Hour + 31*time.Minute), EndedAt: ended(at(23*time.Hour+31*time.Minute), time.Hour), Usage: runtime.Usage{InputTokens: 1_000_000}},
		{Status: store.TurnFailed, StartedAt: at(30 * time.Hour)},
	}
	got := store.SummarizeActivity(turns, store.ActivityQuery{Now: now})

	if got.Range != store.ActivityDay || got.Step != store.StepHour || len(got.Buckets) != 24 {
		t.Fatalf("shape: %s by %s, %d buckets", got.Range, got.Step, len(got.Buckets))
	}
	if got.Turns != 4 || got.Failed != 1 {
		t.Errorf("totals: %d turns, %d failed; want 4 and 1", got.Turns, got.Failed)
	}
	counts := map[int][3]int64{}
	for i, bucket := range got.Buckets {
		if bucket.Turns > 0 {
			counts[i] = [3]int64{int64(bucket.Turns), int64(bucket.Failed), bucket.Tokens}
		}
	}
	want := map[int][3]int64{0: {1, 0, 5}, 22: {1, 1, 7}, 23: {2, 0, 460}}
	if len(counts) != len(want) || counts[0] != want[0] || counts[22] != want[22] || counts[23] != want[23] {
		t.Errorf("buckets with turns (turns, failed, tokens): %v, want %v", counts, want)
	}
	if want := (runtime.Usage{InputTokens: 57, CacheReadTokens: 400, CacheWriteTokens: 3, OutputTokens: 12}); got.Usage != want {
		t.Errorf("usage = %+v, want %+v", got.Usage, want)
	}
	if !got.Buckets[0].Start.Equal(time.Date(2026, 9, 15, 13, 0, 0, 0, time.UTC)) || !got.Buckets[23].Start.Equal(time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("bucket starts: first %v, last %v", got.Buckets[0].Start, got.Buckets[23].Start)
	}
	// Ended ones took 10 s, 30 s and a minute: the middle one.
	if got.MedianMS != 30_000 {
		t.Errorf("median = %d ms, want 30000", got.MedianMS)
	}

	even := store.SummarizeActivity(turns[:2], store.ActivityQuery{Now: now})
	if even.MedianMS != 45_000 {
		t.Errorf("median of two = %d ms, want 45000", even.MedianMS)
	}
	if empty := store.SummarizeActivity(nil, store.ActivityQuery{Range: store.ActivityMonth, Now: now}); empty.Turns != 0 || empty.MedianMS != 0 || len(empty.Buckets) != 30 || empty.Step != store.StepDay {
		t.Errorf("nothing: %+v", empty)
	}
}
