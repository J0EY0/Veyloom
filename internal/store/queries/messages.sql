-- name: CreateMessage :one
INSERT INTO messages (room_id, thread_id, sender_kind, user_id, member_id, body, mentions, turn_id, title)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, sqlc.arg('title'))
RETURNING *;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = $1;

-- name: ListRoomMessagesAfter :many
-- Top-level messages of a room newer than a cursor, oldest first.
SELECT * FROM messages
WHERE room_id = $1 AND thread_id IS NULL AND seq > $2
ORDER BY seq
LIMIT $3;

-- name: ListRoomMessagesBefore :many
-- Top-level messages of a room older than a cursor, newest first; callers
-- reverse the page to present it chronologically.
SELECT * FROM messages
WHERE room_id = $1 AND thread_id IS NULL AND seq < $2
ORDER BY seq DESC
LIMIT $3;

-- name: ListThreadMessagesAfter :many
-- Replies in a thread newer than a cursor, oldest first. The root message
-- is not a reply and is fetched separately.
SELECT * FROM messages
WHERE thread_id = $1 AND seq > $2
ORDER BY seq
LIMIT $3;

-- name: LastAgentMessageInThread :one
-- The most recent agent message in a topic, counting the root; used to
-- pick who answers a message that mentions nobody.
SELECT * FROM messages
WHERE sender_kind = 'agent'
  AND (thread_id = $1 OR id = (SELECT root_message_id FROM threads WHERE id = $1))
ORDER BY seq DESC
LIMIT 1;

-- name: TalkingMemberInThread :many
-- The members in a person's latest exchange in a topic (design.md 4.2),
-- the root counted as the topic's: a member's message that mentions them;
-- a message of theirs that names a member, whose wake may still wait in a
-- queue; a turn a message of theirs set going (a reply given in one go,
-- heading its topic, mentions no one). One member is the one they talk
-- with; several, one message having asked them all, are none of them.
WITH topic AS (
    SELECT th.root_message_id FROM threads th WHERE th.id = sqlc.arg(thread_id)
), exchanges AS (
    SELECT m.member_id::text AS member_id, m.created_at AS at FROM messages m
    WHERE m.sender_kind = 'agent'
      AND (m.thread_id = sqlc.arg(thread_id) OR m.id = (SELECT root_message_id FROM topic))
      AND m.mentions @> jsonb_build_array(jsonb_build_object('kind', 'user', 'id', sqlc.arg(user_id)::uuid::text))
    UNION ALL
    SELECT x.mention->>'id', m.created_at FROM messages m, jsonb_array_elements(m.mentions) AS x(mention)
    WHERE m.sender_kind = 'user' AND m.user_id = sqlc.arg(user_id)::uuid
      AND (m.thread_id = sqlc.arg(thread_id) OR m.id = (SELECT root_message_id FROM topic))
      AND x.mention->>'kind' = 'agent'
    UNION ALL
    SELECT t.member_id::text, q.created_at FROM turns t
    JOIN messages q ON q.id = t.trigger_message_id
    WHERE t.thread_id = sqlc.arg(thread_id) AND q.sender_kind = 'user' AND q.user_id = sqlc.arg(user_id)::uuid
)
SELECT DISTINCT e.member_id::text FROM exchanges e
WHERE e.at = (SELECT max(at) FROM exchanges);

-- name: UpdateMessageBody :one
-- Fills in a topic root once the agent's first reply text is known, with
-- whoever that text mentions.
UPDATE messages SET body = $2, turn_id = $3, mentions = $4
WHERE id = $1
RETURNING *;

-- name: SetMessageTitle :one
-- Names the task a message hands on: a topic root an agent filled in with
-- what it sent (design.md 5.24).
UPDATE messages SET title = $2
WHERE id = $1
RETURNING *;

-- name: ThreadSummaries :many
-- What the room timeline shows under each topic root: reply count, last
-- reply time, the latest turn, and, under the topic where the latest turn's
-- piece of work began, that piece of work across all its topics, with how
-- it stands by its latest turn: running while one runs, else how the turn
-- that ended last ended.
SELECT t.root_message_id,
       t.id AS thread_id,
       t.number AS thread_number,
       (SELECT count(*) FROM messages m WHERE m.thread_id = t.id) AS reply_count,
       (SELECT max(m.created_at) FROM messages m WHERE m.thread_id = t.id)::timestamptz AS last_reply_at,
       (SELECT count(*) FROM turns tu WHERE tu.thread_id = t.id) AS turn_count,
       coalesce(lt.id::text, '')::text AS last_turn_id,
       coalesce(lt.member_id::text, '')::text AS last_turn_member_id,
       coalesce(lt.status, '') AS last_turn_status,
       coalesce(lt.error, '') AS last_turn_error,
       lt.started_at AS last_turn_started_at,
       lt.ended_at AS last_turn_ended_at,
       coalesce(w.chain::text, '')::text AS work_chain,
       coalesce(w.turns, 0)::int AS work_turns,
       w.started_at::timestamptz AS work_started_at,
       w.ended_at::timestamptz AS work_ended_at,
       coalesce(w.running, false)::bool AS work_running,
       coalesce(w.last_status, '')::text AS work_last_status
