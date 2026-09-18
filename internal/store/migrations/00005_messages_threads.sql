-- +goose Up

-- A message is one post in a room. Top-level messages have no thread; a
-- reply lives in the thread rooted at a top-level message.
CREATE TABLE messages (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Global, monotonically increasing sequence. It orders messages, serves
    -- as the pagination cursor, and later tells an agent what happened
    -- since its last turn.
    seq               bigint      GENERATED ALWAYS AS IDENTITY NOT NULL UNIQUE,
    room_id           uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    -- NULL for a top-level message. The foreign key is added below, after
    -- the threads table exists.
    thread_id         uuid,
    sender_kind       text        NOT NULL CHECK (sender_kind IN ('user', 'agent', 'system')),
    -- Exactly one of these is set, matching sender_kind; system messages
    -- have neither.
    user_id           uuid        REFERENCES users (id),
    member_id         uuid        REFERENCES members (id),
    body              text        NOT NULL CHECK (btrim(body) <> ''),
    -- Structured @-mentions supplied by the client:
    -- [{"kind": "user" | "agent", "id": "..."}]. Routing reads these, never
    -- the body text.
    mentions          jsonb       NOT NULL DEFAULT '[]'::jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT messages_sender_matches_kind CHECK (
        (sender_kind = 'user')  = (user_id IS NOT NULL) AND
        (sender_kind = 'agent') = (member_id IS NOT NULL)
    )
);

-- A thread groups the replies to one top-level message. It is created on
-- the first reply, so a message without replies has no thread row.
CREATE TABLE threads (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id         uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    root_message_id uuid        NOT NULL UNIQUE REFERENCES messages (id) ON DELETE CASCADE,
    -- The topic's number in its room, written #12: what people and agents
    -- call it by. Rising and unique per room; a gap is harmless.
    number          integer     NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT threads_number_per_room UNIQUE (room_id, number)
);

ALTER TABLE messages
    ADD CONSTRAINT messages_thread_id_fkey
    FOREIGN KEY (thread_id) REFERENCES threads (id) ON DELETE CASCADE;

-- Room timeline: top-level messages in order.
CREATE INDEX messages_room_timeline ON messages (room_id, seq) WHERE thread_id IS NULL;
-- Thread view: replies in order.
CREATE INDEX messages_by_thread ON messages (thread_id, seq) WHERE thread_id IS NOT NULL;

-- +goose Down
ALTER TABLE messages DROP CONSTRAINT messages_thread_id_fkey;
DROP TABLE threads;
DROP TABLE messages;
