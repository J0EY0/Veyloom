package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
)

// A page long unchecked is due; the maintainer is given it at the next
// upkeep and confirms it, which checks it; a later turn that changes a file
// the page names makes it due again. An agent cannot write who confirmed a
// page by hand.
func TestLoop_TheMaintainerChecksPagesAgain(t *testing.T) {
	l, dir := wikiLoop(t)
	project := l.project()
	// Written by hand long ago, as far as the page says.
	if _, err := l.h.WikiCatalog(l.ctx, project.ID); err != nil {
		t.Fatal(err)
	}
	old := "---\ntype: Decision\ntitle: The payload is json\ndescription: jsonb reorders keys.\ngenerated:\n  by: codex/default\n  at: 2020-01-02T03:04:05Z\n---\n\nKept as json, see internal/hub/approvals.go.\n"
	file := filepath.Join(dir, "projects", project.WikiSlug, "decisions", "payload-json.md")
	if err := os.WriteFile(file, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	review := func() *WikiReview {
		t.Helper()
		catalog, err := l.h.WikiCatalog(l.ctx, project.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range catalog.Pages {
			if p.Path == "/decisions/payload-json.md" {
				if p.CheckedAt == nil {
					t.Error("the page says when it was last checked")
				}
				return p.Review
			}
		}
		t.Fatal("the page is not listed")
		return nil
	}
	if r := review(); r == nil || r.Why != reviewPeriod || r.Every != 180 {
		t.Fatalf("long unchecked, the decision is due: %+v", r)
	}

	keeper := l.member("Keeper", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolPatch, map[string]any{"path": "/decisions/payload-json.md", "edits": []any{map[string]any{
			"op": "replace", "target": "type: Decision", "content": "type: Decision\nverified:\n  - by: human:alice\n    at: 2026-09-23T00:00:00Z",
		}}}),
		call(runtime.UpkeepToolConfirm, map[string]any{"path": "/decisions/payload-json.md"}),
		call(runtime.UpkeepToolConfirm, map[string]any{"path": wiki.MemoryPath}),
	}})
	l.keep(keeper, store.UpkeepDaily)
	if _, err := l.h.PostUserMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, UserID: l.user.ID, Body: "审批的 payload 还是 json 吗？"}); err != nil {
		t.Fatal(err)
	}
	l.h.checkUpkeep(l.ctx)
	upkeep := l.upkeeps(1)[0]
	wantInOrder(t, promptOf(t, upkeep),
		"Check each page under \"Pages to check again\"",
		"Pages to check again (1, in this order):\n- /decisions/payload-json.md \"The payload is json\" (Decision), last checked 2020-01-02 (",
		"pages like it are checked every 180 days",
	)
	reply := l.upkeepReply(upkeep)
	for _, want := range []string{
		"verified says who confirmed the page and when, and is not written by hand",
		"Confirmed /decisions/payload-json.md as it is",
		"the project memory is kept entry by entry",
	} {
		if !strings.Contains(reply, want) {
			t.Errorf("the maintainer hears %q:\n%s", want, reply)
		}
	}
	page, err := l.h.WikiPage(l.ctx, project.ID, "/decisions/payload-json.md")
	if err != nil {
		t.Fatal(err)
	}
	if page.Tier != "machine-confirmed" || len(page.Verified) != 1 || page.Verified[0].By != "fake/default" || page.Review != nil {
		t.Errorf("confirmed by the maintainer, and checked: %+v %+v", page.Verified, page.Review)
	}

	// A turn afterwards changes the file the page names.
	coder := l.member("Coder", map[string]any{"changes": []any{"internal/hub/approvals.go"}, "reply": "改好了。"})
	asked := l.say("@Coder 把 payload 改成 jsonb", "", coder)
	eventually(t, func() bool {
		turns, _ := l.s.ListThreadTurns(l.ctx, l.topic(asked).ID)
		return len(turns) == 1 && turns[0].Status == store.TurnDone
	}, "Coder's turn")
	if r := review(); r == nil || r.Why != reviewChanged || r.File != "internal/hub/approvals.go" || r.TopicNumber != l.topic(asked).Number {
		t.Errorf("the file it names changed, so the page is due again: %+v", r)
	}
}
