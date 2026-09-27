-- +goose Up

-- A file a person attached to a message. Uploads land here first with no
-- message; posting the message claims them. The bytes live on the hub's
-- disk under the attachment directory; path is relative to it.
CREATE TABLE attachments (
    id             uuid        PRIMARY KEY,
    room_id        uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    message_id     uuid        REFERENCES messages (id) ON DELETE CASCADE,
    filename       text        NOT NULL CHECK (btrim(filename) <> ''),
    media_type     text        NOT NULL,
    -- What the chat draws it as (docs/webui.md 4.21): a picture, a video,
    -- sound, a PDF, text or code, an office document, an archive, or a
    -- file to download.
    kind           text        NOT NULL DEFAULT 'other'
                               CHECK (kind IN ('image', 'video', 'audio', 'pdf', 'text', 'office', 'archive', 'other')),
    size           bigint      NOT NULL CHECK (size >= 0),
    -- A picture's size in pixels as a browser shows it, so the chat keeps
    -- its place before it loads; 0 when not known.
    width          integer     NOT NULL DEFAULT 0 CHECK (width >= 0),
    height         integer     NOT NULL DEFAULT 0 CHECK (height >= 0),
    path           text        NOT NULL,
    -- A smaller copy of a big picture, beside the file and relative to the
    -- same directory; empty when the picture is shown as it is.
    thumbnail_path text        NOT NULL DEFAULT '',
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX attachments_by_message ON attachments (message_id) WHERE message_id IS NOT NULL;
-- What messages of a room carry, newest first: the attachments tab.
CREATE INDEX attachments_by_room ON attachments (room_id, created_at DESC) WHERE message_id IS NOT NULL;
-- Uploads no message carries, oldest first: what is swept away.
CREATE INDEX attachments_unclaimed ON attachments (created_at) WHERE message_id IS NULL;

-- A person may post a file with no words; only the system always speaks.
-- The API refuses a user message that has neither text nor attachments.
ALTER TABLE messages DROP CONSTRAINT messages_body_check;
ALTER TABLE messages ADD CONSTRAINT messages_body_check
    CHECK (sender_kind <> 'system' OR btrim(body) <> '');

-- +goose Down
ALTER TABLE messages DROP CONSTRAINT messages_body_check;
ALTER TABLE messages ADD CONSTRAINT messages_body_check
    CHECK (sender_kind = 'agent' OR btrim(body) <> '');
DROP TABLE attachments;
