package hub

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// A member notes what a person asks it to remember, in the project memory
// or, meant for every project, in the personal one; each is one commit of
// the turn, and the next turn carries both.
func TestLoop_AMemberRemembers(t *testing.T) {
	l, _ := wikiLoop(t)
	writer := l.member("Writer", map[string]any{"tool_calls": []any{
		call(runtime.MemoryToolRemember, map[string]any{"entries": []any{"回复用中文。"}}),
		call(runtime.MemoryToolRemember, map[string]any{"entries": []any{"提交说明用英文。"}, "scope": "personal"}),
		call(runtime.MemoryToolRemember, map[string]any{"entries": []any{"回复用中文。"}}),
		call(runtime.MemoryToolForget, map[string]any{"entries": []any{"没有这一条"}}),
		call(runtime.WikiToolPatch, map[string]any{"path": "/memory.md", "edits": []any{map[string]any{"op": "append", "content": "- sneaked in"}}}),
	}})
	asked := l.say("@Writer 记住：回复用中文；以后所有项目的提交说明都用英文", "", writer)
	first := l.waitTurns(1, store.TurnDone, "Writer's turn")[0]
	project := l.project()
	today := time.Now().Format("2006-01-02")

	mine, err := l.h.Memory(l.ctx, project.ID)
	if want := []wiki.MemoryEntry{{Text: "回复用中文。", Date: today, Source: "Writer in topic #1"}}; err != nil || !slices.Equal(mine.Entries, want) {
		t.Errorf("the project memory: %+v %v", mine.Entries, err)
	}
	everywhere, err := l.h.Memory(l.ctx, "")
	if want := []wiki.MemoryEntry{{Text: "提交说明用英文。", Date: today, Source: "Writer in topic #1 of " + project.Name}}; err != nil || !slices.Equal(everywhere.Entries, want) {
		t.Errorf("the personal memory: %+v %v", everywhere.Entries, err)
	}
	reply := l.root(l.topic(asked)).Body
	for _, want := range []string{
		"Noted in the project memory:\n- 回复用中文。\n",
		"Noted in the personal memory:\n- 提交说明用英文。\n",
		"Already in the project memory, left as it is:\n- 回复用中文。\n",
		`no entry of the project memory says "没有这一条"`,
		"the project memory changes with remember and forget",
	} {
		if !strings.Contains(reply, want) {
			t.Errorf("the agent hears %q:\n%s", want, reply)
		}
	}

	if commits, err := l.h.WikiHistory(l.ctx, project.ID, wiki.MemoryPath, 5); err != nil || len(commits) != 1 || commits[0].Subject != "Writer in topic #1" || commits[0].TurnID != first.ID {
		t.Errorf("the project memory's change: %+v %v", commits, err)
	}
	if commits, err := l.h.MemoryHistory(l.ctx, 5); err != nil || len(commits) == 0 || commits[0].Subject != "Writer of "+project.WikiSlug+" in topic #1" || commits[0].ProjectName != project.Name {
		t.Errorf("the personal memory's change: %+v %v", commits, err)
	}

	l.setOptions(writer, map[string]any{"reply": "好的。"})
	l.say("@Writer 继续", "", writer)
	turns := l.waitTurns(2, store.TurnDone, "the second turn")
	next := turns[0]
	if next.ID == first.ID {
		next = turns[1]
	}
	wantInOrder(t, promptOf(t, next),
		"Personal memory, what the person you work for wants in every project (where the project memory says otherwise, it wins):\n- 提交说明用英文。 ("+today+", Writer in topic #1 of "+project.Name+")\n",
		"Project memory, how to work in this project:\n- 回复用中文。 ("+today+", Writer in topic #1)\n",
		"In this chat",
	)
}

