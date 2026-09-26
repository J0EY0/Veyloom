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

-- The project's "wiki" topic in its chat, where changes other projects
-- propose to the skills it owns wait for a person, and where its wiki's
-- maintainer works (docs/design.md 5.10, 5.12). Opened the first time it
-- is needed.
ALTER TABLE projects
    ADD COLUMN wiki_thread_id uuid REFERENCES threads (id) ON DELETE SET NULL;

-- The note in the project's chat that offered a wiki maintainer, once
-- enough topics waited to be gone over and none was chosen (design.md
-- 5.16); the chat draws it as a card. Offered once.
ALTER TABLE projects
    ADD COLUMN wiki_offer_message_id uuid REFERENCES messages (id) ON DELETE SET NULL;

-- The project's "setup" topic, where its leader sets the project up for
-- its members' worktrees (design.md 5.21); opened the first time it is
-- needed. And the note there that shows a person the steps waiting for
-- them to adopt, drawn as a card.
ALTER TABLE projects
    ADD COLUMN setup_thread_id uuid REFERENCES threads (id) ON DELETE SET NULL,
    ADD COLUMN workspace_pending_message_id uuid REFERENCES messages (id) ON DELETE SET NULL;

-- Room timeline: top-level messages in order.
CREATE INDEX messages_room_timeline ON messages (room_id, seq) WHERE thread_id IS NULL;
-- Thread view: replies in order.
CREATE INDEX messages_by_thread ON messages (thread_id, seq) WHERE thread_id IS NOT NULL;
-- What a person has read of the messages that mention them: their inbox
-- counts the rest (docs/webui.md 4.19). Read in the inbox, by opening the
-- topic it is in, or all at once. The person is no users row: the account
-- lives in the state dir (00011).
CREATE TABLE inbox_reads (
    user_id    uuid        NOT NULL,
    message_id uuid        NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    read_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, message_id)
);

-- +goose Down
DROP TABLE inbox_reads;
ALTER TABLE projects DROP COLUMN workspace_pending_message_id, DROP COLUMN setup_thread_id;
ALTER TABLE projects DROP COLUMN wiki_offer_message_id, DROP COLUMN wiki_thread_id;
ALTER TABLE messages DROP CONSTRAINT messages_thread_id_fkey;
DROP TABLE threads;
DROP TABLE messages;
