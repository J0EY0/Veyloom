package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// sendsPerTurn caps the messages a turn posts with send_message, so an
// agent does not flood the chat.
const sendsPerTurn = 10

// sendTextMax caps the text of one message it posts, in characters, and
// sendTitleMax the title of the task it hands on.
const (
	sendTextMax  = 20000
	sendTitleMax = 80
)

// answerSendMessage posts what a member says with send_message, in its
// topic or in the room, and wakes the members it names at once, within the
// limits on agents waking one another (docs/design.md 5.22). The message
// is stored on the turn's executor, after what the turn said so far; the
// woken members' turns start on their own, the answer not waiting on them.
func (m *TurnManager) answerSendMessage(ctx context.Context, at *activeTurn, q runtime.RoomQuery) (string, error) {
	var args struct {
		Text  string `json:"text"`
		To    string `json:"to"`
		Title string `json:"title"`
	}
	if len(q.Args) > 0 {
		if err := json.Unmarshal(q.Args, &args); err != nil {
			return "", fmt.Errorf("send_message: %w", err)
		}
	}
	text := strings.TrimSpace(args.Text)
	switch {
	case at.turn.Kind != store.TurnChat:
		return "", errors.New("send_message is for turns in the chat")
	case text == "":
		return "", errors.New("send_message needs text")
	case utf8.RuneCountInString(text) > sendTextMax:
		return "", fmt.Errorf("send_message takes at most %d characters", sendTextMax)
	case args.To != "" && args.To != "topic" && args.To != "room":
		return "", fmt.Errorf("send_message: to is topic or room, not %q", args.To)
	case utf8.RuneCountInString(args.Title) > sendTitleMax:
		return "", fmt.Errorf("send_message: a title takes at most %d characters", sendTitleMax)
	}
	toRoom := args.To == "room"
	at.mu.Lock()
	if at.sent >= sendsPerTurn {
		at.mu.Unlock()
		return "", fmt.Errorf("a turn sends at most %d messages", sendsPerTurn)
	}
	at.sent++
	at.mu.Unlock()

	named, person := m.mentionsIn(ctx, at, text)
	mentions := make([]store.Mention, 0, len(named)+1)
	for _, a := range named {
		mentions = append(mentions, store.Mention{Kind: store.MentionAgent, ID: a.ID})
	}
	if person != nil {
		mentions = append(mentions, store.Mention{Kind: store.MentionUser, ID: person.ID})
	}

	var msg store.Message
	var thread store.Thread
	var err error
	done := make(chan struct{})
	if !at.enqueue(func() {
		defer close(done)
		pctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
		defer cancel()
		msg, thread, err = m.postSent(pctx, at, sent{text: text, title: strings.TrimSpace(args.Title), mentions: mentions, toRoom: toRoom, names: len(named) > 0})
	}) {
		return "", errors.New("the turn is over")
	}
	select {
	case <-done:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err != nil {
		return "", err
	}

	at.mu.Lock()
	if at.named == nil {
		at.named = make(map[string]bool)
	}
	for _, a := range named {
		at.named[a.ID] = true
	}
	at.mu.Unlock()
	var woken, notWoken []string
	for _, a := range named {
		member, err := m.store.GetMember(ctx, a.ID)
		switch {
		case err != nil || member.Removed():
			notWoken = append(notWoken, a.DisplayName+" (no longer in the project)")
			continue
		case !member.Enabled:
			notWoken = append(notWoken, a.DisplayName+" (turned off)")
			continue
		case at.reportsTo(member.ID):
			notWoken = append(notWoken, a.DisplayName+" (it handed you the work: your answer reaches it once all of it is done, and naming it does not wake it)")
			continue
		case !m.mayWake(ctx, at.asWaker(), member, msg, thread):
			notWoken = append(notWoken, a.DisplayName+" (held back by the limit on agents waking one another; the person is told)")
			continue
		}
		woken = append(woken, member.DisplayName)
		m.handOn(at, member, msg)
		go func() {
			wctx, cancel := context.WithTimeout(context.Background(), m.storeTimeout)
			defer cancel()
			if err := m.TriggerIn(wctx, member, msg, thread); err != nil {
				m.logger.Error("wake a member named in a message", "member", member.ID, "turn", at.turn.ID, "err", err)
			}
		}()
	}
	return sentAnswer(toRoom, thread, woken, notWoken), nil
}

// sent is what a member sends: its text and the title of the task it
// hands on, the members it names, and whether it goes to the room and
// names any member.
type sent struct {
	text, title   string
	mentions      []store.Mention
	toRoom, names bool
}

// postSent stores and announces what a member sent: in its topic, where
// it is what the turn said there (sayInTopic), or in the room, where
// naming members starts a topic at the message for them.
func (m *TurnManager) postSent(ctx context.Context, at *activeTurn, s sent) (store.Message, store.Thread, error) {
	in := store.NewMessage{
		RoomID: at.thread.RoomID, SenderKind: store.SenderAgent, MemberID: at.member.ID,
		Body: s.text, Title: s.title, Mentions: s.mentions, TurnID: at.turn.ID,
	}
	toRoom, names := s.toRoom, s.names
	if !toRoom {
		msg, err := m.sayInTopic(ctx, at, in)
		return msg, at.thread, err
	}
	msg, err := m.store.CreateMessage(ctx, in)
	if err != nil {
		return store.Message{}, store.Thread{}, err
	}
	if !names {
		m.publish(messageEvent(msg))
		return msg, store.Thread{}, nil
	}
	// The members it names work in one topic, rooted at the message, as
	// they would for a person's.
	thread, err := m.store.ThreadForMessage(ctx, msg.ID)
	if err != nil {
		return store.Message{}, store.Thread{}, err
	}
	m.publish(Event{Kind: EventMessage, RoomID: msg.Room, At: msg.CreatedAt, Message: &msg, Thread: topicSummary(thread)})
	return msg, thread, nil
}

// sayInTopic puts what a turn sent to its own topic there as what the turn
// said (design.md 5.24): at the head of a topic the turn opened while that
// is still empty, as its first words would be, else under it. Either way
// it is the turn's latest word, which its end addresses to whoever asked
// when nothing follows it. Runs on the turn's executor, in order with what
// the turn streams.
func (m *TurnManager) sayInTopic(ctx context.Context, at *activeTurn, in store.NewMessage) (store.Message, error) {
	at.mu.Lock()
	root := at.root
	head := root != nil && at.segments == 0
	at.mu.Unlock()

	var msg store.Message
	var err error
	if head {
		msg, err = m.store.UpdateMessageBody(ctx, root.ID, in.Body, in.TurnID, in.Mentions)
		if err == nil && in.Title != "" {
			msg, err = m.store.SetMessageTitle(ctx, root.ID, in.Title)
		}
		if err == nil {
			m.publish(Event{Kind: EventMessage, RoomID: msg.Room, At: msg.CreatedAt, Message: &msg, Thread: topicSummary(at.thread)})
		}
	} else {
		in.ThreadID = at.thread.ID
		msg, err = m.post(ctx, in)
	}
	if err != nil {
		return store.Message{}, err
	}
	at.mu.Lock()
	if head {
		at.root = &msg
	}
	at.segments++
	at.lastReply, at.lastReplyID = msg.Body, msg.ID
	at.mu.Unlock()
	return msg, nil
}

// sentAnswer tells the agent what came of its message.
func sentAnswer(toRoom bool, thread store.Thread, woken, notWoken []string) string {
	var b strings.Builder
	switch {
	case !toRoom:
		b.WriteString("Posted in this topic.")
	case thread.ID != "":
		fmt.Fprintf(&b, "Posted in the room, starting topic #%d.", thread.Number)
	default:
		b.WriteString("Posted in the room. It names no member, so it wakes no one.")
	}
	if len(woken) > 0 {
		b.WriteString(" Woken, working alongside you now: " + strings.Join(woken, ", ") + ".")
	}
	if len(notWoken) > 0 {
		b.WriteString(" Not woken: " + strings.Join(notWoken, "; ") + ".")
	}
	return b.String()
}
