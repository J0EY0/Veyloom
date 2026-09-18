-- name: CreateAttachment :one
-- The id comes from the caller: the file is named after it on disk before
-- the row exists.
INSERT INTO attachments (id, room_id, filename, media_type, size, path)
VALUES ($1, $2, $3, $4, $5, $6)
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
