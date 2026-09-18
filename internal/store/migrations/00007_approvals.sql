-- +goose Up

-- An approval is a permission request raised by a runtime during a turn:
-- "may I run this command?". The row is the durable to-do item; the hub
-- forwards the decision to the machine that is waiting for it.
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
    kind              text        NOT NULL DEFAULT 'tool_use' CHECK (kind IN ('tool_use')),
    -- For tool_use: {"tool": "Bash", "input": {...}} with the full input,
    -- exactly what the person is asked to approve.
    payload           jsonb       NOT NULL,
    status            text        NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'allowed', 'denied', 'expired', 'cancelled')),
    -- The decider's note. On a denial it is shown to the agent.
    message           text        NOT NULL DEFAULT '',
    -- The thread post that presents the request to people.
    message_id        uuid        REFERENCES messages (id) ON DELETE SET NULL,
    -- NULL when the decision was not a person's: a timeout or the turn
    -- ending first.
    decided_by        uuid        REFERENCES users (id),
    created_at        timestamptz NOT NULL DEFAULT now(),
    decided_at        timestamptz,
    UNIQUE (turn_id, request_id)
);

-- What a room's UI shows as waiting for someone.
CREATE INDEX approvals_pending_by_room ON approvals (room_id, created_at) WHERE status = 'pending';
CREATE INDEX approvals_by_turn ON approvals (turn_id, created_at);

-- +goose Down
DROP TABLE approvals;
