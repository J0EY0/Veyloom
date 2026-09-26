-- name: ListMemberRules :many
-- What a member may always ask for without a person being asked, oldest
-- first: every runtime's, or only the one named.
SELECT * FROM member_rules
WHERE member_id = sqlc.arg('member_id')
  AND (sqlc.narg('runtime')::text IS NULL OR runtime = sqlc.narg('runtime')::text)
ORDER BY created_at, rule;

-- name: AddMemberRule :one
-- A rule is kept once: allowing the same one again leaves the first as it
-- was, and returns it.
INSERT INTO member_rules (member_id, runtime, rule, approval_id, created_by)
VALUES ($1, $2, $3, sqlc.narg('approval_id'), sqlc.narg('created_by'))
ON CONFLICT (member_id, runtime, rule) DO UPDATE SET rule = member_rules.rule
RETURNING *;

-- name: DeleteMemberRule :execrows
DELETE FROM member_rules WHERE member_id = $1 AND id = $2;
