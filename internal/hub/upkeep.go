package hub

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// The wiki maintainer (docs/design.md 5.12). A person chooses one member
// of a project to keep its wiki. From then on that member now and then
// runs a turn of its own, an upkeep, in the project's wiki topic: it goes
// over what the chat did since the last one, and over what other projects'
// turns did with the skills the project's team owns, and records what is
// worth keeping. It runs once a topic has gone quiet, once a day (the
// default), every three days, once a week, or when a person asks; never
// when there is nothing new, unless a person asks. A project that has no
// maintainer is offered one in its chat once enough topics wait (5.16).

// upkeepStore is what the maintainer's bookkeeping reads and writes.
type upkeepStore interface {
	ListMaintainedProjects(ctx context.Context) ([]store.Project, error)
	ListUpkeepTurns(ctx context.Context, q store.UpkeepQuery) ([]store.UpkeepTurn, error)
	CountUpkeepWaiting(ctx context.Context, projectID string, owned []string, quietSince time.Time) (store.UpkeepWaiting, error)
	LastUpkeep(ctx context.Context, projectID string) (store.Turn, error)
	RecordWikiReviews(ctx context.Context, projectID, upkeepTurnID string, turnIDs []string) error
	NextPersonMessage(ctx context.Context, threadID string, after time.Time) (store.Message, error)
	CountUpkeepReviews(ctx context.Context, upkeepTurnID string) (int, error)
	CountUpkeepsSince(ctx context.Context, projectID string, since time.Time) (int, error)
	// What people said and sent since the last upkeep (design.md 5.16).
	ListPeopleNews(ctx context.Context, projectID string, after, upto int64, limit int) ([]store.PeopleNews, error)
	ListPeopleFiles(ctx context.Context, projectID string, after, upto int64, limit int) ([]store.PeopleFile, error)
	SetProjectWikiSeen(ctx context.Context, projectID string, seq int64) error
	// Offering a maintainer to a project that has none.
	ListUnmaintainedProjects(ctx context.Context) ([]store.Project, error)
	CountSettledTopicsWaiting(ctx context.Context, projectID string, quietSince time.Time) (int, error)
	SetProjectWikiOffer(ctx context.Context, projectID, messageID string) (bool, error)
}

// upkeepReason is why an upkeep runs.
type upkeepReason string

const (
	// upkeepQuiet: topics went quiet, on a project that runs it then.
	upkeepQuiet upkeepReason = "topics went quiet"
	// upkeepDaily: a day went by, on a project that runs it daily.
	upkeepDaily upkeepReason = "daily"
	// upkeepEvery3Days: three days went by, on a project that runs it so.
	upkeepEvery3Days upkeepReason = "every three days"
	// upkeepWeekly: a week went by, on a project that runs it weekly.
	upkeepWeekly upkeepReason = "weekly"
	// upkeepBacklog: the last upkeep went over as many turns as it may and
	// left the rest to this one, which does not wait for the next day or
	// week.
	upkeepBacklog upkeepReason = "the last upkeep left turns to go over"
	// upkeepAsked: a person asked for it.
	upkeepAsked upkeepReason = "asked by a person"
)

// upkeepDay is how often a daily upkeep runs.
const upkeepDay = 24 * time.Hour

// upkeepEvery says how often a trigger runs upkeeps, and why when it does;
// ok is false for one that only runs when a person asks.
func (h *Hub) upkeepEvery(t store.UpkeepTrigger) (reason upkeepReason, gap time.Duration, ok bool) {
	switch t {
	case store.UpkeepIdle:
		return upkeepQuiet, h.cfg.UpkeepIdle, true
	case store.UpkeepDaily:
		return upkeepDaily, upkeepDay, true
	case store.UpkeepEvery3Days:
		return upkeepEvery3Days, 3 * upkeepDay, true
	case store.UpkeepWeekly:
		return upkeepWeekly, 7 * upkeepDay, true
	}
	return "", 0, false
}

// upkeep is one go of a project's maintainer over what the chat did.
type upkeep struct {
	project store.Project
	member  store.Member
	reason  upkeepReason
	// owned are the library's skills the project's team owns, by name.
	owned []string
	// turns are the project's own turns to go over, oldest first; uses the
	// other projects' turns that used one of owned.
	turns, uses []store.UpkeepTurn
	// trials are the open trials of owned, oldest first (design.md 5.15).
	trials []store.SkillTrial
	// position is where the chat stood as the upkeep was planned: gone
	// over, the project's wiki position moves up to it (design.md 5.16).
	// news and files are what people said and sent since the last upkeep,
	// up to position, for the maintainer to look into itself.
	position int64
	news     []store.PeopleNews
	files    []store.PeopleFile
}

