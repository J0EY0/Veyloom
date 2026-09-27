package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// wikiLoop is a loop whose hub keeps wikis.
func wikiLoop(t *testing.T) (*loop, string) {
	t.Helper()
	dir := t.TempDir()
	return newLoopWith(t, Config{WikiDir: dir}), dir
}

func call(tool string, args map[string]any) map[string]any {
	return map[string]any{"tool": tool, "args": args}
}

func (l *loop) project() store.Project {
	l.t.Helper()
	p, err := l.s.RoomProject(l.ctx, l.room.ID)
	if err != nil {
		l.t.Fatal(err)
	}
	return p
}

// attach puts a file people send into the attachment directory dir and
// records it for the room, not yet sent with a message.
func (l *loop) attach(dir, roomID, name, mediaType, content string) store.Attachment {
	l.t.Helper()
	id := store.NewID()
	rel := roomID + "/" + id
	if err := os.MkdirAll(filepath.Join(dir, roomID), 0o700); err != nil {
		l.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(content), 0o600); err != nil {
		l.t.Fatal(err)
	}
	a, err := l.s.CreateAttachment(l.ctx, store.NewAttachment{ID: id, RoomID: roomID, Filename: name, MediaType: mediaType, Size: int64(len(content)), Path: rel})
	if err != nil {
		l.t.Fatal(err)
	}
	return a
}

// A page keeps a file a person sent, whole: under /files/<slug>/, shown in
// the page, its source the message it came in (design.md 5.16). Only this
// chat's files are the chat's to keep.
func TestLoop_APageKeepsAFilePeopleSent(t *testing.T) {
	files := t.TempDir()
	l := newLoopWith(t, Config{WikiDir: t.TempDir(), AttachmentDir: files})
	diagram := l.attach(files, l.room.ID, "架构图.png", "image/png", "PNG")
	sent, err := l.h.PostUserMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, UserID: l.user.ID, Body: "这是模块边界图", AttachmentIDs: []string{diagram.ID}})
	if err != nil {
		t.Fatal(err)
	}
	// A second file comes in a topic, not in the chat itself.
	talk, err := l.h.PostUserMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, UserID: l.user.ID, Body: "发版流程再定一下"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := l.s.ThreadForMessage(l.ctx, talk.ID)
	if err != nil {
		t.Fatal(err)
	}
	checklist := l.attach(files, l.room.ID, "checklist.md", "text/markdown", "- [ ] tag")
	inTopic, err := l.h.PostUserMessage(l.ctx, store.NewMessage{RoomID: l.room.ID, ThreadID: thread.ID, UserID: l.user.ID, Body: "检查单", AttachmentIDs: []string{checklist.ID}})
	if err != nil {
		t.Fatal(err)
	}
	other := l.otherProject("elsewhere", "Other", nil)
	stranger := l.attach(files, other.room.ID, "secret.txt", "text/plain", "not yours")
	if _, err := l.h.PostUserMessage(l.ctx, store.NewMessage{RoomID: other.room.ID, UserID: l.user.ID, Body: "elsewhere", AttachmentIDs: []string{stranger.ID}}); err != nil {
		t.Fatal(err)
	}
	writer := l.member("Writer", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, map[string]any{
			"type": "Module", "slug": "module-map", "title": "模块边界", "description": "各模块的边界，见图。",
			"body": "图里画了 hub、machine、runtime 三层。", "files": []any{diagram.ID, checklist.ID},
		}),
		call(runtime.WikiToolWrite, map[string]any{
			"type": "Fact", "slug": "stolen", "title": "别处的文件", "description": "不该存进来。", "body": "x", "files": []any{stranger.ID},
		}),
		call(runtime.WikiToolWrite, map[string]any{
			"scope": "library", "type": "Pattern", "slug": "diagram-first", "title": "先画图", "description": "技能库不存文件。", "body": "x", "files": []any{diagram.ID},
		}),
	}})
	asked := l.say("@Writer 把这张图记进 wiki", "", writer)
	l.waitTurns(1, store.TurnDone, "Writer's turn")

	page, err := l.h.WikiPage(l.ctx, l.room.ProjectID, "/modules/module-map.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.Body, "![架构图.png](/files/module-map/file.png)") {
		t.Errorf("the page shows the picture:\n%s", page.Body)
	}
	if !strings.Contains(page.Body, "[checklist.md](/files/module-map/checklist.markdown)") {
		t.Errorf("the page links the checklist, kept as .markdown:\n%s", page.Body)
	}
	// Each file cites the message it came with: who sent it and when, and
	// its topic when it came in one.
	byMessage := map[string]WikiSource{}
	for _, s := range page.Sources {
		byMessage[s.MessageID] = s
	}
	if s := byMessage[sent.ID]; s.SentBy != l.user.Name || s.SentAt == nil || s.ThreadID != "" || s.TopicNumber != 0 || s.RoomID != l.room.ID {
		t.Errorf("the picture's source, said in the chat itself: %+v", s)
	}
	if s := byMessage[inTopic.ID]; s.SentBy != l.user.Name || s.ThreadID != thread.ID || s.TopicNumber != thread.Number {
		t.Errorf("the checklist's source, said in topic #%d: %+v", thread.Number, s)
	}
	if data, err := l.h.WikiFile(l.ctx, l.room.ProjectID, "/files/module-map/file.png"); err != nil || string(data) != "PNG" {
		t.Errorf("the file kept: %q %v", data, err)
	}
	if _, err := l.h.WikiPage(l.ctx, l.room.ProjectID, "/facts/stolen.md"); err == nil {
		t.Error("a page kept another project's file")
	}
	reply := l.root(l.topic(asked)).Body
	if !strings.Contains(reply, "this chat has no file "+stranger.ID) {
		t.Errorf("the agent is told the file is not the chat's:\n%s", reply)
	}
	if !strings.Contains(reply, "files are kept in the project wiki only") {
		t.Errorf("the agent is told the skill library keeps no files:\n%s", reply)
	}
	if _, err := l.h.LibraryPage(l.ctx, "/patterns/diagram-first.md"); err == nil {
		t.Error("a library page was written without its files")
	}
}

