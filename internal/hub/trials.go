package hub

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// Skills evolve as they are used, on trial (docs/design.md 5.15). An agent
// a skill is installed for changes it in a turn, where the task showed it
// wrong or short, and every agent it is installed for uses the new version
// from its next turn. The change is kept once enough turns have used it
// since and ended well, or when a person confirms the skill; a person, or
// the maintainer of the team that owns it, rolls it back to the version
// before. Pattern pages are not rolled back with it.

// trialActor keeps, in the library's history, a change the turns bore out.
const trialActor = "process:skill-trial"

// errSkillNotYours answers a turn changing a skill it may not.
var errSkillNotYours = errors.New("a skill is changed by the agents it is installed for, and by the maintainer of the team that owns it; " +
	"this one is not installed for you. Record what you found as a Pattern page instead (write_wiki, type Pattern, scope library)")

// mayChangeSkill reports whether a turn may change the skill called name,
// whose team is owner: a skill it was given, being installed for its
// agent, or, on an upkeep, one its project's team owns.
func mayChangeSkill(at *activeTurn, tw *turnWiki, name, owner string) bool {
	if at.upkeep != nil && owner != "" && owner == tw.project.WikiSlug {
		return true
	}
	return at.spec.Skills != nil && slices.ContainsFunc(at.spec.Skills.Skills, func(s runtime.Skill) bool { return s.Name == name })
}

// startTrial puts the skill called name on trial for the change the turn
// just made to it: the library's last commit, from before the turn's own,
// is the way back, unless the skill is on trial already and has one.
func (m *TurnManager) startTrial(ctx context.Context, at *activeTurn, tw *turnWiki, name string) error {
	base, err := tw.bundle.Head(ctx)
	if err != nil || base == "" {
		// Without a history there is nothing to go back to.
		return err
	}
	_, err = m.store.StartSkillTrial(ctx, store.NewSkillTrial{
		Skill: name, BaseSHA: base, TurnID: at.turn.ID, ChangedBy: at.member.DisplayName, ProjectName: tw.project.Name,
	})
	return err
}

// noteTrialUses looks, as a turn ends, at the trials of the skills it
// used: a change enough turns have used since, and ended well, is kept.
func (m *TurnManager) noteTrialUses(ctx context.Context, turn store.Turn) {
	if m.wikis == nil {
		return
	}
	for _, name := range turn.SkillsUsed {
		trial, err := m.store.OpenSkillTrial(ctx, name)
		if err != nil {
			continue
		}
		done, _, err := m.store.SkillTrialUses(ctx, name, trial.ChangedAt)
		if err != nil {
			m.logger.Error("count a skill's trial", "skill", name, "err", err)
			continue
		}
		if done < m.trialUses {
			continue
		}
		if err := m.keepTrial(ctx, trial, done); err != nil {
			m.logger.Error("keep a skill's change", "skill", name, "err", err)
		}
	}
}

// keepTrial ends a trial the turns bore out, and says so in the library:
// the skill is confirmed as it is, by the trial.
func (m *TurnManager) keepTrial(ctx context.Context, trial store.SkillTrial, uses int) error {
	m.trialMu.Lock()
	defer m.trialMu.Unlock()
	note := fmt.Sprintf("kept after %d turns used it", uses)
	if _, err := m.store.EndSkillTrial(ctx, trial.ID, store.TrialKept, trialActor, note); err != nil {
		return err
	}
	lib, err := m.wikis.library(ctx)
	if err != nil {
		return err
	}
	w, err := lib.Writer(trialActor)
	if err != nil {
		return err
	}
	page := wiki.SkillPath(trial.Skill)
	if _, err := w.Verify(page); err != nil {
		// Gone from the library by hand, say: the trial is over all the same.
		return err
	}
	if err := w.Note(page, note); err != nil {
		return err
	}
	_, err = w.Commit(ctx, "Keep "+trial.Skill+" after its trial")
	return err
}

// endTrialAsKept ends the open trial of the skill called name, if it has
// one, as kept by a person: they confirmed the skill, or changed it
// themselves.
func (m *TurnManager) endTrialAsKept(ctx context.Context, name, person, reason string) error {
	m.trialMu.Lock()
	defer m.trialMu.Unlock()
	trial, err := m.store.OpenSkillTrial(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = m.store.EndSkillTrial(ctx, trial.ID, store.TrialKept, person, reason)
	return err
}

// rollbackSkill puts the skill called name back the way it was before its
// trial, as actor and for reason, and ends the trial.
func (m *TurnManager) rollbackSkill(ctx context.Context, name, actor, reason string) (store.SkillTrial, error) {
	m.trialMu.Lock()
	defer m.trialMu.Unlock()
	trial, err := m.store.OpenSkillTrial(ctx, name)
	if errors.Is(err, store.ErrNotFound) {
		return store.SkillTrial{}, store.Missing("noTrial", store.Params{"name": name}, "the skill %s is on no trial: there is no change of an agent's to roll back", name)
	}
	if err != nil {
		return store.SkillTrial{}, err
	}
	lib, err := m.wikis.library(ctx)
	if err != nil {
		return store.SkillTrial{}, err
	}
	if _, err := lib.RestoreSkill(ctx, name, trial.BaseSHA, actor, reason); err != nil {
		return store.SkillTrial{}, err
	}
	return m.store.EndSkillTrial(ctx, trial.ID, store.TrialRolledBack, actor, reason)
}

// skillsEditedOutside ends, as kept, the trials of the skills a person
// changed outside Veyloom, in their editor say: their version stands.
func (m *TurnManager) skillsEditedOutside(ctx context.Context, changed []string, person string) {
	var names []string
	for _, p := range changed {
		if name := wiki.SkillOfFile(p); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	for _, name := range names {
		if err := m.endTrialAsKept(ctx, name, person, "changed by hand"); err != nil {
			m.logger.Error("end a skill's trial", "skill", name, "err", err)
		}
	}
}

// rollbackBy answers the maintainer's rollback_skill: a skill its team
// owns, on trial, goes back to the version before.
func (m *TurnManager) rollbackBy(ctx context.Context, at *activeTurn, args upkeepArgs) (string, error) {
	name, reason := strings.TrimSpace(args.Skill), strings.TrimSpace(args.Reason)
	if name == "" || reason == "" {
		return "", errors.New("name the skill, as skill, and say what went worse with the change, as reason")
	}
	if !slices.Contains(at.upkeep.owned, name) {
		return "", fmt.Errorf("%s is not a skill this project's team owns: its own team's maintainer, or a person, rolls it back", name)
	}
	trial, err := m.rollbackSkill(ctx, name, agentActor(at.agent.Runtime, at.spec.Model), reason)
	if err != nil {
		return "", err
	}
	m.wikis.notify("", at.thread.RoomID)
	return fmt.Sprintf("Rolled %s back to the version before its trial (%s), with your reason in the library's log. "+
		"Record what went wrong as a Pattern page if it is not there yet.", name, shortSHA(trial.BaseSHA)), nil
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
