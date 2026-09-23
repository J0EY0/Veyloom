package hub

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/J0EY0/veyloom/internal/runtime"
	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

func TestWikiActors(t *testing.T) {
	for _, tc := range []struct{ runtime, model, want string }{
		{"claude", "claude-sonnet-5", "claude-code/claude-sonnet-5"},
		{"codex", "", "codex/default"},
		{"pi", "deepseek/deepseek-chat", "pi/deepseek/deepseek-chat"},
		{"", "some model", "agent/some-model"},
	} {
		got := agentActor(tc.runtime, tc.model)
		if got != tc.want || !okf.ValidActor(got) {
			t.Errorf("agentActor(%q, %q) = %q, want %q", tc.runtime, tc.model, got, tc.want)
		}
	}
	if got := humanActor(" Jing Hao "); got != "human:Jing-Hao" || !okf.ValidActor(got) {
		t.Errorf("humanActor = %q", got)
	}
	if humanActor("  ") != "" {
		t.Error("no name, no actor")
	}
}

func TestWikiPageTypesAreTheLayouts(t *testing.T) {
	var types []string
	for _, d := range wiki.ProjectLayout.Dirs {
		types = append(types, d.Type)
	}
	if !slices.Equal(types, runtime.WikiPageTypes) {
		t.Errorf("write_wiki offers %v, the wiki keeps %v", runtime.WikiPageTypes, types)
	}
}

func TestWikiShelf_OpensOnceAndArchives(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	shelf := newWikiShelf(root, func() string { return "alice" }, slog.Default())
	p := store.Project{ID: "p1", WikiSlug: "veyloom"}
	b, err := shelf.project(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := shelf.project(ctx, p); again != b {
		t.Error("a project's wiki is opened once")
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "veyloom", "index.md")); err != nil {
		t.Errorf("the wiki lives in projects/<slug>: %v", err)
	}
	if _, err := shelf.project(ctx, store.Project{ID: "p2"}); err == nil {
		t.Error("a project without a wiki folder has no wiki")
	}
	now := time.Date(2026, 9, 21, 8, 30, 0, 0, time.UTC)
	if err := shelf.archive("p1", "veyloom", now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "archive", "projects", "veyloom-20260921-083000", "log.md")); err != nil {
		t.Errorf("the wiki should be put aside, not deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "veyloom")); !os.IsNotExist(err) {
		t.Error("the folder is free for a new project")
	}
	if err := shelf.archive("p9", "never-opened", now); err != nil {
		t.Errorf("archiving a wiki never written is nothing: %v", err)
	}
	var none *wikiShelf
	if _, err := none.project(ctx, p); !errors.Is(err, ErrNoWikis) {
		t.Errorf("a hub without wikis: %v", err)
	}
}
