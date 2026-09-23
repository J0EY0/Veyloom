package hub

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Reading turns (docs/design.md 5.7, 5.12). Every turn has read_turn, for
// the turns of its own project's chat. The wiki maintainer's upkeep turns
// also have list_turns, and both reach further there: to other projects'
// turns that used a skill the project's team owns. What a call may see
// comes from the turn answering it, never from the call.

// Limits of what read_turn shows of a turn.
const (
	readTurnAsked  = 2000
	readTurnSaid   = 1500
	readTurnTool   = 300
	readTurnMax    = 16000
	readTurnHead   = 9000
	readTurnPerson = 2000
)

// upkeepArgs are the maintainer tools' arguments.
type upkeepArgs struct {
	Topic  int    `json:"topic"`
	Skills bool   `json:"skills"`
	Before string `json:"before"`
	Limit  int    `json:"limit"`
	Turn   string `json:"turn"`
	// rollback_skill's.
	Skill  string `json:"skill"`
	Reason string `json:"reason"`
	// confirm_wiki's.
	Path string `json:"path"`
}

// answerUpkeep answers a maintainer tool call of a running turn.
func (m *TurnManager) answerUpkeep(ctx context.Context, at *activeTurn, q runtime.RoomQuery) (string, error) {
	if at.upkeep == nil {
		return "", errors.New("only the wiki maintainer's upkeep turns have this tool")
	}
	var args upkeepArgs
	if len(q.Args) > 0 {
		if err := json.Unmarshal(q.Args, &args); err != nil {
			return "", fmt.Errorf("bad arguments: %v", err)
		}
	}
	switch q.Tool {
	case runtime.UpkeepToolListTurns:
		return m.listTurns(ctx, at.upkeep, args)
	case runtime.UpkeepToolRollback:
		return m.rollbackBy(ctx, at, args)
	case runtime.UpkeepToolConfirm:
		return m.confirmWiki(ctx, at, args.Path)
	}
	return "", fmt.Errorf("unknown tool %q", q.Tool)
}

func (m *TurnManager) listTurns(ctx context.Context, up *upkeep, args upkeepArgs) (string, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = roomPageDefault
	}
	limit = min(limit, roomPageMax)
	q := store.UpkeepQuery{ProjectID: up.project.ID, Topic: args.Topic, Limit: limit}
	heading := "Turns of this chat, newest first:"
	if args.Skills {
		if len(up.owned) == 0 {
			return "This project's team owns no skills of the library, so no other project's turn is yours to read.", nil
		}
		if args.Topic != 0 {
			return "", errors.New("topic picks a topic of this project's chat; leave it out to list other projects' turns")
		}
		q.Skills, q.Owned = true, up.owned
		heading = "Turns of other projects that used this team's skills, newest first:"
	}
	if args.Before != "" {
		before, err := m.store.GetTurn(ctx, args.Before)
		if err != nil {
			return "", fmt.Errorf("there is no turn %s to list what came before", args.Before)
		}
		q.Before = &before.StartedAt
	}
	turns, err := m.store.ListUpkeepTurns(ctx, q)
	if err != nil {
		return "", err
	}
	if len(turns) == 0 {
		if args.Before != "" {
			return "No older turns.", nil
		}
		return "No turns.", nil
	}
	var b strings.Builder
	b.WriteString(heading + "\n")
	for _, t := range turns {
		line := upkeepTurnLine(t, args.Skills)
		if t.Reviewed {
			line += " (gone over)"
		}
		b.WriteString(line + "\n")
	}
	if len(turns) == limit {
		fmt.Fprintf(&b, "(Older ones: list_turns with before %q.)\n", turns[len(turns)-1].ID)
	}
	return b.String(), nil
}

// answerReadTurn answers read_turn: a turn of the asking turn's own
// project, or, for a maintainer's upkeep, of another project that used a
// skill the team owns.
func (m *TurnManager) answerReadTurn(ctx context.Context, at *activeTurn, q runtime.RoomQuery) (string, error) {
	var args upkeepArgs
	if len(q.Args) > 0 {
		if err := json.Unmarshal(q.Args, &args); err != nil {
			return "", fmt.Errorf("bad arguments: %v", err)
		}
	}
	id := strings.TrimSpace(args.Turn)
	if at.upkeep != nil {
		return m.readTurn(ctx, at.upkeep.project, at.upkeep.owned, id)
	}
	project, err := m.store.RoomProject(ctx, at.thread.RoomID)
	if err != nil {
		return "", err
	}
	return m.readTurn(ctx, project, nil, id)
}

