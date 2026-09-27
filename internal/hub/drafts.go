package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// What a member drafts for a person to do with one press (design.md
// 5.23.5): put a member's work on the main line, give it up, install a
// skill of the library for a member. The draft is a card in the topic the
// member drafted it in. A person runs it, which is what the branches and
// the skill library do anyway, or turns it down. When the member said what
// it does then, it is woken with what came of it, in a piece of work of
// its own that the person's press starts.

const (
	// draftsPerTurn caps what a turn drafts; draftTextMax a merge's
	// message or why work is given up, draftThenMax what the member does
	// then, in characters.
	draftsPerTurn = 5
	draftTextMax  = 5000
	draftThenMax  = 2000
)

// answerDraft drafts what a person is to do, on a card in at's topic.
func (m *TurnManager) answerDraft(ctx context.Context, at *activeTurn, q runtime.RoomQuery) (string, error) {
	var args struct {
		Kind    string `json:"kind"`
		Member  string `json:"member"`
		Message string `json:"message"`
		Reason  string `json:"reason"`
		Skill   string `json:"skill"`
		Then    string `json:"then"`
	}
	if len(q.Args) > 0 {
		if err := json.Unmarshal(q.Args, &args); err != nil {
			return "", fmt.Errorf("%s: %w", runtime.MessageToolDraft, err)
		}
	}
	if at.turn.Kind != store.TurnChat {
		return "", errors.New("draft_action is for turns in the chat")
	}
	then := strings.TrimSpace(args.Then)
	if utf8.RuneCountInString(then) > draftThenMax {
		return "", fmt.Errorf("then takes at most %d characters", draftThenMax)
	}
	target, err := m.draftTarget(ctx, at, strings.TrimSpace(args.Member))
	if err != nil {
		return "", err
	}
	d := store.NewDraft{
		RoomID: at.thread.RoomID, ThreadID: at.thread.ID, MemberID: at.member.ID, TurnID: at.turn.ID,
		Kind: store.DraftKind(args.Kind), TargetID: target.ID, Then: then,
	}
	switch d.Kind {
	case store.DraftMerge, store.DraftSetAside:
		text, what := strings.TrimSpace(args.Message), "a merge needs its message, the first line saying what the work does"
		if d.Kind == store.DraftSetAside {
			text, what = strings.TrimSpace(args.Reason), "giving work up needs the reason why"
		}
		switch {
		case text == "":
			return "", errors.New(what)
		case utf8.RuneCountInString(text) > draftTextMax:
			return "", fmt.Errorf("it takes at most %d characters", draftTextMax)
		case target.WorktreeDir == "":
			return "", fmt.Errorf("%s has no branch of its own: it works in the project's checkout, or has not worked in a worktree yet", target.DisplayName)
		}
		d.Subject = "work:" + target.ID
		if d.Kind == store.DraftMerge {
			d.Params.Message = text
		} else {
			d.Params.Reason = text
		}
	case store.DraftInstallSkill:
		name := strings.TrimSpace(args.Skill)
		if err := m.wikis.checkSkills(ctx, []string{name}); err != nil {
			return "", err
		}
		agent, err := m.store.GetAgent(ctx, target.AgentID)
		if err != nil {
			return "", err
		}
		if slices.Contains(agent.Skills, name) {
			return "", fmt.Errorf("%s has the skill %s installed already", target.DisplayName, name)
		}
		d.Subject, d.Params.Skill = "skill:"+target.ID+":"+name, name
	default:
		return "", errors.New("kind is merge, set_aside or install_skill")
	}

	// A turn's share is taken first, so two calls at once cannot both take
	// the last of it, and given back on a failure.
	at.mu.Lock()
	if at.drafted >= draftsPerTurn {
		at.mu.Unlock()
		return "", fmt.Errorf("a turn drafts at most %d", draftsPerTurn)
	}
	at.drafted++
	at.mu.Unlock()
	drafted, err := m.draft(ctx, at, d, target)
	if err != nil {
		at.mu.Lock()
		at.drafted--
		at.mu.Unlock()
		return "", err
	}
	answer := "Drafted: a card in this topic lets a person " + draftDoing(drafted, target.DisplayName) + " with one press."
	if then != "" {
		answer += " You are woken with what comes of it once they run it."
	} else {
		answer += " You are not woken for it: its card shows what comes of it."
	}
	return answer + " Drafting the same again replaces this card.", nil
}

