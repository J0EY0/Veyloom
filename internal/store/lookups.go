package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/J0EY0/veyloom/internal/store/db"
)

// WikiLookup is one search or read of a chat turn in a wiki (design.md
// 5.23.7): a search has its words and how many pages matched them as
// written, a read the page it read, as the wiki names it.
type WikiLookup struct {
	ID        int64     `json:"id"`
	ProjectID string    `json:"project_id"`
	TurnID    string    `json:"turn_id"`
	Scope     WikiScope `json:"scope"`
	Query     string    `json:"query,omitempty"`
	Hits      int       `json:"hits"`
	Path      string    `json:"path,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Search says the lookup is a search rather than a read.
func (l WikiLookup) Search() bool { return l.Query != "" }

// RecordWikiLookup records a search or a read of a turn in a wiki: the
// project's or the skill library. Exactly one of Query and Path is set.
func (s *Store) RecordWikiLookup(ctx context.Context, l WikiLookup) error {
	if (l.Query == "") == (l.Path == "") || l.Hits < 0 || (l.Scope != WikiProject && l.Scope != WikiLibrary) {
		return fmt.Errorf("wiki lookup %+v: %w", l, ErrInvalidInput)
	}
	pid, err := parseUUID(l.ProjectID)
	if err != nil {
		return err
	}
	tid, err := parseUUID(l.TurnID)
	if err != nil {
		return err
	}
	if err := s.q.RecordWikiLookup(ctx, db.RecordWikiLookupParams{
		ProjectID: pid, TurnID: tid, Scope: string(l.Scope), Query: l.Query, Hits: int32(l.Hits), Path: l.Path,
	}); err != nil {
		return mapPGError("record a wiki lookup", err)
	}
	return nil
}

// ListTurnWikiLookups returns the lookups of the turns given, in the order
// they were made.
func (s *Store) ListTurnWikiLookups(ctx context.Context, turnIDs []string) ([]WikiLookup, error) {
	if len(turnIDs) == 0 {
		return nil, nil
	}
	ids := make([]pgtype.UUID, 0, len(turnIDs))
	for _, id := range turnIDs {
		uid, err := parseUUID(id)
		if err != nil {
			return nil, err
		}
		ids = append(ids, uid)
	}
	rows, err := s.q.ListTurnWikiLookups(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list the wiki lookups of %d turns: %w", len(turnIDs), err)
	}
	out := make([]WikiLookup, 0, len(rows))
	for _, r := range rows {
		out = append(out, WikiLookup{
			ID: r.ID, ProjectID: uuidString(r.ProjectID), TurnID: uuidString(r.TurnID), Scope: WikiScope(r.Scope),
			Query: r.Query, Hits: int(r.Hits), Path: r.Path, CreatedAt: r.CreatedAt.Time,
		})
	}
	return out, nil
}

// WikiUse is how a project's wiki was looked up since a time.
type WikiUse struct {
	// Read are the project wiki's pages read since.
	Read map[string]bool
	// Used says the project wiki was looked up since at all; Covered that
	// lookups were kept from before, so that a page not read since is one
	// no turn read, not one read before lookups were kept.
	Used, Covered bool
}

// WikiLookedUpSince says how a project's wiki was looked up since a time.
func (s *Store) WikiLookedUpSince(ctx context.Context, projectID string, since time.Time) (WikiUse, error) {
	pid, err := parseUUID(projectID)
	if err != nil {
		return WikiUse{}, err
	}
	at := pgtype.Timestamptz{Time: since, Valid: true}
	paths, err := s.q.WikiLookedUpSince(ctx, db.WikiLookedUpSinceParams{ProjectID: pid, CreatedAt: at})
	if err != nil {
		return WikiUse{}, fmt.Errorf("what the wiki of %s was looked up for: %w", projectID, err)
	}
	use := WikiUse{Read: make(map[string]bool, len(paths)), Used: len(paths) > 0}
	for _, p := range paths {
		if p != "" {
			use.Read[p] = true
		}
	}
	if use.Covered, err = s.q.WikiLookupsBefore(ctx, db.WikiLookupsBeforeParams{ProjectID: pid, CreatedAt: at}); err != nil {
		return WikiUse{}, fmt.Errorf("the lookups %s kept: %w", projectID, err)
	}
	return use, nil
}

// PruneWikiLookups drops the lookups older than before, of every project,
// and says how many.
func (s *Store) PruneWikiLookups(ctx context.Context, before time.Time) (int64, error) {
	n, err := s.q.PruneWikiLookups(ctx, pgtype.Timestamptz{Time: before, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("prune the wiki lookups: %w", err)
	}
	return n, nil
}
