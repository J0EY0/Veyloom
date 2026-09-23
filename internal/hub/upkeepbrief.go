package hub

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// An upkeep's brief (docs/design.md 5.12). It is not a chat turn's brief:
// nobody asked the maintainer anything. It says what to do, lists the
// turns to go over and the uses of the team's skills, and shows the wiki
// as a whole: every page, the latest changes and what a health check
// found.

// Limits of an upkeep's brief.
const (
	// upkeepCatalog caps the pages listed; a larger wiki lists the ones
	// changed last and says how many more there are.
	upkeepCatalog = 200
	// upkeepHistory is how many of the wiki's latest changes are shown.
	upkeepHistory = 10
	// upkeepListed caps the pages each finding of the health check names.
	upkeepListed = 20
)

// upkeepBrief puts the brief of an upkeep together.
func (m *TurnManager) upkeepBrief(ctx context.Context, up *upkeep, member store.Member, topic store.Thread) (string, error) {
	bundle, err := m.wikis.project(ctx, up.project)
	if err != nil {
		return "", fmt.Errorf("open the project wiki: %w", err)
	}
	// What was edited outside Veyloom counts like the rest.
	m.wikis.sync(ctx, bundle, up.project.ID, topic.RoomID)

	w := &briefWriter{}
	fmt.Fprintf(&w.sb, "You are %q, the wiki maintainer of the project %q. This is an upkeep, not a chat turn: no person waits on a reply, "+
		"and nobody will answer a question or approve a plan before you end, so do the work yourself rather than suggest it. "+
		"You go over what the project's chat did since the last upkeep, and what other projects did with the skills this project's team owns, "+
		"and keep the project wiki and those skills worth reading. Why now: %s.\n", member.DisplayName, up.project.Name, up.reason)
	if about := strings.TrimSpace(up.project.Description); about != "" {
		w.section("About the project:")
		w.sb.WriteString(about + "\n")
	}
	m.wikis.writeMemories(ctx, w, bundle)

	mounts := m.wikis.mounts(ctx, up.project)
	health := mountedHealth(bundle.Health(time.Now()), mounts)
	checks, due := m.pagesToCheck(ctx, up.project, bundle)
	prefs := m.wikis.memoryPrefs()
	upkeepSteps(w, up, prefs, !health.Empty(m.residentBudget), len(checks) > 0)
	fmt.Fprintf(&w.sb, "Your veyloom tools: %s; the wiki tools (%s, with scope library for the skill library); %sand for reading the chat %s.\n",
		strings.Join(runtime.UpkeepToolNames, ", "), strings.Join(runtime.WikiToolNames, ", "), memoryToolsLine(prefs), strings.Join(runtime.RoomToolNames, ", "))
	if mounted := mountsLine(mounts); mounted != "" {
		w.sb.WriteString(mounted + " Link to their pages where they bear on the wiki's; they are not yours to keep.\n")
	}

	w.section(fmt.Sprintf("Turns of this chat to go over (%d, oldest first):", len(up.turns)))
	if len(up.turns) == 0 {
		w.sb.WriteString("None: nothing new since the last upkeep. Look over the wiki as a whole instead.\n")
	}
	for _, t := range up.turns {
		w.sb.WriteString(upkeepTurnLine(t, false) + "\n")
	}
	if len(up.owned) > 0 {
		w.section(fmt.Sprintf("Turns of other projects that used this team's skills (%d):", len(up.uses)))
		if len(up.uses) == 0 {
			w.sb.WriteString("None since the last upkeep.\n")
		}
		for _, t := range up.uses {
			w.sb.WriteString(upkeepTurnLine(t, true) + "\n")
		}
		w.section("The skills this team owns in the skill library:")
		for _, name := range up.owned {
			fmt.Fprintf(&w.sb, "- %s: %s\n", name, wiki.SkillPath(name))
		}
		if len(up.trials) > 0 {
			w.section(fmt.Sprintf("Changes on trial to those skills (%d):", len(up.trials)))
			for _, trial := range up.trials {
				w.sb.WriteString(m.trialLine(ctx, trial) + "\n")
			}
		}
	}

	m.peoplePart(w, up)
	checksPart(w, checks, due, m.wikis.now())
	upkeepCatalogPart(w, bundle)
	m.upkeepHistoryPart(ctx, w, bundle)
	upkeepHealthPart(w, health, m.residentBudget)
	return w.sb.String(), nil
}