// A person keeps the memories in the UI: what they leave as it was keeps
// its day and source, a change from a stale read is refused, and each
// save is one commit they can undo.
func TestHub_APersonKeepsTheMemories(t *testing.T) {
	l, _ := wikiLoop(t)
	project := l.project()
	today := time.Now().Format("2006-01-02")
	saved, err := l.h.SetMemory(l.ctx, project.ID, []string{"回复用中文。", " ", "- 小步提交。"}, "", l.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := []wiki.MemoryEntry{{Text: "回复用中文。", Date: today, Source: l.user.Name}, {Text: "小步提交。", Date: today, Source: l.user.Name}}
	if !slices.Equal(saved.Entries, want) || saved.Hash == "" || saved.Budget != 3000 || saved.Chars != wiki.MemoryChars(want) || !strings.HasSuffix(saved.File, "/memory.md") {
		t.Fatalf("saved %+v", saved)
	}
	if _, err := l.h.SetMemory(l.ctx, project.ID, []string{"别的"}, "", l.user.ID); !errors.Is(err, store.ErrConflict) {
		t.Errorf("a save from before the memory was there: %v", err)
	}
	if again, err := l.h.SetMemory(l.ctx, project.ID, []string{"回复用中文。", "小步提交。"}, saved.Hash, l.user.ID); err != nil || again.Hash != saved.Hash {
		t.Errorf("saving it as it is changes nothing: %+v %v", again, err)
	}
	commits, err := l.h.WikiHistory(l.ctx, project.ID, wiki.MemoryPath, 5)
	if err != nil || len(commits) != 1 || commits[0].Subject != "Changed the project memory" || commits[0].Author != "human:"+l.user.Name {
		t.Errorf("one commit, the person's: %+v %v", commits, err)
	}
	if _, err := l.h.SetWikiResident(l.ctx, project.ID, wiki.MemoryPath, true, l.user.ID); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("the memory is carried already, not made resident: %v", err)
	}

	// The personal memory, and undoing a change to it.
	if _, err := l.h.SetMemory(l.ctx, "", []string{"提交说明用英文。"}, "", l.user.ID); err != nil {
		t.Fatal(err)
	}
	history, err := l.h.MemoryHistory(l.ctx, 5)
	if err != nil || len(history) != 1 || history[0].Subject != "Changed the personal memory" {
		t.Fatalf("the one change, not the setting up of the bundle: %+v %v", history, err)
	}
	if _, err := l.h.RevertMemory(l.ctx, history[0].SHA, l.user.ID, "too soon"); err != nil {
		t.Fatal(err)
	}
	if undone, _ := l.h.Memory(l.ctx, ""); len(undone.Entries) != 0 || undone.Hash != "" {
		t.Errorf("undone, the memory is gone again: %+v", undone)
	}
}

// The maintainer is told the memories and to keep them.
func TestLoop_TheMaintainerKeepsTheMemories(t *testing.T) {
	l, _ := wikiLoop(t)
	keeper := l.member("Keeper", map[string]any{"tool_calls": []any{
		call(runtime.MemoryToolRemember, map[string]any{"entries": []any{"发版前先跑端到端。"}, "topic": 3}),
	}})
	l.keep(keeper, store.UpkeepDaily)
	if _, err := l.h.SetMemory(l.ctx, l.room.ProjectID, []string{"回复用中文。"}, "", l.user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := l.h.PostUserMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, UserID: l.user.ID, Body: "以后发版前都先跑一遍端到端。"}); err != nil {
		t.Fatal(err)
	}
	l.h.checkUpkeep(l.ctx)
	upkeep := l.upkeeps(1)[0]
	wantInOrder(t, promptOf(t, upkeep),
		"Project memory, how to work in this project:\n- 回复用中文。",
		"Keep the memories, which every turn of every member carries whole.",
		"the memory tools (remember, forget, with scope personal for the personal memory)",
	)
	mine, _ := l.h.Memory(l.ctx, l.room.ProjectID)
	if n := len(mine.Entries); n != 2 || mine.Entries[1].Text != "发版前先跑端到端。" || mine.Entries[1].Source != "Keeper in topic #3" {
		t.Errorf("the maintainer notes where a person said it: %+v", mine.Entries)
	}
}
