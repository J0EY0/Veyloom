-- name: GetOpenSession :one
-- The session a member's next turn resumes; no row when it has none.
SELECT * FROM member_sessions WHERE member_id = $1 AND ended_at IS NULL;

-- name: GetSession :one
SELECT * FROM member_sessions WHERE id = $1;

-- name: CreateSession :one
-- The id comes from the caller: the hub hands it to the runtime as the
-- session's name before the row exists.
INSERT INTO member_sessions (id, member_id, runtime, machine_id, work_dir, session_ref)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: EndOpenSession :execrows
-- Ends the member's open session, if it has one, and says why.
UPDATE member_sessions SET ended_at = now(), end_reason = $2
WHERE member_id = $1 AND ended_at IS NULL;

-- name: SetSessionRef :execrows
-- Stores the runtime's own reference to the session as soon as it is known.
UPDATE member_sessions SET session_ref = $2 WHERE id = $1;

-- name: ListMemberSessions :many
-- Every session a member has had, newest first.
SELECT * FROM member_sessions WHERE member_id = $1 ORDER BY started_at DESC, id;

-- name: AdvanceSession :execrows
-- Moves a session's reading positions forward after a turn that took its
-- brief in: the room's, the one topic's it was briefed in, and the project
-- wiki's when the brief showed it (NULL leaves it). Positions never move
-- back.
UPDATE member_sessions SET
    room_seen   = GREATEST(room_seen, sqlc.arg(room_seen)::bigint),
    thread_seen = thread_seen || jsonb_build_object(
        sqlc.arg(thread_id)::text,
        GREATEST(coalesce((thread_seen ->> sqlc.arg(thread_id)::text)::bigint, 0), sqlc.arg(thread_seen)::bigint)),
    wiki_seen   = GREATEST(wiki_seen, sqlc.narg(wiki_seen)::timestamptz)
WHERE id = sqlc.arg(id);

-- name: NoteSessionCompactions :execrows
-- Counts compactions the runtime reported and forgets what the session had
-- read of each topic and of the wiki's catalog, as the session itself just
-- did with the detail.
UPDATE member_sessions SET
    compactions = compactions + sqlc.arg(count)::int,
    thread_seen = '{}'::jsonb,
    wiki_seen   = NULL
WHERE id = sqlc.arg(id);

-- name: CountSessionTurns :one
-- How many turns have run in a session.
SELECT count(*) FROM turns WHERE session_id = $1;

-- name: MemberHasRunningTurn :one
SELECT EXISTS (SELECT 1 FROM turns WHERE member_id = $1 AND status = 'running');
