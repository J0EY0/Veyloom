-- name: CreateAttachment :one
-- The id comes from the caller: the file is named after it on disk before
-- the row exists.
INSERT INTO attachments (id, room_id, filename, media_type, kind, size, width, height, path, thumbnail_path)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetAttachment :one
SELECT * FROM attachments WHERE id = $1;

-- name: ClaimAttachments :many
-- Ties uploads to the message that carries them. Only unclaimed uploads
-- of the same room qualify, so an id from elsewhere is simply not returned.
UPDATE attachments SET message_id = sqlc.arg(message_id)
WHERE id = ANY(sqlc.arg(ids)::uuid[]) AND room_id = sqlc.arg(room_id) AND message_id IS NULL
RETURNING *;

-- name: ListAttachmentsByMessages :many
SELECT * FROM attachments
WHERE message_id = ANY(sqlc.arg(message_ids)::uuid[])
ORDER BY created_at, id;

-- name: ListRoomAttachments :many
-- The attachments messages of a room carry, for the attachments tab
-- (docs/webui.md 4.21): each with the message that carried it and who sent
-- it, narrowed by kind, sender and words, sorted newest or oldest first,
-- biggest first or by name. Every one of the patterns matches the file's
-- name, the message's text, the member's or the person's name; people's
-- names are in the account file, so they come in as people_ids and
-- people_names, side by side, and a person's is found by position. Newest first is oldest first turned round,
-- a message's files too, so the viewer can walk either way.
SELECT sqlc.embed(a), m.thread_id, m.sender_kind, m.user_id, m.member_id, m.seq AS message_seq, m.body AS message_body,
       coalesce(mb.display_name, (sqlc.arg(people_names)::text[])[array_position(sqlc.arg(people_ids)::uuid[], m.user_id)], '')::text AS sender_name, coalesce(th.number, 0)::int AS thread_number
FROM attachments a
JOIN messages m ON m.id = a.message_id
LEFT JOIN members mb ON mb.id = m.member_id
LEFT JOIN threads th ON th.id = m.thread_id
WHERE a.room_id = sqlc.arg(room_id)
  AND (cardinality(sqlc.arg(kinds)::text[]) = 0 OR a.kind = ANY(sqlc.arg(kinds)::text[]))
  AND (sqlc.narg(user_id)::uuid IS NULL OR m.user_id = sqlc.narg(user_id))
  AND (sqlc.narg(member_id)::uuid IS NULL OR m.member_id = sqlc.narg(member_id))
  AND NOT EXISTS (
    SELECT 1 FROM unnest(sqlc.arg(patterns)::text[]) AS w(pattern)
    WHERE NOT (a.filename ILIKE w.pattern OR m.body ILIKE w.pattern
               OR coalesce(mb.display_name, '') ILIKE w.pattern OR coalesce((sqlc.arg(people_names)::text[])[array_position(sqlc.arg(people_ids)::uuid[], m.user_id)], '') ILIKE w.pattern))
ORDER BY
    CASE WHEN sqlc.arg(sort)::text = 'size' THEN a.size END DESC,
    CASE WHEN sqlc.arg(sort)::text = 'name' THEN lower(a.filename) END,
    CASE WHEN sqlc.arg(sort)::text = 'oldest' THEN m.seq END,
    CASE WHEN sqlc.arg(sort)::text = 'oldest' THEN a.created_at END,
    CASE WHEN sqlc.arg(sort)::text = 'oldest' THEN a.id END,
    m.seq DESC,
    a.created_at DESC, a.id DESC
LIMIT sqlc.arg(max) OFFSET sqlc.arg(skip);

-- name: CountRoomAttachments :one
-- How many attachments ListRoomAttachments finds, all its pages.
SELECT count(*)
FROM attachments a
JOIN messages m ON m.id = a.message_id
LEFT JOIN members mb ON mb.id = m.member_id
WHERE a.room_id = sqlc.arg(room_id)
  AND (cardinality(sqlc.arg(kinds)::text[]) = 0 OR a.kind = ANY(sqlc.arg(kinds)::text[]))
  AND (sqlc.narg(user_id)::uuid IS NULL OR m.user_id = sqlc.narg(user_id))
  AND (sqlc.narg(member_id)::uuid IS NULL OR m.member_id = sqlc.narg(member_id))
  AND NOT EXISTS (
    SELECT 1 FROM unnest(sqlc.arg(patterns)::text[]) AS w(pattern)
    WHERE NOT (a.filename ILIKE w.pattern OR m.body ILIKE w.pattern
               OR coalesce(mb.display_name, '') ILIKE w.pattern OR coalesce((sqlc.arg(people_names)::text[])[array_position(sqlc.arg(people_ids)::uuid[], m.user_id)], '') ILIKE w.pattern));

-- name: ListRoomAttachmentsByID :many
-- Some of the attachments messages of a room carry, to download together.
SELECT * FROM attachments
WHERE room_id = sqlc.arg(room_id) AND id = ANY(sqlc.arg(ids)::uuid[]) AND message_id IS NOT NULL
ORDER BY created_at, id;

-- name: ListUnclaimedAttachments :many
-- Uploads no message took, older than a moment: left behind when a person
-- attached a file and then did not send it.
SELECT * FROM attachments
WHERE message_id IS NULL AND created_at < sqlc.arg(before)
ORDER BY created_at
LIMIT sqlc.arg(max);

-- name: DeleteUnclaimedAttachment :one
-- Forgets an upload, unless a message took it meanwhile.
DELETE FROM attachments WHERE id = $1 AND message_id IS NULL
RETURNING *;
