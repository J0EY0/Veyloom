-- +goose Up

-- A file a person attached to a message. Uploads land here first with no
-- message; posting the message claims them. The bytes live on the hub's
-- disk under the attachment directory; path is relative to it.
CREATE TABLE attachments (
    id         uuid        PRIMARY KEY,
    room_id    uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    message_id uuid        REFERENCES messages (id) ON DELETE CASCADE,
    filename   text        NOT NULL CHECK (btrim(filename) <> ''),
    media_type text        NOT NULL,
    size       bigint      NOT NULL CHECK (size >= 0),
    path       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX attachments_by_message ON attachments (message_id) WHERE message_id IS NOT NULL;

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
