-- +goose Up

-- A project is a codebase the team and its agents work on together.
CREATE TABLE projects (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL CHECK (btrim(name) <> ''),
    -- Where the code is checked out on the machines its members run on.
    -- The members a project starts with work there; empty leaves them to
    -- wherever their machine runs.
    repo_path  text        NOT NULL DEFAULT '',
    -- What the project is, in a paragraph: what it is for, its goals, its
    -- stack. A person writes it once and every agent's brief opens with it.
    description text       NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);

-- A room is a group chat. Every project gets exactly one 'main' room when it
-- is created; further 'topic' rooms can be added for focused work. Agents
-- are attached to rooms, not projects.
CREATE TABLE rooms (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    name       text        NOT NULL CHECK (btrim(name) <> ''),
    kind       text        NOT NULL CHECK (kind IN ('main', 'topic')),
    -- The number given to the room's newest topic; the next one gets the
    -- one after. Kept here so that handing out a number locks one row.
    last_thread_number integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX rooms_one_main_per_project ON rooms (project_id) WHERE kind = 'main';
CREATE INDEX rooms_by_project ON rooms (project_id, created_at);

-- +goose Down
DROP TABLE rooms;
DROP TABLE projects;