// trialLine tells one change on trial: whose, since when, and how the
// turns that used the skill since have ended.
func (m *TurnManager) trialLine(ctx context.Context, trial store.SkillTrial) string {
	line := fmt.Sprintf("- %s: changed by %s", trial.Skill, trial.ChangedBy)
	if trial.ProjectName != "" {
		line += fmt.Sprintf(" of project %q", trial.ProjectName)
	}
	line += " on " + trial.ChangedAt.Local().Format("2006-01-02 15:04")
	if trial.Changes > 1 {
		line += fmt.Sprintf(" (%s in this trial)", count(trial.Changes, "change"))
	}
	done, failed, err := m.store.SkillTrialUses(ctx, trial.Skill, trial.ChangedAt)
	if err != nil {
		return line
	}
	line += fmt.Sprintf("; since then %s ended well and %s failed, of the %d that keep it", count(done, "turn"), count(failed, "turn"), m.trialUses)
	return line
}

// upkeepSteps says what the upkeep is to do, only the steps there is
// something for: a step with nothing to do only draws the eye from the
// ones that have.
func upkeepSteps(w *briefWriter, up *upkeep, prefs store.MemoryPrefs, unhealthy, checks bool) {
	w.section("What to do, in this order:")
	var steps []string
	if len(up.turns)+len(up.uses) > 0 {
		steps = append(steps,
			"Read each turn listed below with read_turn; list_turns finds older ones. What a person said after a turn is often a correction, and outweighs what the agent concluded.",
			"Record in the project wiki, with write_wiki or by patch_wiki on the page that has it, everything those turns settled that later work needs and cannot read off the code: "+
				"a decision and why it was made, an agreed convention, a fact that was checked or that a person stated, a pitfall and the way around it, what a module is for; "+
				"and for a topic that came to an end, what it came to (a Topic page). A correction replaces what it corrects. Search first, so no page is written twice. "+
				"Cite the topics a page draws on (topics: [12]). Decisions and conventions take effect at once, like the rest; a change a person undid in the wiki's history stays undone unless something new calls for it. "+
				"This is what an upkeep is for: one that had turns to go over and recorded nothing has most likely missed something.")
	} else if len(up.news) == 0 {
		steps = append(steps, "There is nothing new to go over: read the wiki as a whole for what is out of date, contradicts another page, or is missing a link, and set it right.")
	}
	if len(up.news) > 0 {
		steps = append(steps, "Go over what people said and sent since the last upkeep, listed below, as you see fit: read what bears on the project with read_room and read_topic, "+
			"and open the files people sent with your own tools at the paths given (a model that takes no images cannot see a picture: say so rather than guess at it). "+
			"Record what later work needs: a decision, a convention or a fact a person stated, and what their files hold that matters. What a person says outweighs what an agent concluded. "+
			"Keep a file itself in the wiki with write_wiki's files, by its id, when it is worth having whole: a diagram, a spec, a data file; the page says what is in it.")
	}
	if step := memoryStep(prefs); step != "" && len(up.turns)+len(up.news) > 0 {
		steps = append(steps, step)
	}
	if checks {
		steps = append(steps, "Check each page under \"Pages to check again\" against the repository and what the chat said since it was last checked; its line says what calls for it, and read_turn reads the turn that changed a file. "+
			"Where a page no longer holds, set it right with patch_wiki, or deprecate it naming the page that takes over; where it holds as written, confirm it with confirm_wiki, which counts as checking it. "+
			"A page you neither change nor confirm stays due for the next upkeep.")
	}
	if unhealthy {
		steps = append(steps, "Set right what the health check below found: link orphan pages from the pages they belong with, mend or drop broken links, "+
			"bring stale pages up to date or deprecate them, shorten resident pages that no longer fit. "+
			"Where two pages name the same path and one bears on the other, link it from the other in a sentence that says how (it depends on it, it is the reason for it, it contradicts it); related_wiki shows how pages already connect.")
	}
	if len(up.owned) > 0 {
		steps = append(steps, "For the skills this team owns: where a turn that used one went wrong, or a person corrected it, write a Pattern page in the skill library (write_wiki, scope library) with the symptom, "+
			"the root cause, the exact commands and the fix, and set the skill right with patch_wiki (scope library): the change takes effect at once, on trial like any agent's, "+
			"and a change you only describe in your reply reaches no one. Name another project's turns in the page's body by project and topic (project Storefront, topic #3); "+
			"topics: [...] is for this chat's own. "+
			"A way of working that keeps coming back and holds beyond this repository goes in a Pattern page too; skills themselves are added by people.")
	}
	if len(up.trials) > 0 {
		steps = append(steps, "For each change on trial listed below: read the turns that used the skill since the change (read_turn) and what people said after them. "+
			"If the change made things worse, roll it back with rollback_skill, saying what went worse, and record why as a Pattern page; a change that goes well you leave alone: it is kept once enough turns have used it.")
	}
	steps = append(steps,
		"The wikis are no part of the repository: the wiki tools write them even where you may not change the repository. Change nothing in the repository and run nothing of it.",
		"Make every change with the wiki tools before you end: what you only describe in your reply is lost. Then end with a few lines on what you recorded and what you left for a person, in the language the team works in.")
	for i, step := range steps {
		fmt.Fprintf(&w.sb, "%d. %s\n", i+1, step)
	}
}

