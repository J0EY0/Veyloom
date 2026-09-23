package hub

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// acmeRetail is OKF's own sample bundle, which the tests mount.
var acmeRetail = filepath.Join("..", "wiki", "okf", "testdata", "acme_retail")

func (l *loop) mount(dirs ...string) {
	l.t.Helper()
	abs := make([]string, 0, len(dirs))
	for _, d := range dirs {
		a, err := filepath.Abs(d)
		if err != nil {
			l.t.Fatal(err)
		}
		abs = append(abs, a)
	}
	if _, err := l.s.UpdateProject(l.ctx, l.room.ProjectID, store.ProjectPatch{WikiExternalBundles: &abs}); err != nil {
		l.t.Fatal(err)
	}
}

func TestLoop_AgentsReadMountedBundles(t *testing.T) {
	l, _ := wikiLoop(t)
	l.mount(acmeRetail)
	page := "/@acme-retail/metrics/gross-margin.md"
	reader := l.member("Reader", map[string]any{"tool_calls": []any{
		call(runtime.WikiToolSearch, map[string]any{"query": "gross margin"}),
		call(runtime.WikiToolRead, map[string]any{"path": page}),
		call(runtime.WikiToolPatch, map[string]any{"path": page, "reason": "r", "edits": []any{map[string]any{"op": "append", "content": "x"}}}),
		call(runtime.WikiToolRead, map[string]any{"path": "/@nobody/x.md"}),
	}})
	l.say("@Reader what is gross margin?", "", reader)
	turn := l.waitTurns(1, store.TurnDone, "Reader's turn")[0]
	answer := l.root(l.topic(l.topLevel()[0])).Body
	for _, want := range []string{
		page + ": ", // the search found it
		"type: Metric", "(from the bundle " + mustAbs(t, acmeRetail) + " the project mounts, read-only",
		"linked from: /@acme-retail/",
		"read-only: write to the project's own wiki instead",
		"mounts no bundle nobody",
	} {
		if !strings.Contains(answer, want) {
			t.Errorf("the agent lacks %q:\n%s", want, answer)
		}
	}
	if strings.Contains(answer, "store:") {
		t.Errorf("the tools' refusals are told without the store's sentinels:\n%s", answer)
	}
	if brief := promptOf(t, turn); !strings.Contains(brief, "mounts bundles from elsewhere, read-only: acme-retail (9 pages)") {
		t.Errorf("the brief names the mount:\n%s", brief)
	}
}

func mustAbs(t *testing.T, p string) string {
	t.Helper()
	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestHub_MountedBundlesInTheWikiTab(t *testing.T) {
	l, _ := wikiLoop(t)
	before := mustReadDir(t, acmeRetail)
	gone := t.TempDir()
	l.mount(acmeRetail, gone)
	os.RemoveAll(gone)

	catalog, err := l.h.WikiCatalog(l.ctx, l.room.ProjectID)
	if err != nil || len(catalog.Mounts) != 2 {
		t.Fatalf("catalog %+v %v", catalog.Mounts, err)
	}
	acme := catalog.Mounts[0]
	if acme.Name != "acme-retail" || len(acme.Pages) != 9 || acme.Error != "" || !strings.HasPrefix(acme.Pages[0].Path, "/@acme-retail/") || acme.Pages[0].Mount != "acme-retail" {
		t.Errorf("the sample %+v", acme)
	}
	if broken := catalog.Mounts[1]; broken.Error == "" || broken.ErrorCode != "mountNotFolder" || broken.ErrorParams["path"] != gone || len(broken.Pages) != 0 {
		t.Errorf("a folder that went away %+v", broken)
	}

	page := "/@acme-retail/metrics/gross-margin.md"
	view, err := l.h.WikiPage(l.ctx, l.room.ProjectID, page)
	if err != nil || view.Path != page || view.Mount != "acme-retail" {
		t.Fatalf("the page %+v %v", view.WikiPageInfo, err)
	}
	if !strings.Contains(view.Body, "](/@acme-retail/") {
		t.Errorf("its links stay in the mount:\n%s", view.Body)
	}
	if !slices.ContainsFunc(view.Backlinks, func(b WikiLink) bool { return strings.HasPrefix(b.Path, "/@acme-retail/") }) {
		t.Errorf("backlinks %+v", view.Backlinks)
	}
	if !strings.HasPrefix(view.File, mustAbs(t, acmeRetail)) {
		t.Errorf("file %s", view.File)
	}

	hits, err := l.h.SearchWiki(l.ctx, l.room.ProjectID, "gross margin", 5)
	if err != nil || len(hits) == 0 || hits[0].Path != page || hits[0].Mount != "acme-retail" {
		t.Errorf("search %+v %v", hits, err)
	}
	if commits, err := l.h.WikiHistory(l.ctx, l.room.ProjectID, page, 5); err != nil || len(commits) != 0 {
		t.Errorf("history %+v %v", commits, err)
	}
	if _, err := l.h.VerifyWikiPage(l.ctx, l.room.ProjectID, page, l.user.ID); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("confirming a mounted page: %v", err)
	}
	if _, err := l.h.SetWikiResident(l.ctx, l.room.ProjectID, page, true, l.user.ID); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("making a mounted page resident: %v", err)
	}
	if _, err := l.h.WikiPage(l.ctx, l.room.ProjectID, "/@acme-retail/nope.md"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a page the mount lacks: %v", err)
	}
	if after := mustReadDir(t, acmeRetail); len(after) != len(before) {
		t.Errorf("the mount is written: %d entries, were %d", len(after), len(before))
	}
}

