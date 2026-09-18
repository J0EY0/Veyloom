package hub

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
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
// A session that has read nothing gets the whole story from the same code:
// there is no separate "full brief".
type briefBuilder struct {
	store  briefStore
	limits briefLimits
	// attachmentDir turns an attachment's stored path into one the runtime
	// can open.
	attachmentDir string
}

func newBriefBuilder(store briefStore, limits briefLimits, attachmentDir string) *briefBuilder {
	return &briefBuilder{store: store, limits: limits, attachmentDir: attachmentDir}
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
}

// brief is a composed prompt and the position of the room it was taken at:
// once the session has taken the brief in, that is how far it has read.
type brief struct {
	Prompt   string
	Position int64
}

// Build renders the brief for one turn.
func (b *briefBuilder) Build(ctx context.Context, in briefInput) (brief, error) {
	position, err := b.store.RoomPosition(ctx, in.Thread.RoomID)
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

	if err := b.header(ctx, w, in); err != nil {
		return brief{}, err
	}
	topic, err := b.readTopic(ctx, in, position)
	if err != nil {
		return brief{}, err
	}
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
	return brief{Prompt: strings.TrimRight(w.sb.String(), "\n") + "\n", Position: position}, nil
}

// header writes what every brief opens with.
func (b *briefBuilder) header(ctx context.Context, w *briefWriter, in briefInput) error {
	project, err := b.store.RoomProject(ctx, in.Thread.RoomID)
	if err != nil {
		return fmt.Errorf("brief: %w", err)
	}
	fmt.Fprintf(&w.sb, "You are %q, an agent in the team chat of the project %q. Lines marked with >> are addressed to you; reply to them.\n", in.Member.DisplayName, project.Name)
	if in.NewSession != "" {
		fmt.Fprintf(&w.sb, "\nThis is a new session: your earlier session in this project could not be continued (%s), so you do not remember your earlier turns here. What follows is the hub's record; rely on that, and on the repository, rather than on memory.\n", sessionEndPhrase(in.NewSession))
	}
	if about := strings.TrimSpace(project.Description); about != "" {
		w.section("About the project:")
		w.sb.WriteString(about)
		w.sb.WriteString("\n")
	}

	members, err := b.store.ListRoomMembers(ctx, in.Thread.RoomID)
	if err != nil {
		return fmt.Errorf("brief: %w", err)
	}
	w.section("In this chat (mention one as @Name to hand something over):")
	for _, m := range members {
		if m.Removed() || !m.Enabled {
			continue
		}
		line := "- " + m.DisplayName
		if m.ID == in.Member.ID {
			line += " (you)"
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
	// Said every time, like the rest of the header: a brief shows only what
	// is new, and this is how the agent gets at everything else.
	w.sb.WriteString("\nYou are shown what is new since you last looked. For anything else in this chat, such as earlier messages, another topic or what a turn changed, use your veyloom tools: " + strings.Join(runtime.RoomToolNames, ", ") + ". Topics are numbered; #12 is read with read_topic.\n")
	return nil
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
	number  int
	root    store.Message
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
	return topicPart{number: in.Thread.Number, root: root, replies: replies, total: total, been: been}, nil
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
