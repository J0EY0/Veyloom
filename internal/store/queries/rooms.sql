-- name: CreateRoom :one
INSERT INTO rooms (project_id, name, kind)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetRoom :one
SELECT * FROM rooms WHERE id = $1;

-- name: ListRoomsByProject :many
-- The main room comes first, then topic rooms in creation order.
SELECT * FROM rooms
WHERE project_id = $1
ORDER BY (kind = 'main') DESC, created_at;
