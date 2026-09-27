-- name: RecordWikiLookup :exec
-- Records one search or read of a chat turn in a wiki (design.md 5.23.7).
INSERT INTO wiki_lookups (project_id, turn_id, scope, query, hits, path)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListTurnWikiLookups :many
-- The lookups of the turns given, in the order they were made.
SELECT * FROM wiki_lookups WHERE turn_id = ANY(@turn_ids::uuid[]) ORDER BY id;

-- name: WikiLookedUpSince :many
-- What the project's wiki was looked up for since a time: every page read,
-- and an empty path when there was any search.
SELECT DISTINCT path FROM wiki_lookups
WHERE project_id = $1 AND scope = 'project' AND created_at >= $2;

-- name: WikiLookupsBefore :one
-- Whether the project kept any lookups from before a time.
SELECT EXISTS (SELECT 1 FROM wiki_lookups WHERE project_id = $1 AND created_at < $2);

-- name: PruneWikiLookups :execrows
-- Drops the lookups older than a time, of every project.
DELETE FROM wiki_lookups WHERE created_at < $1;
