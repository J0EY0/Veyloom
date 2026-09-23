package hub

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// briefWiki is newBriefRoom's project with a wiki, and a builder that shows
// it.
type briefWiki struct {
	t      *testing.T
	st     *fakeBriefStore
	in     briefInput
	bundle *wiki.Bundle
	b      *briefBuilder
	clock  time.Time
}

func newBriefWiki(t *testing.T) *briefWiki {
	t.Helper()
	st, in := newBriefRoom()
	in.Triggers = []store.Message{st.messages["m1"]}
	st.project.ID, st.project.WikiSlug = "p1", "veyloom"
	f := &briefWiki{t: t, st: st, in: in, clock: time.Date(2026, 9, 21, 8, 0, 0, 0, time.UTC)}
	// The shelf's bundle, opened on a clock of the test's, so pages are
	// stamped in the order they are written.
	shelf := newWikiShelf(t.TempDir(), func() string { return "alice" }, slog.Default())
	stamps := f.clock
	bundle, err := wiki.Open(context.Background(), filepath.Join(shelf.root, "projects", "veyloom"), wiki.Options{
		Layout: wiki.ProjectLayout, Git: shelf.git, Human: shelf.human(),
		Now: func() time.Time { stamps = stamps.Add(time.Minute); return stamps },
	})
	if err != nil {
		t.Fatal(err)
	}
	shelf.bundles[st.project.ID] = bundle
	f.bundle = bundle
	f.b = newBriefBuilder(st, briefLimits{Thread: 40, Room: 30, Topics: 10, WikiPages: 30, Resident: 4000}, "/var/veyloom/attachments")
	f.b.wikis = shelf
	return f
}

// page writes a page as author and dates the file a minute after the last,
// so the order pages changed in is the order a test wrote them.
func (f *briefWiki) page(author, p, typ, title, description, body string, tags ...string) {
	f.t.Helper()
	d := okf.New(typ)
	d.SetString(okf.KeyTitle, title)
	d.SetString(okf.KeyDescription, description)
	d.SetTags(tags)
	d.SetBody(body)
	w, err := f.bundle.Writer(author)
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := w.Create(p, d); err != nil {
		f.t.Fatal(err)
	}
	if _, err := w.Commit(context.Background(), "test"); err != nil {
		f.t.Fatal(err)
	}
	f.clock = f.clock.Add(time.Minute)
	if err := os.Chtimes(f.bundle.Dir()+p, f.clock, f.clock); err != nil {
		f.t.Fatal(err)
	}
}

func (f *briefWiki) build() brief {
	f.t.Helper()
	b, err := f.b.Build(context.Background(), f.in)
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}

func TestBriefWiki_TheFirstBriefListsEveryPage(t *testing.T) {
	f := newBriefWiki(t)
	f.page("human:alice", "/conventions/naming.md", "Convention", "命名规范", "文件名用小写短横线。", "文件名用小写 ASCII 短横线。\n\n标题照常写中文。", "resident")
	f.page("codex/default", "/decisions/payload-json.md", "Decision", "审批的 payload 用 json", "jsonb 会重排键。", "用 json 原样保存。")
	f.page("codex/default", "/pitfalls/migrations.md", "Pitfall", "改迁移后要删库重建", "goose 不会重跑改过的迁移。", "删库重建。")
	f.page("codex/default", "/facts/old.md", "Fact", "Old", "", "Old.")
	w, _ := f.bundle.Writer("codex/default")
	if _, err := w.Deprecate("/facts/old.md", "", "wrong"); err != nil {
		t.Fatal(err)
	}
	w.Commit(context.Background(), "test")

	b := f.build()
	wantInOrder(t, b.Prompt,
		"The project keeps a wiki of what the team has learned",
		"Resident pages of the project wiki, carried in every turn:",
		"--- /conventions/naming.md: 命名规范\n文件名用小写 ASCII 短横线。\n\n标题照常写中文。\n",
		"The project wiki's pages (read one with read_wiki):",
		"   /decisions/payload-json.md: 审批的 payload 用 json (Decision) - jsonb 会重排键。\n",
		"   /pitfalls/migrations.md: 改迁移后要删库重建 (Pitfall) - goose 不会重跑改过的迁移。\n",
		"This topic, #7",
		">> [alice] @Claude plan the auth refactor",
	)
	for _, not := range []string{"/facts/old.md", "   /conventions/naming.md"} {
		if strings.Contains(b.Prompt, not) {
			t.Errorf("the catalog leaves out deprecated pages and the ones carried in full (%s):\n%s", not, b.Prompt)
		}
	}
	if latest := f.bundle.Latest(); b.Wiki.IsZero() || !b.Wiki.Equal(latest) {
		t.Errorf("the brief's wiki position %v, want the latest change %v", b.Wiki, latest)
	}
}

