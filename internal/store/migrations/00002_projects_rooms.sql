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
    -- The folder the project's wiki lives in, under the hub's state dir
    -- (wiki/projects/<wiki_slug>): lowercase words from the name when it
    -- has any. Set once when the project is created, so the folder never
    -- moves (docs/design.md 5.9).
    wiki_slug  text        NOT NULL UNIQUE CHECK (wiki_slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    -- When the wiki maintainer, once a person has chosen one (see 00004),
    -- goes over what the chat did (design.md 5.12, 5.16): once a topic has
    -- been quiet a while, once a day, every three days, once a week, or
    -- only when a person asks.
    wiki_maintainer_trigger text NOT NULL DEFAULT 'daily'
        CHECK (wiki_maintainer_trigger IN ('idle', 'daily', 'every_3_days', 'weekly', 'manual')),
    -- When a person said no to a wiki maintainer, offered in the chat to a
    -- project that has none (design.md 5.16): it is not offered again.
    wiki_offer_declined_at timestamptz,
    -- How far the wiki maintainer has gone over what people said in the
    -- chat, as messages.seq (design.md 5.16): what they said after it is
    -- news to the next upkeep. It only moves forward.
    wiki_seen_seq bigint NOT NULL DEFAULT 0,
    -- OKF bundles from elsewhere the project's wiki mounts, read-only, by
    -- their folders on the hub's machine (design.md 5.9): searched and read
    -- with the wiki, never written.
    wiki_external_bundles text[] NOT NULL DEFAULT '{}',
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
