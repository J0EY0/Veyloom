-- name: CreateApproval :one
-- The caller may supply the id so it can announce the approval before the
-- row exists; a null id lets the database assign one.
INSERT INTO approvals (id, turn_id, room_id, thread_id, member_id, request_id, kind, payload, message_id)
VALUES (COALESCE(sqlc.narg('id')::uuid, gen_random_uuid()), $1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CreateReviewedApproval :one
-- A request the runtime settled on its own, recorded already decided so the
-- room sees who decided and why.
INSERT INTO approvals (turn_id, room_id, thread_id, member_id, request_id, kind, payload, message_id,
                       status, message, reviewer, answer, decided_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, sqlc.narg('answer'), now())
RETURNING *;

-- name: GetApproval :one
SELECT * FROM approvals WHERE id = $1;

-- name: DecideApproval :one
-- The first decision wins: only a pending approval can be decided, so two
-- people answering at once cannot both succeed.
UPDATE approvals SET
    status     = $2,
    message    = $3,
    decided_by = $4,
    answer     = sqlc.narg('answer'),
    decided_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: ResolveTurnApprovals :many
-- Closes every pending approval of a turn, for when the turn ends before
-- anyone decided.
UPDATE approvals SET
    status     = $2,
    message    = $3,
    decided_at = now()
WHERE turn_id = $1 AND status = 'pending'
RETURNING *;

-- name: ListPendingRoomApprovals :many
-- Oldest first: the order people should deal with them in.
SELECT * FROM approvals WHERE room_id = $1 AND status = 'pending' ORDER BY created_at;

-- name: ListTurnApprovals :many
SELECT * FROM approvals WHERE turn_id = $1 ORDER BY created_at;

-- name: ListPendingApprovals :many
-- Every request waiting for a person, across every room, oldest first,
-- with the names the "for me" page shows so it needs no second lookup.
SELECT sqlc.embed(ap), mb.display_name AS member_name, p.name AS project_name
FROM approvals ap
JOIN members mb ON mb.id = ap.member_id
JOIN rooms r ON r.id = ap.room_id
JOIN projects p ON p.id = r.project_id
WHERE ap.status = 'pending'
ORDER BY ap.created_at;