func TestBriefWiki_LaterBriefsListWhatChanged(t *testing.T) {
	f := newBriefWiki(t)
	f.page("codex/default", "/facts/a.md", "Fact", "A", "The first fact.", "A.")
	seen := f.build().Wiki
	f.in.Session = store.MemberSession{ID: "s1", WikiSeen: &seen, ThreadSeen: map[string]int64{"t7": 80}, RoomSeen: 80}

	if b := f.build(); strings.Contains(b.Prompt, "project wiki's pages") || strings.Contains(b.Prompt, "/facts/a.md") {
		t.Errorf("nothing changed, so no catalog:\n%s", b.Prompt)
	}
	f.page("claude-code/haiku", "/facts/b.md", "Fact", "B", "The second fact.", "B.")
	b := f.build()
	wantInOrder(t, b.Prompt,
		"Pages of the project wiki added or changed since you last looked:",
		"   /facts/b.md: B (Fact) - The second fact.\n",
	)
	if strings.Contains(b.Prompt, "/facts/a.md") {
		t.Errorf("a page the session has seen is not listed again:\n%s", b.Prompt)
	}
	if !b.Wiki.After(seen) {
		t.Errorf("the position moves on: %v, was %v", b.Wiki, seen)
	}
}

func TestBriefWiki_CapsAreCounted(t *testing.T) {
	f := newBriefWiki(t)
	f.b.limits.WikiPages, f.b.limits.Resident = 2, 120
	f.page("human:alice", "/conventions/first.md", "Convention", "First", "", "Short.", "resident")
	f.page("human:alice", "/conventions/long.md", "Convention", "Long", "", strings.Repeat("长", 200), "resident")
	f.page("human:alice", "/conventions/last.md", "Convention", "Last", "", "Short too.", "resident")
	for _, p := range []string{"/facts/a.md", "/facts/b.md", "/facts/c.md"} {
		f.page("codex/default", p, "Fact", strings.ToUpper(p[7:8]), "", "x")
	}
	b := f.build()
	// The latest confirmed first; a page that does not fit ends the part,
	// so the one after it is named too even though it would fit.
	wantInOrder(t, b.Prompt,
		"--- /conventions/last.md: Last\nShort too.\n",
		"(2 more resident pages did not fit here; read_wiki has them: /conventions/long.md, /conventions/first.md)",
		"The project wiki's pages, the 2 changed last of 3 (search_wiki finds the others; read one with read_wiki):",
		"   /facts/b.md: B (Fact)\n   /facts/c.md: C (Fact)\n",
	)
	if strings.Contains(b.Prompt, "长长长") || strings.Contains(b.Prompt, "/facts/a.md") {
		t.Errorf("what does not fit is named, not shown:\n%s", b.Prompt)
	}

	seen := time.Time{}
	f.in.Session = store.MemberSession{ID: "s1", WikiSeen: &seen}
	b = f.build()
	wantInOrder(t, b.Prompt,
		"Pages of the project wiki added or changed since you last looked:",
		"   (and 1 more; search_wiki finds them)",
	)
}

func TestBriefWiki_PagesThatBearOnTheTopic(t *testing.T) {
	f := newBriefWiki(t)
	f.b.limits.WikiPages = 1
	f.page("codex/default", "/modules/brief.md", "Module", "简报怎么组装", "每轮发给 agent 的输入。", "见 `internal/hub/brief.go`。")
	f.page("codex/default", "/pitfalls/auth.md", "Pitfall", "auth refactor needs the session table", "", "x")
	f.page("codex/default", "/facts/unrelated.md", "Fact", "Unrelated", "", "y")
	f.st.members[0].RepoPath = "/Users/alice/veyloom"
	f.st.turns = []store.Turn{{FilesChanged: []string{"/Users/alice/veyloom/internal/hub/brief.go"}}}

	b := f.build()
	wantInOrder(t, b.Prompt,
		"The project wiki's pages, the 1 changed last of 3",
		"   /facts/unrelated.md: Unrelated (Fact)\n",
		"Pages of the project wiki that may bear on this:",
		"   /modules/brief.md: 简报怎么组装 (Module)",
		"   /pitfalls/auth.md: auth refactor needs the session table (Pitfall)",
		"This topic, #7",
	)

	// Back in the topic, only what the agent is asked counts.
	seen := f.bundle.Latest()
	f.in.Session = store.MemberSession{ID: "s1", WikiSeen: &seen, ThreadSeen: map[string]int64{"t7": 80}, RoomSeen: 80}
	f.st.messages["m2"] = user("m2", "@Claude and the auth part?")
	f.st.replies = []store.Message{f.st.messages["m2"]}
	f.in.Triggers = []store.Message{f.st.messages["m2"]}
	b = f.build()
	if !strings.Contains(b.Prompt, "/pitfalls/auth.md") || strings.Contains(b.Prompt, "/modules/brief.md") {
		t.Errorf("later turns look for what they are asked:\n%s", b.Prompt)
	}
}

