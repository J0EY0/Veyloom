-- +goose Up

-- A machine runs agent CLIs on behalf of the hub. The row
-- is the durable registration; whether the machine is currently connected is
-- tracked by the hub in memory and reflected here through the timestamps.
--
-- Identity is the id: the hub assigns it on first contact, the machine stores
-- it locally and presents it when reconnecting. The name is only a display
-- label, so two machines may share one.
CREATE TABLE machines (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name            text        NOT NULL,
    -- Result of the machine's latest runtime discovery, as []runtime.Info.
    runtimes        jsonb       NOT NULL DEFAULT '[]'::jsonb,
    connected_at    timestamptz NOT NULL DEFAULT now(),
    last_seen_at    timestamptz NOT NULL DEFAULT now(),
    -- NULL while connected; set when the connection ends.
    disconnected_at timestamptz
);

-- +goose Down
DROP TABLE machines;
