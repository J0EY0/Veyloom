package store

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store/db"
)

// Where the tokens went, across every machine (docs/webui.md 4.20): the
// usage page's figures, turn by turn today and day by day over a week or a
// month, and by runtime, by member and by piece of work.

// UsageRange is how far back the usage page looks.
type UsageRange string

const (
	// UsageToday is today since midnight, turn by turn.
	UsageToday UsageRange = "today"
	// UsageWeek and UsageMonth are the last 7 and 30 days, today one of
	// them, day by day.
	UsageWeek  UsageRange = "7d"
	UsageMonth UsageRange = "30d"
)

// Valid reports whether r is one of the ranges.
func (r UsageRange) Valid() bool {
	return r == UsageToday || r == UsageWeek || r == UsageMonth
}

// How the usage page's points go.
const (
	UsageStepTurn = "turn"
	UsageStepDay  = "day"
)

// UsageQuery says which usage to sum up.
type UsageQuery struct {
	// Range is how far back to look; empty is today.
	Range UsageRange
	// ProjectID keeps one project's turns; empty keeps every one's.
	ProjectID string
	// Location is where days begin; nil is UTC.
	Location *time.Location
	// Now is where the range ends.
	Now time.Time
}

// Usage is where the tokens went over a range.
type Usage struct {
	Range UsageRange `json:"range"`
	// Step says what a point is: a turn (UsageStepTurn) or a day.
	Step string `json:"step"`
	// Total is what every turn spent, part by part; Turns how many there
	// were.
	Total  runtime.Usage `json:"total"`
	Turns  int           `json:"turns"`
	Points []UsagePoint  `json:"points"`
	// Runtimes, Members and Works share the tokens out, the most first.
	Runtimes []UsageRuntime `json:"runtimes"`
	Members  []UsageMember  `json:"members"`
	Works    []UsageWork    `json:"works"`
}

// UsagePoint is a turn, or a day's turns.
type UsagePoint struct {
	// At is when the turn began, or the day did.
	At time.Time `json:"at"`
	// Tokens is every token spent; Output the part the model wrote.
	Tokens int64 `json:"tokens"`
	Output int64 `json:"output"`
	// DurationMS is how long the turn took, or the day's took in all; one
	// still running counts up to now.
	DurationMS int64 `json:"duration_ms"`
	Turns      int   `json:"turns"`
	// For a turn: which, whose, and in what topic of what room.
	TurnID       string `json:"turn_id,omitempty"`
	MemberID     string `json:"member_id,omitempty"`
	Member       string `json:"member,omitempty"`
	RoomID       string `json:"room_id,omitempty"`
	ThreadNumber int    `json:"thread_number,omitempty"`
}

// UsageRuntime is what one runtime's turns spent.
type UsageRuntime struct {
	Runtime string `json:"runtime"`
	Tokens  int64  `json:"tokens"`
	Turns   int    `json:"turns"`
}