// upkeepTurnLine is one turn to go over: its id to read it by, where and
// by whom it ran, how it ended and what it touched.
func upkeepTurnLine(t store.UpkeepTurn, elsewhere bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- %s: ", t.ID)
	if elsewhere {
		fmt.Fprintf(&b, "project %q, ", t.ProjectName)
	}
	fmt.Fprintf(&b, "topic #%d %q, %s, %s", t.TopicNumber, excerpt(topicTitleOf(t.RootBody), topicTitleExcerpt), t.MemberName, t.Status)
	if t.Error != "" {
		fmt.Fprintf(&b, " (%s)", excerpt(firstLine(t.Error), upkeepErrorExcerpt))
	}
	fmt.Fprintf(&b, ", %s", t.StartedAt.Local().Format("2006-01-02 15:04"))
	if len(t.FilesChanged) > 0 {
		fmt.Fprintf(&b, "; changed %s", strings.Join(capList(t.FilesChanged, upkeepFiles), ", "))
	}
	if len(t.SkillsUsed) > 0 {
		fmt.Fprintf(&b, "; used the skills %s", strings.Join(t.SkillsUsed, ", "))
	}
	return b.String()
}

// Parts of a turn's line that are cut short.
const (
	topicTitleExcerpt  = 80
	upkeepErrorExcerpt = 160
	upkeepFiles        = 8
)

// topicTitleOf is a topic's title from the text heading it.
func topicTitleOf(root string) string {
	if title := firstLine(root); title != "" {
		return title
	}
	return "(untitled)"
}

// capList keeps the first n of items and says how many more there were.
func capList(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	return append(append([]string(nil), items[:n]...), fmt.Sprintf("%d more", len(items)-n))
}

