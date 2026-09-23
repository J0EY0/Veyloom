package wiki

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

func paths[T interface{ path() string }](items []T) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.path()
	}
	return out
}

func (s Summary) path() string { return s.Path }
func (p Page) path() string    { return p.Path }
func (h Hit) path() string     { return h.Path }

// touchAt sets when a page last changed, as an editor saving it would.
func touchAt(t *testing.T, b *Bundle, p string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(b.file(p), at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBundle_LatestAndChanged(t *testing.T) {
	b := openTest(t, Options{})
	if !b.Latest().IsZero() || len(b.Changed(time.Time{})) != 0 {
		t.Fatal("a wiki without pages has no position")
	}
	w := writer(t, b, "codex/default")
	for _, p := range []string{"facts/a.md", "facts/b.md", "facts/c.md"} {
		if _, err := w.Create(p, fact(p)); err != nil {
			t.Fatal(err)
		}
	}
	// Nanoseconds a database would not keep.
	at := time.Date(2026, 9, 21, 2, 0, 0, 123456789, time.UTC)
	touchAt(t, b, "/facts/a.md", at)
	touchAt(t, b, "/facts/b.md", at.Add(time.Minute))
	touchAt(t, b, "/facts/c.md", at.Add(2*time.Minute))

	latest := b.Latest()
	if want := at.Add(2 * time.Minute).Truncate(time.Microsecond); !latest.Equal(want) {
		t.Errorf("Latest = %v, want %v", latest, want)
	}
	if got := b.Changed(latest); len(got) != 0 {
		t.Errorf("nothing changed after the latest change: %v", paths(got))
	}
	// A position read back from where it was stored finds what is newer,
	// newest first.
	stored := at.Truncate(time.Microsecond)
	if got := paths(b.Changed(stored)); !slices.Equal(got, []string{"/facts/c.md", "/facts/b.md"}) {
		t.Errorf("Changed = %v", got)
	}
	if got := b.Changed(time.Time{}); len(got) != 3 {
		t.Errorf("from the start every page is news: %v", paths(got))
	}
}

func TestBundle_Resident(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true})
	agent := writer(t, b, "claude-code/claude-sonnet-5")
	convention := func(title string, tags ...string) *okf.Document {
		d := okf.New("Convention")
		d.SetString(okf.KeyTitle, title)
		d.SetTags(tags)
		d.SetBody(title + ".")
		return d
	}
	// An agent's page, tagged resident, is carried at once (design.md
	// 5.15), with no person's word for it yet.
	naming, _ := agent.Create("conventions/naming.md", convention("命名", "resident"))
	agent.Create("conventions/untagged.md", convention("Untagged"))
	errs, _ := agent.Create("conventions/errors.md", convention("Errors", "Resident", "go"))
	agent.Commit(ctx, "Turn 1")
	if got := paths(b.Resident()); !slices.Equal(got, []string{errs.Path, naming.Path}) {
		t.Fatalf("the tagged pages: %v", got)
	}

	// A person's word puts a page ahead of those only an agent tagged:
	// they are the ones a full brief names instead of carrying.
	person := writer(t, b, okf.Human("owner"))
	person.Verify(naming.Path)
	person.Commit(ctx, "Confirm")
	if got := paths(b.Resident()); !slices.Equal(got, []string{naming.Path, errs.Path}) {
		t.Fatalf("the confirmed page first: %v", got)
	}

	// An agent changing a confirmed page leaves it carried, though no longer
	// as a person left it.
	later := writer(t, b, "codex/default")
	later.Edit(naming.Path, []Edit{{Op: OpAppend, Content: "Changed by an agent."}}, "")
	later.Commit(ctx, "Turn 2")
	if got := paths(b.Resident()); !slices.Equal(got, []string{errs.Path, naming.Path}) {
		t.Errorf("both carried, neither vouched for now: %v", got)
	}

	// A person's own page counts as it is, and a page written by hand in
	// an editor, with no stamp, is a person's too.
	own, _ := person.Create("conventions/own.md", convention("Own", "resident"))
	person.Commit(ctx, "Write")
	hand := "---\ntype: Convention\ntitle: By hand\ntags: [resident]\n---\n\nWritten in an editor.\n"
	if err := os.WriteFile(b.file("/conventions/hand.md"), []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	b.Sync(ctx)
	got := paths(b.Resident())
	if len(got) != 4 || !slices.Contains(got[:2], own.Path) || !slices.Contains(got[:2], "/conventions/hand.md") {
		t.Errorf("a person's pages come first: %v", got)
	}

	// A deprecated page is carried no longer.
	retire := writer(t, b, okf.Human("owner"))
	if _, err := retire.Deprecate(errs.Path, "", "folded into the naming page"); err != nil {
		t.Fatal(err)
	}
	if got := paths(b.Resident()); slices.Contains(got, errs.Path) {
		t.Errorf("deprecated pages are not carried: %v", got)
	}
}

func TestBundle_Relevant(t *testing.T) {
	b := openTest(t, Options{})
	w := writer(t, b, "codex/default")
	page := func(p, typ, title, description, body string) {
		t.Helper()
		d := okf.New(typ)
		d.SetString(okf.KeyTitle, title)
		d.SetString(okf.KeyDescription, description)
		d.SetBody(body)
		if _, err := w.Create(p, d); err != nil {
			t.Fatal(err)
		}
	}
	page("pitfalls/migrations.md", "Pitfall", "改迁移后要删库重建", "goose 不会重跑改过的迁移。", "# 范围\n\n- `internal/store/migrations/00007_approvals.sql`\n")
	page("modules/brief.md", "Module", "简报怎么组装", "每轮发给 agent 的输入。", "见 `internal/hub/brief.go`。")
	page("facts/sqlc.md", "Fact", "sqlc can run without Docker", "go run the pinned version.", "Use go run github.com/sqlc-dev/sqlc.")
	page("topics/misc.md", "Topic", "杂项", "什么都有一点。", "迁移、简报、重建都提过。")
	old := okf.New("Fact")
	old.SetString(okf.KeyTitle, "sqlc needs Docker")
	old.SetBody("internal/store/migrations")
	w.Create("facts/old.md", old)
	w.Deprecate("/facts/old.md", "/facts/sqlc.md", "")

	for _, tc := range []struct {
		name string
		r    Relevance
		want []string
	}{
		{"a file the topic changed", Relevance{Paths: []string{"internal/store/migrations/00007_approvals.sql"}}, []string{"/pitfalls/migrations.md"}},
		{"a file under a directory a page names", Relevance{Paths: []string{"./internal/hub/brief_test.go"}}, []string{"/modules/brief.md"}},
		{"a word in a title", Relevance{Text: "Why does SQLC fail here?"}, []string{"/facts/sqlc.md"}},
		{"Chinese, two characters at a time", Relevance{Text: "我改了迁移文件，库要重建吗？"}, []string{"/pitfalls/migrations.md"}},
		{"a single pair is not enough", Relevance{Text: "简报"}, nil},
		{"only common words", Relevance{Text: "can you please check this for me 看看这个问题"}, nil},
		{"nothing", Relevance{}, nil},
	} {
		if got := paths(b.Relevant(tc.r, 5)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: Relevant = %v, want %v", tc.name, got, tc.want)
		}
	}
	both := b.Relevant(Relevance{Paths: []string{"internal/hub/brief.go"}, Text: "goose 迁移 sqlc"}, 2)
	if got := paths(both); len(got) != 2 || got[0] != "/modules/brief.md" {
		t.Errorf("the named file ranks first, and the limit holds: %v", got)
	}
}

func TestRelevanceTerms(t *testing.T) {
	got := asciiWords("Fix the brief.go of internal/hub, 42 times, and the API; ./web/src/")
	if !slices.Equal(got, []string{"brief.go", "internal/hub", "times", "api", "web/src"}) {
		t.Errorf("asciiWords = %v", got)
	}
	if got := hanPairs("改迁移，删库"); !slices.Equal(got, []string{"改迁", "迁移", "删库"}) {
		t.Errorf("hanPairs = %v", got)
	}
	terms := pathTerms([]string{"/internal/store/migrations/00007_approvals.sql", "main.go", "web/app.tsx"})
	want := map[string]int{
		"internal/store/migrations/00007_approvals.sql": 6, "internal/store/migrations": 4, "internal/store": 2,
		"main.go": 6, "web/app.tsx": 6,
	}
	if len(terms) != len(want) {
		t.Errorf("pathTerms = %v", terms)
	}
	for _, term := range terms {
		if want[term.term] != term.weight {
			t.Errorf("pathTerms: %s weighs %d, want %d", term.term, term.weight, want[term.term])
		}
	}
}

func TestWriter_Tag(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true})
	agent := writer(t, b, "codex/default")
	page, _ := agent.Create("facts/port.md", fact("The hub listens on 7788."))
	agent.Commit(ctx, "Turn 1")

	// A person makes it resident: the tag, and their word for the page.
	person := writer(t, b, okf.Human("owner"))
	tagged, err := person.Tag(page.Path, ResidentTag, true)
	if err != nil {
		t.Fatal(err)
	}
	if !tagged.Tagged("resident") || tagged.Generated.By != "codex/default" || !tagged.Generated.At.Equal(page.Generated.At) {
		t.Errorf("tagging classifies the page, it does not rewrite it: %+v", tagged.Summary)
	}
	if !tagged.Carried() {
		t.Error("the tag makes the page resident")
	}
	person.Verify(page.Path)
	if _, err := person.Commit(ctx, "Resident"); err != nil {
		t.Fatal(err)
	}
	now, _ := b.Page(page.Path)
	if !now.Carried() || now.VouchedAt.IsZero() {
		t.Errorf("tagged and confirmed, the page is carried: %+v", now.Summary)
	}
	if c := history(t, b, page.Path)[0]; c.Author != "human:owner" || len(c.Changes) != 1 || c.Changes[0].Kind != "Update" {
		t.Errorf("the person's commit: %+v", c)
	}

	// Tagging again changes nothing; taking it off does.
	again := writer(t, b, okf.Human("owner"))
	if _, err := again.Tag(page.Path, "RESIDENT", true); err != nil || len(again.Pending()) != 0 {
		t.Errorf("a page tagged already is left alone: %v %v", again.Pending(), err)
	}
	off, err := again.Tag(page.Path, ResidentTag, false)
	if err != nil || off.Tagged(ResidentTag) || off.Carried() {
		t.Errorf("untagged: %+v %v", off.Summary, err)
	}
	if _, err := again.Tag(page.Path, " ", true); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a blank tag: %v", err)
	}
}
