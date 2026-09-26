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
    -- What the turn was for: answering the chat, the wiki maintainer
    -- going over what the chat did (design.md 5.12), or the project's
    -- leader setting the project up for its members' worktrees (5.21).
    -- The last two run in sessions of their own and are not gone over.
    kind               text        NOT NULL DEFAULT 'chat' CHECK (kind IN ('chat', 'upkeep', 'setup')),
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
    -- The skills of the library the turn used, by name, each once: seen in
    -- its tool calls, not merely given (docs/design.md 5.10). How a skill's
    -- team learns where it was used and how that went.
    skills_used        text[]      NOT NULL DEFAULT '{}',
    -- The pages of the project's wiki the turn wrote, by path, each once:
    -- what the task board says a task left behind (docs/webui.md 4.20).
    wiki_pages         text[]      NOT NULL DEFAULT '{}',
    -- The piece of work the turn is part of (design.md 5.22): the person's
    -- message it started from, or the note a person let a held wake go on
    -- from. A turn an agent woke carries on its waker's; woken_by_turn_id
    -- is that turn, null for one a person woke.
    chain_message_id   uuid        REFERENCES messages (id) ON DELETE SET NULL,
    woken_by_turn_id   uuid        REFERENCES turns (id) ON DELETE SET NULL,
    -- The turn did work: called a tool other than those that follow and
    -- talk in the chat, or changed a file. Known once it ends.
    worked             boolean     NOT NULL DEFAULT false,
    -- A person let the rest of the turn's requests through (docs/design.md
    -- 4.6): who and since when, NULL when nobody did or they took it back.
    -- The account is no users row, hence no reference.
    trusted_by         uuid,
    trusted_at         timestamptz,
    started_at         timestamptz NOT NULL DEFAULT now(),
    ended_at           timestamptz
);

CREATE INDEX turns_by_member ON turns (member_id, started_at DESC);
CREATE INDEX turns_by_room ON turns (room_id, started_at DESC);
-- A machine's activity over the last day, week or month.
CREATE INDEX turns_by_machine ON turns (machine_id, started_at);
-- The turns that used a skill.
CREATE INDEX turns_by_skill ON turns USING gin (skills_used);
-- A room's maintainer turns, newest first.
CREATE INDEX turns_upkeep ON turns (room_id, started_at DESC) WHERE kind = 'upkeep';
-- The turns agents woke in a piece of work, the latest to end first.
CREATE INDEX turns_woken ON turns (chain_message_id, ended_at DESC) WHERE woken_by_turn_id IS NOT NULL;
-- A piece of work across its topics, in the order its turns began.
CREATE INDEX turns_by_chain ON turns (chain_message_id, started_at);

-- A wake that one of the limits on agents waking one another held back
-- (design.md 5.22): the note telling the person, with what to do should
-- they let it go on after all. It goes on once.
CREATE TABLE relay_holds (
    message_id         uuid        PRIMARY KEY REFERENCES messages (id) ON DELETE CASCADE,
    -- Who was not woken, where, and by what message.
    member_id          uuid        NOT NULL REFERENCES members (id),
    thread_id          uuid        NOT NULL REFERENCES threads (id) ON DELETE CASCADE,
    trigger_message_id uuid        NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    -- idle: the last turns agents woke did no work; limit: the piece of
    -- work reached the project's relay limit.
    reason             text        NOT NULL CHECK (reason IN ('idle', 'limit')),
    created_at         timestamptz NOT NULL DEFAULT now(),
    continued_at       timestamptz
);

-- What became of a member's branch (design.md 5.21): its work put on the
-- main line by a merge, or the branch reset to the main line with its work
-- archived under a ref. A merged branch that held other members' work is a
-- row for each of them too, via the member whose branch was merged. The
-- task board tells from these which of a member's tasks were merged or set
-- aside, and which still wait for it (docs/webui.md 4.20).
CREATE TABLE branch_events (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    member_id     uuid        NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    kind          text        NOT NULL CHECK (kind IN ('merged', 'reset')),
    -- merged: the commit on the main line; reset: the ref the work is
    -- archived under.
    commit_sha    text        NOT NULL DEFAULT '',
    ref           text        NOT NULL DEFAULT '',
    -- The member whose branch was merged, when it was not this one's.
    via_member_id uuid        REFERENCES members (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX branch_events_by_member ON branch_events (member_id, created_at);

-- +goose Down
DROP TABLE branch_events;
DROP TABLE relay_holds;
DROP TABLE turns;
