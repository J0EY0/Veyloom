package wiki

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

func fact(body string) *okf.Document {
	d := okf.New("Fact")
	d.SetBody(body)
	return d
}

func TestRevert_UndoesATurnButKeepsTheLog(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true})
	setup := writer(t, b, "codex/gpt-5.5")
	kept, _ := setup.Create("facts/kept.md", fact("Kept.\n\nSecond line."))
	edited, _ := setup.Create("facts/edited.md", fact("Original."))
	setup.Commit(ctx, "Turn 1")

	turn := writer(t, b, "claude-code/claude-sonnet-5")
	turn.Create("facts/new.md", fact("Made in turn 2."))
	turn.Edit(edited.Path, []Edit{{Op: OpReplace, Target: "Original.", Content: "Changed in turn 2."}}, "")
	sha, err := turn.Commit(ctx, "Turn 2")
	if err != nil {
		t.Fatal(err)
	}
	later := writer(t, b, "codex/gpt-5.5")
	later.Edit(kept.Path, []Edit{{Op: OpAppend, Content: "Added in turn 3."}}, "")
	later.Commit(ctx, "Turn 3")

	undo, err := b.Revert(ctx, sha[:10], okf.Human("owner"), "  it was  wrong ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Page("/facts/new.md"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("the page turn 2 made should be gone: %v", err)
	}
	if p, _ := b.Page(edited.Path); !strings.Contains(p.Doc.Body(), "Original.") {
		t.Errorf("the page turn 2 changed should be back: %q", p.Doc.Body())
	}
	if p, _ := b.Page(kept.Path); !strings.Contains(p.Doc.Body(), "Added in turn 3.") {
		t.Errorf("turn 3's change should stay: %q", p.Doc.Body())
	}
	log := readFile(t, b, "/log.md")
	if !strings.Contains(log, "[new](/facts/new.md) by claude-code/claude-sonnet-5") || !strings.Contains(log, "* **Revert**: undid "+sha[:7]+" (Turn 2) by human:owner: it was wrong") {
		t.Errorf("the log keeps what happened and says what was undone:\n%s", log)
	}
	if strings.Contains(readFile(t, b, "/facts/index.md"), "/facts/new.md") {
		t.Error("the index should no longer list the undone page")
	}
	commits := history(t, b, "")
	if commits[0].SHA != undo || commits[0].Author != "human:owner" || commits[0].Subject != "Undo "+sha[:7]+": Turn 2" {
		t.Errorf("the undo is a commit of its own: %+v", commits[0])
	}
	if status, _ := b.git.status(ctx); len(status) != 0 {
		t.Errorf("nothing should be left uncommitted: %v", status)
	}
}

func TestRevert_StopsWhenALaterChangeGetsInTheWay(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true})
	w := writer(t, b, "codex/gpt-5.5")
	page, _ := w.Create("facts/a.md", fact("One."))
	w.Commit(ctx, "Turn 1")
	w.Edit(page.Path, []Edit{{Op: OpReplace, Target: "One.", Content: "Two."}}, "")
	sha, _ := w.Commit(ctx, "Turn 2")
	w.Edit(page.Path, []Edit{{Op: OpReplace, Target: "Two.", Content: "Three."}}, "")
	w.Commit(ctx, "Turn 3")

	if _, err := b.Revert(ctx, sha, okf.Human("owner"), ""); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("undoing under a later change: %v", err)
	}
	if p, _ := b.Page(page.Path); !strings.Contains(p.Doc.Body(), "Three.") {
		t.Errorf("nothing should change: %q", p.Doc.Body())
	}
	if status, _ := b.git.status(ctx); len(status) != 0 {
		t.Errorf("an abandoned undo leaves nothing behind: %v", status)
	}
	if _, err := os.Stat(filepath.Join(b.Dir(), ".git", "REVERT_HEAD")); !os.IsNotExist(err) {
		t.Error("git should not be left mid-revert")
	}
	if _, err := b.Revert(ctx, "0000000", okf.Human("owner"), ""); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("an unknown commit: %v", err)
	}
}

func TestSync_CommitsOutsideEditsUnderThePersonsName(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true, Human: okf.Human("owner")})
	w := writer(t, b, "codex/gpt-5.5")
	page, _ := w.Create("facts/a.md", fact("A."))
	w.Commit(ctx, "Turn 1")

	// A turn is writing a page it has not committed yet...
	pending := writer(t, b, "codex/gpt-5.5")
	pending.Create("facts/pending.md", fact("Not committed yet."))
	// ...while the person edits one page and adds another in an editor.
	os.WriteFile(b.file(page.Path), []byte("---\ntype: Fact\ntitle: A, edited\n---\n\nEdited by hand.\n"), 0o644)
	os.WriteFile(b.file("/facts/by-hand.md"), []byte("---\ntype: Fact\n---\n\nWritten by hand.\n"), 0o644)
	os.WriteFile(b.file("/notes.md"), []byte("no frontmatter\n"), 0o644)

	changed, err := b.Sync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(changed, " ") != "/facts/a.md /facts/by-hand.md /notes.md" {
		t.Errorf("changed %v", changed)
	}
	if p, _ := b.Page(page.Path); p.Title != "A, edited" {
		t.Errorf("the index should see the edit: %+v", p.Summary)
	}
	if probs := b.Problems(); len(probs) != 1 || probs[0].Path != "/notes.md" {
		t.Errorf("a file that is not a concept is a problem, not a page: %v", probs)
	}
	commits := history(t, b, "")
	if commits[0].Author != "human:owner" || commits[0].Subject != "Record changes made outside Veyloom" {
		t.Errorf("latest commit %+v", commits[0])
	}
	if status, _ := b.git.status(ctx); strings.Join(status, " ") != "/facts/pending.md" {
		t.Errorf("only the pending page stays uncommitted: %v", status)
	}
	log := readFile(t, b, "/log.md")
	for _, want := range []string{
		"* **Update**: [A, edited](/facts/a.md) outside Veyloom",
		"* **Creation**: [by-hand](/facts/by-hand.md) outside Veyloom",
		"* **Creation**: /notes.md outside Veyloom",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log should say %q:\n%s", want, log)
		}
	}
	if _, err := pending.Commit(ctx, "Turn 2"); err != nil {
		t.Fatal(err)
	}
	if commits := history(t, b, "/facts/pending.md"); len(commits) != 1 || commits[0].Author != "codex/gpt-5.5" {
		t.Errorf("the pending page is its writer's: %+v", commits)
	}
}

