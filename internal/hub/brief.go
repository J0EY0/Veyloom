package hub

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// briefStore is the slice of Store the brief builder reads.
type briefStore interface {
	GetMessage(ctx context.Context, id string) (store.Message, error)
	GetUser(ctx context.Context, id string) (store.User, error)
	GetMember(ctx context.Context, id string) (store.Member, error)
	GetAgent(ctx context.Context, id string) (store.Agent, error)
	ListRoomMembers(ctx context.Context, roomID string) ([]store.Member, error)
	RoomProject(ctx context.Context, roomID string) (store.Project, error)
	RoomPosition(ctx context.Context, roomID string) (int64, error)
	RoomNews(ctx context.Context, q store.NewsQuery) ([]store.RoomNewsItem, int, error)
	TopicNews(ctx context.Context, q store.NewsQuery, exceptThreadID string) ([]store.TopicNewsItem, int, error)
	ThreadNews(ctx context.Context, threadID string, q store.NewsQuery) ([]store.Message, int, error)
	ListThreadTurns(ctx context.Context, threadID string) ([]store.Turn, error)
	// The member's reminders not yet due, and the topics they are in.
	ListMemberPendingReminders(ctx context.Context, memberID string) ([]store.Reminder, error)
	GetThread(ctx context.Context, id string) (store.Thread, error)
}

// briefLimits caps the parts of a brief. What a cap leaves out is counted
// in the brief, so the agent never takes a cut conversation for a whole one.
type briefLimits struct {
	// Thread caps the messages of the topic the turn is in.
	Thread int
	// Room caps the top-level messages new to the session.
	Room int
	// Topics caps the other topics listed as having news.
	Topics int
	// WikiPages caps the pages of the project wiki's catalog a brief lists,
	// all of them or those changed since the session last looked.
	WikiPages int
	// Resident caps, in characters, the resident pages a brief carries in
	// full.
	Resident int
}

// briefBuilder composes the prompt an agent receives for a turn (design.md
// 5.2, 5.23.1). A member keeps one session with its runtime for the whole
// room, so a brief does not repeat what the session has read: it tells
// only what is new since the session last looked, as far as the session's
// reading positions say.
//
//   - The member's standing instructions (standing.go) are the system
//     prompt of a runtime that takes one with every run. One that fixes it
//     when a session starts gets them in a brief, when its session has not
//     seen them as they are.
//   - The parts that change now and then (briefparts.go), each when the
//     session has not seen it as it is: what the project is, the memories,
//     who is in the chat, the others' branches, the skills installed for
//     the member, the bundles the wiki mounts, the resident pages.
//   - What changes by the turn: whether the session is new, who is at work,
//     how many more turns agents may wake, whose work this is.
//   - The wiki's catalog, all of it the first time and after a compaction,
//     otherwise the pages changed since; and the pages that may bear on
//     the topic.
//   - New top-level messages of the room.
//   - Other topics with new replies, one line each: a directory, not the
//     text.
//   - The topic the turn is in: all of it the first time the session is
//     there (and again after a compaction), otherwise only what is new.
//
// A session that has read nothing gets the whole story from the same code:
// there is no separate "full brief".
type briefBuilder struct {
	store  briefStore
	limits briefLimits
	// attachmentDir turns an attachment's stored path into one the runtime
	// can open.
	attachmentDir string
	// wikis are the projects' wikis; nil when the hub keeps none, and the
	// brief then says nothing of a wiki.
	wikis *wikiShelf
	// trialUses is how many turns keep a change to a skill.
	trialUses int
	// now is the clock, which says how long the members at work have been.
	now func() time.Time
}

func newBriefBuilder(store briefStore, limits briefLimits, attachmentDir string) *briefBuilder {
	return &briefBuilder{store: store, limits: limits, attachmentDir: attachmentDir, now: time.Now}
}

