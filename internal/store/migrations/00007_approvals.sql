-- +goose Up

-- An approval is a request a runtime raises during a turn for a person to
-- answer: "may I run this command?", a question, a form to fill, a link to
-- open. The row is the durable to-do item; the hub forwards the answer to
-- the machine that is waiting for it. A decision the runtime made on its
-- own, such as Codex's automatic review, is recorded the same way, already
-- decided, so people see it (docs/design.md 4.6).
CREATE TABLE approvals (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    turn_id           uuid        NOT NULL REFERENCES turns (id) ON DELETE CASCADE,
    -- Copied from the turn so the pending list of a room and the link back
    -- to the thread need no join.
    room_id           uuid        NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    thread_id         uuid        NOT NULL REFERENCES threads (id) ON DELETE CASCADE,
    member_id         uuid        NOT NULL REFERENCES members (id),
    -- The runtime's own id for the request, unique within a turn; the
    -- decision is routed back with it.
    request_id        text        NOT NULL,
    kind              text        NOT NULL DEFAULT 'tool_use'
                      CHECK (kind IN ('tool_use', 'question', 'form', 'link')),
    -- {"tool": ..., "input": {...}}. For tool_use the tool and its full
    -- input, exactly what the person is asked to approve; for the other
    -- kinds what is asked: the questions, the form's fields, the link.
    -- json, not jsonb: the text is kept as the runtime sent it, keys in
    -- their order, since jsonb would reorder them and a form's fields are
    -- shown in the order the server listed them.
    payload           json        NOT NULL,
    -- What an allow can take in besides, for the rest of the turn, as the
    -- runtime offered: rules such as Bash(go test *), a permission mode,
    -- folders, or the same request again ({"rules": [...], "mode": ...,
    -- "dirs": [...], "same": true}). NULL when it offered nothing.
    similar_offer     jsonb,
    -- How far a person's allow went (docs/design.md 4.6): this request
    -- only; the like of it for the rest of the turn, as offered; the like
    -- of it from now on, kept for the member in member_rules; or every
    -- request the turn makes from here on.
    scope             text        NOT NULL DEFAULT 'once'
                      CHECK (scope IN ('once', 'similar', 'always', 'turn')),
    status            text        NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'allowed', 'denied', 'expired', 'cancelled')),
    -- The decider's note. On a denial it is shown to the agent.
    message           text        NOT NULL DEFAULT '',
    -- The thread post that presents the request to people.
    message_id        uuid        REFERENCES messages (id) ON DELETE SET NULL,
    -- NULL when the decision was not a person's: a timeout, the turn
    -- ending first, or a reviewer of the runtime's own.
    decided_by        uuid        REFERENCES users (id),
    -- Empty when a person or the hub decided. A runtime that decided on its
    -- own names its reviewer here, such as codex_auto_review; such a row
    -- is recorded already decided and nothing waits for it. The hub names
    -- itself the same way when it answered for a person: rule when the
    -- member may always make the request (member_rules), turn when a
    -- person let the rest of the turn through (decided_by is who).
    reviewer          text        NOT NULL DEFAULT '',
    -- What came with the decision: the answers to a question, the content
    -- of a form, or a reviewer's findings such as the risk it saw.
    answer            jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    decided_at        timestamptz,
    UNIQUE (turn_id, request_id)
);

-- The kinds of request a member may always make without a person being
-- asked (docs/design.md 4.6): a person allowed one, and the like of it from
-- now on. Each runtime has its own syntax: Claude Code's permission rules,
-- such as Bash(go test *), handed to it with --allowedTools; for Codex the
-- leading words of a command as a JSON array, such as ["go","test"], which
-- the hub matches itself.
CREATE TABLE member_rules (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    member_id   uuid        NOT NULL REFERENCES members (id) ON DELETE CASCADE,
    runtime     text        NOT NULL,
    rule        text        NOT NULL,
    -- The approval it was allowed with, and who allowed it: the account
    -- lives in the state directory, not the users table (migration 00011).
    approval_id uuid        REFERENCES approvals (id) ON DELETE SET NULL,
    created_by  uuid,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (member_id, runtime, rule)
);

