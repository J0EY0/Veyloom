package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// The room tools' answers (design.md 5.7). A brief pushes what is new to a
// session; with these an agent reads everything else when it wants it:
// older messages, other topics, what another agent's turn changed. They
// read the room of the turn that asks and no other: the room comes from the
// turn, never from the call.

// roomStore is what answering a room tool reads.
type roomStore interface {
	nameStore
	GetMessage(ctx context.Context, id string) (store.Message, error)
	ThreadByNumber(ctx context.Context, roomID string, number int) (store.Thread, error)
	ListRoomTopics(ctx context.Context, roomID string, before int64, limit int) ([]store.TopicListing, error)
	ListRoomMessagesBefore(ctx context.Context, roomID string, before int64, limit int) ([]store.Message, error)
	ListThreadMessagesBefore(ctx context.Context, threadID string, before int64, limit int) ([]store.Message, error)
	ListThreadTurns(ctx context.Context, threadID string) ([]store.Turn, error)
	SearchRoomMessages(ctx context.Context, roomID, phrase string, before int64, limit int) ([]store.RoomNewsItem, error)
}

// Limits of a room tool's answer. An answer is read by a model, whose
// context it spends: a page is a screenful, and a long message is quoted
// up to a number of characters, briefs included, with the rest a
// read_message away, which reads one whole up to a number of its own.
const (
	roomPageDefault = 20
	roomPageMax     = 50
	roomBodyMax     = 4000
	readMessageMax  = 100_000
)

// OnRoomQuery answers a room tool call of a running turn. It returns at
// once: the database is read on a goroutine of its own, so the connection
// loop never waits for it, and the answer goes back over conn.
func (m *TurnManager) OnRoomQuery(conn protocol.Conn, q protocol.RoomQuery) {
	m.mu.Lock()
	at := m.active[q.TurnID]
	m.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
		defer cancel()
		res := protocol.RoomResult{TurnID: q.TurnID, QueryID: q.QueryID}
		var text string
		var err error
		switch {
		case at == nil:
			err = errors.New("the turn is over")
		case slices.Contains(runtime.WikiToolNames, q.Query.Tool):
			text, err = m.answerWiki(ctx, at, q.Query)
		case slices.Contains(runtime.MemoryToolNames, q.Query.Tool):
			text, err = m.answerMemory(ctx, at, q.Query)
		case q.Query.Tool == runtime.RoomToolReadTurn:
			text, err = m.answerReadTurn(ctx, at, q.Query)
		case q.Query.Tool == runtime.RoomToolReadMessage:
			text, err = m.answerReadMessage(ctx, at, q.Query)
		case slices.Contains(runtime.UpkeepToolNames, q.Query.Tool):
			text, err = m.answerUpkeep(ctx, at, q.Query)
		case q.Query.Tool == runtime.SetupToolSteps:
			text, err = m.answerSetupSteps(ctx, at, q.Query)
		case q.Query.Tool == runtime.MessageToolSend:
			text, err = m.answerSendMessage(ctx, at, q.Query)
		case q.Query.Tool == runtime.MessageToolRemind:
			text, err = m.answerRemind(ctx, at, q.Query)
		case q.Query.Tool == runtime.MessageToolCancelReminder:
			text, err = m.answerCancelReminder(ctx, at, q.Query)
		case q.Query.Tool == runtime.MessageToolDraft:
			text, err = m.answerDraft(ctx, at, q.Query)
		default:
			text, err = answerRoomQuery(ctx, m.store, at.thread.RoomID, m.attachmentDir, q.Query)
		}
		if err != nil {
			res.Error = store.Reason(err)
		} else {
			res.Text = text
		}
		m.send(conn, res)
	}()
}

// answerRoomQuery reads what a room tool asked for and renders it as text.
// attachmentDir is where the files people sent are, for the agent to open.
func answerRoomQuery(ctx context.Context, st roomStore, roomID, attachmentDir string, q runtime.RoomQuery) (string, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = roomPageDefault
	}
	limit = min(limit, roomPageMax)
	r := &roomReader{store: st, names: newNameResolver(st), roomID: roomID, attachmentDir: attachmentDir}
	switch q.Tool {
	case runtime.RoomToolListTopics:
		return r.listTopics(ctx, q.Before, limit)
	case runtime.RoomToolReadTopic:
		return r.readTopic(ctx, q.Topic, q.Before, limit)
	case runtime.RoomToolReadRoom:
		return r.readRoom(ctx, q.Before, limit)
	case runtime.RoomToolSearch:
		return r.search(ctx, q.Text, q.Before, limit)
	default:
		return "", fmt.Errorf("unknown room tool %q", q.Tool)
	}
}

// roomReader renders one answer.
type roomReader struct {
	// attachmentDir is where the files people sent are kept.
	attachmentDir string
	store         roomStore
	names         *nameResolver
	roomID        string
	sb            strings.Builder
}

