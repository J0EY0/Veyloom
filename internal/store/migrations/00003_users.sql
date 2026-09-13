-- +goose Up

-- A user is a human member of the team. External identities (Slack, Feishu)
-- attach here later.
CREATE TABLE users (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text        NOT NULL CHECK (btrim(name) <> ''),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE users;