// draftTarget is the member of at's room named name, or at's own when name
// is empty.
func (m *TurnManager) draftTarget(ctx context.Context, at *activeTurn, name string) (store.Member, error) {
	name = strings.TrimPrefix(name, "@")
	if name == "" || strings.EqualFold(name, at.member.DisplayName) {
		// Read again: its worktree may have come about in this very turn.
		return m.store.GetMember(ctx, at.member.ID)
	}
	members, err := m.store.ListRoomMembers(ctx, at.thread.RoomID)
	if err != nil {
		return store.Member{}, err
	}
	for _, member := range members {
		if strings.EqualFold(member.DisplayName, name) && !member.Removed() {
			return member, nil
		}
	}
	return store.Member{}, fmt.Errorf("the project has no member %s", name)
}

// draft records d, which gives way to what the project had pending about
// the same, and tells of it on a card in at's topic, in order with what
// the turn says there. One that could not be told of is turned down: no
// one could see it to run it.
func (m *TurnManager) draft(ctx context.Context, at *activeTurn, d store.NewDraft, target store.Member) (store.Draft, error) {
	project, err := m.store.RoomProject(ctx, at.thread.RoomID)
	if err != nil {
		return store.Draft{}, err
	}
	d.ProjectID = project.ID
	drafted, gone, err := m.store.CreateDraft(ctx, d)
	if err != nil {
		return store.Draft{}, err
	}
	for _, g := range gone {
		m.publish(draftEvent(g))
	}
	told := make(chan error, 1)
	if !at.enqueue(func() { told <- m.showDraft(at, &drafted, target.DisplayName) }) {
		m.untellable(drafted.ID)
		return store.Draft{}, errors.New("the turn is over")
	}
	select {
	case err := <-told:
		if err != nil {
			return store.Draft{}, err
		}
		return drafted, nil
	case <-ctx.Done():
		return store.Draft{}, fmt.Errorf("the draft was not confirmed in time; the topic shows whether it was: %w", ctx.Err())
	}
}

// showDraft puts d's card in at's topic, on the turn's executor.
func (m *TurnManager) showDraft(at *activeTurn, d *store.Draft, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()
	card, err := m.post(ctx, store.NewMessage{
		RoomID: at.thread.RoomID, ThreadID: at.thread.ID, SenderKind: store.SenderSystem,
		Body: draftNote(at.member.DisplayName, target, *d), TurnID: at.turn.ID,
	})
	if err != nil {
		m.untellable(d.ID)
		return err
	}
	if err := m.store.SetDraftMessage(ctx, d.ID, card.ID); err != nil {
		m.logger.Warn("note the card of a draft", "draft", d.ID, "err", err)
	} else {
		d.MessageID = card.ID
	}
	m.publish(draftEvent(*d))
	m.logger.Info("drafted", "draft", d.ID, "kind", d.Kind, "member", at.member.DisplayName)
	return nil
}

// untellable turns down a draft no card shows.
func (m *TurnManager) untellable(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
	defer cancel()
	if _, err := m.store.DeclineDraft(ctx, id, ""); err != nil {
		m.logger.Error("turn down a draft no card shows", "draft", id, "err", err)
	}
}

// DraftEdit is what a person changed of a merge they run: its message,
// empty for the drafted one, and the new files they leave out.
type DraftEdit struct {
	Message string
	Leave   []string
}

// RunDraft does what a draft has a person do, as the person userID, a
// merge as they edited it. What
// cannot be done for now, the member at work or its machine away, is
// refused as the branches refuse it, and the draft waits again; a merge
// that meets conflicts is what came of it. The member that drafted it is
// woken with what came of it when it said what it does then.
func (h *Hub) RunDraft(ctx context.Context, id, userID string, edit DraftEdit) (store.Draft, error) {
	d, err := h.store.ClaimDraft(ctx, id)
	if err != nil {
		return store.Draft{}, h.draftSettled(ctx, id, err)
	}
	h.events.publish(draftEvent(d))
	status, result, err := h.runDraft(ctx, d, edit)
	if err != nil {
		if released, rerr := h.store.ReleaseDraft(context.WithoutCancel(ctx), id); rerr == nil {
			h.events.publish(draftEvent(released))
		} else {
			h.logger.Error("let a draft wait again", "draft", id, "err", rerr)
		}
		return store.Draft{}, err
	}
	settled, err := h.store.SettleDraft(context.WithoutCancel(ctx), id, status, result, userID)
	if err != nil {
		return store.Draft{}, err
	}
	h.events.publish(draftEvent(settled))
	h.logger.Info("ran a draft", "draft", id, "kind", d.Kind, "status", status)
	if settled.Then != "" {
		h.turns.tellDrafter(context.WithoutCancel(ctx), settled, userID)
	}
	return settled, nil
}

