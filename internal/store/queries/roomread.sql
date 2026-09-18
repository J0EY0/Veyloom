-- What an agent reads of its room on request (the read tools, design.md
-- 5.7): the directory of topics, a topic by its number, a search.

-- name: GetThreadByNumber :one
SELECT * FROM threads WHERE room_id = $1 AND number = $2;

-- name: ListRoomTopics :many
-- A room's topics, most recently active first, older than a cursor: how
-- many replies each has, its root, and its last message (the root itself
-- while nobody has replied).
SELECT x.thread_id, x.number, x.reply_count, x.last_seq, sqlc.embed(r), sqlc.embed(l)
FROM (
    SELECT t.id AS thread_id, t.number, t.root_message_id,
           (SELECT count(*) FROM messages m WHERE m.thread_id = t.id) AS reply_count,
           coalesce((SELECT max(m.seq) FROM messages m WHERE m.thread_id = t.id), rm.seq)::bigint AS last_seq
    FROM threads t
    JOIN messages rm ON rm.id = t.root_message_id
    WHERE t.room_id = sqlc.arg(room_id)
) x
JOIN messages r ON r.id = x.root_message_id
JOIN messages l ON l.seq = x.last_seq
WHERE x.last_seq < sqlc.arg(before)::bigint
ORDER BY x.last_seq DESC
LIMIT sqlc.arg(max_rows);

-- name: SearchRoomMessages :many
-- Messages of a room whose text holds a phrase, newest first, each with
-- the number of the topic it is in or heads (0 for neither). The pattern
-- arrives escaped, wildcards included.
SELECT sqlc.embed(m), coalesce(t.number, rt.number, 0)::int AS topic_number
FROM messages m
LEFT JOIN threads t ON t.id = m.thread_id
LEFT JOIN threads rt ON rt.root_message_id = m.id
WHERE m.room_id = sqlc.arg(room_id)
  AND m.body ILIKE sqlc.arg(pattern)::text
  AND m.seq < sqlc.arg(before)::bigint
ORDER BY m.seq DESC
LIMIT sqlc.arg(max_rows);
