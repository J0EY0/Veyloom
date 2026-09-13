-- name: CreateWorker :one
-- Registers a worker the hub has not seen before.
INSERT INTO workers (name, engines)
VALUES ($1, $2)
RETURNING *;

-- name: ReconnectWorker :one
-- Re-registers a known worker: refreshes its label and engines and resets
-- the connection timestamps. Yields no row when the id is unknown.
UPDATE workers SET
    name            = $2,
    engines         = $3,
    connected_at    = now(),
    last_seen_at    = now(),
    disconnected_at = NULL
WHERE id = $1
RETURNING *;

-- name: TouchWorker :exec
-- Records that the worker was heard from.
UPDATE workers SET last_seen_at = now() WHERE id = $1;

-- name: UpdateWorkerEngines :exec
UPDATE workers SET engines = $2, last_seen_at = now() WHERE id = $1;

-- name: MarkWorkerDisconnected :exec
UPDATE workers SET disconnected_at = now() WHERE id = $1;

-- name: GetWorker :one
SELECT * FROM workers WHERE id = $1;

-- name: ListWorkers :many
SELECT * FROM workers ORDER BY name, connected_at;
