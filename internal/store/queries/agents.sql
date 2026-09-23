-- name: CreateAgent :one
INSERT INTO agents (name, avatar, machine_id, runtime, model, role_card, permission_preset, runtime_options, skills)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id;

-- name: GetAgent :one
-- An agent with the name of its machine and the projects it is a current
-- member of.
SELECT sqlc.embed(ag), m.name AS machine_name,
       coalesce(array_agg(DISTINCT p.name ORDER BY p.name) FILTER (WHERE p.name IS NOT NULL), '{}')::text[] AS projects
FROM agents ag
JOIN machines m ON m.id = ag.machine_id
LEFT JOIN members mb ON mb.agent_id = ag.id AND mb.removed_at IS NULL
LEFT JOIN rooms r ON r.id = mb.room_id
LEFT JOIN projects p ON p.id = r.project_id
WHERE ag.id = $1
GROUP BY ag.id, m.name;

-- name: ListAgents :many
-- Oldest first: the web UI's default sort is "as added", and the page
-- lets you reorder by name or by last edit. Each with its machine's name
-- and the projects it is a current member of.
SELECT sqlc.embed(ag), m.name AS machine_name,
       coalesce(array_agg(DISTINCT p.name ORDER BY p.name) FILTER (WHERE p.name IS NOT NULL), '{}')::text[] AS projects
FROM agents ag
JOIN machines m ON m.id = ag.machine_id
LEFT JOIN members mb ON mb.agent_id = ag.id AND mb.removed_at IS NULL
LEFT JOIN rooms r ON r.id = mb.room_id
LEFT JOIN projects p ON p.id = r.project_id
GROUP BY ag.id, m.name
ORDER BY ag.created_at, ag.name;

-- name: UpdateAgent :one
-- Replaces every editable field. The machine changes only while the agent
-- is a member of no project, since its members run there; no row back is
-- an unknown agent or a move refused.
UPDATE agents AS ag SET
    name              = sqlc.arg(name),
    avatar            = sqlc.arg(avatar),
    machine_id        = sqlc.arg(machine_id),
    runtime           = sqlc.arg(runtime),
    model             = sqlc.arg(model),
    role_card         = sqlc.arg(role_card),
    permission_preset = sqlc.arg(permission_preset),
    runtime_options   = sqlc.arg(runtime_options),
    skills            = sqlc.arg(skills)::text[],
    updated_at        = now()
WHERE ag.id = sqlc.arg(id)
  AND (ag.machine_id = sqlc.arg(machine_id)
       OR NOT EXISTS (SELECT 1 FROM members AS mb WHERE mb.agent_id = ag.id AND mb.removed_at IS NULL))
RETURNING ag.id;

-- name: SetAgentSkill :execrows
-- Installs a skill for an agent, once, or takes it off.
UPDATE agents SET
    skills = CASE
        WHEN NOT sqlc.arg(installed)::boolean THEN array_remove(skills, sqlc.arg(skill)::text)
        WHEN sqlc.arg(skill)::text = ANY(skills) THEN skills
        ELSE array_append(skills, sqlc.arg(skill)::text)
    END,
    updated_at = now()
WHERE id = sqlc.arg(id);

-- name: ListSkillAgents :many
-- The agents a skill is installed for, by name.
SELECT id, name FROM agents WHERE sqlc.arg(skill)::text = ANY(skills) ORDER BY name;

-- name: CountAgentsWithAvatar :one
-- How many agents show an avatar file, asked before the file is removed.
SELECT count(*) FROM agents WHERE avatar = $1;

-- name: DeleteAgent :execrows
-- Members already taken out of their projects lose the link; a current
-- member makes members_current_have_agent refuse the delete.
DELETE FROM agents WHERE id = $1;

-- name: ListAgentProjects :many
-- The projects where the agent is still a current member, for telling
-- someone where to take it out before deleting it.
SELECT DISTINCT p.name
FROM members mb
JOIN rooms r ON r.id = mb.room_id
JOIN projects p ON p.id = r.project_id
WHERE mb.agent_id = $1 AND mb.removed_at IS NULL
ORDER BY p.name;

-- name: CreateMember :one
-- A member runs on the machine its agent is set up on and, unless given
-- one, goes by the agent's name. created_at is the clock, not the
-- transaction's start: members a project starts with join together, and
-- the list keeps the order they joined in even after their rows change.
INSERT INTO members (room_id, agent_id, machine_id, display_name, repo_path, branch_mode, model, permission_preset, created_at)
SELECT sqlc.arg(room_id), ag.id, ag.machine_id, coalesce(nullif(sqlc.arg(display_name)::text, ''), ag.name), sqlc.arg(repo_path), sqlc.arg(branch_mode), sqlc.arg(model), sqlc.arg(permission_preset), clock_timestamp()
FROM agents AS ag
WHERE ag.id = sqlc.arg(agent_id)
RETURNING *;

-- name: GetMember :one
SELECT * FROM members WHERE id = $1;

-- name: ListRoomMembers :many
-- Every member the room has had, including those taken out of the project:
-- their messages and turns still need their names. Whoever wants only the
-- current members skips the rows with removed_at set.
SELECT * FROM members WHERE room_id = $1 ORDER BY created_at;

-- name: UpdateMember :one
-- Changes the fields a person may edit after adding a member; a NULL
-- argument keeps the current value. A member taken out of its project is
-- not edited any more.
UPDATE members
SET display_name      = coalesce(sqlc.narg('display_name'), display_name),
    model             = coalesce(sqlc.narg('model'), model),
    permission_preset = coalesce(sqlc.narg('permission_preset'), permission_preset),
    repo_path         = coalesce(sqlc.narg('repo_path'), repo_path),
    enabled           = coalesce(sqlc.narg('enabled'), enabled)
WHERE id = $1 AND removed_at IS NULL
RETURNING *;

-- name: RemoveMember :one
-- Takes a member out of its project unless one of its turns is still
-- running. The row stays for the history that points at it.
UPDATE members AS mb
SET removed_at = now()
WHERE mb.id = $1
  AND mb.removed_at IS NULL
  AND NOT EXISTS (SELECT 1 FROM turns AS t WHERE t.member_id = mb.id AND t.status = 'running')
RETURNING *;

-- name: ClearWikiMaintainer :exec
-- A member taken out of its project keeps its wiki no longer.
UPDATE projects SET wiki_maintainer_member_id = NULL WHERE wiki_maintainer_member_id = $1;

-- name: ListMachineMembers :many
-- The current members a machine runs, across every project: the project
-- each is in, its turn in flight if it has one, and the oldest request of
-- its that waits for a person.
SELECT sqlc.embed(mb),
       p.id AS project_id,
       p.name AS project_name,
       running.id AS turn_id,
       running.thread_id AS turn_thread_id,
       running.started_at AS turn_started_at,
       pending.id AS approval_id,
       pending.payload AS approval_payload
FROM members mb
JOIN rooms r ON r.id = mb.room_id
JOIN projects p ON p.id = r.project_id
LEFT JOIN LATERAL (
    SELECT t.id, t.thread_id, t.started_at
    FROM turns t
    WHERE t.member_id = mb.id AND t.status = 'running'
    ORDER BY t.started_at DESC
    LIMIT 1
) running ON true
LEFT JOIN LATERAL (
    SELECT ap.id, ap.payload
    FROM approvals ap
    WHERE ap.member_id = mb.id AND ap.status = 'pending'
    ORDER BY ap.created_at
    LIMIT 1
) pending ON true
WHERE mb.machine_id = $1 AND mb.removed_at IS NULL
ORDER BY p.name, mb.created_at;