func (r *roomReader) listTopics(ctx context.Context, before int64, limit int) (string, error) {
	topics, err := r.store.ListRoomTopics(ctx, r.roomID, before, limit)
	if err != nil {
		return "", err
	}
	if len(topics) == 0 {
		if before > 0 {
			return "No older topics.", nil
		}
		return "This chat has no topics yet.", nil
	}
	r.sb.WriteString("Topics, most recently active first:\n")
	for _, t := range topics {
		replies := "replies"
		if t.ReplyCount == 1 {
			replies = "reply"
		}
		fmt.Fprintf(&r.sb, "#%d %q: %d %s, last %s from %s: %q\n",
			t.Number, excerpt(topicTitle(t.Root), titleExcerpt), t.ReplyCount, replies,
			stamp(t.Last.CreatedAt), r.names.of(ctx, t.Last), excerpt(t.Last.Body, lineExcerpt))
	}
	if len(topics) == limit {
		fmt.Fprintf(&r.sb, "(older topics: %s with before=%d)\n", runtime.RoomToolListTopics, topics[len(topics)-1].LastSeq)
	}
	return r.sb.String(), nil
}

func (r *roomReader) readTopic(ctx context.Context, number int, before int64, limit int) (string, error) {
	if number <= 0 {
		return "", errors.New("which topic? give its number, the 12 of #12")
	}
	thread, err := r.store.ThreadByNumber(ctx, r.roomID, number)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("this chat has no topic #%d; %s shows the ones it has", number, runtime.RoomToolListTopics)
	}
	if err != nil {
		return "", err
	}
	root, err := r.store.GetMessage(ctx, thread.RootMessageID)
	if err != nil {
		return "", err
	}
	replies, err := r.store.ListThreadMessagesBefore(ctx, thread.ID, before, limit)
	if err != nil {
		return "", err
	}
	// Each agent turn in the topic, named after its last word with the
	// files it changed, for read_turn to tell the rest.
	turns, err := r.store.ListThreadTurns(ctx, thread.ID)
	if err != nil {
		return "", err
	}
	byID := make(map[string]store.Turn, len(turns))
	for _, turn := range turns {
		byID[turn.ID] = turn
	}

	fmt.Fprintf(&r.sb, "Topic #%d %q, oldest first:\n", number, excerpt(topicTitle(root), titleExcerpt))
	shown := replies
	if len(replies) < limit {
		// The page reaches back to the start of the topic: the root heads
		// it, after the question in the room it answers if an agent's reply
		// opened the topic.
		if opened, ok := byID[root.TurnID]; ok && opened.TriggerMessageID != "" {
			if asked, err := r.store.GetMessage(ctx, opened.TriggerMessageID); err == nil && asked.ThreadID == "" {
				r.message(ctx, asked, "(asked in the room) ")
			}
		}
		shown = append([]store.Message{root}, replies...)
	} else {
		fmt.Fprintf(&r.sb, "(earlier ones: %s with topic=%d before=%d)\n", runtime.RoomToolReadTopic, number, replies[0].Seq)
	}
	for i, msg := range shown {
		r.message(ctx, msg, "")
		turn, ok := byID[msg.TurnID]
		if !ok || (i < len(shown)-1 && shown[i+1].TurnID == msg.TurnID) {
			continue
		}
		if len(turn.FilesChanged) > 0 {
			fmt.Fprintf(&r.sb, "     (turn %s; it changed: %s)\n", turn.ID, strings.Join(turn.FilesChanged, ", "))
		} else {
			fmt.Fprintf(&r.sb, "     (turn %s)\n", turn.ID)
		}
	}
	return r.sb.String(), nil
}

func (r *roomReader) readRoom(ctx context.Context, before int64, limit int) (string, error) {
	msgs, err := r.store.ListRoomMessagesBefore(ctx, r.roomID, before, limit)
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		if before > 0 {
			return "No older messages.", nil
		}
		return "Nobody has said anything in this chat yet.", nil
	}
	r.sb.WriteString("The chat's top-level messages, oldest first:\n")
	if len(msgs) == limit {
		fmt.Fprintf(&r.sb, "(earlier ones: %s with before=%d)\n", runtime.RoomToolReadRoom, msgs[0].Seq)
	}
	// The topic each one heads, looked up in one go.
	numbers := r.topicNumbers(ctx, msgs)
	for _, msg := range msgs {
		tag := ""
		if n := numbers[msg.ID]; n > 0 {
			tag = fmt.Sprintf("#%d ", n)
		}
		r.message(ctx, msg, tag)
	}
	return r.sb.String(), nil
}