// briefInput is what a brief is made for.
type briefInput struct {
	Member store.Member
	// Runtime names the member's runtime, which says where its standing
	// instructions go (runtime.TraitsOf).
	Runtime  string
	Thread   store.Thread
	Triggers []store.Message
	// Session is the session the turn runs in; its reading positions decide
	// what is new. The zero session has read nothing.
	Session store.MemberSession
	// NewSession is set when the turn starts a session that replaces an
	// earlier one, to why that one ended. The agent is told, so it knows
	// that what follows is all it has and does not take the turn for a
	// continuation of things it no longer remembers.
	NewSession store.SessionEndReason
	// Stopped is the turn a person cancelled asking for the new session,
	// when that is why the earlier one ended (design.md 5.23.8).
	Stopped *stoppedTurn
	// Skills are the library's skills the turn is given, which the agent
	// may improve as it uses them (design.md 5.15); BuiltinSkills
	// Veyloom's own, which every agent has (5.23.6).
	Skills        []string
	BuiltinSkills []string
	// Busy are the room's other members at work as the brief is put
	// together (see busyIn).
	Busy []busyMember
	// Dir is where the member works this turn: its checkout, or the git
	// worktree of its own (design.md 5.21).
	Dir string
	// Relays is how the turn's piece of work stands against the limit on
	// agents waking one another (design.md 5.22); nil for a turn that
	// cannot wake anyone.
	Relays *relaysLeft
	// HandedOn is what came of the work the member handed on, on the turn
	// it sums that work up in; HandedBy whose work a woken turn is part of
	// (handedon.go).
	HandedOn []handedResult
	HandedBy *handedBy
}

// brief is a composed prompt and the positions it was taken at: once the
// session has taken the brief in, that is how far it has read.
type brief struct {
	Prompt string
	// Position is where the room stood, as messages.seq.
	Position int64
	// Wiki is where the project wiki stood (wiki.Bundle.Latest); zero when
	// the brief showed no wiki.
	Wiki time.Time
	// Leads says the member is the project's leader, whose turns get the
	// tool for writing down how worktrees are got ready.
	Leads bool
	// Standing is the member's standing instructions, which a runtime that
	// takes its system prompt with every run is given there.
	Standing string
	// Parts is what the brief showed, or left out as seen, of its parts
	// that change now and then: what the session has seen of them once it
	// takes the brief in.
	Parts map[string]string
}

// Build renders the brief for one turn.
func (b *briefBuilder) Build(ctx context.Context, in briefInput) (brief, error) {
	position, err := b.store.RoomPosition(ctx, in.Thread.RoomID)
	if err != nil {
		return brief{}, fmt.Errorf("brief: %w", err)
	}
	project, err := b.store.RoomProject(ctx, in.Thread.RoomID)
	if err != nil {
		return brief{}, fmt.Errorf("brief: %w", err)
	}
	members, err := b.store.ListRoomMembers(ctx, in.Thread.RoomID)
	if err != nil {
		return brief{}, fmt.Errorf("brief: %w", err)
	}
	w := &briefWriter{
		names:         newNameResolver(b.store),
		attachmentDir: b.attachmentDir,
		addressed:     make(map[string]bool, len(in.Triggers)),
		written:       make(map[string]bool),
	}
	for _, t := range in.Triggers {
		w.addressed[t.ID] = true
	}

	bundle := b.projectWiki(ctx, project, in.Thread.RoomID)
	standing := b.standing(in, project)
	parts := newBriefParts(in.Session.BriefSeen)
	// The memory is carried whole with the memories, not listed again.
	listed := map[string]bool{wiki.MemoryPath: true}
	b.header(ctx, w, in, project, members, bundle, standing, parts, listed)
	topic, err := b.readTopic(ctx, in, position)
	if err != nil {
		return brief{}, err
	}
	wikiAt := b.wiki(ctx, w, in, bundle, members, topic, listed)
	news := store.NewsQuery{RoomID: in.Thread.RoomID, After: in.Session.RoomSeen, UpTo: position, SessionID: in.Session.ID}
	// What the agent is to answer comes last, where it reads it last: in
	// the topic when it was asked there, in the room when it was asked
	// there and its reply is what opens the topic.
	if topic.opening() {
		if err := b.topicNews(ctx, w, news, in.Thread.ID); err != nil {
			return brief{}, err
		}
		topic.write(ctx, w)
		if err := b.roomNews(ctx, w, news); err != nil {
			return brief{}, err
		}
	} else {
		if err := b.roomNews(ctx, w, news); err != nil {
			return brief{}, err
		}
		if err := b.topicNews(ctx, w, news, in.Thread.ID); err != nil {
			return brief{}, err
		}
		topic.write(ctx, w)
	}
	// A trigger no part showed (it should not happen, and must not cost
	// the agent its question if it does).
	var missed []store.Message
	for _, t := range in.Triggers {
		if !w.written[t.ID] {
			missed = append(missed, t)
		}
	}
	if len(missed) > 0 {
		w.section("Addressed to you:")
		for _, t := range missed {
			w.message(ctx, t, "")
		}
	}
	if line := coAskedLine(in.Member, in.Triggers, members); line != "" {
		w.sb.WriteString("\n" + line)
	}
	if line := dispatchLine(in.Member, in.Triggers, project, members); line != "" {
		w.sb.WriteString("\n" + line)
	}
	if len(in.HandedOn) > 0 {
		w.sb.WriteString("\n" + handedOnSection(in.HandedOn))
	}
	return brief{
		Prompt: strings.TrimLeft(strings.TrimRight(w.sb.String(), "\n")+"\n", "\n"), Position: position, Wiki: wikiAt,
		Leads: in.Member.ID == project.LeaderID, Standing: standing, Parts: parts.now,
	}, nil
}

