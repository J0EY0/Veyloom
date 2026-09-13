-- +goose Up

-- An agent template is a reusable role definition: which engine and model
-- to run, how to behave (the role card) and what it may do (the permission
-- preset). Templates are placed into rooms as agent instances.
CREATE TABLE agent_templates (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name              text        NOT NULL UNIQUE CHECK (btrim(name) <> ''),
    -- Engine identifier as reported by discovery: claude, codex, pi.
    engine            text        NOT NULL CHECK (btrim(engine) <> ''),
    -- Model for the engine; empty means the engine's own default.
    model             text        NOT NULL DEFAULT '',
    -- Instructions given to every turn: who the agent is and how it should
    -- behave in the room.
    role_card         text        NOT NULL DEFAULT '',
    permission_preset text        NOT NULL CHECK (permission_preset IN ('read_only', 'edit_with_approval', 'full_auto')),
    -- Engine-specific settings the worker passes through untouched.
    engine_options    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    -- Shipped with veyloom and re-created by name if missing. Users may
    -- edit them.
    builtin           boolean     NOT NULL DEFAULT false,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- An agent instance is a template placed in a room: the member people @ in
-- chat. It runs on one worker, works in one checkout and keeps its own
-- engine session between turns.
CREATE TABLE agent_instances (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id            uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    template_id        uuid        NOT NULL REFERENCES agent_templates (id),
    worker_id          uuid        NOT NULL REFERENCES workers (id),
    -- Shown in chat and used for @-mentions; unique within the room.
    display_name       text        NOT NULL CHECK (btrim(display_name) <> ''),
    -- Path of the project checkout on the worker's machine.
    repo_path          text        NOT NULL DEFAULT '',
    -- worktree: the instance gets its own git worktree per room;
    -- shared: it works in the checkout directly.
    branch_mode        text        NOT NULL DEFAULT 'worktree' CHECK (branch_mode IN ('worktree', 'shared')),
    -- Per-instance overrides; empty means "use the template's".
    model              text        NOT NULL DEFAULT '',
    permission_preset  text        NOT NULL DEFAULT '' CHECK (permission_preset IN ('', 'read_only', 'edit_with_approval', 'full_auto')),
    -- Opaque engine session reference, e.g. a Claude Code session id, so
    -- the next turn resumes the same conversation.
    engine_session_ref text        NOT NULL DEFAULT '',
    -- Disabled instances stay in the room for history but take no turns.
    enabled            boolean     NOT NULL DEFAULT true,
    created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX agent_instances_name_per_room ON agent_instances (room_id, display_name);

-- +goose Down
DROP TABLE agent_instances;
DROP TABLE agent_templates;
