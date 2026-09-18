-- name: CreateProject :one
INSERT INTO projects (name, repo_path, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetProject :one
SELECT sqlc.embed(projects),
       (SELECT r.id FROM rooms r WHERE r.project_id = projects.id AND r.kind = 'main' ORDER BY r.created_at LIMIT 1) AS main_room_id
FROM projects WHERE projects.id = $1;

-- name: ListProjects :many
SELECT sqlc.embed(projects),
       (SELECT r.id FROM rooms r WHERE r.project_id = projects.id AND r.kind = 'main' ORDER BY r.created_at LIMIT 1) AS main_room_id
FROM projects ORDER BY projects.created_at, projects.name;

-- name: UpdateProject :one
-- Renames a project, moves its checkout or rewrites its description; a
-- NULL argument keeps the current value. The checkout it had comes back beside it, for moving the
-- members with it.
WITH before AS (
    SELECT repo_path FROM projects WHERE id = sqlc.arg('id') FOR UPDATE
)
UPDATE projects
SET name        = coalesce(sqlc.narg('name'), projects.name),
    repo_path   = coalesce(sqlc.narg('repo_path'), projects.repo_path),
    description = coalesce(sqlc.narg('description'), projects.description)
FROM before
WHERE projects.id = sqlc.arg('id')
RETURNING projects.id, projects.name, projects.repo_path, projects.created_at, before.repo_path AS old_repo_path;

-- name: MoveProjectMembers :exec
-- Members work under their project's checkout, so when it moves the
-- project's current members move with it: a path below the old checkout
-- keeps its place below the new one, any other path becomes the new
-- checkout, and no checkout leaves them all to wherever their machine
-- runs. Members taken out of the project keep the path they last had.
UPDATE members
SET repo_path = CASE
        WHEN sqlc.arg('new_path')::text = '' THEN ''
        WHEN sqlc.arg('old_path')::text <> '' AND starts_with(members.repo_path, sqlc.arg('old_path')::text || '/')
            THEN sqlc.arg('new_path')::text || substr(members.repo_path, length(sqlc.arg('old_path')::text) + 1)
        ELSE sqlc.arg('new_path')::text
    END
FROM rooms
WHERE members.room_id = rooms.id
  AND rooms.project_id = sqlc.arg('project_id')
  AND members.removed_at IS NULL;

-- name: ProjectAttachmentPaths :many
-- Where the project's uploads lie in the attachment directory.
SELECT a.path FROM attachments a JOIN rooms r ON r.id = a.room_id WHERE r.project_id = $1;

-- name: ProjectTurnIDs :many
-- The project's turns, whose transcripts are named after them.
SELECT t.id FROM turns t JOIN rooms r ON r.id = t.room_id WHERE r.project_id = $1;

-- name: DeleteProject :execrows
-- Deletes a project and, through its rooms, the whole chat, unless one of
-- its turns is still running.
DELETE FROM projects AS p
WHERE p.id = $1
  AND NOT EXISTS (
      SELECT 1 FROM turns t JOIN rooms r ON r.id = t.room_id
      WHERE r.project_id = p.id AND t.status = 'running'
  );

-- name: GetRoomProject :one
-- The project a room belongs to.
SELECT p.* FROM projects p JOIN rooms r ON r.project_id = p.id WHERE r.id = $1;
