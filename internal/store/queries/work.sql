-- What the task board, a piece of work's page and the usage page read
-- (docs/webui.md 4.20).

-- name: RecordBranchEvent :one
-- What became of a member's branch: merged onto the main line, or reset
-- to it with its work archived.
INSERT INTO branch_events (member_id, kind, commit_sha, ref, via_member_id)
VALUES ($1, $2, $3, $4, sqlc.narg('via_member_id'))
RETURNING *;

-- name: ListRoomBranchEvents :many
-- What became of the branches of a room's members since a moment, oldest
-- first.
SELECT be.* FROM branch_events be
JOIN members m ON m.id = be.member_id
WHERE m.room_id = $1 AND be.created_at >= sqlc.arg('since')
ORDER BY be.created_at;

-- name: ListRoomTaskTurns :many
-- The chat turns of a room's latest pieces of work, oldest first, with what
-- the task board needs of each: its topic's number, the message that set
-- it off (its words, what a member handing it on called it, who wrote it)
-- and whether a request of it waits for a person.
WITH chains AS (
    SELECT ct.chain_message_id, min(ct.started_at) AS began
    FROM turns ct
    WHERE ct.room_id = sqlc.arg('room_id') AND ct.kind = 'chat' AND ct.chain_message_id IS NOT NULL
    GROUP BY ct.chain_message_id
    ORDER BY began DESC
    LIMIT sqlc.arg('chains')
)
SELECT sqlc.embed(t), th.number AS thread_number,
       coalesce(tm.body, '')::text AS trigger_body,
       coalesce(tm.title, '')::text AS trigger_title,
       coalesce(tm.sender_kind, '')::text AS trigger_kind,
       EXISTS (SELECT 1 FROM approvals ap WHERE ap.turn_id = t.id AND ap.status = 'pending')::boolean AS waiting
FROM turns t
JOIN chains c ON c.chain_message_id = t.chain_message_id
JOIN threads th ON th.id = t.thread_id
LEFT JOIN messages tm ON tm.id = t.trigger_message_id
WHERE t.kind = 'chat'
ORDER BY t.started_at;

-- name: ListChainTurns :many
-- A piece of work's turns, oldest first, with their topics' numbers and
-- the messages that set them off.
SELECT sqlc.embed(t), th.number AS thread_number,
       coalesce(tm.body, '')::text AS trigger_body,
       coalesce(tm.title, '')::text AS trigger_title,
       coalesce(tm.sender_kind, '')::text AS trigger_kind
FROM turns t
JOIN threads th ON th.id = t.thread_id
LEFT JOIN messages tm ON tm.id = t.trigger_message_id
WHERE t.chain_message_id = $1
ORDER BY t.started_at;

-- name: ListChainWaits :many
-- How long a piece of work's turns waited for a person: the requests
-- people were asked, when each was raised and settled.
SELECT ap.turn_id, ap.created_at, ap.decided_at, ap.status
FROM approvals ap
JOIN turns t ON t.id = ap.turn_id
WHERE t.chain_message_id = $1 AND ap.reviewer = ''
ORDER BY ap.created_at;

-- name: ListUsageTurns :many
-- The turns begun since a moment, of one project or every one, with what
-- the usage page sums them by: who ran them on what, in which topic, as
-- part of which piece of work.
SELECT t.id, t.member_id, t.room_id, t.thread_id, th.number AS thread_number, t.runtime, t.kind, t.status,
       t.chain_message_id, t.started_at, t.ended_at,
       t.input_tokens, t.cache_read_tokens, t.cache_write_tokens, t.output_tokens,
       mb.display_name AS member_name, mb.agent_id, coalesce(nullif(mb.model, ''), ag.model, '')::text AS model,
       r.project_id, p.name AS project_name, coalesce(cm.body, '')::text AS chain_body
FROM turns t
JOIN threads th ON th.id = t.thread_id
JOIN members mb ON mb.id = t.member_id
LEFT JOIN agents ag ON ag.id = mb.agent_id
JOIN rooms r ON r.id = t.room_id
JOIN projects p ON p.id = r.project_id
LEFT JOIN messages cm ON cm.id = t.chain_message_id
WHERE t.started_at >= sqlc.arg('since')
  AND (sqlc.narg('project_id')::uuid IS NULL OR r.project_id = sqlc.narg('project_id')::uuid)
ORDER BY t.started_at;
