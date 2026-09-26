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
// 5.2). A member keeps one session with its runtime for the whole room, so
// a brief does not repeat what the session has read: it opens with a short
// fixed header, then tells only what is new since the session last looked,
// as far as the session's reading positions say.
//
//   - The header comes with every brief: who the agent is, the project, who
//     else is in the chat. It is small, and a runtime that compacts its
//     session would otherwise lose what was said once at the start.
//   - New top-level messages of the room.
//   - Other topics with new replies, one line each: a directory, not the
//     text.
//   - The topic the turn is in: all of it the first time the session is
//     there (and again after a compaction), otherwise only what is new.
//
// With a project wiki, the header is followed by what the brief shows of
// it: the resident pages in full, every turn; the catalog, all of it the
// first time and after a compaction, otherwise the pages changed since;
// and the pages that may bear on the topic.
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
	Member   store.Member
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
	// Skills are the library's skills the turn is given, which the agent
	// may improve as it uses them (design.md 5.15).
	Skills []string
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
	b.header(ctx, w, in, project, members, bundle)
	topic, err := b.readTopic(ctx, in, position)
	if err != nil {
		return brief{}, err
	}
	wikiAt := b.wiki(ctx, w, in, bundle, members, topic)
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
	if len(in.HandedOn) > 0 {
		w.sb.WriteString("\n" + handedOnSection(in.HandedOn))
	}
	return brief{Prompt: strings.TrimRight(w.sb.String(), "\n") + "\n", Position: position, Wiki: wikiAt, Leads: in.Member.ID == project.LeaderID}, nil
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

// header writes what every brief opens with. bundle is the project's
// wiki, nil when there is none to show.
func (b *briefBuilder) header(ctx context.Context, w *briefWriter, in briefInput, project store.Project, members []store.Member, bundle *wiki.Bundle) {
	fmt.Fprintf(&w.sb, "You are %q, an agent in the team chat of the project %q. Lines marked with >> are addressed to you; reply to them. "+
		"Write to the chat in the language its people write in, what you say as you work included, whatever language this brief is in.\n", in.Member.DisplayName, project.Name)
	if in.NewSession != "" {
		fmt.Fprintf(&w.sb, "\nThis is a new session: your earlier session in this project could not be continued (%s), so you do not remember your earlier turns here. What follows is the hub's record; rely on that, and on the repository, rather than on memory.\n", sessionEndPhrase(in.NewSession))
	}
	if about := strings.TrimSpace(project.Description); about != "" {
		w.section("About the project:")
		w.sb.WriteString(about)
		w.sb.WriteString("\n")
	}
	if b.wikis != nil {
		b.wikis.writeMemories(ctx, w, bundle)
	}

	w.section("In this chat (mention one as @Name to hand something over):")
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
		w.sb.WriteString(line + "\n")
	}
	workplace(w, in, project, members)
	b.atWork(w, in.Busy)
	// Said every time, like the rest of the header: a brief shows only what
	// is new, and this is how the agent gets at everything else.
	w.sb.WriteString("\nYou are shown what is new since you last looked. For anything else in this chat, such as earlier messages, another topic, or what an agent did in a turn, the commands it ran and what came of them, use your veyloom tools: " + strings.Join(runtime.RoomToolNames, ", ") + ". Topics are numbered; #12 is read with read_topic, which names each agent turn for read_turn.\n")
	if in.Relays != nil {
		w.sb.WriteString(relaysLine(*in.Relays, in.HandedBy != nil))
	}
	if in.HandedBy != nil {
		w.sb.WriteString(handedByLine(*in.HandedBy))
	}
	if b.wikis != nil {
		line := "\nThe project keeps a wiki of what the team has learned: decisions, conventions, facts, pitfalls, what modules are for, what finished topics came to. Look things up in it with search_wiki and read_wiki. " +
			"Write to it only when a person asks you to, now or as a standing rule of this chat: then write_wiki a new page, patch_wiki the page that has it, or deprecate_wiki one that no longer holds; " +
			"the change takes effect at once, and a person can undo it. When a person says a page is wrong, check it against the code, or ask them, set it right and say what you changed."
		line += " related_wiki shows how pages bear on each other, and, given a path of the repository, which pages name it: look before you change a file."
		line += " Every project also shares a skill library, the same tools with scope library: patterns of how tasks went wrong or right, and skills, which people add and install for agents; your runtime loads the ones installed for you when a task calls for them."
		line += memoryLine(b.wikis.memoryPrefs())
		if len(in.Skills) > 0 {
			line += fmt.Sprintf(" Installed for you: %s. Those you improve as you use them, without being asked: when one proves wrong or short in your task, or you find a better way, "+
				"set it right with patch_wiki (scope library), one focused change to its SKILL.md or a page of its folder, and record what happened as a Pattern page. "+
				"The change reaches every agent the skill is installed for from its next turn, on trial until %d turns have used it and ended well; a person or the skill's team can roll it back.",
				strings.Join(in.Skills, ", "), b.trialUses)
		}
		if mounted := mountsLine(b.wikis.mounts(ctx, project)); mounted != "" {
			line += " " + mounted
		}
		w.sb.WriteString(line + "\n")
	}
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
func (b *briefBuilder) wiki(ctx context.Context, w *briefWriter, in briefInput, bundle *wiki.Bundle, members []store.Member, topic topicPart) time.Time {
	if bundle == nil {
		return time.Time{}
	}
	at := bundle.Latest()
	// The memory is carried whole in the header, not listed again.
	listed := map[string]bool{wiki.MemoryPath: true}
	b.residentPages(w, bundle, listed)
	b.wikiCatalog(w, bundle, in.Session.WikiSeen, listed)
	b.relatedPages(ctx, w, bundle, b.relevance(ctx, in, members, topic), listed)
	return at
}