func TestLoop_AgentWritesTheWiki(t *testing.T) {
	l, dir := wikiLoop(t)
	writer := l.member("Writer", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, map[string]any{
			"type": "Pitfall", "slug": "edit-migration-rebuild-db", "title": "改迁移后要删库重建",
			"description": "开发期直接改原迁移，goose 不会重跑，旧库要删掉重建。",
			"body":        "goose 只跑没跑过的迁移。[^goose]\n\n[^goose]: goose 的文档",
			"tags":        []any{"store"}, "topics": []any{1},
			"sources": []any{map[string]any{"id": "goose", "resource": "https://pressly.github.io/goose/", "title": "goose"}},
		}),
		call(runtime.WikiToolPatch, map[string]any{"path": "/pitfalls/edit-migration-rebuild-db.md", "edits": []any{
			map[string]any{"op": "append", "content": "重建用 make db-test。"},
		}, "reason": "补上重建的命令"}),
	}})
	l.say("@Writer note the migration trap", "", writer)
	turns := l.waitTurns(1, store.TurnDone, "Writer's turn")
	// Why it changed the page goes into the wiki's log beside the change.
	if commits, err := l.h.WikiHistory(l.ctx, l.room.ProjectID, "/pitfalls/edit-migration-rebuild-db.md", 5); err != nil || len(commits) == 0 ||
		len(commits[0].Changes) == 0 || !strings.HasSuffix(commits[0].Changes[0].Text, ": 补上重建的命令") {
		t.Errorf("the reason is in the log: %+v %v", commits, err)
	}

	page := filepath.Join(dir, "projects", l.project().WikiSlug, "pitfalls", "edit-migration-rebuild-db.md")
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatalf("the page should be written: %v", err)
	}
	for _, want := range []string{
		"type: Pitfall", "title: 改迁移后要删库重建", "by: fake/default",
		"resource: veyloom://turns/" + turns[0].ID, "resource: veyloom://rooms/" + l.room.ID + "/topics/1", "重建用 make db-test。",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("the page lacks %q:\n%s", want, data)
		}
	}
	answer := l.root(l.topic(l.topLevel()[0])).Body
	if !strings.Contains(answer, "Saved /pitfalls/edit-migration-rebuild-db.md") || !strings.Contains(answer, "Changed /pitfalls/edit-migration-rebuild-db.md") {
		t.Errorf("the agent should hear what happened:\n%s", answer)
	}

	// The turn's writes are one commit, signed by the agent, naming the turn.
	b, err := l.h.wikis.project(l.ctx, l.project())
	if err != nil {
		t.Fatal(err)
	}
	history, err := b.History(l.ctx, "/pitfalls/edit-migration-rebuild-db.md", 5)
	if err != nil || len(history) != 1 {
		t.Fatalf("history %+v %v", history, err)
	}
	if c := history[0]; c.Author != "fake/default" || c.Subject != "Writer in topic #1" || c.Trailers["Veyloom-Turn"] != turns[0].ID || c.Trailers["Veyloom-Member"] != "Writer" {
		t.Errorf("commit %+v", c)
	}
	// And the turn keeps the page it wrote, for the task board.
	if pages := turns[0].WikiPages; len(pages) != 1 || pages[0] != "/pitfalls/edit-migration-rebuild-db.md" {
		t.Errorf("the turn's wiki pages: %q", pages)
	}
	if standing := systemPromptOf(t, turns[0]); !strings.Contains(standing, "The project keeps a wiki of what the team has learned") || !strings.Contains(standing, "Write to it only when a person asks you to") {
		t.Errorf("the standing instructions should point at the wiki:\n%s", standing)
	}

	// Another member finds it.
	reader := l.member("Reader", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolSearch, map[string]any{"query": "迁移"}),
		call(runtime.WikiToolRead, map[string]any{"path": "/pitfalls/edit-migration-rebuild-db.md"}),
	}})
	asked := l.say("@Reader what do we know about migrations?", "", reader)
	l.waitTurns(2, store.TurnDone, "Reader's turn")
	found := l.root(l.topic(asked)).Body
	for _, want := range []string{
		`Pages matching "迁移", best first:`,
		"/pitfalls/edit-migration-rebuild-db.md: 改迁移后要删库重建 (Pitfall)",
		"goose 只跑没跑过的迁移。",
	} {
		if !strings.Contains(found, want) {
			t.Errorf("Reader's answer lacks %q:\n%s", want, found)
		}
	}
}

