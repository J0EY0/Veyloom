-- What a brief is put together from: what a session has not read yet. Every
-- query is bounded above by the position the brief is taken at, so the
-- parts of one brief agree on what "now" is, and leaves out what the
-- session said itself (messages of turns that ran in it), which it
-- remembers without being told.

-- name: RoomPosition :one
-- The seq of the room's newest message: where the room stands now.
SELECT coalesce(max(seq), 0)::bigint AS position FROM messages WHERE room_id = $1;

-- name: ListRoomNews :many
-- Top-level messages of a room in (after, up_to], the newest `limit` of
-- them, newest first, each with the number of the topic it heads (0 when
-- none) and the count of all that matched.
SELECT sqlc.embed(m),
       coalesce(t.number, 0)::int AS topic_number,
       count(*) OVER () AS total
FROM messages m
LEFT JOIN threads t ON t.root_message_id = m.id
LEFT JOIN turns tu ON tu.id = m.turn_id
WHERE m.room_id = sqlc.arg(room_id)
  AND m.thread_id IS NULL
  AND m.seq > sqlc.arg(after)::bigint AND m.seq <= sqlc.arg(up_to)::bigint
  AND (sqlc.narg(session_id)::uuid IS NULL OR tu.session_id IS DISTINCT FROM sqlc.narg(session_id)::uuid)
ORDER BY m.seq DESC
LIMIT sqlc.arg(max_rows);

-- name: ListTopicNews :many
-- Topics of a room with replies in (after, up_to], other than one, most
-- recently active first: how many replies, the root and the last of them,
-- and the count of all topics that matched.
SELECT n.thread_id, n.number, n.new_count, n.total, sqlc.embed(r), sqlc.embed(l)
FROM (
    SELECT t.id AS thread_id, t.number, t.root_message_id,
           count(*) AS new_count,
           max(m.seq)::bigint AS last_seq,
           count(*) OVER () AS total
    FROM messages m
    JOIN threads t ON t.id = m.thread_id
    LEFT JOIN turns tu ON tu.id = m.turn_id
    WHERE m.room_id = sqlc.arg(room_id)
      AND m.seq > sqlc.arg(after)::bigint AND m.seq <= sqlc.arg(up_to)::bigint
      AND t.id <> sqlc.arg(except_thread_id)::uuid
      AND (sqlc.narg(session_id)::uuid IS NULL OR tu.session_id IS DISTINCT FROM sqlc.narg(session_id)::uuid)
    GROUP BY t.id, t.number, t.root_message_id
    ORDER BY last_seq DESC
    LIMIT sqlc.arg(max_rows)
) n
JOIN messages r ON r.id = n.root_message_id
JOIN messages l ON l.seq = n.last_seq
ORDER BY n.last_seq DESC;

-- name: ListThreadNews :many
-- Replies of a thread in (after, up_to], the newest `limit` of them,
-- newest first, with the count of all that matched. With after 0 and no
-- session it is the tail of the whole thread.
SELECT sqlc.embed(m), count(*) OVER () AS total
FROM messages m
LEFT JOIN turns tu ON tu.id = m.turn_id
WHERE m.thread_id = sqlc.arg(thread_id)
  AND m.seq > sqlc.arg(after)::bigint AND m.seq <= sqlc.arg(up_to)::bigint
  AND (sqlc.narg(session_id)::uuid IS NULL OR tu.session_id IS DISTINCT FROM sqlc.narg(session_id)::uuid)
ORDER BY m.seq DESC
LIMIT sqlc.arg(max_rows);
