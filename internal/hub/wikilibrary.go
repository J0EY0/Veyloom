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
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// The skill library through the wiki tools (docs/design.md 5.10, 5.15),
// scope library. Patterns, how tasks go wrong or right, are written at
// once, by anyone. Skills come from people, and each is looked after by
// the team of one project, named in its metadata; the agents a skill is
// installed for, and that team's maintainer, change it at once, on trial
// (trials.go).

// wikiTopicIntro opens a project's wiki topic.
const wikiTopicIntro = "Wiki: the wiki maintainer keeps the project's wiki and this team's skills here."

func (m *TurnManager) writeLibrary(ctx context.Context, at *activeTurn, tw *turnWiki, args wikiArgs) (string, error) {
	if !slices.Contains(runtime.LibraryPageTypes, args.Type) {
		return "", fmt.Errorf("the skill library holds %s; a %s goes in the project's wiki, scope project", strings.Join(runtime.LibraryPageTypes, " and "), args.Type)
	}
	for name, v := range map[string]string{"slug": args.Slug, "title": args.Title, "description": args.Description, "body": args.Body} {
		if strings.TrimSpace(v) == "" {
			return "", fmt.Errorf("give the page a %s", name)
		}
	}
	if args.Type == "Skill" {
		return "", errSkillsArePeoples
	}
	slug := strings.TrimSpace(args.Slug)
	path := "/patterns/" + slug + ".md"
	if _, err := tw.bundle.Page(path); err == nil {
		return "", fmt.Errorf("there is a page at %s already; read it with read_wiki and change it with patch_wiki, both with scope library", path)
	}
	d := okf.New(args.Type)
	d.SetString(okf.KeyTitle, strings.TrimSpace(args.Title))
	d.SetString(okf.KeyDescription, strings.Join(strings.Fields(args.Description), " "))
	d.SetTags(args.Tags)
	for _, s := range args.Sources {
		d.AddSource(okf.Source{ID: s.ID, Resource: s.Resource, Title: s.Title})
	}
	for _, n := range args.Topics {
		if n > 0 {
			d.AddSource(okf.Source{ID: fmt.Sprintf("topic-%d", n), Resource: fmt.Sprintf("veyloom://rooms/%s/topics/%d", at.thread.RoomID, n), Title: fmt.Sprintf("Topic #%d of %s", n, tw.project.Name)})
		}
	}
	d.AddSource(okf.Source{ID: "veyloom-turn", Resource: "veyloom://turns/" + at.turn.ID, Title: fmt.Sprintf("%s in topic #%d of %s", at.member.DisplayName, at.thread.Number, tw.project.Name)})
	d.SetBody(args.Body)

	if _, err := tw.writer.Create(path, d); err != nil {
		return "", err
	}
	return fmt.Sprintf("Saved %s in the skill library. It goes into the library's history as part of this turn when the turn ends.", path), nil
}

// Skills come from people (docs/design.md 5.15): a person adds one and
// installs it for agents, which then improve it as they use it.
var (
	errSkillsArePeoples = errors.New("skills are added by people: a person writes or imports one into the skill library and installs it for agents. " +
		"Record the way of working as a Pattern page instead (write_wiki, type Pattern, scope library)")
	errSkillRetiredByPeople = errors.New("a skill is retired by a person, who takes it off the agents or removes it. " +
		"If it misleads, set it right with patch_wiki, or record why as a Pattern page")
)

func (m *TurnManager) patchLibrary(ctx context.Context, at *activeTurn, tw *turnWiki, args wikiArgs) (string, error) {
	page, err := tw.bundle.Page(args.Path)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("the skill library has no page %s; add one with write_wiki, scope library", args.Path)
	}
	if err != nil {
		return "", err
	}
	edited, hash, err := tw.bundle.Edited(page.Path, args.Edits)
	if err != nil {
		return "", err
	}
	if err := keepsConfirmations(page.Doc, edited); err != nil {
		return "", err
	}
	name := wiki.SkillOfFile(page.Path)
	if name == "" {
		// A pattern, which stays one: skills come from people.
		if edited.Type() == "Skill" {
			return "", errSkillsArePeoples
		}
		if _, err := tw.writer.Edit(page.Path, args.Edits, hash); err != nil {
			return "", err
		}
		if err := noteWhy(tw, page.Path, args.Reason); err != nil {
			return "", err
		}
		return fmt.Sprintf("Changed %s in the skill library. It goes into the library's history as part of this turn when the turn ends.", page.Path), nil
	}
	// The skill's SKILL.md, or a page of its folder: the skill changes.
	skill, err := tw.bundle.Page(wiki.SkillPath(name))
	if err != nil {
		return "", fmt.Errorf("%s is in the folder of a skill %s the library does not have", page.Path, name)
	}
	if !mayChangeSkill(at, tw, name, skill.Team) {
		return "", errSkillNotYours
	}
	if page.Type == "Skill" && edited.Metadata()[wiki.TeamKey] != page.Team {
		return "", errors.New("a skill's team is not changed by patching it: a person hands a skill over to another team in the skill library")
	}
	if page.Type != edited.Type() {
		return "", fmt.Errorf("%s stays a %s", page.Path, page.Type)
	}
	if _, err := tw.writer.Edit(page.Path, args.Edits, hash); err != nil {
		return "", err
	}
	if err := noteWhy(tw, page.Path, args.Reason); err != nil {
		return "", err
	}
	if err := m.startTrial(ctx, at, tw, name); err != nil {
		m.logger.Error("put a skill on trial", "skill", name, "turn", at.turn.ID, "err", err)
	}
	return fmt.Sprintf("Changed the skill %s. Every agent it is installed for uses the new version from its next turn. "+
		"It is on trial until %d turns have used it and ended well; a person, or the maintainer of its team, can roll it back. "+
		"If you have not yet, record what you found as a Pattern page (write_wiki, type Pattern, scope library).", name, m.trialUses), nil
}