// readTurn tells a turn for an agent to read: one of project's, or of
// another project's that used one of the skills owned.
func (m *TurnManager) readTurn(ctx context.Context, own store.Project, owned []string, id string) (string, error) {
	if id == "" {
		return "", errors.New("which turn? give its id as turn")
	}
	turn, err := m.store.GetTurn(ctx, id)
	if err != nil {
		return "", fmt.Errorf("there is no turn %s", id)
	}
	project, err := m.store.RoomProject(ctx, turn.RoomID)
	if err != nil {
		return "", err
	}
	if project.ID != own.ID && !slices.ContainsFunc(turn.SkillsUsed, func(s string) bool { return slices.Contains(owned, s) }) {
		if len(owned) == 0 {
			return "", fmt.Errorf("turn %s is another project's: it is not yours to read", id)
		}
		return "", fmt.Errorf("turn %s is another project's and used none of this team's skills: it is not yours to read", id)
	}
	thread, err := m.store.GetThread(ctx, turn.ThreadID)
	if err != nil {
		return "", err
	}
	names := newNameResolver(m.store)
	var b strings.Builder
	member := "a member"
	if mb, err := m.store.GetMember(ctx, turn.MemberID); err == nil {
		member = mb.DisplayName
	}
	title := "(untitled)"
	if root, err := m.store.GetMessage(ctx, thread.RootMessageID); err == nil {
		title = topicTitleOf(root.Body)
	}
	fmt.Fprintf(&b, "Turn %s of %s (%s) in topic #%d %q of project %q: %s", turn.ID, member, turn.Runtime, thread.Number, excerpt(title, topicTitleExcerpt), project.Name, turn.Status)
	if turn.Kind == store.TurnUpkeep {
		b.WriteString(", a wiki upkeep")
	}
	fmt.Fprintf(&b, ", started %s", turn.StartedAt.Local().Format("2006-01-02 15:04"))
	if turn.EndedAt != nil {
		fmt.Fprintf(&b, ", took %s", turn.EndedAt.Sub(turn.StartedAt).Round(1e9))
	}
	b.WriteString(".\n")
	if turn.EndedAt == nil {
		b.WriteString("It is still running: what it has done so far may not all be written down yet.\n")
	}
	if turn.Error != "" {
		fmt.Fprintf(&b, "It ended with: %s\n", excerpt(turn.Error, upkeepErrorExcerpt*2))
	}
	if len(turn.FilesChanged) > 0 {
		fmt.Fprintf(&b, "Files it changed: %s\n", strings.Join(turn.FilesChanged, ", "))
	}
	if len(turn.SkillsUsed) > 0 {
		fmt.Fprintf(&b, "Skills of the library it used: %s\n", strings.Join(turn.SkillsUsed, ", "))
	}
	if turn.TriggerMessageID != "" {
		if asked, err := m.store.GetMessage(ctx, turn.TriggerMessageID); err == nil {
			fmt.Fprintf(&b, "\nWhat it was asked, by %s at %s:\n%s\n", names.of(ctx, asked), asked.CreatedAt.Local().Format("15:04"), excerpt(asked.Body, readTurnAsked))
		}
	}
	b.WriteString("\nWhat happened:\n")
	b.WriteString(m.turnStory(turn))
	if turn.EndedAt != nil {
		switch next, err := m.store.NextPersonMessage(ctx, thread.ID, *turn.EndedAt); {
		case err == nil:
			fmt.Fprintf(&b, "\nWhat a person said next in the topic, %s at %s:\n%s\n", names.of(ctx, next), next.CreatedAt.Local().Format("2006-01-02 15:04"), excerpt(next.Body, readTurnPerson))
		case errors.Is(err, store.ErrNotFound):
			b.WriteString("\nNobody has said anything in the topic since.\n")
		default:
			return "", err
		}
	}
	return b.String(), nil
}

// turnStory tells what a turn did, from its transcript: what the agent
// said, the tools it called and what they answered, what it asked people.
// A long story keeps its beginning and its end.
func (m *TurnManager) turnStory(turn store.Turn) string {
	path := turn.TranscriptPath
	if path == "" {
		path = filepath.Join(m.transcripts, turn.ID+".jsonl")
	}
	f, err := os.Open(path)
	if err != nil {
		return "(its transcript is gone)\n"
	}
	defer f.Close()
	var b, said strings.Builder
	flush := func() {
		if text := strings.TrimSpace(said.String()); text != "" {
			fmt.Fprintf(&b, "Said: %s\n", excerpt(text, readTurnSaid))
		}
		said.Reset()
	}
	r := bufio.NewReader(f)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var l transcriptLine
			if json.Unmarshal(line, &l) == nil {
				tell(&b, &said, flush, l)
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				fmt.Fprintf(&b, "(the transcript breaks off: %v)\n", err)
			}
			break
		}
	}
	flush()
	story := b.String()
	if story == "" {
		return "(nothing was recorded)\n"
	}
	if n := utf8.RuneCountInString(story); n > readTurnMax {
		runes := []rune(story)
		tail := readTurnMax - readTurnHead
		story = string(runes[:readTurnHead]) + fmt.Sprintf("\n(… %d characters left out …)\n", n-readTurnMax) + string(runes[n-tail:])
	}
	return story
}

// tell adds one transcript line to the story.
func tell(b, said *strings.Builder, flush func(), l transcriptLine) {
	switch {
	case l.Kind == "approval" && l.Approval != nil:
		flush()
		fmt.Fprintf(b, "  → %s", l.Approval.Status)
		if l.Approval.Message != "" {
			fmt.Fprintf(b, ": %s", excerpt(l.Approval.Message, readTurnTool))
		}
		b.WriteString("\n")
	case l.Kind != "event" || l.Event == nil:
	case l.Event.Kind == runtime.EventText:
		said.WriteString(l.Event.Text)
	case l.Event.Kind == runtime.EventToolCall:
		flush()
		fmt.Fprintf(b, "Called %s %s\n", l.Event.Tool, excerpt(oneLine(l.Event.Input), readTurnTool))
	case l.Event.Kind == runtime.EventToolResult:
		fmt.Fprintf(b, "  → %s\n", excerpt(oneLine(l.Event.Text), readTurnTool))
	case l.Event.Kind == runtime.EventApprovalRequest:
		flush()
		fmt.Fprintf(b, "Asked a person about %s %s\n", l.Event.Tool, excerpt(oneLine(l.Event.Input), readTurnTool))
	case l.Event.Kind == runtime.EventNotice || l.Event.Kind == runtime.EventError:
		flush()
		fmt.Fprintf(b, "Notice: %s\n", excerpt(oneLine(l.Event.Text), readTurnTool))
	}
}

// oneLine puts text on one line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