// Caps of what an upkeep lists of what people said and sent; the
// maintainer finds the rest with the room tools.
const (
	upkeepPeopleNews  = 30
	upkeepPeopleFiles = 30
)

// peopleMessages counts what people said since the last upkeep.
func (u *upkeep) peopleMessages() int {
	n := 0
	for _, news := range u.news {
		n += news.Messages
	}
	return n
}

// turnIDs are the turns the upkeep goes over.
func (u *upkeep) turnIDs() []string {
	ids := make([]string, 0, len(u.turns)+len(u.uses))
	for _, t := range u.turns {
		ids = append(ids, t.ID)
	}
	for _, t := range u.uses {
		ids = append(ids, t.ID)
	}
	return ids
}

// Why an upkeep does not start.
var (
	ErrNoMaintainer = store.Invalid("noMaintainer", nil, "this project has no wiki maintainer: a person chooses one in the project's settings")
	errUpkeepBusy   = store.Conflicting("upkeepBusy", nil, "an upkeep of this wiki is running or waiting to")
)

// UpkeepStatus is how a project's wiki upkeep stands.
type UpkeepStatus struct {
	// MemberID and MemberName are the maintainer; empty when there is none.
	MemberID   string              `json:"member_id,omitempty"`
	MemberName string              `json:"member_name,omitempty"`
	Trigger    store.UpkeepTrigger `json:"trigger"`
	// IdleMinutes is how long a topic stays quiet before an upkeep on idle
	// topics goes over it.
	IdleMinutes int `json:"idle_minutes"`
	// Waiting is what the next upkeep would go over; Settled counts what
	// ran in topics quiet for IdleMinutes.
	Waiting store.UpkeepWaiting `json:"waiting"`
	// Queued says an upkeep waits for its member to be free.
	Queued bool `json:"queued"`
	// Last is the latest upkeep, running or not.
	Last *store.Turn `json:"last,omitempty"`
	// ThreadID is the project's wiki topic, where upkeeps run; empty until
	// the first.
	ThreadID string `json:"thread_id,omitempty"`
}

// UpkeepStatus says how a project's wiki upkeep stands.
func (h *Hub) UpkeepStatus(ctx context.Context, projectID string) (UpkeepStatus, error) {
	project, err := h.store.GetProject(ctx, projectID)
	if err != nil {
		return UpkeepStatus{}, err
	}
	status := UpkeepStatus{Trigger: project.WikiMaintainerTrigger, IdleMinutes: int(h.cfg.UpkeepIdle / time.Minute), ThreadID: project.WikiThreadID}
	if project.WikiMaintainerMemberID != "" {
		if member, err := h.store.GetMember(ctx, project.WikiMaintainerMemberID); err == nil {
			status.MemberID, status.MemberName = member.ID, member.DisplayName
		}
	}
	owned := h.turns.ownedSkills(ctx, project)
	if status.Waiting, err = h.store.CountUpkeepWaiting(ctx, project.ID, owned, h.now().Add(-h.cfg.UpkeepIdle)); err != nil {
		return UpkeepStatus{}, err
	}
	status.Queued = h.turns.upkeepQueued(project.ID)
	switch last, err := h.store.LastUpkeep(ctx, project.ID); {
	case err == nil:
		status.Last = &last
	case !errors.Is(err, store.ErrNotFound):
		return UpkeepStatus{}, err
	}
	return status, nil
}

// StartUpkeep starts an upkeep of a project's wiki now, as a person asked:
// one with nothing new to go over is a health check.
func (h *Hub) StartUpkeep(ctx context.Context, projectID string) (UpkeepStatus, error) {
	project, err := h.store.GetProject(ctx, projectID)
	if err != nil {
		return UpkeepStatus{}, err
	}
	if h.cfg.WikiDir == "" {
		return UpkeepStatus{}, ErrNoWikis
	}
	if project.WikiMaintainerMemberID == "" {
		return UpkeepStatus{}, ErrNoMaintainer
	}
	if err := h.upkeep(ctx, project, upkeepAsked); err != nil {
		return UpkeepStatus{}, err
	}
	return h.UpkeepStatus(ctx, projectID)
}

// RunUpkeep starts the upkeeps that are due, looking every UpkeepCheck
// until ctx ends.
func (h *Hub) RunUpkeep(ctx context.Context) {
	tick := time.NewTicker(h.cfg.UpkeepCheck)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			h.checkUpkeep(ctx)
		}
	}
}

