-- name: CreateMachine :one
-- Registers a machine the hub has not seen before.
INSERT INTO machines (name, runtimes)
VALUES ($1, $2)
RETURNING *;

-- name: ReconnectMachine :one
-- Re-registers a known machine: refreshes its label and runtimes and resets
-- the connection timestamps. Yields no row when the id is unknown.
UPDATE machines SET
    name            = $2,
    runtimes         = $3,
    connected_at    = now(),
    last_seen_at    = now(),
    disconnected_at = NULL
WHERE id = $1
RETURNING *;

-- name: TouchMachine :exec
-- Records that the machine was heard from.
UPDATE machines SET last_seen_at = now() WHERE id = $1;

-- name: UpdateMachineRuntimes :exec
UPDATE machines SET runtimes = $2, last_seen_at = now() WHERE id = $1;

-- name: MarkMachineDisconnected :exec
UPDATE machines SET disconnected_at = now() WHERE id = $1;

-- name: GetMachine :one
SELECT * FROM machines WHERE id = $1;

-- name: ListMachines :many
SELECT * FROM machines ORDER BY name, connected_at;
