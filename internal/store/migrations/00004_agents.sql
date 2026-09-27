-- +goose Up

-- An agent is set up on one machine's runtime: which runtime and model to
-- run, how to behave (the role card) and what it may do (the permission
-- preset). Agents are added to projects as members, which run on the same
-- machine.
CREATE TABLE agents (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name              text        NOT NULL UNIQUE CHECK (btrim(name) <> ''),
    -- The picture people see for the agent: a file in the hub's avatar
    -- directory, named after its bytes. Empty shows the runtime's mark.
    avatar            text        NOT NULL DEFAULT '',
    -- The machine the agent is set up on. Its members run there; it moves
    -- only while it is in no project.
    machine_id        uuid        NOT NULL REFERENCES machines (id),
    -- Runtime identifier as reported by discovery: claude, codex, pi.
    runtime           text        NOT NULL CHECK (btrim(runtime) <> ''),
    -- Model for the runtime; empty means the runtime's own default.
    model             text        NOT NULL DEFAULT '',
    -- Instructions given to every turn: who the agent is and how it should
    -- behave in the room.
    role_card         text        NOT NULL DEFAULT '',
    permission_preset text        NOT NULL CHECK (permission_preset IN ('read_only', 'edit_with_approval', 'auto_review', 'full_auto')),
    -- Runtime-specific settings the machine passes through untouched.
    runtime_options   jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- The skills of the skill library a person installed for the agent, by
    -- name: its turns are given these and no others (docs/design.md 5.15).
    skills            text[]      NOT NULL DEFAULT '{}',
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- A member is an agent added to a room: the one people @ in chat. It runs
-- on one machine, works in one checkout and keeps its own runtime session
-- between turns.
CREATE TABLE members (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id             uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    -- Null only once a member taken out of its project has had its agent
    -- deleted. The check below keeps it set for current members, which is
    -- what turns down deleting an agent still in a project.
    agent_id            uuid        REFERENCES agents (id) ON DELETE SET NULL,
    -- The machine it runs on: its agent's when it was added, kept here for
    -- the history of a member whose agent is gone.
    machine_id          uuid        NOT NULL REFERENCES machines (id),
    -- Shown in chat and used for @-mentions; unique among the room's
    -- current members.
    display_name        text        NOT NULL CHECK (btrim(display_name) <> ''),
    -- Path of the project checkout on the machine.
    repo_path           text        NOT NULL DEFAULT '',
    -- worktree: the member gets its own git worktree per room;
    -- shared: it works in the checkout directly.
    branch_mode         text        NOT NULL DEFAULT 'worktree' CHECK (branch_mode IN ('worktree', 'shared')),
    -- The member's git worktree once made (design.md 5.21): its folder on
    -- the machine, where in it the member works (the same place as its
    -- checkout has in the repository), the branch checked out there, and
    -- when it was got ready the way the project's leader wrote down. Made
    -- once and kept: a rename changes none of it.
    worktree_dir        text        NOT NULL DEFAULT '',
    work_dir            text        NOT NULL DEFAULT '',
    branch              text        NOT NULL DEFAULT '',
    prepared_at         timestamptz,
    -- The files of its work the chat was told another member's work
    -- changed too (design.md 5.21): an overlap is said once, and again
    -- only after it went away.
    overlaps_noted      text[]      NOT NULL DEFAULT '{}',
    -- Per-member overrides; empty means "use the agent's".
    model               text        NOT NULL DEFAULT '',
    permission_preset   text        NOT NULL DEFAULT '' CHECK (permission_preset IN ('', 'read_only', 'edit_with_approval', 'auto_review', 'full_auto')),
    -- Disabled members stay in the room for history but take no turns.
    enabled             boolean     NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT now(),
    -- When the member was taken out of its project. The row stays: the
    -- messages, turns and approvals it left behind point at it and still
    -- need its name.
    removed_at          timestamptz,
    CONSTRAINT members_current_have_agent CHECK (removed_at IS NOT NULL OR agent_id IS NOT NULL)
);

-- Only current members need distinct names, so a member taken out can be
-- added back under the same one.
CREATE UNIQUE INDEX members_name_per_room ON members (room_id, display_name) WHERE removed_at IS NULL;

-- One conversation of a member with its runtime. A member has at most one
-- open at a time and every turn resumes it; the hub's records are the
-- truth and a session is a cache of them, so ending one and opening the
-- next loses nothing that cannot be rebuilt (design.md 5.6).
CREATE TABLE member_sessions (
    -- Also what a runtime that can be told its session id is told: Claude
    -- Code's --session-id, the name of Pi's session file.
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    member_id   uuid        NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    -- What the session was opened on. A turn whose member no longer matches
    -- all three opens a new session instead of resuming this one: a runtime
    -- cannot resume another's session, nor one from another machine, and
    -- resuming in another directory fails or stalls depending on the CLI.
    runtime     text        NOT NULL CHECK (btrim(runtime) <> ''),
    machine_id  uuid        NOT NULL REFERENCES machines (id),
    work_dir    text        NOT NULL DEFAULT '',
    -- The runtime's own reference, stored as soon as the runtime reports
    -- it. Empty until then, which is also how the hub knows the next turn
    -- is the session's first.
    session_ref text        NOT NULL DEFAULT '',
    -- How far the session has read, as messages.seq: a brief tells it only
    -- what came after. room_seen is where the room stood when its last
    -- brief was put together; thread_seen says the same per topic it was
    -- briefed in, {"<thread id>": seq}, and a topic missing from it is
    -- shown in full. Both start at nothing, which is why a new session's
    -- first brief is the whole story with no code of its own.
    room_seen   bigint      NOT NULL DEFAULT 0,
    thread_seen jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Times the runtime compacted the session. A compaction forgets
    -- detail, so it also empties thread_seen: topics are shown in full
    -- again the next time the session is briefed in them.
    compactions integer     NOT NULL DEFAULT 0,
    -- When the session was last shown the project wiki's catalog: the next
    -- brief lists only the pages changed since. NULL until the first.
    wiki_seen   timestamptz,
    -- What the session was last shown of the parts of a brief that change
    -- now and then (design.md 5.23.1), {"<part>": "<digest>"}: a brief
    -- leaves out a part the session saw as it is. Emptied by a compaction,
    -- like thread_seen, so the next brief shows them all.
    brief_seen  jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- A digest of the role card the session started with, for a runtime
    -- that fixes its system prompt when a session starts and ignores what a
    -- resume passes (Codex; design.md 5.6): a turn whose agent's role card
    -- differs starts a new session, the only way the runtime is told it.
    -- Set with session_ref; empty for a runtime that takes its system
    -- prompt with every run, and until the runtime reports the session.
    role_card_digest text   NOT NULL DEFAULT '',
    started_at  timestamptz NOT NULL DEFAULT now(),
    ended_at    timestamptz,
    -- Why it ended; empty while open.
    end_reason  text        NOT NULL DEFAULT '' CHECK (end_reason IN (
        '', 'runtime_changed', 'machine_changed', 'dir_changed', 'role_card_changed',
        'not_found', 'context_overflow', 'resume_failed', 'manual', 'cancelled', 'member_removed')),
    CONSTRAINT member_sessions_ended_has_reason CHECK ((ended_at IS NULL) = (end_reason = ''))
);

CREATE UNIQUE INDEX member_sessions_one_open ON member_sessions (member_id) WHERE ended_at IS NULL;
CREATE INDEX member_sessions_by_member ON member_sessions (member_id, started_at DESC);

-- The member a person chose to keep the project's wiki once its upkeep is
-- on (design.md 5.12, 5.21): it runs turns of its own, in the project's
-- wiki topic. NULL leaves it to the project's leader, as every project
-- starts; taking the member out of the project clears it.
ALTER TABLE projects ADD COLUMN wiki_maintainer_member_id uuid REFERENCES members (id) ON DELETE SET NULL;

-- The member a person made the project's leader (design.md 5.21): it works
-- in the checkout itself, sets the project up for the others' worktrees
-- and keeps the wiki unless someone else was chosen to. NULL leaves it to
-- the first member to have joined; taking the member out of the project
-- clears it.
ALTER TABLE projects ADD COLUMN leader_member_id uuid REFERENCES members (id) ON DELETE SET NULL;

-- The project's leader as it stands: the member a person made it, or,
-- when none was, the first of the current, enabled members to have joined
-- (members.created_at is the clock, so the members a project starts with
-- keep the order they were picked in). NULL when there is none.
-- +goose StatementBegin
CREATE FUNCTION project_leader_id(project uuid) RETURNS uuid
LANGUAGE sql STABLE AS $$
    SELECT m.id
    FROM members m
    JOIN rooms r ON r.id = m.room_id
    JOIN projects p ON p.id = r.project_id
    WHERE p.id = project
      AND m.removed_at IS NULL
      AND CASE WHEN p.leader_member_id IS NULL THEN m.enabled ELSE m.id = p.leader_member_id END
    ORDER BY m.created_at, m.id
    LIMIT 1
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION project_leader_id(uuid);
ALTER TABLE projects DROP COLUMN leader_member_id;
ALTER TABLE projects DROP COLUMN wiki_maintainer_member_id;
DROP TABLE member_sessions;
DROP TABLE members;
DROP TABLE agents;