// dispatchLine tells the leader that a person's message to the room named
// no member, among several, and so came to it to take on or hand on
// (design.md 4.2). Nothing for another member, or a message that named one.
func dispatchLine(member store.Member, triggers []store.Message, project store.Project, members []store.Member) string {
	if member.ID != project.LeaderID {
		return ""
	}
	// With every other member gone or turned off, there is no one to hand
	// it on to: it is the leader's, as the only member's would be.
	others := slices.ContainsFunc(members, func(m store.Member) bool { return m.ID != member.ID && m.Enabled && !m.Removed() })
	// What names anyone at all is theirs; the router hands the leader none of it.
	unaddressed := slices.ContainsFunc(triggers, func(t store.Message) bool {
		return t.SenderKind == store.SenderUser && t.ThreadID == "" && len(t.Mentions) == 0
	})
	if !others || !unaddressed {
		return ""
	}
	return "The person addressed this to no member, so it came to you as the project's leader: take it yourself if it is small or yours; " +
		"hand it on to the member it fits with " + runtime.MessageToolSend + ", saying why; ask the person if you cannot tell which.\n"
}

// coAskedLine tells a member a person asked in the same message as other
// members that the parts are theirs to share out: each does the one
// addressed to it, now, and waits for none of the others unless asked to.
// Nothing when no person's message asked another member too.
func coAskedLine(member store.Member, triggers []store.Message, members []store.Member) string {
	var others []string
	for _, t := range triggers {
		if t.SenderKind != store.SenderUser {
			continue
		}
		for _, mention := range t.Mentions {
			if mention.Kind != store.MentionAgent || mention.ID == member.ID {
				continue
			}
			i := slices.IndexFunc(members, func(m store.Member) bool { return m.ID == mention.ID })
			if i >= 0 && !slices.Contains(others, members[i].DisplayName) {
				others = append(others, members[i].DisplayName)
			}
		}
	}
	if len(others) == 0 {
		return ""
	}
	who := andList(others)
	return fmt.Sprintf("The person asked %s in the same message as you: each of you does the part addressed to it, the words after its name. "+
		"Do yours now, and do not wait for %s unless the person asked you to.\n", who, who)
}

// header writes what a brief opens with: the standing instructions of a
// runtime that fixes its system prompt when a session starts, whether the
// session is new, and around what changes by the turn, the parts that
// change now and then, each when the session has not seen it as it is.
// bundle is the project's wiki, nil when there is none to show; listed
// takes the pages the brief carries whole.
func (b *briefBuilder) header(ctx context.Context, w *briefWriter, in briefInput, project store.Project, members []store.Member, bundle *wiki.Bundle, standing string, parts *briefParts, listed map[string]bool) {
	if !runtime.TraitsOf(in.Runtime).SystemPromptEachRun {
		parts.put(w, partStanding, standing)
	}
	switch {
	case in.NewSession == store.SessionCancelled:
		w.sb.WriteString(stoppedLine(in.Stopped))
	case in.NewSession != "":
		fmt.Fprintf(&w.sb, "\nThis is a new session: your earlier session in this project could not be continued (%s), so you do not remember your earlier turns here. What follows is the hub's record; rely on that, and on the repository, rather than on memory.\n", sessionEndPhrase(in.NewSession))
	}
	parts.put(w, partAbout, aboutText(project))
	if b.wikis != nil {
		if text, read := b.wikis.memoriesText(ctx, bundle); read {
			parts.put(w, partMemories, text)
		} else {
			parts.keep(partMemories)
		}
	}
	parts.put(w, partMembers, b.membersText(ctx, in, project, members))
	parts.put(w, partBranches, branchesText(in, members))
	if text, read := b.remindersText(ctx, in.Member); read {
		parts.put(w, partReminders, text)
	} else {
		parts.keep(partReminders)
	}
	b.atWork(w, in.Busy)
	if in.Relays != nil && in.Relays.Limit > 0 {
		w.sb.WriteString(relaysLeftLine(*in.Relays))
	}
	if in.HandedBy != nil {
		w.sb.WriteString(handedByLine(*in.HandedBy))
	}
	// The skills as the runtime gets them this turn, those of a library
	// that could not be read left out of both.
	parts.put(w, partSkills, skillsText(in.BuiltinSkills, in.Skills))
	if b.wikis != nil {
		if mounts := b.wikis.mounts(ctx, project); mountsRead(mounts) {
			parts.put(w, partMounts, mountsText(mounts))
		} else {
			parts.keep(partMounts)
		}
	}
	if bundle != nil {
		parts.put(w, partResident, b.residentText(bundle, listed))
	} else {
		parts.keep(partResident)
	}
	parts.unchanged(w)
}