FROM threads t
LEFT JOIN LATERAL (
    SELECT * FROM turns tu WHERE tu.thread_id = t.id ORDER BY tu.started_at DESC LIMIT 1
) lt ON true
LEFT JOIN LATERAL (
    SELECT c.chain_message_id AS chain,
           count(*) AS turns,
           min(c.started_at) AS started_at,
           CASE WHEN bool_or(c.ended_at IS NULL) THEN NULL ELSE max(c.ended_at) END AS ended_at,
           bool_or(c.status = 'running') AS running,
           (SELECT l.status FROM turns l WHERE l.chain_message_id = lt.chain_message_id
            ORDER BY l.ended_at DESC NULLS FIRST, l.started_at DESC LIMIT 1) AS last_status
    FROM turns c
    WHERE c.chain_message_id = lt.chain_message_id
    GROUP BY c.chain_message_id
    HAVING (SELECT o.thread_id FROM turns o WHERE o.chain_message_id = lt.chain_message_id ORDER BY o.started_at LIMIT 1) = t.id
) w ON true
WHERE t.root_message_id = ANY($1::uuid[]);

-- name: ChainWork :one
-- A piece of work across its topics: the topic it began in, how many turns
-- it took, when it began and, once none runs, when it ended, how it stands
-- by its latest turn (running while one runs, else how the turn that ended
-- last ended), and the members who took turns in it, in the order they
-- first did.
WITH c AS (
    SELECT * FROM turns WHERE chain_message_id = $1
), origin AS (
    SELECT thread_id FROM c ORDER BY started_at LIMIT 1
)
SELECT (SELECT thread_id FROM origin)::uuid AS thread_id,
       coalesce((SELECT th.number FROM threads th WHERE th.id = (SELECT thread_id FROM origin)), 0)::int AS thread_number,
       (SELECT count(*) FROM c)::int AS turns,
       (SELECT min(started_at) FROM c)::timestamptz AS started_at,
       (SELECT CASE WHEN bool_or(ended_at IS NULL) THEN NULL ELSE max(ended_at) END FROM c)::timestamptz AS ended_at,
       coalesce((SELECT bool_or(status = 'running') FROM c), false)::bool AS running,
       coalesce((SELECT status FROM c ORDER BY ended_at DESC NULLS FIRST, started_at DESC LIMIT 1), '')::text AS last_status,
       coalesce((SELECT array_agg(x.member_id ORDER BY x.first) FROM (
           SELECT member_id, min(started_at) AS first FROM c GROUP BY member_id
       ) x), '{}')::uuid[] AS members;

-- name: ListThreadMessagesBefore :many
-- The most recent replies in a thread, newest first; callers reverse the
-- page to present it chronologically.
SELECT * FROM messages
WHERE thread_id = $1 AND seq < $2
ORDER BY seq DESC
LIMIT $3;

-- name: ListUserMentions :many
-- Messages that mention one user, newest first, with the names the inbox
-- shows so it needs no second lookup, and whether the user read them.
SELECT m.*, r.name AS room_name, p.name AS project_name, coalesce(u.name, mb.display_name, '')::text AS sender_name,
       (ir.message_id IS NOT NULL)::boolean AS read
FROM messages m
JOIN rooms r ON r.id = m.room_id
JOIN projects p ON p.id = r.project_id
LEFT JOIN users u ON u.id = m.user_id
LEFT JOIN members mb ON mb.id = m.member_id
LEFT JOIN inbox_reads ir ON ir.message_id = m.id AND ir.user_id = sqlc.arg('user_id')
WHERE m.mentions @> sqlc.arg('needle')::jsonb AND m.seq < sqlc.arg('before')
ORDER BY m.seq DESC
LIMIT sqlc.arg('max');

-- name: CountUnreadMentions :one
-- How many of the messages that mention one user they have not read.
SELECT count(*) FROM messages m
WHERE m.mentions @> sqlc.arg('needle')::jsonb
  AND NOT EXISTS (SELECT 1 FROM inbox_reads ir WHERE ir.user_id = sqlc.arg('user_id') AND ir.message_id = m.id);

-- name: MarkMentionsRead :execrows
-- Marks read, of the messages that mention one user, those named, those in
-- a topic (its root and replies), or all up to a seq; zero or empty picks
-- none by that way. How many were not read before.
INSERT INTO inbox_reads (user_id, message_id)
SELECT sqlc.arg('user_id'), m.id FROM messages m
WHERE m.mentions @> sqlc.arg('needle')::jsonb
  AND (m.id = ANY (sqlc.arg('ids')::uuid[])
       OR m.thread_id = sqlc.narg('thread_id')
       OR m.id = (SELECT t.root_message_id FROM threads t WHERE t.id = sqlc.narg('thread_id'))
       OR m.seq <= sqlc.arg('up_to'))
ON CONFLICT DO NOTHING;
