-- name: CreateRelayHold :exec
INSERT INTO relay_holds (message_id, member_id, thread_id, trigger_message_id, reason)
VALUES ($1, $2, $3, $4, $5);

-- name: GetRelayHold :one
SELECT * FROM relay_holds WHERE message_id = $1;

-- name: ContinueRelayHold :one
-- Lets a held wake go on, once.
UPDATE relay_holds SET continued_at = now()
WHERE message_id = $1 AND continued_at IS NULL
RETURNING *;

-- name: ListThreadRelayHolds :many
-- The wakes held back that notes in a topic tell of.
SELECT h.* FROM relay_holds h
JOIN messages m ON m.id = h.message_id
WHERE m.thread_id = $1
ORDER BY h.created_at;