// Decisions, conventions and deprecations take effect at once like the
// rest (docs/design.md 5.15): a person looks afterwards, and undoes what
// they do not want, saying why for the log.
func TestLoop_DecisionsTakeEffectAtOnce(t *testing.T) {
	l, dir := wikiLoop(t)
	recorder := l.member("Recorder", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, map[string]any{
			"type": "Decision", "slug": "payload-json", "title": "审批的 payload 用 json",
			"description": "jsonb 会重排键。", "body": "用 json 原样保存。",
		}),
		call(runtime.WikiToolWrite, map[string]any{
			"type": "Decision", "slug": "payload-json", "title": "again", "description": "d", "body": "b",
		}),
	}})
	asked := l.say("@Recorder record the payload decision in the wiki", "", recorder)
	l.waitTurns(1, store.TurnDone, "Recorder's turn")
	project := l.project()
	b, _ := l.h.wikis.project(l.ctx, project)
	written, err := b.Page("/decisions/payload-json.md")
	if err != nil {
		t.Fatalf("the decision is written at once: %v", err)
	}
	if written.Generated.By != "fake/default" || written.Tier == okf.HumanReviewed {
		t.Errorf("the page is the agent's, not yet confirmed: %+v", written.Summary)
	}
	answer := l.root(l.topic(asked)).Body
	if !strings.Contains(answer, "Saved /decisions/payload-json.md") || !strings.Contains(answer, "there is a page at /decisions/payload-json.md already") {
		t.Errorf("the agent should hear it is saved, and the second try that it is there:\n%s", answer)
	}

	// A deprecation, at once too; the person undoes it, saying why.
	deprecator := l.member("Deprecator", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolDeprecate, map[string]any{"path": "/decisions/payload-json.md", "reason": "we moved to protobuf"}),
	}})
	l.say("@Deprecator retire the payload decision", "", deprecator)
	turns := l.waitTurns(2, store.TurnDone, "Deprecator's turn")
	if gone, _ := b.Page("/decisions/payload-json.md"); gone.Status != okf.Deprecated {
		t.Fatalf("deprecated at once: %+v", gone.Summary)
	}
	history, err := l.h.WikiHistory(l.ctx, project.ID, "/decisions/payload-json.md", 5)
	if err != nil || len(history) != 2 || history[0].TurnID != turns[0].ID {
		t.Fatalf("history %+v %v", history, err)
	}
	if _, err := l.h.RevertWiki(l.ctx, project.ID, history[0].SHA, l.user.ID, "not yet"); err != nil {
		t.Fatal(err)
	}
	if back, _ := b.Page("/decisions/payload-json.md"); back.Status != okf.Stable {
		t.Errorf("the undo brings it back: %+v", back.Summary)
	}
	log, _ := os.ReadFile(filepath.Join(dir, "projects", project.WikiSlug, "log.md"))
	if !strings.Contains(string(log), "by human:alice: not yet") {
		t.Errorf("the log keeps why it was undone:\n%s", log)
	}
}