-- What a room's UI shows as waiting for someone.
CREATE INDEX approvals_pending_by_room ON approvals (room_id, created_at) WHERE status = 'pending';
CREATE INDEX approvals_by_turn ON approvals (turn_id, created_at);

-- The turns a project's wiki maintainer has gone over (design.md 5.12):
-- its own project's, and other projects' that used the skills its team
-- owns. A turn is gone over once per project, whichever maintainer turn
-- did it; what is not here is what the next one looks at.
CREATE TABLE wiki_reviews (
    project_id     uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    turn_id        uuid        NOT NULL REFERENCES turns (id) ON DELETE CASCADE,
    -- The maintainer's turn that went over it.
    upkeep_turn_id uuid        REFERENCES turns (id) ON DELETE SET NULL,
    reviewed_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, turn_id)
);

-- What the chat's turns looked up in the wikis (design.md 5.23.7): each
-- search, with how many pages it found as written, and each page read.
-- An upkeep shows its maintainer the searches that found nothing, with
-- what their turns read next, and the pages no turn reads. Only chat
-- turns' are kept, for 90 days.
CREATE TABLE wiki_lookups (
    id         bigserial   PRIMARY KEY,
    project_id uuid        NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    turn_id    uuid        NOT NULL REFERENCES turns (id) ON DELETE CASCADE,
    scope      text        NOT NULL CHECK (scope IN ('project', 'library')),
    -- A search's words, as the agent gave them, and how many pages matched
    -- them as written; or the page a read read, as the wiki names it.
    query      text        NOT NULL DEFAULT '',
    hits       integer     NOT NULL DEFAULT 0 CHECK (hits >= 0),
    path       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT wiki_lookups_search_or_read CHECK ((query = '') <> (path = ''))
);

-- An upkeep reads the lookups of the turns it goes over, and the pages of
-- its project read lately; the hub drops the old ones of every project.
CREATE INDEX wiki_lookups_by_turn ON wiki_lookups (turn_id);
CREATE INDEX wiki_lookups_by_project ON wiki_lookups (project_id, created_at);
CREATE INDEX wiki_lookups_by_time ON wiki_lookups (created_at);

-- A skill of the library on trial (design.md 5.15). An agent it is
-- installed for changed it in a turn, and every agent it is installed for
-- uses the new version at once. It is kept once enough turns have used it
-- since and ended well, or when a person confirms it; a person or the
-- maintainer of the team that owns it can roll it back to the version
-- before. How it is going is counted from the turns that used it since the
-- last change (turns.skills_used), so a change during the trial starts the
-- count again, from the same version to go back to.
CREATE TABLE skill_trials (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    skill        text        NOT NULL CHECK (skill ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    -- The library's commit before the first change: what a rollback goes
    -- back to.
    base_sha     text        NOT NULL CHECK (btrim(base_sha) <> ''),
    started_at   timestamptz NOT NULL DEFAULT now(),
    -- The last change: when, in which turn, by whom, of which project, by
    -- their names then; and how many changes the trial has had.
    changed_at   timestamptz NOT NULL DEFAULT now(),
    turn_id      uuid        REFERENCES turns (id) ON DELETE SET NULL,
    changed_by   text        NOT NULL DEFAULT '',
    project_name text        NOT NULL DEFAULT '',
    changes      integer     NOT NULL DEFAULT 1 CHECK (changes > 0),
    status       text        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'kept', 'rolled_back')),
    -- Who ended it, an OKF actor: human:alice, process:skill-trial, or the
    -- maintainer's runtime and model; and why, when they said.
    ended_by     text        NOT NULL DEFAULT '',
    ended_at     timestamptz,
    reason       text        NOT NULL DEFAULT '',
    CONSTRAINT skill_trials_ended CHECK ((status = 'open') = (ended_at IS NULL))
);

-- One trial of a skill is open at a time.
CREATE UNIQUE INDEX skill_trials_one_open ON skill_trials (skill) WHERE status = 'open';
CREATE INDEX skill_trials_by_skill ON skill_trials (skill, started_at DESC);

-- +goose Down
DROP TABLE skill_trials;
DROP TABLE wiki_lookups;
DROP TABLE wiki_reviews;
DROP TABLE member_rules;
DROP TABLE approvals;
