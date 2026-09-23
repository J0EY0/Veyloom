-- name: CreateProject :one
-- Adds a project unless its wiki folder name is taken, in which case no row
-- comes back and the caller tries the next name.
INSERT INTO projects (name, repo_path, description, wiki_slug)
VALUES ($1, $2, $3, $4)
ON CONFLICT (wiki_slug) DO NOTHING
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
-- Renames a project, moves its checkout, rewrites its description, turns
-- its members' wiki writes on or off, changes its wiki maintainer or when
-- that runs, or the bundles its wiki mounts; a NULL argument keeps the
-- current value, except that the
-- maintainer is set, to a member or to none, whenever set_maintainer is.
-- The checkout it had comes back beside it, for moving the members with
-- it.
WITH before AS (
    SELECT repo_path FROM projects WHERE id = sqlc.arg('id') FOR UPDATE
)
UPDATE projects
SET name               = coalesce(sqlc.narg('name'), projects.name),
    repo_path          = coalesce(sqlc.narg('repo_path'), projects.repo_path),
    description        = coalesce(sqlc.narg('description'), projects.description),
    wiki_maintainer_member_id = CASE WHEN sqlc.arg('set_maintainer')::boolean
        THEN sqlc.narg('maintainer_member_id')::uuid ELSE projects.wiki_maintainer_member_id END,
    wiki_maintainer_trigger = coalesce(sqlc.narg('maintainer_trigger'), projects.wiki_maintainer_trigger),
    wiki_external_bundles = coalesce(sqlc.narg('external_bundles')::text[], projects.wiki_external_bundles),
    wiki_offer_declined_at = CASE WHEN sqlc.arg('decline_offer')::boolean
        THEN coalesce(projects.wiki_offer_declined_at, now()) ELSE projects.wiki_offer_declined_at END
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

-- name: GetProjectBySlug :one
-- The project whose wiki is in the folder of that name: whose team owns
-- the skills that name it.
SELECT sqlc.embed(projects),
       (SELECT r.id FROM rooms r WHERE r.project_id = projects.id AND r.kind = 'main' ORDER BY r.created_at LIMIT 1) AS main_room_id
FROM projects WHERE projects.wiki_slug = $1;

-- name: ListMaintainedProjects :many
-- The projects a person has given a wiki maintainer.
SELECT sqlc.embed(projects),
       (SELECT r.id FROM rooms r WHERE r.project_id = projects.id AND r.kind = 'main' ORDER BY r.created_at LIMIT 1) AS main_room_id
FROM projects WHERE projects.wiki_maintainer_member_id IS NOT NULL
ORDER BY projects.created_at;

-- name: IsCurrentProjectMember :one
-- Whether a member is in a project now: one of its rooms, not taken out.
SELECT EXISTS (
    SELECT 1 FROM members m JOIN rooms r ON r.id = m.room_id
    WHERE m.id = sqlc.arg(member_id) AND r.project_id = sqlc.arg(project_id) AND m.removed_at IS NULL
);

-- name: SetProjectMaintainer :exec
-- Gives a project its wiki maintainer and when that runs, as the project
-- is created.
UPDATE projects SET wiki_maintainer_member_id = sqlc.arg(member_id), wiki_maintainer_trigger = sqlc.arg(trigger)
WHERE id = sqlc.arg(id);

-- name: ListUnmaintainedProjects :many
-- The projects a wiki maintainer may yet be offered to: none chosen, none
-- offered, none declined (design.md 5.16).
SELECT sqlc.embed(projects),
       (SELECT r.id FROM rooms r WHERE r.project_id = projects.id AND r.kind = 'main' ORDER BY r.created_at LIMIT 1) AS main_room_id
FROM projects
WHERE projects.wiki_maintainer_member_id IS NULL AND projects.wiki_offer_message_id IS NULL AND projects.wiki_offer_declined_at IS NULL
ORDER BY projects.created_at;

-- name: SetProjectWikiOffer :execrows
-- Records the note that offered the project a wiki maintainer, unless one
-- was chosen, offered or declined meanwhile.
UPDATE projects SET wiki_offer_message_id = sqlc.arg(message_id)
WHERE id = sqlc.arg(id) AND wiki_maintainer_member_id IS NULL AND wiki_offer_message_id IS NULL AND wiki_offer_declined_at IS NULL;

-- name: SetProjectWikiThread :execrows
-- Records the project's wiki topic, unless it has one already.
UPDATE projects SET wiki_thread_id = sqlc.arg(thread_id)
WHERE id = sqlc.arg(id) AND wiki_thread_id IS NULL;
