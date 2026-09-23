package hub

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/J0EY0/veyloom/internal/store"
	"github.com/J0EY0/veyloom/internal/wiki"
	"github.com/J0EY0/veyloom/internal/wiki/okf"
)

// Every project's wiki at once, for the Wiki page of the sidebar
// (docs/design.md 5.18): a line about each, and a search through all of
// them.

// WikiSummary is one project's wiki as the list of them all shows it.
type WikiSummary struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	RoomID      string `json:"room_id"`
	// Pages counts the current pages, the project memory not among them.
	Pages int `json:"pages"`
	// Due counts those due to be checked again (5.16).
	Due int `json:"due"`
	// ChangedAt is when the wiki last changed: its latest commit, or, kept
	// without git, the latest a page was written.
	ChangedAt *time.Time `json:"changed_at,omitempty"`
}

// WikiProjectHit is a page found searching every project's wiki, with the
// project whose wiki holds it.
type WikiProjectHit struct {
	WikiHit
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	RoomID      string `json:"room_id"`
}

// Wikis lists every project's wiki, in the order the projects were made.
// A wiki that cannot be read costs the list its line, not the list.
func (h *Hub) Wikis(ctx context.Context) ([]WikiSummary, error) {
	projects, err := h.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	now := h.now()
	out := make([]WikiSummary, 0, len(projects))
	for _, p := range projects {
		r, err := h.openProjectWiki(ctx, p)
		if err != nil {
			h.logger.Warn("list the wikis: open one", "project", p.ID, "err", err)
			continue
		}
		var live []wiki.Summary
		for _, s := range r.bundle.Pages() {
			if s.Path != wiki.MemoryPath && s.Status != okf.Deprecated {
				live = append(live, s)
			}
		}
		reviews, err := wikiReviews(ctx, h.store, p.ID, live, now)
		if err != nil {
			h.logger.Warn("list the wikis: find the pages to check again", "project", p.ID, "err", err)
		}
		out = append(out, WikiSummary{
			ProjectID: p.ID, ProjectName: p.Name, RoomID: p.MainRoomID,
			Pages: len(live), Due: len(reviews), ChangedAt: h.lastChange(ctx, r.bundle),
		})
	}
	return out, nil
}

// SearchWikis finds pages in every project's wiki by their text, best
// first. Every wiki scores alike, so their hits rank together. The bundles
// a project mounts are left out: several projects may mount one folder.
func (h *Hub) SearchWikis(ctx context.Context, query string, limit int) ([]WikiProjectHit, error) {
	projects, err := h.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	now := h.now()
	type scored struct {
		hit   WikiProjectHit
		score int
	}
	var all []scored
	for _, p := range projects {
		r, err := h.openProjectWiki(ctx, p)
		if err != nil {
			h.logger.Warn("search the wikis: open one", "project", p.ID, "err", err)
			continue
		}
		for _, hit := range r.bundle.Search(query, limit) {
			all = append(all, scored{
				hit: WikiProjectHit{
					WikiHit:   WikiHit{WikiPageInfo: pageInfo(hit.Summary, now), Snippet: hit.Snippet},
					ProjectID: p.ID, ProjectName: p.Name, RoomID: p.MainRoomID,
				},
				score: hit.Score(),
			})
		}
	}
	// Projects in the order they were made, pages as each wiki ranks them,
	// break ties.
	slices.SortStableFunc(all, func(a, b scored) int { return cmp.Compare(b.score, a.score) })
	out := make([]WikiProjectHit, 0, min(len(all), limit))
	for _, s := range all[:min(len(all), limit)] {
		out = append(out, s.hit)
	}
	return out, nil
}

// openProjectWiki opens a project's wiki, as openWiki does one looked up.
func (h *Hub) openProjectWiki(ctx context.Context, project store.Project) (wikiRef, error) {
	b, err := h.wikis.project(ctx, project)
	if err != nil {
		return wikiRef{}, err
	}
	h.wikis.sync(ctx, b, project.ID, project.MainRoomID)
	return wikiRef{scope: store.WikiProject, project: project, bundle: b}, nil
}

// lastChange is when a wiki last changed: its latest commit, or, kept
// without git, the latest a page was written; nil for a wiki never
// written.
func (h *Hub) lastChange(ctx context.Context, b *wiki.Bundle) *time.Time {
	commits, err := b.History(ctx, "", 1)
	if err != nil {
		h.logger.Warn("list the wikis: read the latest change", "err", err)
	}
	if len(commits) > 0 {
		return &commits[0].At
	}
	var latest time.Time
	for _, s := range b.Pages() {
		if s.Modified.After(latest) {
			latest = s.Modified
		}
	}
	if latest.IsZero() {
		return nil
	}
	return &latest
}
