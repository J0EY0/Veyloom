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
    -- is recorded already decided and nothing waits for it.
    reviewer          text        NOT NULL DEFAULT '',
    -- What came with the decision: the answers to a question, the content
    -- of a form, or a reviewer's findings such as the risk it saw.
    answer            jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    decided_at        timestamptz,
    UNIQUE (turn_id, request_id)
);

-- What a room's UI shows as waiting for someone.
CREATE INDEX approvals_pending_by_room ON approvals (room_id, created_at) WHERE status = 'pending';
CREATE INDEX approvals_by_turn ON approvals (turn_id, created_at);

-- +goose Down
DROP TABLE approvals;