func TestBriefWiki_AnUnreadableWikiCostsOnlyTheWiki(t *testing.T) {
	f := newBriefWiki(t)
	f.st.project.WikiSlug = ""
	b := f.build()
	if !b.Wiki.IsZero() || !strings.Contains(b.Prompt, ">> [alice] @Claude plan the auth refactor") {
		t.Errorf("the brief goes on without the wiki: %v\n%s", b.Wiki, b.Prompt)
	}
}

func TestRepoPath(t *testing.T) {
	roots := []string{"/Users/alice/veyloom/web", "/Users/alice/veyloom"}
	for _, tc := range []struct{ in, want string }{
		{"/Users/alice/veyloom/internal/hub/brief.go", "internal/hub/brief.go"},
		{"/Users/alice/veyloom/web/src/app.tsx", "src/app.tsx"},
		{"internal/./hub/turns.go", "internal/hub/turns.go"},
		{"/elsewhere/deep/in/some/file.go", "in/some/file.go"},
		{"/file.go", "file.go"},
		{"/Users/alice/veyloom", "Users/alice/veyloom"},
	} {
		if got := repoPath(tc.in, roots); got != tc.want {
			t.Errorf("repoPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// memory writes a memory's entries: the project's, or the personal one.
func (f *briefWiki) memory(scope store.WikiScope, entries ...wiki.MemoryEntry) {
	f.t.Helper()
	b := f.bundle
	if scope == store.WikiPersonal {
		var err error
		if b, err = f.b.wikis.personalMemory(context.Background()); err != nil {
			f.t.Fatal(err)
		}
	}
	kind := memoryOf(scope)
	_, hash, err := b.Memory()
	if err != nil {
		f.t.Fatal(err)
	}
	w, err := b.Writer("human:alice")
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := w.PutMemory(kind.title, kind.description, entries, hash); err != nil {
		f.t.Fatal(err)
	}
	if _, err := w.Commit(context.Background(), "test"); err != nil {
		f.t.Fatal(err)
	}
}

// Every brief carries both memories whole, after what the project is and
// before who is in the chat: the personal one first, and the project's,
// which wins where they differ. Neither is listed again with the pages.
func TestBriefWiki_CarriesTheMemories(t *testing.T) {
	f := newBriefWiki(t)
	f.memory(store.WikiPersonal, wiki.MemoryEntry{Text: "提交说明用英文。", Date: "2026-09-20", Source: "alice"})
	f.memory(store.WikiProject,
		wiki.MemoryEntry{Text: "Reply in Chinese.", Date: "2026-09-21", Source: "alice"},
		wiki.MemoryEntry{Text: "Run make test-db before saying a change is done.", Date: "2026-09-22", Source: "Claude in topic #4"})
	first := f.build()
	if !strings.Contains(first.Prompt, "Project memory, how to work in this project:") || strings.Contains(first.Prompt, "/memory.md") {
		t.Errorf("the first brief carries the memory, and does not list it with the pages:\n%s", first.Prompt)
	}
	// Later briefs carry them too, like the resident pages.
	f.in.Session = store.MemberSession{ID: "s1", WikiSeen: &first.Wiki, ThreadSeen: map[string]int64{"t7": 80}, RoomSeen: 80}
	b := f.build()
	wantInOrder(t, b.Prompt,
		"About the project:",
		"Personal memory, what the person you work for wants in every project (where the project memory says otherwise, it wins):\n- 提交说明用英文。 (2026-09-20, alice)\n",
		"Project memory, how to work in this project:\n- Reply in Chinese. (2026-09-21, alice)\n- Run make test-db before saying a change is done. (2026-09-22, Claude in topic #4)\n",
		"In this chat (mention one as @Name to hand something over):",
		"note it with remember: in the project memory, or with scope personal when they mean every project",
	)

	// One past its budget, edited by hand say, is carried as far as it fits.
	f.b.wikis.budgets.Project = 45
	b = f.build()
	wantInOrder(t, b.Prompt,
		"Project memory, how to work in this project:\n- Reply in Chinese. (2026-09-21, alice)\n(1 more entry did not fit in the 45 characters it may take)\n",
		"In this chat",
	)
}
