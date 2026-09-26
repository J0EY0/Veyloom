package hub

import (
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// Work a member hands on with a title is a task of its own on the board,
// named by that title and part of the work a person asked for; the work's
// page tells the split, the part and the summing up apart (docs/webui.md
// 4.20).
func TestLoop_HandedOnWorkOnTheBoard(t *testing.T) {
	l := newLoop(t)
	handOff := map[string]any{"tool": runtime.MessageToolSend, "args": map[string]any{
		"text": "@Coder please build the API with pagination and auth", "title": "Build the API",
	}}
	lead := l.member("Lead", map[string]any{"tool_calls": []any{handOff}, "reply": "Planned.", "summary_reply": "Coder built it."})
	coder := l.member("Coder", map[string]any{"reply": "Built.", "write": []any{"api.go"}})
	msg := l.say("@Lead plan the API: pages and auth", "", lead)
	l.settle(3, "Lead, Coder, and Lead summing up")

	var titled bool
	for _, turn := range l.turns() {
		if turn.MemberID == coder.ID {
			trigger, err := l.s.GetMessage(l.ctx, turn.TriggerMessageID)
			titled = err == nil && trigger.Title == "Build the API"
		}
	}
	if !titled {
		t.Error("the hand-off message keeps its title")
	}

	tasks, err := l.s.ListRoomTasks(l.ctx, l.room.ID)
	if err != nil || len(tasks) != 2 {
		t.Fatalf("tasks: %+v %v", tasks, err)
	}
	asked, part := tasks[0], tasks[1]
	if asked.MemberID != lead.ID || asked.Title != "plan the API" || asked.Parts == nil || asked.Parts.Done != 1 || asked.Parts.Total != 1 || asked.Turns != 2 || asked.State != store.TaskDone {
		t.Errorf("the task asked for: %+v (parts %+v)", asked, asked.Parts)
	}
	if part.MemberID != coder.ID || part.Title != "Build the API" || part.Work == nil || part.Work.Title != "plan the API" || part.Work.Chain != msg.ID {
		t.Errorf("the part handed on: %+v (work %+v)", part, part.Work)
	}
	// The fake writes the file in the room's checkout, not a worktree of
	// its own: nothing waits to be merged.
	if part.State != store.TaskDone {
		t.Errorf("the part's state: %s", part.State)
	}

	work, err := l.s.GetWork(l.ctx, msg.ID)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, turn := range work.Turns {
		kinds = append(kinds, turn.Kind+":"+turn.Title)
	}
	if strings.Join(kinds, ", ") != "split:, task:Build the API, sumup:" || work.Title != "plan the API" || work.Ask != "plan the API: pages and auth" {
		t.Errorf("the work: %q (%q / %q)", kinds, work.Title, work.Ask)
	}
	if len(work.Turns[0].Woke) != 1 || work.Turns[0].Woke[0] != coder.ID || len(work.Asked) != 1 || work.Asked[0] != lead.ID {
		t.Errorf("who woke whom: %+v, asked %v", work.Turns[0].Woke, work.Asked)
	}
}