// upkeepCatalogPart lists every page of the wiki, the ones changed last
// first when there are more than fit.
func upkeepCatalogPart(w *briefWriter, bundle *wiki.Bundle) {
	pages := bundle.Changed(time.Time{})
	w.section(fmt.Sprintf("The project wiki (%s, the last changed first):", count(len(pages), "page")))
	if len(pages) == 0 {
		w.sb.WriteString("Empty so far.\n")
		return
	}
	for i, s := range pages {
		if i == upkeepCatalog {
			fmt.Fprintf(&w.sb, "(%d more; search_wiki finds them)\n", len(pages)-i)
			break
		}
		line := fmt.Sprintf("- %s (%s", s.Path, s.Type)
		if s.Status == okf.Deprecated {
			line += ", deprecated"
		}
		line += "): " + s.Title
		if d := strings.TrimSpace(s.Description); d != "" {
			line += ": " + excerpt(d, descriptionExcerpt)
		}
		w.sb.WriteString(line + "\n")
	}
}

// upkeepHistoryPart shows the wiki's latest changes: who made them and
// what they touched.
func (m *TurnManager) upkeepHistoryPart(ctx context.Context, w *briefWriter, bundle *wiki.Bundle) {
	if !bundle.KeepsHistory() {
		return
	}
	commits, err := bundle.History(ctx, "", upkeepHistory)
	if err != nil {
		m.logger.Warn("upkeep brief: the wiki's history", "err", err)
		return
	}
	if len(commits) == 0 {
		return
	}
	w.section("The wiki's latest changes, newest first:")
	for _, c := range commits {
		fmt.Fprintf(&w.sb, "- %s, %s: %s\n", c.At.Local().Format("2006-01-02 15:04"), c.Author, c.Subject)
		for _, ch := range capChanges(c.Changes) {
			fmt.Fprintf(&w.sb, "  %s\n", ch)
		}
	}
}

// capChanges is what a commit did, page by page, cut short.
func capChanges(changes []wiki.Change) []string {
	var out []string
	for i, ch := range changes {
		if i == upkeepFiles {
			out = append(out, fmt.Sprintf("(%d more)", len(changes)-i))
			break
		}
		out = append(out, ch.Kind+": "+ch.Text)
	}
	return out
}

// upkeepHealthPart says what the health check found.
func upkeepHealthPart(w *briefWriter, h wiki.Health, budget int) {
	w.section("Health check:")
	if h.Empty(budget) {
		w.sb.WriteString("Nothing found.\n")
		return
	}
	paths := func(pages []wiki.Summary) string {
		out := make([]string, 0, len(pages))
		for _, s := range pages {
			out = append(out, s.Path)
		}
		return strings.Join(capList(out, upkeepListed), ", ")
	}
	if len(h.Orphans) > 0 {
		fmt.Fprintf(&w.sb, "- No other page links to: %s\n", paths(h.Orphans))
	}
	if len(h.Broken) > 0 {
		links := make([]string, 0, len(h.Broken))
		for _, l := range h.Broken {
			links = append(links, l.From+" links to "+l.To)
		}
		fmt.Fprintf(&w.sb, "- Links to pages that are not there: %s\n", strings.Join(capList(links, upkeepListed), "; "))
	}
	if len(h.Stale) > 0 {
		fmt.Fprintf(&w.sb, "- Past their stale_after: %s\n", paths(h.Stale))
	}
	if h.ResidentChars > budget {
		fmt.Fprintf(&w.sb, "- The resident pages come to %d characters, more than the %d every brief carries; the last of them are only named there.\n", h.ResidentChars, budget)
	}
	if len(h.Problems) > 0 {
		probs := make([]string, 0, len(h.Problems))
		for _, p := range h.Problems {
			probs = append(probs, fmt.Sprintf("%s: %s", p.Path, p.Message))
		}
		fmt.Fprintf(&w.sb, "- Files that break the wiki's format: %s\n", strings.Join(capList(probs, upkeepListed), "; "))
	}
	if len(h.Unlinked) > 0 {
		pairs := make([]string, 0, len(h.Unlinked))
		for _, p := range h.Unlinked {
			pairs = append(pairs, fmt.Sprintf("%s and %s both name %s", p.A, p.B, p.File))
		}
		fmt.Fprintf(&w.sb, "- Pages naming the same path of the repository that do not link each other: %s\n", strings.Join(pairs, "; "))
	}
}

