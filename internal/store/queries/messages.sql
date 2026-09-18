-- name: CreateMessage :one
INSERT INTO messages (room_id, thread_id, sender_kind, user_id, member_id, body, mentions, turn_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
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

-- name: UpdateMessageBody :one
-- Fills in a topic root once the agent's first reply text is known, with
-- whoever that text mentions.
UPDATE messages SET body = $2, turn_id = $3, mentions = $4
WHERE id = $1
RETURNING *;

-- name: ThreadSummaries :many
-- What the room timeline shows under each topic root: reply count, last
-- reply time, and the latest turn.
SELECT t.root_message_id,
       t.id AS thread_id,
       t.number AS thread_number,
       (SELECT count(*) FROM messages m WHERE m.thread_id = t.id) AS reply_count,
       (SELECT max(m.created_at) FROM messages m WHERE m.thread_id = t.id)::timestamptz AS last_reply_at,
       (SELECT count(*) FROM turns tu WHERE tu.thread_id = t.id) AS turn_count,
       coalesce(lt.id::text, '')::text AS last_turn_id,
       coalesce(lt.status, '') AS last_turn_status,
       coalesce(lt.error, '') AS last_turn_error,
       lt.started_at AS last_turn_started_at,
       lt.ended_at AS last_turn_ended_at
FROM threads t
LEFT JOIN LATERAL (
    SELECT * FROM turns tu WHERE tu.thread_id = t.id ORDER BY tu.started_at DESC LIMIT 1
) lt ON true
WHERE t.root_message_id = ANY($1::uuid[]);

-- name: ListThreadMessagesBefore :many
-- The most recent replies in a thread, newest first; callers reverse the
-- page to present it chronologically.
SELECT * FROM messages
WHERE thread_id = $1 AND seq < $2
ORDER BY seq DESC
LIMIT $3;

-- name: ListUserMentions :many
-- Messages that mention one user, newest first, with the names the inbox
-- shows so it needs no second lookup.
SELECT m.*, r.name AS room_name, p.name AS project_name, coalesce(u.name, mb.display_name, '')::text AS sender_name
FROM messages m
JOIN rooms r ON r.id = m.room_id
JOIN projects p ON p.id = r.project_id
LEFT JOIN users u ON u.id = m.user_id
LEFT JOIN members mb ON mb.id = m.member_id
WHERE m.mentions @> $1::jsonb AND m.seq < $2
ORDER BY m.seq DESC
LIMIT $3;
