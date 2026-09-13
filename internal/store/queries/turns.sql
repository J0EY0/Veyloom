-- name: CreateTurn :one
INSERT INTO turns (agent_instance_id, room_id, thread_id, trigger_message_id, worker_id, transcript_path)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: FinishTurn :one
UPDATE turns SET
    status           = $2,
    error            = $3,
    reply_message_id = $4,
    transcript_path  = $5,
    ended_at         = now()
WHERE id = $1
RETURNING *;

-- name: GetTurn :one
SELECT * FROM turns WHERE id = $1;

-- name: ListRoomTurns :many
-- Most recent turns of a room first.
SELECT * FROM turns WHERE room_id = $1 ORDER BY started_at DESC LIMIT $2;