func TestOpen_RecordsChangesMadeWhileStopped(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{Git: true})
	w := writer(t, b, "codex/gpt-5.5")
	page, _ := w.Create("facts/a.md", fact("A."))
	w.Commit(ctx, "Turn 1")
	os.WriteFile(b.file(page.Path), []byte("---\ntype: Fact\n---\n\nChanged while stopped.\n"), 0o644)

	again, err := Open(ctx, b.Dir(), Options{Layout: ProjectLayout, Git: true, Human: okf.Human("owner")})
	if err != nil {
		t.Fatal(err)
	}
	commits := history(t, again, page.Path)
	if len(commits) != 2 || commits[0].Author != "human:owner" || commits[0].Subject != "Record changes found when Veyloom started" {
		t.Errorf("history %+v", commits)
	}
	if status, _ := again.git.status(ctx); len(status) != 0 {
		t.Errorf("opening leaves nothing uncommitted: %v", status)
	}
}

func TestOpen_MountsAForeignBundleReadOnly(t *testing.T) {
	root := filepath.Join("okf", "testdata", "acme_retail")
	before, _ := os.ReadDir(root)
	b, err := Open(context.Background(), root, Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(b.Pages()); n != 9 {
		t.Errorf("the sample has 9 concepts, indexed %d", n)
	}
	if probs := b.Problems(); len(probs) > 0 {
		t.Errorf("the sample conforms: %v", probs)
	}
	hits := b.Search("gross margin", 3)
	if len(hits) == 0 || hits[0].Path != "/metrics/gross-margin.md" {
		t.Errorf("search %+v", hits)
	}
	if got := b.Backlinks("/metrics/gross-margin-legacy.md"); !strings.Contains(strings.Join(got, " "), "/metrics/gross-margin.md") {
		t.Errorf("relative links resolve too: %v", got)
	}
	if p, _ := b.Page("/metrics/gross-margin.md"); p.Tier != okf.HumanReviewed || p.Status != okf.Stable {
		t.Errorf("trust read from the sample: %+v", p.Summary)
	}
	if _, err := b.Writer(okf.Human("owner")); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("a mounted bundle is not written: %v", err)
	}
	after, _ := os.ReadDir(root)
	if len(before) != len(after) {
		t.Error("opening a read-only bundle must not add files")
	}
}

func TestBundle_WithoutGit(t *testing.T) {
	ctx := context.Background()
	b := openTest(t, Options{})
	w := writer(t, b, "codex/gpt-5.5")
	w.Create("facts/a.md", fact("A."))
	if sha, err := w.Commit(ctx, "Turn 1"); sha != "" || err != nil {
		t.Errorf("commit without git: %q %v", sha, err)
	}
	if !strings.Contains(readFile(t, b, "/log.md"), "* **Creation**: [a](/facts/a.md)") {
		t.Error("the log is kept without git too")
	}
	if commits, err := b.History(ctx, "", 5); commits != nil || err != nil {
		t.Errorf("no history: %v %v", commits, err)
	}
	if _, err := b.Revert(ctx, "abc", okf.Human("owner"), ""); !errors.Is(err, ErrNoHistory) {
		t.Errorf("revert without git: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b.Dir(), ".git")); !os.IsNotExist(err) {
		t.Error("no repository without Options.Git")
	}
}

func TestBundle_Edited(t *testing.T) {
	b := openTest(t, Options{Git: true})
	w := writer(t, b, "codex/gpt-5.5")
	page, _ := w.Create("facts/f.md", fact("One."))
	edited, hash, err := b.Edited(page.Path, []Edit{{Op: OpReplace, Target: "One.", Content: "Two."}})
	if err != nil || hash != page.Hash || !strings.Contains(edited.Body(), "Two.") {
		t.Fatalf("edited %v %q %v", edited, hash, err)
	}
	if p, _ := b.Page(page.Path); !strings.Contains(p.Doc.Body(), "One.") {
		t.Error("previewing edits writes nothing")
	}
	if _, _, err := b.Edited(page.Path, []Edit{{Op: OpReplace, Target: "Three.", Content: "x"}}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("edits that do not match: %v", err)
	}
}
