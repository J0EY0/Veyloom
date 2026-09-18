-- +goose Up

-- A turn is one run of a member's runtime, triggered by a message.
-- The row is the durable summary; the full event stream is a JSONL file
-- under the hub's state directory, referenced by transcript_path.
CREATE TABLE turns (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    member_id          uuid        NOT NULL REFERENCES members (id),
    room_id            uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    -- The thread the agent replies in. Always set: a top-level trigger gets
    -- a thread created for it before the turn starts.
    thread_id          uuid        NOT NULL REFERENCES threads (id) ON DELETE CASCADE,
    -- The message that caused the turn; the last one when several were
    -- merged into a single turn.
    trigger_message_id uuid        REFERENCES messages (id) ON DELETE SET NULL,
    machine_id         uuid        NOT NULL REFERENCES machines (id),
    -- The member's session the turn ran in. Null for a turn that never got
    -- one: it failed before the hub could open or find a session.
    session_id         uuid        REFERENCES member_sessions (id) ON DELETE SET NULL,
    -- The runtime that ran the turn, as the member's agent named it when
    -- the turn started: an agent changed or deleted later leaves it be.
    runtime            text        NOT NULL DEFAULT '',
    status             text        NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'done', 'failed', 'cancelled')),
    error              text        NOT NULL DEFAULT '',
    -- The agent's reply, once posted.
    reply_message_id   uuid        REFERENCES messages (id) ON DELETE SET NULL,
    transcript_path    text        NOT NULL DEFAULT '',
    -- The tokens the turn spent as its runtime reported them, in parts that
    -- do not overlap: fresh input, input read from and written to a prompt
    -- cache, and output. Zero until the turn ends. No cost is kept: a
    -- runtime's own figure is a list-price guess.
    input_tokens       bigint      NOT NULL DEFAULT 0,
    cache_read_tokens  bigint      NOT NULL DEFAULT 0,
    cache_write_tokens bigint      NOT NULL DEFAULT 0,
    output_tokens      bigint      NOT NULL DEFAULT 0,
    -- The files the turn wrote, as its runtime reported them, each once and
    -- in the order first touched. What another agent reading the topic is
    -- told the turn changed.
    files_changed      text[]      NOT NULL DEFAULT '{}',
    started_at         timestamptz NOT NULL DEFAULT now(),
    ended_at           timestamptz
);

CREATE INDEX turns_by_member ON turns (member_id, started_at DESC);
CREATE INDEX turns_by_room ON turns (room_id, started_at DESC);
-- A machine's activity over the last day, week or month.
CREATE INDEX turns_by_machine ON turns (machine_id, started_at);

-- +goose Down
DROP TABLE turns;