// runDraft does what d has a person do, by the flow that does it anyway.
func (h *Hub) runDraft(ctx context.Context, d store.Draft, edit DraftEdit) (store.DraftStatus, store.DraftResult, error) {
	switch d.Kind {
	case store.DraftMerge:
		message := strings.TrimSpace(edit.Message)
		if message == "" {
			message = d.Params.Message
		}
		res, err := h.Merge(ctx, d.TargetID, message, edit.Leave)
		switch {
		case err != nil:
			return "", store.DraftResult{}, err
		case len(res.Conflicts) > 0:
			return store.DraftConflicted, store.DraftResult{Conflicts: res.Conflicts}, nil
		}
		return store.DraftDone, store.DraftResult{Commit: res.Commit, Unsettled: res.Unsettled}, nil
	case store.DraftSetAside:
		ref, err := h.SetAside(ctx, d.TargetID)
		if err != nil {
			return "", store.DraftResult{}, err
		}
		return store.DraftDone, store.DraftResult{Ref: ref}, nil
	case store.DraftInstallSkill:
		target, err := h.store.GetMember(ctx, d.TargetID)
		if err != nil {
			return "", store.DraftResult{}, err
		}
		if _, err := h.InstallSkill(ctx, d.Params.Skill, target.AgentID, true); err != nil {
			return "", store.DraftResult{}, err
		}
		return store.DraftDone, store.DraftResult{}, nil
	case store.DraftSetupSteps:
		if err := h.adoptSteps(ctx, d.ProjectID); err != nil {
			return "", store.DraftResult{}, err
		}
		return store.DraftDone, store.DraftResult{}, nil
	}
	return "", store.DraftResult{}, fmt.Errorf("a draft of kind %s is not run this way", d.Kind)
}

// DeclineDraft turns a draft down, as the person userID. The member that
// drafted it is not woken: a person says why, if they wish to.
func (h *Hub) DeclineDraft(ctx context.Context, id, userID string) (store.Draft, error) {
	d, err := h.store.DeclineDraft(ctx, id, userID)
	if err != nil {
		return store.Draft{}, h.draftSettled(ctx, id, err)
	}
	h.events.publish(draftEvent(d))
	if d.Kind == store.DraftSetupSteps {
		// The members waiting for the steps are told they were turned down.
		if err := h.dropSteps(context.WithoutCancel(ctx), d.ProjectID); err != nil && !errors.Is(err, store.ErrNotFound) {
			h.logger.Error("turn setup steps down", "project", d.ProjectID, "err", err)
		}
	}
	return d, nil
}

// draftSettled is why a draft could not be run or turned down: gone, or
// settled or running already.
func (h *Hub) draftSettled(ctx context.Context, id string, err error) error {
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	d, gerr := h.store.GetDraft(ctx, id)
	if gerr != nil {
		return gerr
	}
	return store.Conflicting("draftSettled", store.Params{"status": string(d.Status)}, "the draft is %s already", d.Status)
}

// ThreadDrafts lists the drafts of a topic, in the order drafted.
func (h *Hub) ThreadDrafts(ctx context.Context, threadID string) ([]store.Draft, error) {
	return h.store.ListThreadDrafts(ctx, threadID)
}