func (m *TurnManager) deprecateLibrary(ctx context.Context, at *activeTurn, tw *turnWiki, args wikiArgs) (string, error) {
	page, err := tw.bundle.Page(args.Path)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("the skill library has no page %s", args.Path)
	}
	if err != nil {
		return "", err
	}
	if page.Status == okf.Deprecated {
		return "", fmt.Errorf("%s is deprecated already", page.Path)
	}
	successor := strings.TrimSpace(args.Successor)
	if successor != "" {
		next, err := tw.bundle.Page(successor)
		if err != nil || next.Path == page.Path || next.Status == okf.Deprecated {
			return "", fmt.Errorf("%s cannot take over from %s: it should be another current page of the skill library", successor, page.Path)
		}
		successor = next.Path
	}
	if page.Type == "Skill" {
		return "", errSkillRetiredByPeople
	}
	// A page of a skill's folder is part of the skill.
	name := wiki.SkillOfFile(page.Path)
	if name != "" {
		skill, err := tw.bundle.Page(wiki.SkillPath(name))
		if err != nil || !mayChangeSkill(at, tw, name, skill.Team) {
			return "", errSkillNotYours
		}
	}
	if strings.TrimSpace(args.Reason) == "" {
		return "", errors.New("say why the page no longer holds, as reason")
	}
	if _, err := tw.writer.Deprecate(page.Path, successor, args.Reason); err != nil {
		return "", err
	}
	if name != "" {
		if err := m.startTrial(ctx, at, tw, name); err != nil {
			m.logger.Error("put a skill on trial", "skill", name, "turn", at.turn.ID, "err", err)
		}
		return fmt.Sprintf("Deprecated %s, a page of the skill %s, which is on trial for it like any change to the skill.", page.Path, name), nil
	}
	return fmt.Sprintf("Deprecated %s in the skill library. It goes into the library's history as part of this turn when the turn ends.", page.Path), nil
}

// wikiTopic is a project's wiki topic in its chat, opened the first time
// something is put to it.
func (m *TurnManager) wikiTopic(ctx context.Context, project store.Project) (store.Thread, error) {
	if project.WikiThreadID != "" {
		return m.store.GetThread(ctx, project.WikiThreadID)
	}
	if project.MainRoomID == "" {
		fresh, err := m.store.GetProject(ctx, project.ID)
		if err != nil {
			return store.Thread{}, err
		}
		project = fresh
		if project.WikiThreadID != "" {
			return m.store.GetThread(ctx, project.WikiThreadID)
		}
	}
	root, err := m.store.CreateMessage(ctx, store.NewMessage{RoomID: project.MainRoomID, SenderKind: store.SenderSystem, Body: wikiTopicIntro})
	if err != nil {
		return store.Thread{}, err
	}
	thread, err := m.store.ThreadForMessage(ctx, root.ID)
	if err != nil {
		return store.Thread{}, err
	}
	set, err := m.store.SetProjectWikiThread(ctx, project.ID, thread.ID)
	if err != nil {
		return store.Thread{}, err
	}
	if !set {
		// Another opened it first: that one is the topic.
		fresh, err := m.store.GetProject(ctx, project.ID)
		if err != nil {
			return store.Thread{}, err
		}
		return m.store.GetThread(ctx, fresh.WikiThreadID)
	}
	// Announced once the project names it, with the topic it heads, as a
	// turn's root is: the room reads the project again on hearing of it.
	m.publish(Event{Kind: EventMessage, RoomID: root.Room, At: root.CreatedAt, Message: &root, Thread: &store.ThreadSummary{ID: thread.ID, Number: thread.Number}})
	return thread, nil
}