// aboutText is what the project is, as its people describe it.
func aboutText(project store.Project) string {
	about := strings.TrimSpace(project.Description)
	if about == "" {
		return ""
	}
	return "\nAbout the project:\n" + about + "\n"
}

// membersText names who is in the chat, each with what it is for.
func (b *briefBuilder) membersText(ctx context.Context, in briefInput, project store.Project, members []store.Member) string {
	var sb strings.Builder
	sb.WriteString("\nIn this chat (mention one as @Name to hand something over):\n")
	for _, m := range members {
		if m.Removed() || !m.Enabled {
			continue
		}
		line := "- " + m.DisplayName
		var marks []string
		if m.ID == in.Member.ID {
			marks = append(marks, "you")
		}
		if m.ID == project.LeaderID {
			// The one the others' work waits on to set the project up
			// (docs/design.md 5.21).
			marks = append(marks, "the leader")
		}
		if len(marks) > 0 {
			line += " (" + strings.Join(marks, ", ") + ")"
		}
		// The first line of the role card says what the member is for; the
		// card itself is the agent's own system prompt and not repeated.
		if agent, err := b.store.GetAgent(ctx, m.AgentID); err == nil {
			if role := firstLine(agent.RoleCard); role != "" {
				line += ": " + excerpt(role, roleExcerpt)
			}
		}
		sb.WriteString(line + "\n")
	}
	return sb.String()
}

// branchesText names the branches the other members work on, to a member
// in a worktree of its own (design.md 5.21): it builds on one by merging.
func branchesText(in briefInput, members []store.Member) string {
	if !worksInOwnWorktree(in) {
		return ""
	}
	var others []string
	for _, m := range members {
		if m.ID != in.Member.ID && m.Branch != "" && !m.Removed() {
			others = append(others, fmt.Sprintf("%s (%s)", m.Branch, m.DisplayName))
		}
	}
	if len(others) == 0 {
		return ""
	}
	return fmt.Sprintf("\nThe others' work is on their branches of the same repository: %s. To build on what one of them committed, merge its branch into yours (git merge %s, say).\n",
		strings.Join(others, ", "), strings.SplitN(others[0], " ", 2)[0])
}