func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestHub_LinksIntoAMountAreNotBroken(t *testing.T) {
	l, _ := wikiLoop(t)
	l.mount(acmeRetail)
	project, err := l.s.GetProject(l.ctx, l.room.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := l.h.wikis.project(l.ctx, project)
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.Writer(okf.Human("alice"))
	if err != nil {
		t.Fatal(err)
	}
	d := okf.New("Fact")
	d.SetString(okf.KeyTitle, "Margin in the dashboard")
	d.SetBody("The dashboard shows [gross margin](/@acme-retail/metrics/gross-margin.md), not [net margin](/@acme-retail/metrics/net-margin.md), nor [ours](/facts/gone.md).")
	if _, err := w.Create("/facts/dashboard-margin.md", d); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Commit(l.ctx, "test"); err != nil {
		t.Fatal(err)
	}
	h := mountedHealth(b.Health(time.Now()), l.h.wikis.mounts(l.ctx, project))
	var broken []string
	for _, link := range h.Broken {
		broken = append(broken, link.To)
	}
	if !slices.Equal(broken, []string{"/@acme-retail/metrics/net-margin.md", "/facts/gone.md"}) {
		t.Errorf("broken %v, want the links to pages that are nowhere", broken)
	}
}

func TestCleanWikiMounts(t *testing.T) {
	dir, other := t.TempDir(), t.TempDir()
	got, err := CleanWikiMounts([]string{" " + dir + "/ ", "", dir, other})
	if err != nil || !slices.Equal(got, []string{dir, other}) {
		t.Fatalf("cleaned %v %v", got, err)
	}
	if got, err := CleanWikiMounts(nil); err != nil || got == nil || len(got) != 0 {
		t.Errorf("none %#v %v", got, err)
	}
	for _, bad := range [][]string{{"relative/path"}, {filepath.Join(dir, "missing")}} {
		if _, err := CleanWikiMounts(bad); !errors.Is(err, store.ErrInvalidInput) {
			t.Errorf("%v: %v", bad, err)
		}
	}
	many := make([]string, maxMounts+1)
	for i := range many {
		many[i] = t.TempDir()
	}
	if _, err := CleanWikiMounts(many); !errors.Is(err, store.ErrInvalidInput) {
		t.Errorf("too many: %v", err)
	}
	if names := mountNames([]string{"/a/Data Catalog", "/b/data-catalog", "/c/数据"}); !slices.Equal(names, []string{"data-catalog", "data-catalog-2", "bundle"}) {
		t.Errorf("names %v", names)
	}
}
