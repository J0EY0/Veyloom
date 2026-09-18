package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store/db"
)

// What a machine runs and has done, for the machines page: its members
// across every project, and its turns over the last day.

// MachineMember is a current member a machine runs: the agent, the project
// it is in, and what it is doing now.
type MachineMember struct {
	Member      Member `json:"member"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	// Turn is the member's turn in flight, if it has one.
	Turn *MemberTurn `json:"turn,omitempty"`
	// Approval is the oldest request of the member's that waits for a
	// person, if there is one.
	Approval *MemberApproval `json:"approval,omitempty"`
}

// MemberApproval is what a member waits for a person to allow: the tool
// and its input, as a pending approval holds them.
type MemberApproval struct {
	ID    string          `json:"id"`
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

// MemberTurn is the turn a member has in flight.
type MemberTurn struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"thread_id"`
	StartedAt time.Time `json:"started_at"`
}

// ListMachineMembers returns the current members a machine runs, by project
// name and then in the order they were added. Members taken out of their
// project are left out; a machine with none, or an unknown one, gets an
// empty list.
func (s *Store) ListMachineMembers(ctx context.Context, machineID string) ([]MachineMember, error) {
	uid, err := parseUUID(machineID)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListMachineMembers(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("list members of machine %s: %w", machineID, err)
	}
	out := make([]MachineMember, 0, len(rows))
	for _, row := range rows {
		m := MachineMember{
			Member:      toMember(row.Member),
			ProjectID:   uuidString(row.ProjectID),
			ProjectName: row.ProjectName,
		}
		if row.TurnID.Valid {
			m.Turn = &MemberTurn{ID: uuidString(row.TurnID), ThreadID: uuidString(row.TurnThreadID), StartedAt: row.TurnStartedAt.Time}
		}
		if row.ApprovalID.Valid {
			var payload approvalPayload
			if err := json.Unmarshal(row.ApprovalPayload, &payload); err != nil {
				return nil, fmt.Errorf("decode payload of approval %s: %w", uuidString(row.ApprovalID), err)
			}
			m.Approval = &MemberApproval{ID: uuidString(row.ApprovalID), Tool: payload.Tool, Input: payload.Input}
		}
		out = append(out, m)
	}
	return out, nil
}

// ActivityRange is how far back a machine's activity looks, and so in what
// steps it is summed: the last 24 hours hour by hour, the last 7 and 30
// days day by day.
type ActivityRange string

const (
	ActivityDay   ActivityRange = "24h"
	ActivityWeek  ActivityRange = "7d"
	ActivityMonth ActivityRange = "30d"
)

// Valid reports whether r is one of the ranges.
func (r ActivityRange) Valid() bool {
	return r == ActivityDay || r == ActivityWeek || r == ActivityMonth
}

// ActivityStep is how long one bucket of activity is.
type ActivityStep string

const (
	StepHour ActivityStep = "hour"
	StepDay  ActivityStep = "day"
)

// ActivityQuery says which of a machine's activity to sum up.
type ActivityQuery struct {
	// Range is how far back to look; empty is the last 24 hours.
	Range ActivityRange
	// Runtime keeps only the turns that runtime ran; empty keeps them all.
	Runtime string
	// Location is where hours and days begin; nil is UTC.
	Location *time.Location
	// Now is where the range ends.
	Now time.Time
}

// ActivityBucket is one hour or day of a machine's turns.
type ActivityBucket struct {
	// Start is when the hour or day began.
	Start time.Time `json:"start"`
	// Turns is how many turns started in it; Failed is how many of those
	// failed.
	Turns  int `json:"turns"`
	Failed int `json:"failed"`
	// Tokens is every token those turns spent.
	Tokens int64 `json:"tokens"`
}

// MachineActivity is what a machine did over a range.
type MachineActivity struct {
	Range ActivityRange `json:"range"`
	Step  ActivityStep  `json:"step"`
	// Buckets are the range's hours or days, oldest first; the last one
	// holds now.
	Buckets []ActivityBucket `json:"buckets"`
	Turns   int              `json:"turns"`
	Failed  int              `json:"failed"`
	// MedianMS is the median duration of the turns that have ended, in
	// milliseconds; zero when none has.
	MedianMS int64 `json:"median_ms"`
	// Usage is the tokens all the turns spent, part by part.
	Usage runtime.Usage `json:"usage"`
}

