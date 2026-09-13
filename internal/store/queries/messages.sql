-- name: CreateMessage :one
INSERT INTO messages (room_id, thread_id, sender_kind, user_id, agent_instance_id, body, mentions)
VALUES ($1, $2, $3, $4, $5, $6, $7)
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
-- The most recent agent reply in a thread; used to pick who answers a
-- message that mentions nobody.
SELECT * FROM messages
WHERE thread_id = $1 AND sender_kind = 'agent'
ORDER BY seq DESC
LIMIT 1;

-- name: ListThreadMessagesBefore :many
-- The most recent replies in a thread, newest first; callers reverse the
-- page to present it chronologically.
SELECT * FROM messages
WHERE thread_id = $1 AND seq < $2
ORDER BY seq DESC
LIMIT $3;