// checkUpkeep starts the upkeep of every project whose maintainer is due.
func (h *Hub) checkUpkeep(ctx context.Context) {
	if h.cfg.WikiDir == "" {
		return
	}
	projects, err := h.store.ListMaintainedProjects(ctx)
	if err != nil {
		h.logger.Error("find the wikis to keep", "err", err)
		return
	}
	for _, project := range projects {
		reason, due, err := h.upkeepDue(ctx, project)
		if err != nil {
			h.logger.Error("check the wiki upkeep", "project", project.ID, "err", err)
			continue
		}
		if !due {
			continue
		}
		if err := h.upkeep(ctx, project, reason); err != nil && !errors.Is(err, errUpkeepBusy) {
			h.logger.Error("start the wiki upkeep", "project", project.ID, "err", err)
		}
	}
	h.offerMaintainers(ctx)
}

// upkeepDue says whether a project's maintainer should run now, and why.
// On idle topics it runs once one has been quiet for UpkeepIdle, on the
// others once their day, three days or week has gone by, and either only
// when there is something new; never sooner than that after the last,
// however that went, unless the last went over as many turns as it may and
// ended well: the rest then follow at once, up to UpkeepRunsPerDay a day.
func (h *Hub) upkeepDue(ctx context.Context, project store.Project) (upkeepReason, bool, error) {
	reason, gap, ok := h.upkeepEvery(project.WikiMaintainerTrigger)
	if !ok || h.turns.upkeepQueued(project.ID) {
		return "", false, nil
	}
	now := h.now()
	switch last, err := h.store.LastUpkeep(ctx, project.ID); {
	case err == nil:
		if last.Status == store.TurnRunning {
			return "", false, nil
		}
		if now.Sub(last.StartedAt) < gap {
			left, err := h.upkeepLeftTurns(ctx, project, last)
			if err != nil || !left {
				return "", false, err
			}
			reason = upkeepBacklog
		}
	case !errors.Is(err, store.ErrNotFound):
		return "", false, err
	}
	waiting, err := h.store.CountUpkeepWaiting(ctx, project.ID, h.turns.ownedSkills(ctx, project), now.Add(-h.cfg.UpkeepIdle))
	if err != nil {
		return "", false, err
	}
	if project.WikiMaintainerTrigger == store.UpkeepIdle {
		// Only what went quiet, even to catch up.
		return reason, waiting.Settled+waiting.PeopleSettled > 0, nil
	}
	return reason, waiting.Anything(), nil
}

// upkeepLeftTurns reports whether the last upkeep, done within its gap,
// may have left turns for the next: it ended well having gone over as many
// as one may, and the day's runs are not used up.
func (h *Hub) upkeepLeftTurns(ctx context.Context, project store.Project, last store.Turn) (bool, error) {
	if last.Status != store.TurnDone {
		return false, nil
	}
	reviewed, err := h.store.CountUpkeepReviews(ctx, last.ID)
	if err != nil || reviewed < h.cfg.UpkeepTurns {
		return false, err
	}
	runs, err := h.store.CountUpkeepsSince(ctx, project.ID, h.now().Add(-upkeepDay))
	if err != nil {
		return false, err
	}
	return runs < h.cfg.UpkeepRunsPerDay, nil
}

// upkeep starts one upkeep of the project's wiki: it puts together what to
// go over, says so in the wiki topic and queues the maintainer's turn.
func (h *Hub) upkeep(ctx context.Context, project store.Project, reason upkeepReason) error {
	if !h.turns.reserveUpkeep(project.ID) {
		return errUpkeepBusy
	}
	queued := false
	defer func() {
		if !queued {
			h.turns.upkeepStarted(project.ID)
		}
	}()
	if last, err := h.store.LastUpkeep(ctx, project.ID); err == nil && last.Status == store.TurnRunning {
		return errUpkeepBusy
	}
	up, err := h.planUpkeep(ctx, project, reason)
	if err != nil {
		return err
	}
	if reason != upkeepAsked && len(up.turns)+len(up.uses)+len(up.news) == 0 {
		// Due by the count, but what counted is not settled any more.
		return nil
	}
	topic, err := h.turns.wikiTopic(ctx, project)
	if err != nil {
		return err
	}
	note, err := h.turns.post(ctx, store.NewMessage{RoomID: topic.RoomID, ThreadID: topic.ID, SenderKind: store.SenderSystem, Body: upkeepNote(up)})
	if err != nil {
		return err
	}
	queued = true
	return h.turns.TriggerUpkeep(ctx, up.member, note, topic, up)
}

