-- The wiki maintainer's bookkeeping (docs/design.md 5.12): which turns a
-- project's maintainer looks at, which it has gone over, when it last ran.

-- name: ListUpkeepTurns :many
-- Finished turns a project's wiki maintainer may look at: the project's own
-- that answered its chat, or with skills set, other projects' turns that
-- used one of the skills its team owns. unreviewed keeps the ones its
-- maintainer has not gone over; topic keeps one topic of the project's
-- chat (zero keeps all); settled_by keeps the ones in topics settled by
-- then (see CountUpkeepWaiting); before pages back. The maintainer's brief
-- reads them oldest first, list_turns newest first.
SELECT t.id, t.thread_id, t.room_id, th.number AS topic_number, coalesce(root.body, '')::text AS root_body,
       mb.display_name AS member_name, t.runtime, t.status, t.error, t.files_changed, t.skills_used,
       t.started_at, t.ended_at, p.id AS project_id, p.name AS project_name,
       EXISTS (SELECT 1 FROM wiki_reviews w WHERE w.project_id = sqlc.arg(project_id) AND w.turn_id = t.id) AS reviewed
FROM turns t
JOIN threads th ON th.id = t.thread_id
LEFT JOIN messages root ON root.id = th.root_message_id
JOIN members mb ON mb.id = t.member_id
JOIN rooms r ON r.id = t.room_id
JOIN projects p ON p.id = r.project_id
WHERE t.status <> 'running' AND t.kind = 'chat'
  AND CASE WHEN sqlc.arg(skills)::boolean
           THEN r.project_id <> sqlc.arg(project_id) AND t.skills_used && sqlc.arg(owned)::text[]
           ELSE r.project_id = sqlc.arg(project_id) END
  AND (sqlc.arg(topic)::integer = 0 OR th.number = sqlc.arg(topic)::integer)
  AND (NOT sqlc.arg(unreviewed)::boolean
       OR NOT EXISTS (SELECT 1 FROM wiki_reviews w WHERE w.project_id = sqlc.arg(project_id) AND w.turn_id = t.id))
  AND (sqlc.narg(settled_by)::timestamptz IS NULL OR (
          t.ended_at <= sqlc.narg(settled_by)::timestamptz
          AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.thread_id = t.thread_id AND m.created_at > sqlc.narg(settled_by)::timestamptz)
          AND NOT EXISTS (SELECT 1 FROM turns x WHERE x.thread_id = t.thread_id AND x.status = 'running')))
  AND (sqlc.narg(before)::timestamptz IS NULL OR t.started_at < sqlc.narg(before)::timestamptz)
ORDER BY CASE WHEN sqlc.arg(oldest_first)::boolean THEN t.started_at END ASC, t.started_at DESC, t.id
LIMIT sqlc.arg(lim);

-- name: CountUpkeepWaiting :one
-- What waits for a project's wiki maintainer: the finished turns it has not
-- gone over, its project's own and other projects' that used its team's
-- skills, and how many of all those ran in topics settled by quiet_since:
-- the turn ended by then, nothing was said in its topic after, and no turn
-- runs there.
SELECT count(*) FILTER (WHERE r.project_id = sqlc.arg(project_id))::bigint AS own,
       count(*) FILTER (WHERE r.project_id <> sqlc.arg(project_id))::bigint AS uses,
       count(*) FILTER (
           WHERE t.ended_at <= sqlc.arg(quiet_since)::timestamptz
             AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.thread_id = t.thread_id AND m.created_at > sqlc.arg(quiet_since)::timestamptz)
             AND NOT EXISTS (SELECT 1 FROM turns x WHERE x.thread_id = t.thread_id AND x.status = 'running')
       )::bigint AS settled
FROM turns t
JOIN rooms r ON r.id = t.room_id
WHERE t.status <> 'running' AND t.kind = 'chat'
  AND (r.project_id = sqlc.arg(project_id) OR t.skills_used && sqlc.arg(owned)::text[])
  AND NOT EXISTS (SELECT 1 FROM wiki_reviews w WHERE w.project_id = sqlc.arg(project_id) AND w.turn_id = t.id);

-- name: CountSettledTopicsWaiting :one
-- The topics of a project's own chat that went quiet by quiet_since with
-- turns its wiki maintainer has not gone over: what a maintainer is offered
-- on (design.md 5.16).
SELECT count(DISTINCT t.thread_id)::bigint
FROM turns t
JOIN rooms r ON r.id = t.room_id
WHERE r.project_id = sqlc.arg(project_id) AND t.status <> 'running' AND t.kind = 'chat'
  AND t.ended_at <= sqlc.arg(quiet_since)::timestamptz
  AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.thread_id = t.thread_id AND m.created_at > sqlc.arg(quiet_since)::timestamptz)
  AND NOT EXISTS (SELECT 1 FROM turns x WHERE x.thread_id = t.thread_id AND x.status = 'running')
  AND NOT EXISTS (SELECT 1 FROM wiki_reviews w WHERE w.project_id = sqlc.arg(project_id) AND w.turn_id = t.id);

-- name: CountPeopleWaiting :one
-- What people said in a project's chat that its wiki maintainer has not
-- gone over (design.md 5.16): their messages in the main room, the chat
-- the maintainer reads and keeps files from, after the project's wiki
-- position, and how many of them were settled by quiet_since: said by
-- then, in the room itself or in a topic nothing was said in after and no
-- turn runs in.
SELECT count(*)::bigint AS people,
       count(*) FILTER (
           WHERE m.created_at <= sqlc.arg(quiet_since)::timestamptz
             AND (m.thread_id IS NULL OR (
                 NOT EXISTS (SELECT 1 FROM messages x WHERE x.thread_id = m.thread_id AND x.created_at > sqlc.arg(quiet_since)::timestamptz)
                 AND NOT EXISTS (SELECT 1 FROM turns t WHERE t.thread_id = m.thread_id AND t.status = 'running')))
       )::bigint AS settled
