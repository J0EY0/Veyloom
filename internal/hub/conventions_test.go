package hub

import (
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// A project's wiki keeps its own conventions, LLM Wiki's schema: the
// maintainer writes them first when there are none, carries them whole
// when there are, and keeps them up to date with what people say; the
// members follow them when they write.
func TestLoop_WikiConventions(t *testing.T) {
	l, _ := wikiLoop(t)
	home := l.project()
	keeper := l.member("Keeper", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, map[string]any{
			"type": "Convention", "slug": "wiki", "title": "Wiki 约定", "description": "这个 wiki 记什么、不记什么、怎么写。",
			"body": "# 记什么\n\n- 决定和理由。\n\n# 怎么写\n\n- 用中文写，一页一件事。",
		}),
	}})
	l.keep(keeper, store.UpkeepManual)
	if _, err := l.h.StartUpkeep(l.ctx, home.ID); err != nil {
		t.Fatal(err)
	}
	first := l.upkeeps(1)[0]
	if brief := promptOf(t, first); !strings.Contains(brief, "This wiki has no page of its own conventions yet. Write one first, /conventions/wiki.md") {
		t.Errorf("the first upkeep writes the conventions:\n%s", brief)
	}
	r, _ := l.h.openWiki(l.ctx, home.ID)
	if page, err := r.bundle.Page(wiki.ConventionsPath); err != nil || page.Title != "Wiki 约定" {
		t.Fatalf("the conventions page: %+v %v", page.Summary, err)
	}

	l.setOptions(keeper, nil)
	if _, err := l.h.StartUpkeep(l.ctx, home.ID); err != nil {
		t.Fatal(err)
	}
	brief := promptOf(t, l.upkeeps(2)[0])
	for _, want := range []string{
		"The wiki's conventions, /conventions/wiki.md, which people set and you keep; where they and the steps below differ, the conventions win:",
		"- 用中文写，一页一件事。",
		"put it into its conventions page, /conventions/wiki.md, with patch_wiki",
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("the next upkeep's brief lacks %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "no page of its own conventions yet") {
		t.Errorf("it has them now:\n%s", brief)
	}

	// The members follow them when they write.
	coder := l.member("Coder", nil)
	l.say("@Coder hello", "", coder)
	l.settled(3, "Coder's turn")
	var turn store.Turn
	for _, one := range l.turns() {
		if one.MemberID == coder.ID {
			turn = one
		}
	}
	if standing := systemPromptOf(t, turn); !strings.Contains(standing, "The wiki's own conventions, what goes into it and how its pages are written, are on /conventions/wiki.md") {
		t.Errorf("the standing instructions:\n%s", standing)
	}
}