// skillsText names the skills the turn is given: Veyloom's own, which
// every agent has (design.md 5.23.6), and those installed for the agent
// from the library (5.15).
func skillsText(builtin, installed []string) string {
	var b strings.Builder
	if len(builtin) > 0 {
		b.WriteString("\nVeyloom's own skills, which every agent has: " + strings.Join(builtin, ", ") + ".")
	}
	if len(installed) > 0 {
		b.WriteString("\nInstalled for you from the skill library: " + strings.Join(installed, ", ") + ".")
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String() + "\n"
}

// mountsRead reports whether every bundle the project's wiki mounts could
// be read.
func mountsRead(mounts []mountedWiki) bool {
	return !slices.ContainsFunc(mounts, func(m mountedWiki) bool { return m.err != nil })
}

// mountsText names the bundles the project's wiki mounts.
func mountsText(mounts []mountedWiki) string {
	if line := mountsLine(mounts); line != "" {
		return "\n" + line + "\n"
	}
	return ""
}

// relaysLeftLine says how many more turns agents may wake in the piece of
// work the turn belongs to (design.md 5.22).
func relaysLeftLine(r relaysLeft) string {
	return fmt.Sprintf("\nAgents may wake %d more turns of one another in the piece of work this turn belongs to before it waits for the person.\n", max(r.Limit-r.Woken, 0))
}

// Parts of the wiki a brief shows beside the capped ones.
const (
	// wikiRelated caps the pages listed as bearing on the topic.
	wikiRelated = 5
	// topicFiles caps the files of the topic looked for in the wiki.
	topicFiles = 30
	// relevanceText caps the text the related pages are looked for by.
	relevanceText = 4000
	// descriptionExcerpt is how much of a page's description a list shows.
	descriptionExcerpt = 200
)

// projectWiki opens the project's wiki for the brief. Pages edited outside
// Veyloom since it was last looked at are news like any other, so they are
// picked up first. It is nil when the hub keeps no wikis, or when this one
// cannot be read: that costs the brief its wiki, not the turn.
func (b *briefBuilder) projectWiki(ctx context.Context, project store.Project, roomID string) *wiki.Bundle {
	if b.wikis == nil {
		return nil
	}
	bundle, err := b.wikis.project(ctx, project)
	if err != nil {
		b.wikis.logger.Warn("brief: open the project wiki", "project", project.ID, "err", err)
		return nil
	}
	b.wikis.sync(ctx, bundle, project.ID, roomID)
	return bundle
}

// wiki writes what the brief shows of the project wiki (design.md 5.2) and
// returns where the wiki stood, zero when it showed none.
func (b *briefBuilder) wiki(ctx context.Context, w *briefWriter, in briefInput, bundle *wiki.Bundle, members []store.Member, topic topicPart, listed map[string]bool) time.Time {
	if bundle == nil {
		return time.Time{}
	}
	at := bundle.Latest()
	b.wikiCatalog(w, bundle, in.Session.WikiSeen, listed)
	b.relatedPages(ctx, w, bundle, b.relevance(ctx, in, members, topic), listed)
	return at
}

// residentText is the pages a session carries whole, as many as fit; those
// that do not are named so the agent can read them. listed takes the ones
// carried: the catalog does not list them again. One that did not fit is
// the catalog's like any other page, which tells the session when it
// changes, as naming it here would not.
func (b *briefBuilder) residentText(bundle *wiki.Bundle, listed map[string]bool) string {
	pages := bundle.Resident()
	if len(pages) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\nResident pages of the project wiki, carried whole whenever they change:\n")
	room := b.limits.Resident
	var left []string
	for _, p := range pages {
		text := p.ResidentText()
		// In order: what does not fit ends the part, so a page is never
		// cut and the ones carried are the ones confirmed last.
		if n := utf8.RuneCountInString(text); len(left) == 0 && n <= room {
			room -= n
			sb.WriteString(text)
			listed[p.Path] = true
			continue
		}
		left = append(left, p.Path)
	}
	if len(left) > 0 {
		fmt.Fprintf(&sb, "\n(%s did not fit here; read_wiki has %s: %s)\n", count(len(left), "more resident page"), pronoun(len(left)), strings.Join(left, ", "))
	}
	return sb.String()
}

// wikiCatalog lists the wiki's pages: all of them to a session that has
// not seen the catalog, else the ones changed since it did. A long catalog
// keeps the pages changed last.
func (b *briefBuilder) wikiCatalog(w *briefWriter, bundle *wiki.Bundle, seen *time.Time, listed map[string]bool) {
	var pages []wiki.Summary
	var heading string
	if seen == nil {
		for _, s := range bundle.Pages() {
			if s.Status != okf.Deprecated && !listed[s.Path] {
				pages = append(pages, s)
			}
		}
		heading = "The project wiki's pages (read one with read_wiki):"
		if total := len(pages); total > b.limits.WikiPages {
			slices.SortStableFunc(pages, func(x, y wiki.Summary) int { return y.Modified.Compare(x.Modified) })
			pages = pages[:b.limits.WikiPages]
			slices.SortFunc(pages, func(x, y wiki.Summary) int { return strings.Compare(x.Path, y.Path) })
			heading = fmt.Sprintf("The project wiki's pages, the %d changed last of %d (search_wiki finds the others; read one with read_wiki):", len(pages), total)
		}
	} else {
		for _, s := range bundle.Changed(*seen) {
			if !listed[s.Path] {
				pages = append(pages, s)
			}
		}
		heading = "Pages of the project wiki added or changed since you last looked:"
	}
	if len(pages) == 0 {
		return
	}
	w.section(heading)
	more := 0
	if len(pages) > b.limits.WikiPages {
		pages, more = pages[:b.limits.WikiPages], len(pages)-b.limits.WikiPages
	}
	for _, s := range pages {
		listed[s.Path] = true
		w.sb.WriteString("   " + pageLine(s, descriptionExcerpt) + "\n")
	}
	if more > 0 {
		fmt.Fprintf(&w.sb, "   (and %d more; search_wiki finds them)\n", more)
	}
}

// relevance is what the turn is about, for finding the pages that bear on
// it: the question, and the first time the session is in the topic also
// what the topic is and the files its turns changed. Later turns in the
// topic look only at what they are asked, so the same pages are not listed
// every turn for the topic alone.
func (b *briefBuilder) relevance(ctx context.Context, in briefInput, members []store.Member, topic topicPart) wiki.Relevance {
	asked := in.Triggers
	if !topic.been {
		asked = append([]store.Message{topic.root}, asked...)
	}
	var text []string
	said := map[string]bool{}
	for _, m := range asked {
		if !said[m.ID] {
			said[m.ID] = true
			text = append(text, m.Body)
		}
	}
	r := wiki.Relevance{Text: excerpt(strings.Join(text, "\n"), relevanceText)}
	if topic.been {
		return r
	}
	turns, err := b.store.ListThreadTurns(ctx, in.Thread.ID)
	if err != nil {
		return r
	}
	var roots []string
	for _, m := range members {
		if m.RepoPath != "" {
			roots = append(roots, filepath.Clean(m.RepoPath))
		}
		// A worktree's files are named as the checkout names them.
		if m.WorkDir != "" {
			roots = append(roots, filepath.Clean(m.WorkDir))
		}
	}
	// The deepest repository first, for one inside another.
	slices.SortFunc(roots, func(x, y string) int { return cmp.Compare(len(y), len(x)) })
	seen := map[string]bool{}
	for i := len(turns) - 1; i >= 0 && len(r.Paths) < topicFiles; i-- {
		for _, f := range turns[i].FilesChanged {
			if f = repoPath(f, roots); f != "" && !seen[f] && len(r.Paths) < topicFiles {
				seen[f] = true
				r.Paths = append(r.Paths, f)
			}
		}
	}
	return r
}

// repoPath is a changed file as its path in the repository, which is how
// a page names it. Machines name a file in the folder a turn works in
// relative to it (machine/turns.go); a turn's absolute path, from before
// that or of a file elsewhere, is made relative to a member's repository
// it is under, or else keeps its last three parts.
func repoPath(p string, roots []string) string {
	if !filepath.IsAbs(p) {
		return filepath.ToSlash(filepath.Clean(p))
	}
	for _, root := range roots {
		if rel, err := filepath.Rel(root, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	parts := strings.Split(filepath.ToSlash(filepath.Clean(p)), "/")
	return strings.Join(parts[max(len(parts)-3, 1):], "/")
}

// relatedPages lists the pages that may bear on the turn, beyond the ones
// the brief listed already.
func (b *briefBuilder) relatedPages(ctx context.Context, w *briefWriter, bundle *wiki.Bundle, r wiki.Relevance, listed map[string]bool) {
	var related []wiki.Hit
	for _, h := range bundle.Relevant(r, wikiRelated+len(listed)) {
		if !listed[h.Path] && len(related) < wikiRelated {
			related = append(related, h)
		}
	}
	if len(related) == 0 {
		return
	}
	w.section("Pages of the project wiki that may bear on this:")
	for _, h := range related {
		w.sb.WriteString("   " + pageLine(h.Summary, descriptionExcerpt) + "\n")
	}
}

// count is n things, in words: "1 more page", "3 more pages".
func count(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

func pronoun(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

// roomNews writes the top-level messages new to the session.
func (b *briefBuilder) roomNews(ctx context.Context, w *briefWriter, q store.NewsQuery) error {
	q.Limit = b.limits.Room
	items, total, err := b.store.RoomNews(ctx, q)
	if err != nil {
		return fmt.Errorf("brief: %w", err)
	}
	if len(items) == 0 {
		return nil
	}
	w.section(fmt.Sprintf("New in the room since you last looked%s:", leftOut(total-len(items), "message")))
	for _, it := range items {
		tag := ""
		if it.TopicNumber > 0 {
			tag = fmt.Sprintf("#%d ", it.TopicNumber)
		}
		w.message(ctx, it.Message, tag)
	}
	return nil
}

// topicNews lists the other topics with replies new to the session: a
// directory. The text is the agent's to fetch when it wants it.
func (b *briefBuilder) topicNews(ctx context.Context, w *briefWriter, q store.NewsQuery, current string) error {
	q.Limit = b.limits.Topics
	topics, total, err := b.store.TopicNews(ctx, q, current)
	if err != nil {
		return fmt.Errorf("brief: %w", err)
	}
	if len(topics) == 0 {
		return nil
	}
	w.section("Other topics with news:")
	for _, t := range topics {
		replies := "replies"
		if t.NewCount == 1 {
			replies = "reply"
		}
		fmt.Fprintf(&w.sb, "   #%d %q: %d new %s, the last from %s: %q\n",
			t.Number, excerpt(topicTitle(t.Root), titleExcerpt), t.NewCount, replies, w.names.of(ctx, t.Last), excerpt(t.Last.Body, lineExcerpt))
	}
	if more := total - len(topics); more > 0 {
		fmt.Fprintf(&w.sb, "   (and %d more)\n", more)
	}
	return nil
}

// topicPart is the topic a turn is in, as its brief shows it: all of it
// the first time the session is briefed there, otherwise what is new.
type topicPart struct {
	number int
	root   store.Message
	// opener is the room message an agent's reply opened the topic to
	// answer; set when the topic is told in full and it has one.
	opener  *store.Message
	replies []store.Message
	// total counts the replies that matched, shown or not.
	total int
	// been says the session was briefed in the topic before.
	been bool
}

// readTopic fetches the part of the turn's topic its session has not read.
func (b *briefBuilder) readTopic(ctx context.Context, in briefInput, position int64) (topicPart, error) {
	root, err := b.store.GetMessage(ctx, in.Thread.RootMessageID)
	if err != nil {
		return topicPart{}, fmt.Errorf("brief: %w", err)
	}
	seen, been := in.Session.ThreadSeen[in.Thread.ID]
	q := store.NewsQuery{UpTo: position, Limit: b.limits.Thread}
	if been {
		// What the session said here itself it remembers untold.
		q.After, q.SessionID = seen, in.Session.ID
	}
	replies, total, err := b.store.ThreadNews(ctx, in.Thread.ID, q)
	if err != nil {
		return topicPart{}, fmt.Errorf("brief: %w", err)
	}
	part := topicPart{number: in.Thread.Number, root: root, replies: replies, total: total, been: been}
	if !been {
		part.opener = b.opener(ctx, in.Thread.ID, root)
	}
	return part, nil
}

// opener is the room message a topic was opened to answer, when an
// agent's reply to it is the topic's root. Told in full, the topic starts
// there: its first reply says little without the question, and a session
// that compacted what it had read of the room no longer has the question
// (the room is not told again after a compaction). Nil when there is none;
// one that cannot be read the brief goes without.
func (b *briefBuilder) opener(ctx context.Context, threadID string, root store.Message) *store.Message {
	if root.TurnID == "" {
		return nil
	}
	turns, err := b.store.ListThreadTurns(ctx, threadID)
	if err != nil {
		return nil
	}
	for _, t := range turns {
		if t.ID != root.TurnID || t.TriggerMessageID == "" {
			continue
		}
		m, err := b.store.GetMessage(ctx, t.TriggerMessageID)
		if err != nil || m.ThreadID != "" {
			return nil
		}
		return &m
	}
	return nil
}

// opening reports whether the topic has nothing in it yet: the agent was
// asked in the room, and its reply is what will open the topic.
func (t topicPart) opening() bool {
	return !t.been && len(t.replies) == 0 && t.root.Body == "" && len(t.root.Attachments) == 0
}

func (t topicPart) write(ctx context.Context, w *briefWriter) {
	name := fmt.Sprintf("#%d", t.number)
	if title := excerpt(topicTitle(t.root), titleExcerpt); title != "" {
		name += fmt.Sprintf(" %q", title)
	}
	switch {
	case t.opening():
		w.section(fmt.Sprintf("Your reply will open topic #%d.", t.number))
	case t.been && len(t.replies) == 0:
		w.section(fmt.Sprintf("You are in topic %s. Nothing new in it since you last looked.", name))
	case t.been:
		w.section(fmt.Sprintf("This topic, %s, new since you last looked%s:", name, leftOut(t.total-len(t.replies), "message")))
	default:
		w.section(fmt.Sprintf("This topic, %s, in full%s (oldest first):", name, leftOut(t.total-len(t.replies), "reply")))
		// Unless the room part of this brief just showed it.
		if t.opener != nil && !w.written[t.opener.ID] {
			w.message(ctx, *t.opener, "(asked in the room) ")
		}
		w.message(ctx, t.root, "")
	}
	for _, m := range t.replies {
		w.message(ctx, m, "")
	}
}

// Lengths of the excerpts a brief quotes.
const (
	roleExcerpt  = 160
	titleExcerpt = 80
	lineExcerpt  = 120
)

// leftOut says how many of a part's items a cap dropped, the oldest ones,
// as a parenthesis for the part's heading; nothing when none were.
func leftOut(n int, what string) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return fmt.Sprintf(" (1 earlier %s left out)", what)
	case what == "reply":
		return fmt.Sprintf(" (%d earlier replies left out)", n)
	default:
		return fmt.Sprintf(" (%d earlier %ss left out)", n, what)
	}
}

// topicTitle is what a topic is called by: the text of its root, or the
// name of the first file when the root is only files.
func topicTitle(root store.Message) string {
	if strings.TrimSpace(root.Body) != "" {
		return root.Body
	}
	if len(root.Attachments) > 0 {
		return root.Attachments[0].Filename
	}
	return ""
}

// firstLine is the first line of s that says something.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#")); line != "" {
			return line
		}
	}
	return ""
}