// TurnSpan is how one turn went: its status, when it started and, once it
// is over, when it ended and what it spent.
type TurnSpan struct {
	Status    TurnStatus
	StartedAt time.Time
	EndedAt   *time.Time
	Usage     runtime.Usage
}

// MachineActivity sums up a machine's turns over a range. An unknown
// machine has done nothing.
func (s *Store) MachineActivity(ctx context.Context, machineID string, q ActivityQuery) (MachineActivity, error) {
	uid, err := parseUUID(machineID)
	if err != nil {
		return MachineActivity{}, err
	}
	_, starts := ActivityBuckets(q.Range, q.Now, q.Location)
	rows, err := s.q.ListMachineTurnsSince(ctx, db.ListMachineTurnsSinceParams{
		MachineID: uid,
		Since:     pgtype.Timestamptz{Time: starts[0], Valid: true},
		Runtime:   q.Runtime,
	})
	if err != nil {
		return MachineActivity{}, fmt.Errorf("activity of machine %s: %w", machineID, err)
	}
	spans := make([]TurnSpan, 0, len(rows))
	for _, row := range rows {
		span := TurnSpan{
			Status:    TurnStatus(row.Status),
			StartedAt: row.StartedAt.Time,
			Usage:     usageOf(row.InputTokens, row.CacheReadTokens, row.CacheWriteTokens, row.OutputTokens),
		}
		if row.EndedAt.Valid {
			ended := row.EndedAt.Time
			span.EndedAt = &ended
		}
		spans = append(spans, span)
	}
	return SummarizeActivity(spans, q), nil
}

// ActivityBuckets returns the step of a range and where its buckets begin,
// oldest first: whole clock hours or calendar days in loc (UTC when nil),
// the last one holding now. Hours are found by taking the minutes off
// now, so a zone half an hour off UTC still gets its own whole hours and
// a clock turned back does not start one twice; days begin at midnight,
// however long daylight saving makes them.
func ActivityBuckets(r ActivityRange, now time.Time, loc *time.Location) (ActivityStep, []time.Time) {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	if r == ActivityWeek || r == ActivityMonth {
		days := 7
		if r == ActivityMonth {
			days = 30
		}
		starts := make([]time.Time, days)
		for i := range starts {
			starts[i] = time.Date(local.Year(), local.Month(), local.Day()-(days-1-i), 0, 0, 0, 0, loc)
		}
		return StepDay, starts
	}
	hour := local.Add(-time.Duration(local.Minute())*time.Minute - time.Duration(local.Second())*time.Second - time.Duration(local.Nanosecond()))
	starts := make([]time.Time, 24)
	for i := range starts {
		starts[i] = hour.Add(-time.Duration(len(starts)-1-i) * time.Hour)
	}
	return StepHour, starts
}

// SummarizeActivity puts turns into the buckets of q by when they started,
// with the tokens each spent, and takes the median duration of those that
// have ended. A turn from before the first bucket is left out; one stamped
// after now, as a skewed clock can, counts in the last.
func SummarizeActivity(turns []TurnSpan, q ActivityQuery) MachineActivity {
	if !q.Range.Valid() {
		q.Range = ActivityDay
	}
	step, starts := ActivityBuckets(q.Range, q.Now, q.Location)
	out := MachineActivity{Range: q.Range, Step: step, Buckets: make([]ActivityBucket, len(starts))}
	for i, start := range starts {
		out.Buckets[i].Start = start
	}
	var durations []time.Duration
	for _, turn := range turns {
		index := sort.Search(len(starts), func(i int) bool { return starts[i].After(turn.StartedAt) }) - 1
		if index < 0 {
			continue
		}
		bucket := &out.Buckets[index]
		bucket.Turns++
		bucket.Tokens += turn.Usage.Total()
		out.Turns++
		out.Usage = out.Usage.Plus(turn.Usage)
		if turn.Status == TurnFailed {
			bucket.Failed++
			out.Failed++
		}
		if turn.EndedAt != nil {
			durations = append(durations, max(turn.EndedAt.Sub(turn.StartedAt), 0))
		}
	}
	if n := len(durations); n > 0 {
		slices.Sort(durations)
		median := durations[n/2]
		if n%2 == 0 {
			median = (durations[n/2-1] + durations[n/2]) / 2
		}
		out.MedianMS = median.Milliseconds()
	}
	return out
}