// UsageMember is what one member's turns spent, and what it runs.
type UsageMember struct {
	MemberID string `json:"member_id"`
	// AgentID is the agent it is, whose picture it wears; empty once the
	// agent is gone.
	AgentID     string `json:"agent_id,omitempty"`
	Name        string `json:"name"`
	Model       string `json:"model,omitempty"`
	Runtime     string `json:"runtime"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	Tokens      int64  `json:"tokens"`
	Turns       int    `json:"turns"`
}

// UsageWork is what one piece of work spent: a person's ask, or a
// project's setup or its wiki's upkeep, Kind saying which.
type UsageWork struct {
	// Chain is the piece of work a person asked for; empty for the others.
	Chain        string   `json:"chain,omitempty"`
	Kind         TurnKind `json:"kind"`
	RoomID       string   `json:"room_id"`
	ProjectName  string   `json:"project_name"`
	ThreadNumber int      `json:"thread_number,omitempty"`
	Title        string   `json:"title,omitempty"`
	Tokens       int64    `json:"tokens"`
	Turns        int      `json:"turns"`
}

// UsageWorks caps the pieces of work the page ranks.
const UsageWorks = 20

// UsageTurn is a turn as the usage page sums it.
type UsageTurn struct {
	ID, MemberID, RoomID, ThreadID string
	ThreadNumber                   int
	Runtime                        string
	Kind                           TurnKind
	Status                         TurnStatus
	Chain                          string
	StartedAt                      time.Time
	EndedAt                        *time.Time
	Usage                          runtime.Usage
	Member, AgentID, Model         string
	ProjectID, ProjectName         string
	// ChainBody is the words the person began its piece of work with.
	ChainBody string
}

// Usage sums up the turns of a range.
func (s *Store) Usage(ctx context.Context, q UsageQuery) (Usage, error) {
	var pid pgtype.UUID
	if q.ProjectID != "" {
		var err error
		if pid, err = parseUUID(q.ProjectID); err != nil {
			return Usage{}, err
		}
	}
	start, _ := UsageDays(q.Range, q.Now, q.Location)
	rows, err := s.q.ListUsageTurns(ctx, db.ListUsageTurnsParams{Since: pgtype.Timestamptz{Time: start[0], Valid: true}, ProjectID: pid})
	if err != nil {
		return Usage{}, fmt.Errorf("usage: %w", err)
	}
	turns := make([]UsageTurn, len(rows))
	for i, row := range rows {
		turns[i] = UsageTurn{
			ID: uuidString(row.ID), MemberID: uuidString(row.MemberID), RoomID: uuidString(row.RoomID), ThreadID: uuidString(row.ThreadID),
			ThreadNumber: int(row.ThreadNumber), Runtime: row.Runtime, Kind: TurnKind(row.Kind), Status: TurnStatus(row.Status),
			Chain: uuidString(row.ChainMessageID), StartedAt: row.StartedAt.Time,
			Usage:  usageOf(row.InputTokens, row.CacheReadTokens, row.CacheWriteTokens, row.OutputTokens),
			Member: row.MemberName, AgentID: uuidString(row.AgentID), Model: row.Model, ProjectID: uuidString(row.ProjectID), ProjectName: row.ProjectName, ChainBody: row.ChainBody,
		}
		if row.EndedAt.Valid {
			ended := row.EndedAt.Time
			turns[i].EndedAt = &ended
		}
	}
	return SummarizeUsage(turns, q), nil
}

// UsageDays returns where a range's days begin, oldest first, in loc (UTC
// when nil), and whether it goes turn by turn: today is one day, the week
// and the month 7 and 30 of them, the last holding now.
func UsageDays(r UsageRange, now time.Time, loc *time.Location) ([]time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	days := 1
	switch r {
	case UsageWeek:
		days = 7
	case UsageMonth:
		days = 30
	}
	starts := make([]time.Time, days)
	for i := range starts {
		starts[i] = time.Date(local.Year(), local.Month(), local.Day()-(days-1-i), 0, 0, 0, 0, loc)
	}
	return starts, days == 1
}

// SummarizeUsage sums turns, oldest first, up for a range: turn by turn
// for today, else day by day, and shared out by runtime, member and piece
// of work.
func SummarizeUsage(turns []UsageTurn, q UsageQuery) Usage {
	if q.Range == "" {
		q.Range = UsageToday
	}
	days, byTurn := UsageDays(q.Range, q.Now, q.Location)
	u := Usage{Range: q.Range, Step: UsageStepDay, Points: []UsagePoint{}, Runtimes: []UsageRuntime{}, Members: []UsageMember{}, Works: []UsageWork{}}
	if byTurn {
		u.Step = UsageStepTurn
	} else {
		for _, d := range days {
			u.Points = append(u.Points, UsagePoint{At: d})
		}
	}
	runtimes := map[string]*UsageRuntime{}
	members := map[string]*UsageMember{}
	works := map[string]*UsageWork{}
	names := map[string][]string{}
	for _, t := range turns {
		if t.StartedAt.Before(days[0]) {
			continue
		}
		tokens := t.Usage.Total()
		took := q.Now.Sub(t.StartedAt)
		if t.EndedAt != nil {
			took = t.EndedAt.Sub(t.StartedAt)
		}
		u.Total = u.Total.Plus(t.Usage)
		u.Turns++
		if byTurn {
			u.Points = append(u.Points, UsagePoint{
				At: t.StartedAt, Tokens: tokens, Output: t.Usage.OutputTokens, DurationMS: took.Milliseconds(), Turns: 1,
				TurnID: t.ID, MemberID: t.MemberID, Member: t.Member, RoomID: t.RoomID, ThreadNumber: t.ThreadNumber,
			})
		} else {
			i := sort.Search(len(days), func(i int) bool { return days[i].After(t.StartedAt) }) - 1
			p := &u.Points[max(i, 0)]
			p.Tokens += tokens
			p.Output += t.Usage.OutputTokens
			p.DurationMS += took.Milliseconds()
			p.Turns++
		}

		rt := runtimes[t.Runtime]
		if rt == nil {
			rt = &UsageRuntime{Runtime: t.Runtime}
			runtimes[t.Runtime] = rt
		}
		rt.Tokens += tokens
		rt.Turns++

		m := members[t.MemberID]
		if m == nil {
			m = &UsageMember{MemberID: t.MemberID, AgentID: t.AgentID, Name: t.Member, ProjectID: t.ProjectID, ProjectName: t.ProjectName}
			members[t.MemberID] = m
		}
		// What it runs now: its latest turn's.
		m.Model, m.Runtime = t.Model, t.Runtime
		m.Tokens += tokens
		m.Turns++

		key := t.Chain
		switch {
		case t.Kind != TurnChat:
			key = t.RoomID + "/" + string(t.Kind)
		case key == "":
			key = t.ThreadID
		}
		w := works[key]
		if w == nil {
			w = &UsageWork{Kind: t.Kind, RoomID: t.RoomID, ProjectName: t.ProjectName}
			if t.Kind == TurnChat {
				w.Chain, w.ThreadNumber = t.Chain, t.ThreadNumber
			}
			works[key] = w
		}
		w.Tokens += tokens
		w.Turns++
		if !slices.Contains(names[t.RoomID], t.Member) {
			names[t.RoomID] = append(names[t.RoomID], t.Member)
		}
		if w.Chain != "" && w.Title == "" && t.ChainBody != "" {
			// Named once the names of the room's members are known that
			// its words may begin with.
			w.Title = t.ChainBody
		}
	}
	for _, rt := range runtimes {
		u.Runtimes = append(u.Runtimes, *rt)
	}
	for _, m := range members {
		u.Members = append(u.Members, *m)
	}
	for _, w := range works {
		if w.Title != "" {
			w.Title = AskLine(w.Title, names[w.RoomID])
		}
		u.Works = append(u.Works, *w)
	}
	// The most first; ties in a fixed order, so the page does not shuffle.
	sort.Slice(u.Runtimes, func(i, j int) bool {
		a, b := u.Runtimes[i], u.Runtimes[j]
		return a.Tokens > b.Tokens || a.Tokens == b.Tokens && a.Runtime < b.Runtime
	})
	sort.Slice(u.Members, func(i, j int) bool {
		a, b := u.Members[i], u.Members[j]
		return a.Tokens > b.Tokens || a.Tokens == b.Tokens && a.MemberID < b.MemberID
	})
	sort.Slice(u.Works, func(i, j int) bool {
		a, b := u.Works[i], u.Works[j]
		return a.Tokens > b.Tokens || a.Tokens == b.Tokens && a.Chain+a.RoomID+string(a.Kind) < b.Chain+b.RoomID+string(b.Kind)
	})
	if len(u.Works) > UsageWorks {
		u.Works = u.Works[:UsageWorks]
	}
	return u
}
