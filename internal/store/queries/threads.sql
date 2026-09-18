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