func TestLoop_WikiToolsWithoutWikis(t *testing.T) {
	l := newLoop(t)
	agent := l.member("Agent", map[string]any{"tool_calls": []any{call(runtime.WikiToolSearch, map[string]any{"query": "x"})}})
	asked := l.say("@Agent look", "", agent)
	turns := l.waitTurns(1, store.TurnDone, "the turn")
	if answer := l.root(l.topic(asked)).Body; !strings.Contains(answer, "error: this Veyloom keeps no wikis") {
		t.Errorf("answer %q", answer)
	}
	if prompt := promptOf(t, turns[0]); strings.Contains(prompt, "keeps a wiki") {
		t.Error("a hub without wikis does not point at one")
	}
}

// fact is write_wiki's arguments for a fact.
func fact(slug, title, body string) map[string]any {
	return map[string]any{"type": "Fact", "slug": slug, "title": title, "description": title + ".", "body": body}
}

func TestLoop_BriefsListTheWikiOnce(t *testing.T) {
	l, _ := wikiLoop(t)
	writerA := l.member("WriterA", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, fact("parser-grammar", "The grammar lives in one file", "See `internal/parser/grammar.go`.")),
	}})
	l.say("@WriterA note where the grammar is", "", writerA)
	l.waitTurns(1, store.TurnDone, "WriterA's turn")

	// A session that has not seen the wiki gets its catalog.
	reader := l.member("Reader", nil)
	asked := l.say("@Reader hello", "", reader)
	turns := l.waitTurns(2, store.TurnDone, "Reader's first turn")
	if prompt := promptOf(t, turns[0]); !strings.Contains(prompt, "The project wiki's pages (read one with read_wiki):\n   /facts/parser-grammar.md: The grammar lives in one file (Fact)") {
		t.Errorf("the first brief lists the wiki:\n%s", prompt)
	}
	session, err := l.s.GetOpenSession(l.ctx, reader.ID)
	if err != nil || session.WikiSeen == nil {
		t.Fatalf("the session has seen the wiki: %+v %v", session, err)
	}

	// Then only what changed, and nothing when nothing did.
	thread := l.topic(asked)
	l.say("@Reader again", thread.ID, reader)
	turns = l.waitTurns(3, store.TurnDone, "Reader's second turn")
	if prompt := promptOf(t, turns[0]); strings.Contains(prompt, "parser-grammar") || strings.Contains(prompt, "project wiki's pages") {
		t.Errorf("nothing changed in the wiki:\n%s", prompt)
	}
	writerB := l.member("WriterB", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, fact("lexer-tokens", "Tokens are ASCII only", "The lexer rejects the rest.")),
	}})
	l.say("@WriterB note the tokens", "", writerB)
	l.waitTurns(4, store.TurnDone, "WriterB's turn")
	l.say("@Reader and now?", thread.ID, reader)
	turns = l.waitTurns(5, store.TurnDone, "Reader's third turn")
	prompt := promptOf(t, turns[0])
	if !strings.Contains(prompt, "Pages of the project wiki added or changed since you last looked:\n   /facts/lexer-tokens.md: Tokens are ASCII only (Fact)") || strings.Contains(prompt, "parser-grammar") {
		t.Errorf("only the new page is news:\n%s", prompt)
	}
}