// peoplePart lists what people said and sent since the last upkeep
// (docs/design.md 5.16): where and how much, not what, since the maintainer
// reads what it chooses; and the files, with the ids that keep them and the
// paths that open them.
func (m *TurnManager) peoplePart(w *briefWriter, up *upkeep) {
	if len(up.news) == 0 {
		return
	}
	w.section(fmt.Sprintf("What people said since the last upkeep (%s; read them with read_room and read_topic):", count(up.peopleMessages(), "message")))
	for _, n := range up.news {
		where := "the room itself"
		if n.Topic > 0 {
			where = fmt.Sprintf("topic #%d %q", n.Topic, excerpt(topicTitleOf(n.RootBody), topicTitleExcerpt))
		}
		line := fmt.Sprintf("- %s: %s", where, count(n.Messages, "message"))
		if n.Files > 0 {
			line += ", " + count(n.Files, "file")
		}
		w.sb.WriteString(line + ", the last " + n.LastAt.Local().Format("2006-01-02 15:04") + "\n")
	}
	if len(up.news) == upkeepPeopleNews {
		w.sb.WriteString("(Perhaps more: list_topics and read_room find the rest.)\n")
	}
	if len(up.files) == 0 {
		return
	}
	w.section("Files people sent (open one at its path; keep one in the wiki with write_wiki's files, by its id):")
	for _, f := range up.files {
		where := "in the room"
		if f.Topic > 0 {
			where = fmt.Sprintf("in #%d", f.Topic)
		}
		if f.Sender != "" {
			where += " by " + f.Sender
		}
		fmt.Fprintf(&w.sb, "- %s (%s, %s), %s, %s: file %s at %s\n", f.Filename, f.MediaType, byteSize(f.Size), where,
			f.CreatedAt.Local().Format("2006-01-02 15:04"), f.ID, filepath.Join(m.attachmentDir, filepath.FromSlash(f.Path)))
	}
	if len(up.files) == upkeepPeopleFiles {
		w.sb.WriteString("(Perhaps more: read_topic and read_room show every file with its id.)\n")
	}
}

// byteSize says how big a file is, in the unit that reads best.
func byteSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// upkeepChecks caps the pages an upkeep is to check again; the rest wait
// for the next.
const upkeepChecks = 10

// pageCheck is a page due to be checked again, and why.
type pageCheck struct {
	page   wiki.Summary
	review WikiReview
}

// pagesToCheck lists the first pages due to be checked again, in the order
// they are gone over, and how many are due. What cannot be worked out
// costs the upkeep its checks, not the upkeep.
func (m *TurnManager) pagesToCheck(ctx context.Context, project store.Project, bundle *wiki.Bundle) ([]pageCheck, int) {
	pages := bundle.Pages()
	reviews, err := wikiReviews(ctx, m.store, project.ID, pages, m.wikis.now())
	if err != nil {
		m.logger.Warn("find the wiki pages to check again", "project", project.ID, "err", err)
		return nil, 0
	}
	due := reviewOrder(pages, reviews)
	checks := make([]pageCheck, 0, min(len(due), upkeepChecks))
	for _, p := range due[:min(len(due), upkeepChecks)] {
		checks = append(checks, pageCheck{page: p, review: reviews[p.Path]})
	}
	return checks, len(due)
}

