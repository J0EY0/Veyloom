-- name: UpsertThread :one
-- Creates the thread rooted at a message, or returns the existing one. The
-- no-op update makes RETURNING yield the row in both cases.
INSERT INTO threads (room_id, root_message_id)
VALUES ($1, $2)
ON CONFLICT (root_message_id) DO UPDATE SET root_message_id = EXCLUDED.root_message_id
RETURNING *;

-- name: GetThread :one
SELECT * FROM threads WHERE id = $1;
