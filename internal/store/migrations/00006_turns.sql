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
-- A topic's turns: read turn by turn, and asked who runs there and whom a
-- person talks with there (design.md 4.2).
CREATE INDEX turns_by_thread ON turns (thread_id, started_at);

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
    -- work reached the project's relay limit; people: only people wake
    -- members in the project, and a member's reminder came due.
    reason             text        NOT NULL CHECK (reason IN ('idle', 'limit', 'people')),
    created_at         timestamptz NOT NULL DEFAULT now(),
    continued_at       timestamptz
);

-- What a member was asked while it was busy, waiting for its turn: kept so
-- a hub that stops does not lose it, the member woken for it once its
-- machine is back; gone as the turn that answers it starts.
CREATE TABLE queued_wakes (
    member_id  uuid        NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    message_id uuid        NOT NULL REFERENCES messages (id) ON DELETE CASCADE,
    -- The topic it is answered in; NULL for a message to the room, whose
    -- turn opens a topic of its own.
    thread_id  uuid        REFERENCES threads (id) ON DELETE CASCADE,
    -- A piece of work of its own starts at this message: a person let a
    -- held wake go on (design.md 5.22).
    anchor_id  uuid        REFERENCES messages (id) ON DELETE CASCADE,
    queued_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (member_id, message_id)
);

-- What keeps a member's turns from starting for a while (design.md
-- 5.23.3): a runtime's account on a machine that cannot take turns now
-- (signed out, its usage limit reached, too many requests, its provider
-- failing), or a member whose turns keep failing. What the members are
-- asked meanwhile waits in queued_wakes, and goes on once the pause is
-- lifted: when it runs out, or when a person lifts it.
CREATE TABLE pauses (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- An account's: its machine and runtime. A member's: the member.
    machine_id uuid        REFERENCES machines (id) ON DELETE CASCADE,
    runtime    text        NOT NULL DEFAULT '',
    member_id  uuid        REFERENCES members (id) ON DELETE CASCADE,
    -- auth, quota, rate_limit or server for an account's; failing for a
    -- member's.
    reason     text        NOT NULL,
    -- What the runtime said, the last time it failed.
    detail     text        NOT NULL DEFAULT '',
    -- When it runs out; NULL waits for a person.
    ends_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK ((member_id IS NULL) = (machine_id IS NOT NULL AND runtime <> ''))
);

CREATE UNIQUE INDEX pauses_by_account ON pauses (machine_id, runtime) WHERE member_id IS NULL;
CREATE UNIQUE INDEX pauses_by_member ON pauses (member_id) WHERE member_id IS NOT NULL;

-- A member's reminder to itself (design.md 5.23.4): the hub wakes it, in
-- the topic it set it in, once it comes due.
CREATE TABLE reminders (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    member_id        uuid        NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    room_id          uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    thread_id        uuid        NOT NULL REFERENCES threads (id) ON DELETE CASCADE,
    -- The turn that set it: the wake it makes carries on that turn's
    -- piece of work.
    turn_id          uuid        REFERENCES turns (id) ON DELETE SET NULL,
    note             text        NOT NULL,
    due_at           timestamptz NOT NULL,
    -- pending: not yet due; fired: came due; cancelled: taken back, by the
    -- member or a person; dropped: its member was taken out of the project
    -- or switched off by the time it came due.
    status           text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'fired', 'cancelled', 'dropped')),
    -- The note that told of it as it was set, and the message it came due
    -- as.
    set_message_id   uuid        REFERENCES messages (id) ON DELETE SET NULL,
    fired_message_id uuid        REFERENCES messages (id) ON DELETE SET NULL,
    -- The person who cancelled it; NULL for one the member took back. No
    -- reference: the one account lives outside users (00011).
    cancelled_by     uuid,
    created_at       timestamptz NOT NULL DEFAULT now(),
    settled_at       timestamptz
);

CREATE INDEX reminders_pending ON reminders (due_at) WHERE status = 'pending';
CREATE INDEX reminders_by_thread ON reminders (thread_id, created_at);

-- What a member drafted for a person to do with one press (design.md
-- 5.23.5): put a member's work on the main line, give it up, install a
-- skill for a member, adopt the steps new worktrees are got ready with.
-- The card in the topic is drawn from it.
CREATE TABLE drafts (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id        uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    room_id           uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    thread_id         uuid        NOT NULL REFERENCES threads (id) ON DELETE CASCADE,
    -- Who drafted it, in which turn.
    member_id         uuid        NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    turn_id           uuid        REFERENCES turns (id) ON DELETE SET NULL,
    kind              text        NOT NULL CHECK (kind IN ('merge', 'set_aside', 'install_skill', 'setup_steps')),
    -- The member whose work or agent it acts on; NULL for setup steps.
    target_id         uuid        REFERENCES members (id) ON DELETE CASCADE,
    -- What it is about, which one pending draft of the project holds at a
    -- time: a member's work, a skill for a member, the setup steps.
    subject           text        NOT NULL,
    -- The kind's own: a merge's message, why work is given up, the skill,
    -- the steps.
    params            jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- What the member does once it is done: set, it is woken then.
    then_note         text        NOT NULL DEFAULT '',
    status            text        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'done', 'conflicted', 'declined', 'superseded')),
    -- What came of it: the commit, the archive ref, the conflicting files.
    result            jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- The card, and the message that told the member what came of it.
    message_id        uuid        REFERENCES messages (id) ON DELETE SET NULL,
    result_message_id uuid        REFERENCES messages (id) ON DELETE SET NULL,
    -- The person who ran it or turned it down. No reference: the one
    -- account lives outside users (00011).
    decided_by        uuid,
    created_at        timestamptz NOT NULL DEFAULT now(),
    settled_at        timestamptz
);

CREATE INDEX drafts_by_thread ON drafts (thread_id, created_at);
CREATE INDEX drafts_open ON drafts (project_id, subject) WHERE status IN ('pending', 'running');
-- One pending draft of a project about a thing, however many members draft
-- it at once: the one drafted last stands.
CREATE UNIQUE INDEX drafts_one_pending ON drafts (project_id, subject) WHERE status = 'pending';
CREATE INDEX drafts_by_message ON drafts (message_id);
CREATE INDEX drafts_by_result ON drafts (result_message_id);

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
DROP TABLE drafts;
DROP TABLE reminders;
DROP TABLE pauses;
DROP TABLE queued_wakes;
DROP TABLE relay_holds;
DROP TABLE turns;
