-- name: GetThreadByRoot :one
SELECT * FROM threads WHERE root_message_id = $1;

-- name: UpsertThread :one
-- Creates the thread rooted at a message, numbered after the room's last,
-- or returns the existing one: the no-op update makes RETURNING yield the
-- row in both cases. When the thread was already there the number taken
-- here goes unused, so callers look the thread up first and come here only
-- to create it; losing a race to another creator costs a gap, no more.
WITH next AS (
    UPDATE rooms SET last_thread_number = last_thread_number + 1
    WHERE rooms.id = sqlc.arg(room_id)
    RETURNING last_thread_number
)
INSERT INTO threads (room_id, root_message_id, number)
SELECT sqlc.arg(room_id), sqlc.arg(root_message_id), next.last_thread_number FROM next
ON CONFLICT (root_message_id) DO UPDATE SET root_message_id = EXCLUDED.root_message_id
RETURNING *;

-- name: GetThread :one
SELECT * FROM threads WHERE id = $1;

-- name: ListTurnTopics :many
-- The topics some turns ran in, by turn, with each topic's first message:
-- where the wiki pages those turns wrote came from (design.md 5.17).
SELECT t.id AS turn_id, th.id AS thread_id, th.room_id, th.number, coalesce(root.body, '')::text AS root_body
FROM turns t
JOIN threads th ON th.id = t.thread_id
LEFT JOIN messages root ON root.id = th.root_message_id
WHERE t.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: ListTopicsByNumber :many
-- Some topics of a room, by number, with each one's first message.
SELECT th.id AS thread_id, th.room_id, th.number, coalesce(root.body, '')::text AS root_body
FROM threads th
LEFT JOIN messages root ON root.id = th.root_message_id
WHERE th.room_id = sqlc.arg(room_id) AND th.number = ANY(sqlc.arg(numbers)::integer[]);
