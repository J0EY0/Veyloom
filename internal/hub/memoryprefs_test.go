package hub

import (
	"strings"
	"sync"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
)

// The person turns the memories off and on (design.md 5.19): what is off
// is not carried into turns and no member changes it; with memory off as a
// whole, turns hear nothing of it and have no memory tools. What the
// memories hold stays as it was.
func TestLoop_MemorySwitches(t *testing.T) {
	var mu sync.Mutex
	prefs := store.DefaultMemoryPrefs
	set := func(p store.MemoryPrefs) {
		mu.Lock()
		defer mu.Unlock()
		prefs = p
	}
	get := func() store.MemoryPrefs {
		mu.Lock()
		defer mu.Unlock()
		return prefs
	}
	l := newLoopWith(t, Config{WikiDir: t.TempDir()}, WithMemoryPrefs(get))
	writer := l.member("Writer", nil)
	// ask has the writer make the calls in a topic of its own, and gives
	// its turn's prompt and what it replied.
	ask := func(text string, calls ...any) (string, string) {
		t.Helper()
		l.setOptions(writer, map[string]any{"tool_calls": calls})
		asked := l.say("@Writer "+text, "", writer)
		var turn store.Turn
		eventually(t, func() bool {
			turns, _ := l.s.ListThreadTurns(l.ctx, l.topic(asked).ID)
			if len(turns) == 1 && turns[0].Status == store.TurnDone {
				turn = turns[0]
				return true
			}
			return false
		}, "the writer's turn")
		return promptOf(t, turn), l.root(l.topic(asked)).Body
	}

	ask("记住",
		call(runtime.MemoryToolRemember, map[string]any{"entries": []any{"回复用中文。"}}),
		call(runtime.MemoryToolRemember, map[string]any{"entries": []any{"提交说明用英文。"}, "scope": "personal"}),
	)

	set(store.MemoryPrefs{Enabled: true, Personal: false, Project: true})
	prompt, reply := ask("再记一条", call(runtime.MemoryToolRemember, map[string]any{"entries": []any{"每步都写测试。"}, "scope": "personal"}))
	if strings.Contains(prompt, "Personal memory") || !strings.Contains(prompt, "Project memory, how to work in this project:\n- 回复用中文。") {
		t.Errorf("with the personal memory off, only the project's is carried:\n%s", prompt)
	}
	if !strings.Contains(prompt, "note it with remember in the project memory") || strings.Contains(prompt, "scope personal") {
		t.Errorf("the member hears of the project memory alone:\n%s", prompt)
	}
	if !strings.Contains(reply, "the person has turned the personal memory off") {
		t.Errorf("the personal memory refuses a change: %q", reply)
	}
	if personal, err := l.h.Memory(l.ctx, ""); err != nil || len(personal.Entries) != 1 {
		t.Errorf("what it holds stays as it was: %+v %v", personal.Entries, err)
	}

	set(store.MemoryPrefs{Enabled: false, Personal: true, Project: true})
	prompt, reply = ask("第三次", call(runtime.MemoryToolRemember, map[string]any{"entries": []any{"不会记下。"}}))
	if strings.Contains(prompt, "Project memory") || strings.Contains(prompt, "Personal memory") || strings.Contains(prompt, "remember") {
		t.Errorf("with memory off, turns hear nothing of it:\n%s", prompt)
	}
	if !strings.Contains(reply, "this turn has no tool remember") {
		t.Errorf("and have no memory tools: %q", reply)
	}
	if project, err := l.h.Memory(l.ctx, l.room.ProjectID); err != nil || len(project.Entries) != 1 {
		t.Errorf("the project memory keeps what it held: %+v %v", project.Entries, err)
	}
}

// The maintainer keeps the memories turns use, and names only their tools.
func TestUpkeepMemoryStep(t *testing.T) {
	both := memoryStep(store.DefaultMemoryPrefs)
	if !strings.Contains(both, "into the project memory") || !strings.Contains(both, "into the personal memory (scope personal)") {
		t.Errorf("both: %q", both)
	}
	project := memoryStep(store.MemoryPrefs{Enabled: true, Project: true})
	if !strings.Contains(project, "Keep the project memory") || strings.Contains(project, "personal") {
		t.Errorf("the project's alone: %q", project)
	}
	personal := memoryStep(store.MemoryPrefs{Enabled: true, Personal: true})
	if !strings.Contains(personal, "Keep the personal memory") || !strings.Contains(personal, "scope personal") {
		t.Errorf("the personal alone: %q", personal)
	}
	off := store.MemoryPrefs{Personal: true, Project: true}
	if memoryStep(off) != "" || memoryToolsLine(off) != "" {
		t.Error("memory off: no step, no tools")
	}
	if got := memoryToolsLine(store.DefaultMemoryPrefs); got != "the memory tools (remember, forget, with scope personal for the personal memory); " {
		t.Errorf("tools: %q", got)
	}
}
