-- +goose Up

-- A turn is one run of an agent instance's engine, triggered by a message.
-- The row is the durable summary; the full event stream is a JSONL file
-- under the hub's state directory, referenced by transcript_path.
CREATE TABLE turns (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_instance_id  uuid        NOT NULL REFERENCES agent_instances (id),
    room_id            uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    -- The thread the agent replies in. Always set: a top-level trigger gets
    -- a thread created for it before the turn starts.
    thread_id          uuid        NOT NULL REFERENCES threads (id) ON DELETE CASCADE,
    -- The message that caused the turn; the last one when several were
    -- merged into a single turn.
    trigger_message_id uuid        REFERENCES messages (id) ON DELETE SET NULL,
    worker_id          uuid        NOT NULL REFERENCES workers (id),
    status             text        NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'done', 'failed', 'cancelled')),
    error              text        NOT NULL DEFAULT '',
    -- The agent's reply, once posted.
    reply_message_id   uuid        REFERENCES messages (id) ON DELETE SET NULL,
    transcript_path    text        NOT NULL DEFAULT '',
    started_at         timestamptz NOT NULL DEFAULT now(),
    ended_at           timestamptz
);

CREATE INDEX turns_by_instance ON turns (agent_instance_id, started_at DESC);
CREATE INDEX turns_by_room ON turns (room_id, started_at DESC);

-- +goose Down
DROP TABLE turns;