// excerpt is s on one line, cut to max bytes at a rune boundary.
func excerpt(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimSpace(s[:cut]) + "…"
}

// briefWriter renders the parts of a brief.
type briefWriter struct {
	sb            strings.Builder
	names         *nameResolver
	attachmentDir string
	// addressed are the messages the turn answers; written what was shown.
	addressed map[string]bool
	written   map[string]bool
}

// section starts a part under a heading.
func (w *briefWriter) section(heading string) {
	w.sb.WriteString("\n" + heading + "\n")
}

// message writes one message in full, marked when the turn answers it. tag
// goes before the sender: the number of the topic a room message heads.
func (w *briefWriter) message(ctx context.Context, m store.Message, tag string) {
	if m.Body == "" && len(m.Attachments) == 0 {
		// A topic root the agent has not filled in yet says nothing.
		return
	}
	marker, body := "   ", m.Body
	if w.addressed[m.ID] {
		// What the turn answers is told whole, however long.
		marker = ">> "
	} else if cut, more := cutBody(body, roomBodyMax); more > 0 {
		body = cut + cutNote(m.ID, more)
	}
	w.written[m.ID] = true
	fmt.Fprintf(&w.sb, "%s%s[%s] %s\n", marker, tag, w.names.of(ctx, m), body)
	// Files come as paths on this machine: the runtime reads them with
	// its own tools, images included.
	for _, a := range m.Attachments {
		fmt.Fprintf(&w.sb, "     (attached %s, %s, %d bytes: %s)\n", a.Filename, a.MediaType, a.Size, filepath.Join(w.attachmentDir, filepath.FromSlash(a.Path)))
	}
}

