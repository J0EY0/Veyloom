package hub

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/J0EY0/veyloom/internal/store"
)

// The Wiki page of the sidebar lists every project's wiki and searches
// through them all (design.md 5.18).
func TestHub_Wikis(t *testing.T) {
	l, dir := wikiLoop(t)
	l.mount(acmeRetail)
	other := l.otherProject("Reseno", "Helper", nil)
	// Pages written into each wiki's folder, as in an editor.
	write := func(project store.Project, files map[string]string) {
		t.Helper()
		if _, err := l.h.WikiCatalog(l.ctx, project.ID); err != nil {
			t.Fatal(err)
		}
		for p, text := range files {
			if err := os.WriteFile(filepath.Join(dir, "projects", project.WikiSlug, filepath.FromSlash(p)), []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(l.project(), map[string]string{
		"facts/port.md":     "---\ntype: Fact\ntitle: Port 7788\ndescription: The hub's port.\n---\n\nThe hub listens on 7788.\n",
		"decisions/old.md":  "---\ntype: Decision\ntitle: Old port\ndescription: 7700.\nstatus: deprecated\n---\n\nThe hub listened on 7700, the port before.\n",
		"pitfalls/stale.md": "---\ntype: Pitfall\ntitle: Stale port\ndescription: Old.\ngenerated:\n  by: codex/default\n  at: 2020-01-02T03:04:05Z\n---\n\nA port pitfall.\n",
	})
	write(other.project, map[string]string{
		"facts/dev-port.md": "---\ntype: Fact\ntitle: Dev port\ndescription: Vite's port.\n---\n\nThe dev server's port is 5173.\n",
	})

	list, err := l.h.Wikis(l.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("two projects, two wikis: %+v", list)
	}
	// In the order the projects were made; the deprecated page and the
	// memory are not counted, the pitfall unchecked since 2020 is due.
	mine, theirs := list[0], list[1]
	if mine.ProjectID != l.room.ProjectID || mine.RoomID != l.room.ID || mine.Pages != 2 || mine.Due != 1 || mine.ChangedAt == nil {
		t.Errorf("this project's wiki: %+v", mine)
	}
	if theirs.ProjectName != "Reseno" || theirs.RoomID != other.room.ID || theirs.Pages != 1 || theirs.Due != 0 {
		t.Errorf("the other's: %+v", theirs)
	}

	hits, err := l.h.SearchWikis(l.ctx, "port", 10)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, hit := range hits {
		got = append(got, hit.ProjectName+" "+hit.Path)
	}
	// Ranked together, whichever wiki holds them: the other project's page
	// scores best, the deprecated page last.
	want := []string{"Reseno /facts/dev-port.md", mine.ProjectName + " /facts/port.md", mine.ProjectName + " /pitfalls/stale.md", mine.ProjectName + " /decisions/old.md"}
	if len(got) != len(want) {
		t.Fatalf("hits: %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hit %d: %q, want %q", i, got[i], want[i])
		}
	}
	if hits[0].RoomID != other.room.ID || hits[0].Snippet == "" {
		t.Errorf("a hit says where it is: %+v", hits[0])
	}
	if hits, _ := l.h.SearchWikis(l.ctx, "port", 2); len(hits) != 2 {
		t.Errorf("at most the limit: %d", len(hits))
	}
	// A mounted bundle is searched from its project's own wiki only.
	if hits, _ := l.h.SearchWikis(l.ctx, "gross margin", 10); len(hits) != 0 {
		t.Errorf("mounted pages are left out: %+v", hits)
	}
}