// residentPages writes the pages every turn carries, in full, as many as
// fit; those that do not are named so the agent can read them.
func (b *briefBuilder) residentPages(w *briefWriter, bundle *wiki.Bundle, listed map[string]bool) {
	pages := bundle.Resident()
	if len(pages) == 0 {
		return
	}
	w.section("Resident pages of the project wiki, carried in every turn:")
	room := b.limits.Resident
	var left []string
	for _, p := range pages {
		listed[p.Path] = true
		text := p.ResidentText()
		// In order: what does not fit ends the part, so a page is never
		// cut and the ones carried are the ones confirmed last.
		if n := utf8.RuneCountInString(text); len(left) == 0 && n <= room {
			room -= n
			w.sb.WriteString(text)
			continue
		}
		left = append(left, p.Path)
	}
	if len(left) > 0 {
		fmt.Fprintf(&w.sb, "\n(%s did not fit here; read_wiki has %s: %s)\n", count(len(left), "more resident page"), pronoun(len(left)), strings.Join(left, ", "))
	}
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
// a page names it. Runtimes mostly report absolute paths: one under a
// member's repository is made relative to it, any other keeps its last
// three parts.
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
	marker := "   "
	if w.addressed[m.ID] {
		marker = ">> "
	}
	w.written[m.ID] = true
	fmt.Fprintf(&w.sb, "%s%s[%s] %s\n", marker, tag, w.names.of(ctx, m), m.Body)
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
			"forget takes out an entry that no longer holds." + rest + " Every turn carries both memories whole, so each entry is one short line."
	case prefs.UsesProject():
		return " When a person wants a way of working kept for later turns, a preference, a rule, a correction of how you went about something, note it with remember in the project memory; " +
			"forget takes out an entry that no longer holds." + rest + " Every turn carries the project memory whole, so each entry is one short line."
	case prefs.UsesPersonal():
		return " When a person wants a way of working kept for later turns in every project, a preference, a rule, a correction of how you went about something, note it with remember, scope personal; " +
			"forget, scope personal, takes out an entry that no longer holds." + rest + " Every turn carries the personal memory whole, so each entry is one short line."
	}
	return ""
}

// workplace says where the member works (design.md 5.21): the leader in the
// project's checkout, and the others, once they have one, each in a git
// worktree of its own.
func workplace(w *briefWriter, in briefInput, project store.Project, members []store.Member) {
	switch {
	case in.Member.ID == project.LeaderID:
		w.sb.WriteString("\nYou are the project's leader. You work in the project's checkout itself, where people work too; " +
			"when the others work in git worktrees of their own, you write down with " + runtime.SetupToolSteps + " how a new one is got ready, whenever a person asks you to change it. " +
			"Each of them commits on a branch of its own, veyloom/ and its name, which a person merges onto the main line: do not ask them for other branches. " +
			"When they do, commit the files you changed in the checkout yourself, and only those, before your turn ends, with a message saying what the change does: " +
			"changes left there uncommitted are missing from their worktrees, and keep a person from merging work that changes the same files. " +
			"What documents work still on a member's branch, such as the README section for a feature it wrote, goes on that branch with the work: " +
			"ask the member for it, since the checkout would describe what it does not have until a person merges it.\n")
	case in.Dir != "" && in.Member.WorktreeDir != "" && in.Dir == in.Member.WorkDir:
		fmt.Fprintf(&w.sb, "\nYou work in a git worktree of your own, %s, on the branch %s, made from the project's checkout at %s. "+
			"What you change stays there until a person merges it into the branch the checkout is on: you need not commit, and do not push or switch branches. "+
			"Whatever you leave there is merged as your work, so what you build or run only to check it writes outside the worktree, in a temporary folder, "+
			"or you remove what it wrote before your turn ends.",
			in.Dir, in.Member.Branch, project.RepoPath)
		var others []string
		for _, m := range members {
			if m.ID != in.Member.ID && m.Branch != "" && !m.Removed() {
				others = append(others, fmt.Sprintf("%s (%s)", m.Branch, m.DisplayName))
			}
		}
		if len(others) > 0 {
			fmt.Fprintf(&w.sb, " The others' work is on their branches of the same repository: %s. To build on what one of them committed, merge its branch into yours "+
				"(git merge %s, say) rather than copying its files.", strings.Join(others, ", "), strings.SplitN(others[0], " ", 2)[0])
		}
		w.sb.WriteString("\n")
	}
}