// tellDrafter tells the member that drafted d what came of it, in d's
// topic, and wakes it with that: a piece of work of its own, which the
// person's press started, for that person (chainPerson).
func (m *TurnManager) tellDrafter(ctx context.Context, d store.Draft, userID string) {
	ctx, cancel := context.WithTimeout(ctx, m.storeTimeout)
	defer cancel()
	drafter, err := m.store.GetMember(ctx, d.MemberID)
	if err != nil || drafter.Removed() || !drafter.Enabled {
		return
	}
	target := drafter.DisplayName
	if d.TargetID != "" && d.TargetID != drafter.ID {
		if t, err := m.store.GetMember(ctx, d.TargetID); err == nil {
			target = t.DisplayName
		}
	}
	person := "A person"
	if userID != "" {
		if user, err := m.store.GetUser(ctx, userID); err == nil {
			person = user.Name
		}
	}
	thread, err := m.store.GetThread(ctx, d.ThreadID)
	if err != nil {
		m.logger.Error("the topic of a draft", "draft", d.ID, "err", err)
		return
	}
	note, err := m.post(ctx, store.NewMessage{
		RoomID: d.RoomID, ThreadID: d.ThreadID, SenderKind: store.SenderSystem, Body: draftOutcomeNote(person, drafter.DisplayName, target, d),
	})
	if err != nil {
		m.logger.Error("tell a member what came of its draft", "draft", d.ID, "err", err)
		return
	}
	if err := m.store.SetDraftResultMessage(ctx, d.ID, note.ID); err != nil {
		m.logger.Warn("note what told of a draft's outcome", "draft", d.ID, "err", err)
	}
	if err := m.triggerFrom(ctx, drafter, note, thread, note.ID); err != nil {
		m.logger.Error("wake a member with what came of its draft", "draft", d.ID, "err", err)
	}
}

// draftEvent is the live event for a draft that was drafted, run, turned
// down or replaced.
func draftEvent(d store.Draft) Event {
	return Event{Kind: EventDraft, RoomID: d.RoomID, At: time.Now(), Draft: &d}
}

// draftDoing says what running d does, target being whom it acts on.
func draftDoing(d store.Draft, target string) string {
	switch d.Kind {
	case store.DraftMerge:
		return "put " + target + "'s work on the main line"
	case store.DraftSetAside:
		return "give " + target + "'s work up"
	case store.DraftInstallSkill:
		return "install the skill " + d.Params.Skill + " for " + target
	case store.DraftSetupSteps:
		return "adopt the steps new worktrees are got ready with"
	}
	return string(d.Kind)
}

// draftNote says, in the hub's words, that drafter drafted d; the UI draws
// it as the card (systemNote.ts reads it when the draft is not at hand).
func draftNote(drafter, target string, d store.Draft) string {
	switch d.Kind {
	case store.DraftMerge:
		return fmt.Sprintf("%s drafted putting %s's work on the main line, as: %s", drafter, target, d.Params.Message)
	case store.DraftSetAside:
		return fmt.Sprintf("%s drafted giving %s's work up, since: %s", drafter, target, d.Params.Reason)
	case store.DraftSetupSteps:
		var steps store.WorkspaceSteps
		if d.Params.Steps != nil {
			steps = *d.Params.Steps
		}
		return fmt.Sprintf("%s wrote down how a new worktree is got ready: %s. A person adopts the command before it runs.", drafter, stepsLine(steps))
	}
	return fmt.Sprintf("%s drafted installing the skill %s for %s.", drafter, d.Params.Skill, target)
}

// draftOutcomeNote tells the member that drafted d what came of it, as
// the person ran it, and what it said it does then.
func draftOutcomeNote(person, drafter, target string, d store.Draft) string {
	var did string
	switch {
	case d.Kind == store.DraftMerge && d.Status == store.DraftConflicted:
		did = fmt.Sprintf("%s tried putting %s's work on the main line, as %s drafted, but it conflicts with the main line in %s; nothing changed", person, target, drafter, strings.Join(d.Result.Conflicts, ", "))
	case d.Kind == store.DraftMerge:
		did = fmt.Sprintf("%s put %s's work on the main line as %s drafted, as commit %s", person, target, drafter, shortCommit(d.Result.Commit))
	case d.Kind == store.DraftSetAside:
		did = fmt.Sprintf("%s gave %s's work up as %s drafted; it is kept as %s", person, target, drafter, d.Result.Ref)
	default:
		did = fmt.Sprintf("%s installed the skill %s for %s as %s drafted", person, d.Params.Skill, target, drafter)
	}
	return did + ". Then, " + drafter + " said: " + d.Then
}
