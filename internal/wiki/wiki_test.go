package wiki

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// clock hands out times a minute apart, so stamps and commits are ordered.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(time.Minute)
	return c.t
}

func openTest(t *testing.T, opts Options) *Bundle {
	t.Helper()
	if opts.Layout.Dirs == nil {
		opts.Layout = ProjectLayout
	}
	if opts.Now == nil {
		c := &clock{t: time.Date(2026, 9, 21, 1, 0, 0, 0, time.UTC)}
		opts.Now = c.now
	}
	b, err := Open(context.Background(), t.TempDir(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writer(t *testing.T, b *Bundle, author string) *Writer {
	t.Helper()
	w, err := b.Writer(author)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// decision builds a page the way an agent's write_wiki would.
func decision(title, description, body string) *okf.Document {
	d := okf.New("Decision")
	d.SetString(okf.KeyTitle, title)
	d.SetString(okf.KeyDescription, description)
	d.SetBody(body)
	return d
}

func readFile(t *testing.T, b *Bundle, p string) string {
	t.Helper()
	data, err := os.ReadFile(b.file(p))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func history(t *testing.T, b *Bundle, p string) []Commit {
	t.Helper()
	commits, err := b.History(context.Background(), p, 20)
	if err != nil {
		t.Fatal(err)
	}
	return commits
}

func TestOpen_SetsUpABundle(t *testing.T) {
	b := openTest(t, Options{Git: true})
	for p, want := range map[string]string{
		"/index.md":           "okf_version: \"0.2\"",
		"/decisions/index.md": "# Decisions\n",
		"/topics/index.md":    "# Topics\n",
		"/log.md":             "# Project wiki history\n\n## 2026-09-21\n\n* **Initialization**: Set up the bundle.\n",
	} {
		if got := readFile(t, b, p); !strings.Contains(got, want) {
			t.Errorf("%s should contain %q:\n%s", p, want, got)
		}
		if probs := okf.CheckFile(p, []byte(readFile(t, b, p)), p == "/index.md", okf.Strict); len(probs) > 0 {
			t.Errorf("%s: %v", p, probs)
		}
	}
	commits := history(t, b, "")
	if len(commits) != 1 || commits[0].Subject != "Set up the bundle" || commits[0].Author != "process:veyloom" {
		t.Fatalf("a new bundle should have one setup commit: %+v", commits)
	}
	again, err := Open(context.Background(), b.Dir(), Options{Layout: ProjectLayout, Git: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(history(t, again, "")); n != 1 {
		t.Errorf("opening again should not commit, history has %d commits", n)
	}
}

func TestWriter_CreateCommitAndFind(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true})
	w := writer(t, b, "codex/gpt-5.5")
	d := decision("审批的 payload 用 json 不用 jsonb", "jsonb 会重排键，表单字段要按服务端给的顺序显示。",
		"# 结论\n\n用 json 原样保存。[^m812]\n\n[^m812]: 话题 #41 里的讨论")
	d.AddSource(okf.Source{ID: "m812", Resource: "veyloom://rooms/3/messages/812", Title: "话题 #41 里的讨论"})
	page, err := w.Create("decisions/approvals-payload-json.md", d)
	if err != nil {
		t.Fatal(err)
	}
	if page.Path != "/decisions/approvals-payload-json.md" || page.Generated.By != "codex/gpt-5.5" || page.Generated.At.IsZero() || page.Tier != okf.Unverified {
		t.Errorf("created page %+v", page.Summary)
	}
	if got := w.Pending(); len(got) != 1 {
		t.Errorf("pending %v", got)
	}
	if got := b.Search("重排", 5); len(got) != 1 || got[0].Path != page.Path {
		t.Errorf("search by a Chinese word: %+v", got)
	}
	if !strings.Contains(readFile(t, b, "/decisions/index.md"), "* [审批的 payload 用 json 不用 jsonb](/decisions/approvals-payload-json.md) - jsonb 会重排键") {
		t.Errorf("the directory index should list the page:\n%s", readFile(t, b, "/decisions/index.md"))
	}

	sha, err := w.Commit(ctx, "Turn 7 in topic #12", Trailer{"Veyloom-Turn", "7"}, Trailer{"Veyloom-Member", "Codex Implementer"})
	if err != nil || sha == "" {
		t.Fatalf("commit %q %v", sha, err)
	}
	if len(w.Pending()) != 0 {
		t.Error("a commit empties the writer")
	}
	if log := readFile(t, b, "/log.md"); !strings.Contains(log, "* **Creation**: [审批的 payload 用 json 不用 jsonb](/decisions/approvals-payload-json.md) by codex/gpt-5.5\n") {
		t.Errorf("log:\n%s", log)
	}
	commits := history(t, b, page.Path)
	if len(commits) != 1 || commits[0].SHA != sha || commits[0].Author != "codex/gpt-5.5" || commits[0].Subject != "Turn 7 in topic #12" ||
		commits[0].Trailers["Veyloom-Turn"] != "7" || commits[0].Trailers["Veyloom-Member"] != "Codex Implementer" {
		t.Errorf("history %+v", commits)
	}
	want := Change{Kind: "Creation", Path: page.Path, Title: "审批的 payload 用 json 不用 jsonb", Text: "[审批的 payload 用 json 不用 jsonb](/decisions/approvals-payload-json.md) by codex/gpt-5.5"}
	if len(commits[0].Changes) != 1 || commits[0].Changes[0] != want {
		t.Errorf("the commit's changes, read back: %+v", commits[0].Changes)
	}
	if sha, err := w.Commit(ctx, "nothing"); sha != "" || err != nil {
		t.Errorf("committing an empty writer does nothing: %q %v", sha, err)
	}
	status, _ := b.git.status(ctx)
	if len(status) != 0 {
		t.Errorf("everything should be committed: %v", status)
	}
}

func TestWriter_RefusesBadWrites(t *testing.T) {
	b := openTest(t, Options{Git: true})
	w := writer(t, b, "human:owner")
	good := decision("A", "a", "Body.")
	for _, p := range []string{"../x.md", "index.md", "decisions/log.md", ".obsidian/x.md", "decisions/x.txt", "decisions/Upper.md", "决定/x.md", ""} {
		if _, err := w.Create(p, good); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("create %q: %v, want ErrInvalidInput", p, err)
		}
	}
	bad := good.Clone()
	bad.SetString("owner", "me")
	var invalid *InvalidError
	if _, err := w.Create("decisions/bad.md", bad); !errors.As(err, &invalid) || !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a key OKF does not define: %v", err)
	}
	leak := decision("Token", "t", "Use ghp_"+strings.Repeat("a1B2", 9)+" to push.")
	var secret *SecretError
	if _, err := w.Create("decisions/leak.md", leak); !errors.As(err, &secret) || secret.Findings[0].Rule != "github-token" || strings.Contains(err.Error(), "a1B2") {
		t.Errorf("a token should be refused without repeating it: %v", err)
	}
	if _, err := os.Stat(b.file("/decisions/leak.md")); !os.IsNotExist(err) {
		t.Error("a refused page must not be written")
	}
	page, err := w.Create("decisions/a.md", good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Create("decisions/a.md", good); !errors.Is(err, store.ErrConflict) {
		t.Errorf("creating twice: %v", err)
	}
	changed := page.Doc.Clone()
	changed.SetBody("First change.")
	if _, err := w.Put(page.Path, changed, page.Hash); err != nil {
		t.Fatal(err)
	}
	changed.SetBody("From a stale read.")
	if _, err := w.Put(page.Path, changed, page.Hash); !errors.Is(err, store.ErrConflict) {
		t.Errorf("a write from a stale read: %v", err)
	}
	if _, err := b.Writer("nobody"); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("authors are actors: %v", err)
	}
	if _, err := w.Put("decisions/missing.md", good, ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("replacing a missing page: %v", err)
	}
}

func TestWriter_Edit(t *testing.T) {
	b := openTest(t, Options{Git: true})
	page, err := writer(t, b, "codex/gpt-5.5").Create("facts/go-version.md", func() *okf.Document {
		d := okf.New("Fact")
		d.SetString(okf.KeyTitle, "Go version")
		d.SetBody("# Fact\n\n- Go 1.25\n- Postgres 17\n")
		return d
	}())
	if err != nil {
		t.Fatal(err)
	}
	w := writer(t, b, "claude-code/claude-sonnet-5")
	edited, err := w.Edit(page.Path, []Edit{
		{Op: OpReplace, Target: "Go 1.25", Content: "Go 1.26"},
		{Op: OpInsertAfter, Target: "- Go 1.26", Content: "- sqlc through docker"},
		{Op: OpAppend, Content: "Checked on 2026-09-21."},
		{Op: OpReplace, Target: "title: Go version", Content: "title: Toolchain versions"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if body := edited.Doc.Body(); body != "# Fact\n\n- Go 1.26\n- sqlc through docker\n- Postgres 17\nChecked on 2026-09-21.\n" {
		t.Errorf("body %q", body)
	}
	if edited.Title != "Toolchain versions" || edited.Generated.By != "claude-code/claude-sonnet-5" {
		t.Errorf("summary %+v", edited.Summary)
	}
	for _, tc := range []struct {
		edits []Edit
		want  error
	}{
		{[]Edit{{Op: OpReplace, Target: "Go 1.25", Content: "x"}}, store.ErrConflict},
		{[]Edit{{Op: OpReplace, Target: "- ", Content: "x"}}, store.ErrInvalidInput},
		{[]Edit{{Op: "delete", Target: "Go", Content: ""}}, store.ErrInvalidInput},
		{[]Edit{{Op: OpReplace, Target: "type: Fact", Content: "type: [broken"}}, store.ErrInvalidInput},
		{nil, store.ErrInvalidInput},
	} {
		if _, err := w.Edit(page.Path, tc.edits, ""); !errors.Is(err, tc.want) {
			t.Errorf("edits %+v: %v, want %v", tc.edits, err, tc.want)
		}
	}
	if _, err := w.Edit(page.Path, []Edit{{Op: OpAppend, Content: "x"}}, page.Hash); !errors.Is(err, store.ErrConflict) {
		t.Errorf("an edit from a stale read: %v", err)
	}
}

func TestWriter_DeprecateAndVerify(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true})
	agent := writer(t, b, "codex/gpt-5.5")
	old, _ := agent.Create("decisions/payload-jsonb.md", decision("payload 用 jsonb", "查询方便。", "用 jsonb 存 payload。"))
	next, _ := agent.Create("decisions/payload-json.md", decision("payload 用 json", "保留键的顺序。", "用 json 存 payload。"))
	if _, err := agent.Commit(ctx, "Turn 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.Deprecate(old.Path, "decisions/missing.md", ""); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a missing successor: %v", err)
	}
	person := writer(t, b, okf.Human("owner"))
	gone, err := person.Deprecate(old.Path, next.Path, "jsonb 会重排键")
	if err != nil {
		t.Fatal(err)
	}
	if gone.Status != okf.Deprecated || !strings.HasPrefix(gone.Doc.Body(), "> **Deprecated**: jsonb 会重排键\n>\n> Superseded by [payload 用 json](/decisions/payload-json.md).\n\n用 jsonb") {
		t.Errorf("deprecated page %q", gone.Doc.Body())
	}
	successor, _ := b.Page(next.Path)
	if !strings.Contains(successor.Doc.Body(), "> Supersedes [payload 用 jsonb](/decisions/payload-jsonb.md).") {
		t.Errorf("the successor should link back: %q", successor.Doc.Body())
	}
	if _, err := person.Deprecate(old.Path, "", ""); !errors.Is(err, store.ErrConflict) {
		t.Errorf("deprecating twice: %v", err)
	}
	verified, err := person.Verify(next.Path)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Tier != okf.HumanReviewed || verified.Generated.By != "human:owner" {
		// The person wrote the back link, so generated is theirs too.
		t.Errorf("verified %+v", verified.Summary)
	}
	if _, err := person.Commit(ctx, "Retire the jsonb decision"); err != nil {
		t.Fatal(err)
	}
	index := readFile(t, b, "/decisions/index.md")
	if !strings.Contains(index, "# Decisions\n\n* [payload 用 json](/decisions/payload-json.md)") ||
		!strings.Contains(index, "# Deprecated\n\n* [payload 用 jsonb](/decisions/payload-jsonb.md)") {
		t.Errorf("index should list current and deprecated apart:\n%s", index)
	}
	log := readFile(t, b, "/log.md")
	for _, want := range []string{
		"* **Deprecation**: [payload 用 jsonb](/decisions/payload-jsonb.md) by human:owner",
		"* **Update**: [payload 用 json](/decisions/payload-json.md) by human:owner",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log should say %q:\n%s", want, log)
		}
	}
	if hits := b.Search("payload", 5); len(hits) != 2 || hits[0].Path != next.Path {
		t.Errorf("the current page should rank above the deprecated one: %+v", hits)
	}
}

func TestWriter_VerifyAloneIsLoggedAsVerification(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true})
	agent := writer(t, b, "codex/gpt-5.5")
	page, _ := agent.Create("facts/a.md", func() *okf.Document { d := okf.New("Fact"); d.SetBody("A."); return d }())
	agent.Commit(ctx, "Turn 1")
	person := writer(t, b, okf.Human("owner"))
	verified, err := person.Verify(page.Path)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Generated != page.Generated || verified.Tier != okf.HumanReviewed {
		t.Errorf("confirming leaves the generated stamp: %+v vs %+v", verified.Generated, page.Generated)
	}
	person.Commit(ctx, "Confirm")
	if log := readFile(t, b, "/log.md"); !strings.Contains(log, "* **Verification**: [a](/facts/a.md) by human:owner") {
		t.Errorf("log:\n%s", log)
	}
}