FROM messages m
JOIN rooms r ON r.id = m.room_id AND r.kind = 'main'
JOIN projects p ON p.id = r.project_id
WHERE r.project_id = sqlc.arg(project_id) AND m.sender_kind = 'user' AND m.seq > p.wiki_seen_seq;

-- name: ListPeopleNews :many
-- What people said in a project's main room after one position up to
-- another, by topic (the room itself has topic zero): how many messages,
-- how many files they carried, and when the last was said; oldest first.
SELECT m.thread_id, coalesce(th.number, 0)::integer AS topic_number, coalesce(root.body, '')::text AS root_body,
       count(DISTINCT m.id)::bigint AS messages, count(a.id)::bigint AS files, max(m.created_at)::timestamptz AS last_at
FROM messages m
JOIN rooms r ON r.id = m.room_id AND r.kind = 'main'
LEFT JOIN threads th ON th.id = m.thread_id
LEFT JOIN messages root ON root.id = th.root_message_id
LEFT JOIN attachments a ON a.message_id = m.id
WHERE r.project_id = sqlc.arg(project_id) AND m.sender_kind = 'user' AND m.seq > sqlc.arg(after) AND m.seq <= sqlc.arg(upto)
GROUP BY m.thread_id, th.number, root.body
ORDER BY max(m.seq)
LIMIT sqlc.arg(lim);

-- name: ListPeopleFiles :many
-- The files people sent in a project's main room after one position up to
-- another, oldest first, with where and by whom.
SELECT a.id, a.room_id, a.message_id, a.filename, a.media_type, a.size, a.path, a.created_at,
       coalesce(th.number, 0)::integer AS topic_number, coalesce(u.name, '')::text AS sender
FROM attachments a
JOIN messages m ON m.id = a.message_id
JOIN rooms r ON r.id = m.room_id AND r.kind = 'main'
LEFT JOIN threads th ON th.id = m.thread_id
LEFT JOIN users u ON u.id = m.user_id
WHERE r.project_id = sqlc.arg(project_id) AND m.sender_kind = 'user' AND m.seq > sqlc.arg(after) AND m.seq <= sqlc.arg(upto)
ORDER BY m.seq, a.created_at
LIMIT sqlc.arg(lim);

-- name: SetProjectWikiSeen :exec
-- Moves how far a project's maintainer has gone over its chat; forward only.
UPDATE projects SET wiki_seen_seq = greatest(wiki_seen_seq, sqlc.arg(seq)::bigint) WHERE id = sqlc.arg(id);

-- name: CountUpkeepsSince :one
-- How many upkeeps of a project's wiki started since a moment.
SELECT count(*)::bigint FROM turns t
JOIN rooms r ON r.id = t.room_id
WHERE r.project_id = sqlc.arg(project_id) AND t.kind = 'upkeep' AND t.started_at >= sqlc.arg(since)::timestamptz;

-- name: CountUpkeepReviews :one
-- How many turns an upkeep went over.
SELECT count(*)::bigint FROM wiki_reviews WHERE upkeep_turn_id = $1;

-- name: LastUpkeep :one
-- The latest turn of a project's wiki maintainer, running or not.
SELECT t.* FROM turns t
JOIN rooms r ON r.id = t.room_id
WHERE r.project_id = $1 AND t.kind = 'upkeep'
ORDER BY t.started_at DESC
LIMIT 1;

-- name: RecordWikiReviews :exec
-- Marks turns as gone over by a project's maintainer, in the turn that did
-- it; a turn gone over before keeps its first record, and one that is gone
-- by now is skipped.
INSERT INTO wiki_reviews (project_id, turn_id, upkeep_turn_id)
SELECT sqlc.arg(project_id), t.id, sqlc.arg(upkeep_turn_id) FROM turns t WHERE t.id = ANY (sqlc.arg(turn_ids)::uuid[])
ON CONFLICT DO NOTHING;

-- name: NextPersonMessage :one
-- The first thing a person said in a topic after a moment: after an
-- agent's turn, where their verdict on it usually is.
SELECT * FROM messages
WHERE thread_id = $1 AND sender_kind = 'user' AND created_at > $2
ORDER BY seq
LIMIT 1;

-- name: ListChangedFiles :many
-- The files changed by the turns of a project's chat that started after a
-- moment, each once, with the last of those turns to start and its topic:
-- what a wiki page naming one is checked again for (design.md 5.16). A
-- turn is known to have changed a file after a page was written only if it
-- started after, which leaves out the turn that wrote the page. Files are
-- as the turns recorded them.
SELECT DISTINCT ON (f.path) f.path::text AS path, t.id AS turn_id, t.room_id, t.thread_id,
       coalesce(th.number, 0)::integer AS topic_number, t.started_at
FROM turns t
JOIN rooms r ON r.id = t.room_id
CROSS JOIN LATERAL unnest(t.files_changed) AS f(path)
LEFT JOIN threads th ON th.id = t.thread_id
WHERE r.project_id = sqlc.arg(project_id) AND t.started_at > sqlc.arg(since)::timestamptz
ORDER BY f.path, t.started_at DESC
LIMIT sqlc.arg(lim);
