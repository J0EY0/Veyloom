-- name: StartSkillTrial :one
-- A change to a skill (design.md 5.15): opens its trial, or, when one is
-- open, starts its count again from the same version to go back to.
INSERT INTO skill_trials (skill, base_sha, turn_id, changed_by, project_name)
VALUES (sqlc.arg(skill), sqlc.arg(base_sha), sqlc.narg(turn_id), sqlc.arg(changed_by), sqlc.arg(project_name))
ON CONFLICT (skill) WHERE status = 'open' DO UPDATE
SET changed_at = now(), turn_id = EXCLUDED.turn_id, changed_by = EXCLUDED.changed_by,
    project_name = EXCLUDED.project_name, changes = skill_trials.changes + 1
RETURNING *;

-- name: OpenSkillTrial :one
SELECT * FROM skill_trials WHERE skill = sqlc.arg(skill) AND status = 'open';

-- name: LatestSkillTrial :one
-- The skill's open trial, or else the one that ended last.
SELECT * FROM skill_trials
WHERE skill = sqlc.arg(skill)
ORDER BY (status = 'open') DESC, started_at DESC
LIMIT 1;

-- name: EndSkillTrial :one
UPDATE skill_trials
SET status = sqlc.arg(status), ended_by = sqlc.arg(ended_by), ended_at = now(), reason = sqlc.arg(reason)
WHERE id = sqlc.arg(id) AND status = 'open'
RETURNING *;

-- name: SkillTrialUses :one
-- How the turns that used a skill since it last changed ended; a turn that
-- began before the change used the version before.
SELECT count(*) FILTER (WHERE status = 'done')::integer AS done,
       count(*) FILTER (WHERE status = 'failed')::integer AS failed
FROM turns
WHERE sqlc.arg(skill)::text = ANY (skills_used) AND started_at > sqlc.arg(since);

-- name: ListOpenSkillTrials :many
-- The open trials of the skills named, oldest first.
SELECT * FROM skill_trials
WHERE status = 'open' AND skill = ANY (sqlc.arg(skills)::text[])
ORDER BY started_at;