func TestWriter_RenameRewritesLinksAndSources(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true})
	w := writer(t, b, "codex/gpt-5.5")
	target, _ := w.Create("facts/b.md", func() *okf.Document { d := okf.New("Fact"); d.SetBody("B, see [myself](/facts/b.md)."); return d }())
	linking := decision("Uses B", "u", "Based on [B](/facts/b.md).[^b]\n\n[^b]: fact B")
	linking.AddSource(okf.Source{ID: "b", Resource: "/facts/b.md"})
	if _, err := w.Create("decisions/uses-b.md", linking); err != nil {
		t.Fatal(err)
	}
	w.Commit(ctx, "Turn 1")
	if got := b.Backlinks(target.Path); len(got) != 1 || got[0] != "/decisions/uses-b.md" {
		t.Fatalf("backlinks %v", got)
	}
	renamed, err := w.Rename(target.Path, "facts/c.md")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Path != "/facts/c.md" || renamed.Doc.Body() != "B, see [myself](/facts/c.md).\n" {
		t.Errorf("renamed %q %q", renamed.Path, renamed.Doc.Body())
	}
	if _, err := b.Page(target.Path); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the old path should be gone: %v", err)
	}
	uses, _ := b.Page("/decisions/uses-b.md")
	if !strings.Contains(uses.Doc.Body(), "[B](/facts/c.md)") || uses.Doc.Sources()[0].Resource != "/facts/c.md" {
		t.Errorf("links and sources should follow: %q %+v", uses.Doc.Body(), uses.Doc.Sources())
	}
	if _, err := w.Commit(ctx, "Rename B"); err != nil {
		t.Fatal(err)
	}
	if log := readFile(t, b, "/log.md"); !strings.Contains(log, "* **Rename**: [c](/facts/c.md) was /facts/b.md by codex/gpt-5.5") {
		t.Errorf("log:\n%s", log)
	}
	if commits := history(t, b, "/facts/c.md"); len(commits) != 2 {
		t.Errorf("history should follow the rename: %+v", commits)
	}
	if status, _ := b.git.status(ctx); len(status) != 0 {
		t.Errorf("the rename should be fully committed: %v", status)
	}
}

func TestWriter_ConcurrentWriters(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true})
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w, err := b.Writer("codex/gpt-5.5")
			if err != nil {
				errs <- err
				return
			}
			name := string(rune('a' + i))
			if _, err := w.Create("facts/"+name+".md", func() *okf.Document { d := okf.New("Fact"); d.SetBody(name); return d }()); err != nil {
				errs <- err
				return
			}
			if _, err := w.Commit(ctx, "Turn "+name); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := len(b.Pages()); n != 8 {
		t.Errorf("pages %d", n)
	}
	if n := len(history(t, b, "")); n != 9 {
		t.Errorf("each writer should have its own commit: %d", n)
	}
}