func (r *roomReader) search(ctx context.Context, phrase string, before int64, limit int) (string, error) {
	phrase = strings.TrimSpace(phrase)
	if phrase == "" {
		return "", errors.New("search for what? give a phrase as text")
	}
	hits, err := r.store.SearchRoomMessages(ctx, r.roomID, phrase, before, limit)
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return fmt.Sprintf("No message in this chat holds %q.", phrase), nil
	}
	fmt.Fprintf(&r.sb, "Messages holding %q, newest first:\n", phrase)
	for _, hit := range hits {
		where := "room"
		if hit.TopicNumber > 0 {
			where = fmt.Sprintf("#%d", hit.TopicNumber)
		}
		fmt.Fprintf(&r.sb, "%s %s [%s] %s\n", where, stamp(hit.CreatedAt), r.names.of(ctx, hit.Message), excerpt(hit.Body, 2*lineExcerpt))
	}
	if len(hits) == limit {
		fmt.Fprintf(&r.sb, "(older hits: %s with text=%q before=%d)\n", runtime.RoomToolSearch, phrase, hits[len(hits)-1].Seq)
	}
	return r.sb.String(), nil
}

// topicNumbers finds the number of the topic each message heads, by asking
// the directory: a page of the room is small and so is the directory's.
func (r *roomReader) topicNumbers(ctx context.Context, msgs []store.Message) map[string]int {
	out := make(map[string]int, len(msgs))
	if len(msgs) == 0 {
		return out
	}
	topics, err := r.store.ListRoomTopics(ctx, r.roomID, 0, store.MaxPageLimit)
	if err != nil {
		return out
	}
	for _, t := range topics {
		out[t.Root.ID] = t.Number
	}
	return out
}

// message writes one message: when, who, what, and the files it carries by
// name. A long body is cut, since the answer spends the reader's context.
func (r *roomReader) message(ctx context.Context, m store.Message, tag string) {
	if m.Body == "" && len(m.Attachments) == 0 {
		return
	}
	body := m.Body
	if cut, more := cutBody(body, roomBodyMax); more > 0 {
		body = cut + cutNote(m.ID, more)
	}
	fmt.Fprintf(&r.sb, "%s%s [%s] %s\n", tag, stamp(m.CreatedAt), r.names.of(ctx, m), body)
	for _, a := range m.Attachments {
		// The id keeps it in the wiki (write_wiki's files); the path opens it.
		fmt.Fprintf(&r.sb, "     (attached %s, %s, %d bytes; file %s at %s)\n", a.Filename, a.MediaType, a.Size, a.ID, filepath.Join(r.attachmentDir, filepath.FromSlash(a.Path)))
	}
}

// stamp is a time as an answer shows it: to the minute, in UTC, which needs
// no explaining to a reader who may be anywhere.
func stamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format("2006-01-02 15:04")
}

// cutBody is body cut to at most max characters, with its lines as they
// were, and how many characters it left out; body itself, and nothing left
// out, when it fits. Characters, not bytes: a message in Chinese is cut
// where one of as many English characters is.
func cutBody(body string, max int) (string, int) {
	if len(body) <= max {
		return body, 0
	}
	n := 0
	for i := range body {
		if n == max {
			return strings.TrimRight(body[:i], " \t"), utf8.RuneCountInString(body[i:])
		}
		n++
	}
	return body, 0
}

// cutNote says what a message cut short left out, and how to read it whole.
func cutNote(id string, more int) string {
	return fmt.Sprintf(" … (%d more characters: %s with message %s reads it whole)", more, runtime.RoomToolReadMessage, id)
}

// answerReadMessage reads one message of the turn's room whole
// (read_message): a long one a brief or another tool cut short.
func (m *TurnManager) answerReadMessage(ctx context.Context, at *activeTurn, q runtime.RoomQuery) (string, error) {
	var args struct {
		Message string `json:"message"`
	}
	if len(q.Args) > 0 {
		if err := json.Unmarshal(q.Args, &args); err != nil {
			return "", fmt.Errorf("read_message: %w", err)
		}
	}
	id := strings.TrimSpace(args.Message)
	if id == "" {
		return "", errors.New("read which message? give its id as message")
	}
	msg, err := m.store.GetMessage(ctx, id)
	if err != nil || msg.Room != at.thread.RoomID {
		// Another project's messages are not this turn's to read.
		return "", fmt.Errorf("this chat has no message %s", id)
	}
	where := "in the room"
	if msg.ThreadID != "" {
		if thread, err := m.store.GetThread(ctx, msg.ThreadID); err == nil {
			where = fmt.Sprintf("in topic #%d", thread.Number)
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Message %s, from %s, %s, %s:\n", msg.ID, newNameResolver(m.store).of(ctx, msg), stamp(msg.CreatedAt), where)
	body, more := cutBody(msg.Body, readMessageMax)
	sb.WriteString(body + "\n")
	if more > 0 {
		fmt.Fprintf(&sb, "(%d more characters are too many to read here.)\n", more)
	}
	for _, a := range msg.Attachments {
		fmt.Fprintf(&sb, "(attached %s, %s, %d bytes; file %s at %s)\n", a.Filename, a.MediaType, a.Size, a.ID, filepath.Join(m.attachmentDir, filepath.FromSlash(a.Path)))
	}
	return sb.String(), nil
}
