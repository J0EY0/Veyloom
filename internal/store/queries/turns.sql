-- name: CreateTurn :one
INSERT INTO turns (member_id, room_id, thread_id, trigger_message_id, machine_id, runtime, transcript_path, session_id, kind, chain_message_id, woken_by_turn_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: FinishTurn :one
UPDATE turns SET
    status             = $2,
    error              = $3,
    reply_message_id   = $4,
    transcript_path    = $5,
    input_tokens       = $6,
    cache_read_tokens  = $7,
    cache_write_tokens = $8,
    output_tokens      = $9,
    files_changed      = $10,
    skills_used        = $11,
    worked             = $12,
    ended_at           = now()
WHERE id = $1
RETURNING *;

-- name: GetTurn :one
SELECT * FROM turns WHERE id = $1;

-- name: SetTurnSession :execrows
-- Moves a running turn to another session: the one it started in would not
-- resume, and the turn was run again in a new one.
UPDATE turns SET session_id = $2 WHERE id = $1;

-- name: ListRoomTurns :many
-- Most recent turns of a room first.
SELECT * FROM turns WHERE room_id = $1 ORDER BY started_at DESC LIMIT $2;

-- name: ListThreadTurns :many
-- Every turn of a topic, oldest first, so a thread can be read turn by turn.
SELECT * FROM turns
WHERE thread_id = $1
ORDER BY started_at;

-- name: ListRoomTurnsByStatus :many
-- Most recent turns of a room in one status first; the UI opens a room
-- with only the running ones.
SELECT * FROM turns WHERE room_id = $1 AND status = $2 ORDER BY started_at DESC LIMIT $3;

-- name: ListRunningTopics :many
-- The topics with a turn in flight, across every room: what the sidebar
-- lists under each project while its members work.
SELECT t.id AS thread_id, t.room_id, t.root_message_id, m.body AS root_body,
       array_agg(DISTINCT mb.display_name)::text[] AS members,
       min(tu.started_at)::timestamptz AS started_at,
       -- What the topic was asked: the words that set its first turn off,
       -- and the names of the room's members, whose @s lead them.
       coalesce((SELECT a.body FROM turns ft JOIN messages a ON a.id = ft.trigger_message_id
                 WHERE ft.thread_id = t.id ORDER BY ft.started_at LIMIT 1), '')::text AS ask,
       coalesce((SELECT array_agg(rm.display_name) FROM members rm WHERE rm.room_id = t.room_id), '{}')::text[] AS names
FROM turns tu
JOIN threads t ON t.id = tu.thread_id
JOIN messages m ON m.id = t.root_message_id
JOIN members mb ON mb.id = tu.member_id
WHERE tu.status = 'running'
GROUP BY t.id, t.room_id, t.root_message_id, m.body
ORDER BY started_at;

-- name: FailRunningTurns :many
-- Turns still marked running when the hub starts were cut off by its last
-- stop; nothing will ever finish them.
UPDATE turns SET status = 'failed', error = $1, ended_at = now()
WHERE status = 'running'
RETURNING *;

-- name: ListMachineTurnsSince :many
-- How a machine's turns since a moment went: when each started, how it
-- ended and when, and the tokens it spent; only one runtime's unless runtime
-- is empty. The machines page sums them into hours or days.
SELECT status, started_at, ended_at, input_tokens, cache_read_tokens, cache_write_tokens, output_tokens
FROM turns
WHERE machine_id = sqlc.arg(machine_id)
  AND started_at >= sqlc.arg(since)
  AND (sqlc.arg(runtime)::text = '' OR runtime = sqlc.arg(runtime)::text)
ORDER BY started_at;

-- name: ListSkillUses :many
-- The turns that used a skill, newest first, with where they ran: what the
-- team that owns it looks back on.
SELECT t.id, t.status, t.runtime, t.started_at, t.ended_at, t.room_id, t.thread_id,
       th.number AS topic_number, m.display_name AS member_name, p.id AS project_id, p.name AS project_name
FROM turns t
JOIN threads th ON th.id = t.thread_id
JOIN members m ON m.id = t.member_id
JOIN rooms r ON r.id = t.room_id
JOIN projects p ON p.id = r.project_id
WHERE sqlc.arg(skill)::text = ANY (t.skills_used)
ORDER BY t.started_at DESC, t.id
LIMIT sqlc.arg(lim);

-- name: CountChainWakes :one
-- The turns agents woke in a piece of work, under way or over.
SELECT count(*) FROM turns WHERE chain_message_id = $1 AND woken_by_turn_id IS NOT NULL;

-- name: RecentChainWakes :many
-- Whether the last turns agents woke in a piece of work to end did work,
-- the latest first.
SELECT worked FROM turns
WHERE chain_message_id = $1 AND woken_by_turn_id IS NOT NULL AND ended_at IS NOT NULL
ORDER BY ended_at DESC
LIMIT $2;

-- name: SetTurnTrust :one
-- A person lets the rest of a running turn's requests through.
UPDATE turns SET trusted_by = $2, trusted_at = now()
WHERE id = $1 AND status = 'running'
RETURNING *;

-- name: ClearTurnTrust :one
-- A person takes it back. A turn that ended trusted keeps who trusted it,
-- as a record: nothing is let through once it is over.
UPDATE turns SET trusted_by = NULL, trusted_at = NULL
WHERE id = $1
RETURNING *;