// planUpkeep puts together what an upkeep goes over: the turns nobody has
// gone over yet, oldest first, as many as UpkeepTurns; on quiet topics
// only the turns of the topics that are.
func (h *Hub) planUpkeep(ctx context.Context, project store.Project, reason upkeepReason) (*upkeep, error) {
	member, err := h.store.GetMember(ctx, project.WikiMaintainerMemberID)
	if err != nil {
		return nil, fmt.Errorf("the wiki maintainer: %w", err)
	}
	if member.Removed() || !member.Enabled {
		return nil, store.Conflicting("maintainerOff", store.Params{"name": member.DisplayName}, "the wiki maintainer %s is switched off or out of the project: a person chooses another in the project's settings", member.DisplayName)
	}
	up := &upkeep{project: project, member: member, reason: reason, owned: h.turns.ownedSkills(ctx, project)}
	q := store.UpkeepQuery{ProjectID: project.ID, Unreviewed: true, OldestFirst: true, Limit: h.cfg.UpkeepTurns}
	if reason == upkeepQuiet {
		settled := h.now().Add(-h.cfg.UpkeepIdle)
		q.SettledBy = &settled
	}
	if up.turns, err = h.store.ListUpkeepTurns(ctx, q); err != nil {
		return nil, err
	}
	if len(up.owned) > 0 {
		q.Skills, q.Owned, q.Limit = true, up.owned, max(h.cfg.UpkeepTurns/2, 1)
		if up.uses, err = h.store.ListUpkeepTurns(ctx, q); err != nil {
			return nil, err
		}
		if up.trials, err = h.store.ListOpenSkillTrials(ctx, up.owned); err != nil {
			return nil, err
		}
	}
	if project.MainRoomID != "" {
		if up.position, err = h.store.RoomPosition(ctx, project.MainRoomID); err != nil {
			return nil, err
		}
		if up.news, err = h.store.ListPeopleNews(ctx, project.ID, project.WikiSeenSeq, up.position, upkeepPeopleNews); err != nil {
			return nil, err
		}
		if up.files, err = h.store.ListPeopleFiles(ctx, project.ID, project.WikiSeenSeq, up.position, upkeepPeopleFiles); err != nil {
			return nil, err
		}
	}
	return up, nil
}

// upkeepNote is what the wiki topic says as an upkeep starts, in words the
// UI knows to put its own way.
func upkeepNote(up *upkeep) string {
	return fmt.Sprintf("Wiki upkeep by %s (%s): %s of this chat, %s of other projects using this team's skills, %s from people.",
		up.member.DisplayName, up.reason, count(len(up.turns), "turn"), count(len(up.uses), "turn"), count(up.peopleMessages(), "message"))
}

// ownedSkills names the library's skills the project's team owns.
func (m *TurnManager) ownedSkills(ctx context.Context, project store.Project) []string {
	b, err := m.wikis.library(ctx)
	if err != nil || project.WikiSlug == "" {
		return nil
	}
	var names []string
	for _, s := range b.Pages() {
		if name := wiki.SkillName(s.Path); name != "" && s.Team == project.WikiSlug {
			names = append(names, name)
		}
	}
	return names
}

// TriggerUpkeep queues the maintainer's turn for up, announced by note in
// the wiki topic. It runs on its own, never merged with what the member is
// asked in the chat.
func (m *TurnManager) TriggerUpkeep(ctx context.Context, member store.Member, note store.Message, topic store.Thread, up *upkeep) error {
	m.mu.Lock()
	st := m.state(member.ID)
	if st.starting || st.running != nil {
		st.pending = append(st.pending, trigger{msg: note, thread: topic, upkeep: up})
		m.mu.Unlock()
		return nil
	}
	st.starting = true
	m.mu.Unlock()
	m.start(ctx, member.ID, topic, []store.Message{note}, up)
	return nil
}

// reserveUpkeep marks an upkeep of the project as queued, unless one is.
func (m *TurnManager) reserveUpkeep(projectID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.upkeepWaiting[projectID] {
		return false
	}
	m.upkeepWaiting[projectID] = true
	return true
}

// upkeepStarted clears the mark: the upkeep started, or never will.
func (m *TurnManager) upkeepStarted(projectID string) {
	m.mu.Lock()
	delete(m.upkeepWaiting, projectID)
	m.mu.Unlock()
}

// upkeepQueued reports whether an upkeep of the project waits to start.
func (m *TurnManager) upkeepQueued(projectID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.upkeepWaiting[projectID]
}