// checksPart lists the pages to check again, each with when it was last
// checked and what calls for it now.
func checksPart(w *briefWriter, checks []pageCheck, due int, now time.Time) {
	if len(checks) == 0 {
		return
	}
	heading := fmt.Sprintf("Pages to check again (%d, in this order):", due)
	if due > len(checks) {
		heading = fmt.Sprintf("Pages to check again (%d due; these %d first, in this order):", due, len(checks))
	}
	w.section(heading)
	for _, c := range checks {
		checked := c.page.CheckedAt()
		kind := c.page.Type
		if c.page.Carried() {
			kind += ", resident"
		}
		line := fmt.Sprintf("- %s %q (%s), last checked %s (%s): ", c.page.Path, c.page.Title, kind, checked.Local().Format("2006-01-02"), daysAgo(now.Sub(checked)))
		switch c.review.Why {
		case reviewChanged:
			where := "turn " + c.review.TurnID
			if c.review.TopicNumber > 0 {
				where = fmt.Sprintf("topic #%d (turn %s)", c.review.TopicNumber, c.review.TurnID)
			}
			line += fmt.Sprintf("%s was changed since, in %s on %s", c.review.File, where, c.review.ChangedAt.Local().Format("2006-01-02"))
		case reviewStale:
			line += fmt.Sprintf("its stale_after, %s, has come", c.page.StaleAfter.Local().Format("2006-01-02"))
		default:
			line += fmt.Sprintf("pages like it are checked every %d days", c.review.Every)
			if c.page.Carried() {
				line += " (resident ones twice as often)"
			}
		}
		w.sb.WriteString(line + "\n")
	}
	if due > len(checks) {
		fmt.Fprintf(&w.sb, "(%d more wait for a later upkeep.)\n", due-len(checks))
	}
}

// daysAgo says how long ago something was, in days.
func daysAgo(d time.Duration) string {
	switch days := int(d.Hours() / 24); days {
	case 0:
		return "today"
	case 1:
		return "1 day ago"
	default:
		return fmt.Sprintf("%d days ago", days)
	}
}

// memoryStep is the maintainer's step for the memories turns use (design.md
// 5.16, 5.19): what people said about how to work goes into them, folded
// and kept current. Nothing when no memory is used.
func memoryStep(prefs store.MemoryPrefs) string {
	const fold = "Fold entries that say the same into one, and forget what no longer holds or a person took back. "
	switch {
	case prefs.UsesProject() && prefs.UsesPersonal():
		return "Keep the memories, which every turn of every member carries whole. What a person said about how to work here, a preference, a rule, " +
			"a correction of how an agent went about something, goes into the project memory with remember (topic: the one they said it in), one short line each, unless it is there already; " +
			"what they meant for every project goes into the personal memory (scope personal). " + fold +
			"An entry of the project memory that holds in every project moves to the personal memory. What the project memory says the wiki should not keep, it does not keep."
	case prefs.UsesProject():
		return "Keep the project memory, which every turn of every member carries whole. What a person said about how to work here, a preference, a rule, " +
			"a correction of how an agent went about something, goes into it with remember (topic: the one they said it in), one short line each, unless it is there already. " + fold +
			"What the project memory says the wiki should not keep, it does not keep."
	case prefs.UsesPersonal():
		return "Keep the personal memory, which every turn of every member in every project carries whole. What a person said they want in every project, a preference, a rule, " +
			"a correction of how an agent went about something, goes into it with remember, scope personal (topic: the one they said it in), one short line each, unless it is there already. " + fold
	}
	return ""
}

// memoryToolsLine names the memory tools among the maintainer's, while it
// has them.
func memoryToolsLine(prefs store.MemoryPrefs) string {
	switch {
	case prefs.UsesProject() && prefs.UsesPersonal():
		return "the memory tools (" + strings.Join(runtime.MemoryToolNames, ", ") + ", with scope personal for the personal memory); "
	case prefs.UsesProject():
		return "the memory tools (" + strings.Join(runtime.MemoryToolNames, ", ") + ") for the project memory; "
	case prefs.UsesPersonal():
		return "the memory tools (" + strings.Join(runtime.MemoryToolNames, ", ") + ") with scope personal for the personal memory; "
	}
	return ""
}
