-- +goose Up

-- A project is a codebase the team and its agents work on together.
CREATE TABLE projects (
    id             uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name           text        NOT NULL CHECK (btrim(name) <> ''),
    -- Where the code lives: a git URL or a local path. May be filled in
    -- later, so empty is allowed.
    repo_url       text        NOT NULL DEFAULT '',
    default_branch text        NOT NULL DEFAULT 'main',
    created_at     timestamptz NOT NULL DEFAULT now()
);

-- A room is a group chat. Every project gets exactly one 'main' room when it
-- is created; further 'topic' rooms can be added for focused work. Agents
-- are attached to rooms, not projects.
CREATE TABLE rooms (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       text        NOT NULL CHECK (btrim(name) <> ''),
    kind       text        NOT NULL CHECK (kind IN ('main', 'topic')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX rooms_one_main_per_project ON rooms (project_id) WHERE kind = 'main';
CREATE INDEX rooms_by_project ON rooms (project_id, created_at);

-- +goose Down
DROP TABLE rooms;
DROP TABLE projects;
