-- name: CreateAgentTemplate :one
INSERT INTO agent_templates (name, engine, model, role_card, permission_preset, engine_options, builtin)
VALUES ($1, $2, $3, $4, $5, $6, false)
RETURNING *;

-- name: EnsureBuiltinAgentTemplate :exec
-- Seeds a shipped template unless one with that name already exists, so
-- user edits to builtins survive restarts.
INSERT INTO agent_templates (name, engine, model, role_card, permission_preset, engine_options, builtin)
VALUES ($1, $2, $3, $4, $5, $6, true)
ON CONFLICT (name) DO NOTHING;

-- name: GetAgentTemplate :one
SELECT * FROM agent_templates WHERE id = $1;

-- name: ListAgentTemplates :many
SELECT * FROM agent_templates ORDER BY builtin DESC, name;

-- name: UpdateAgentTemplate :one
UPDATE agent_templates SET
    name              = $2,
    engine            = $3,
    model             = $4,
    role_card         = $5,
    permission_preset = $6,
    engine_options    = $7,
    updated_at        = now()
WHERE id = $1
RETURNING *;

-- name: CreateAgentInstance :one
INSERT INTO agent_instances (room_id, template_id, worker_id, display_name, repo_path, branch_mode, model, permission_preset)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetAgentInstance :one
SELECT * FROM agent_instances WHERE id = $1;

-- name: ListRoomAgentInstances :many
SELECT * FROM agent_instances WHERE room_id = $1 ORDER BY created_at;

-- name: UpdateAgentInstanceSession :exec
UPDATE agent_instances SET engine_session_ref = $2 WHERE id = $1;