// nameStore is what resolving a sender's name reads.
type nameStore interface {
	GetUser(ctx context.Context, id string) (store.User, error)
	GetMember(ctx context.Context, id string) (store.Member, error)
}

// nameResolver turns message senders into display names, caching lookups
// for the duration of one brief.
type nameResolver struct {
	store nameStore
	cache map[string]string
}

func newNameResolver(store nameStore) *nameResolver {
	return &nameResolver{store: store, cache: make(map[string]string)}
}

func (n *nameResolver) of(ctx context.Context, m store.Message) string {
	switch m.SenderKind {
	case store.SenderUser:
		return n.lookup(m.UserID, func() (string, error) {
			u, err := n.store.GetUser(ctx, m.UserID)
			return u.Name, err
		})
	case store.SenderAgent:
		return n.lookup(m.MemberID, func() (string, error) {
			a, err := n.store.GetMember(ctx, m.MemberID)
			return a.DisplayName, err
		})
	default:
		return "system"
	}
}

func (n *nameResolver) lookup(id string, fetch func() (string, error)) string {
	if name, ok := n.cache[id]; ok {
		return name
	}
	name, err := fetch()
	if err != nil || name == "" {
		name = "unknown"
	}
	n.cache[id] = name
	return name
}

// memoryLine tells a member how to note what a person wants kept, in the
// memories turns use (design.md 5.19); nothing when none is.
func memoryLine(prefs store.MemoryPrefs) string {
	const rest = " What only matters to the conversation at hand is not noted."
	switch {
	case prefs.UsesProject() && prefs.UsesPersonal():
		return " When a person wants a way of working kept for later turns, a preference, a rule, a correction of how you went about something, note it with remember: in the project memory, or with scope personal when they mean every project; " +
			"forget takes out an entry that no longer holds." + rest + " Both memories reach you whole, in the brief whenever they change, so each entry is one short line."
	case prefs.UsesProject():
		return " When a person wants a way of working kept for later turns, a preference, a rule, a correction of how you went about something, note it with remember in the project memory; " +
			"forget takes out an entry that no longer holds." + rest + " The project memory reaches you whole, in the brief whenever it changes, so each entry is one short line."
	case prefs.UsesPersonal():
		return " When a person wants a way of working kept for later turns in every project, a preference, a rule, a correction of how you went about something, note it with remember, scope personal; " +
			"forget, scope personal, takes out an entry that no longer holds." + rest + " The personal memory reaches you whole, in the brief whenever it changes, so each entry is one short line."
	}
	return ""
}