func TestLoop_BriefsFindPagesByTheFilesOfTheTopic(t *testing.T) {
	l := newLoopWith(t, Config{WikiDir: t.TempDir(), BriefWikiPages: 1})
	writer := l.member("Writer", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, fact("parser-grammar", "The grammar lives in one file", "See `internal/parser/grammar.go`.")),
		call(runtime.WikiToolWrite, fact("release-steps", "Releases go out on Fridays", "Tag, then build.")),
	}})
	l.say("@Writer note these", "", writer)
	l.waitTurns(1, store.TurnDone, "Writer's turn")

	fixer := l.member("Fixer", map[string]any{"changes": []any{"internal/parser/grammar.go"}})
	fixed := l.say("@Fixer fix the parser", "", fixer)
	l.waitTurns(2, store.TurnDone, "Fixer's turn")

	helper := l.member("Helper", nil)
	l.say("@Helper have a look", l.topic(fixed).ID, helper)
	turns := l.waitTurns(3, store.TurnDone, "Helper's turn")
	prompt := promptOf(t, turns[0])
	if !strings.Contains(prompt, "Pages of the project wiki that may bear on this:\n   /facts/parser-grammar.md: The grammar lives in one file (Fact)") {
		t.Errorf("the page on the file the topic changed is pointed out:\n%s", prompt)
	}
}

func TestLoop_ACompactionListsTheWikiAgain(t *testing.T) {
	l, _ := wikiLoop(t)
	writer := l.member("Writer", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolWrite, fact("parser-grammar", "The grammar lives in one file", "See `internal/parser/grammar.go`.")),
	}})
	l.say("@Writer note it", "", writer)
	l.waitTurns(1, store.TurnDone, "Writer's turn")

	echo := l.member("Echo", map[string]any{"compact": true})
	asked := l.say("@Echo start", "", echo)
	l.waitTurns(2, store.TurnDone, "Echo's first turn")
	var session store.MemberSession
	eventually(t, func() bool {
		session, _ = l.s.GetOpenSession(l.ctx, echo.ID)
		return session.Compactions == 1
	}, "the compaction to be counted")
	if session.WikiSeen != nil {
		t.Errorf("a compaction forgets the catalog: %v", session.WikiSeen)
	}
	l.say("and now?", l.topic(asked).ID)
	turns := l.waitTurns(3, store.TurnDone, "Echo's second turn")
	if prompt := promptOf(t, turns[0]); !strings.Contains(prompt, "The project wiki's pages (read one with read_wiki):") {
		t.Errorf("after a compaction the catalog comes in full again:\n%s", prompt)
	}
}
