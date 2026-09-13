-- +goose Up

-- A worker is a machine that runs agent CLIs on behalf of the hub. The row
-- is the durable registration; whether the worker is currently connected is
-- tracked by the hub in memory and reflected here through the timestamps.
--
-- Identity is the id: the hub assigns it on first contact, the worker stores
-- it locally and presents it when reconnecting. The name is only a display
-- label, so two machines may share one.
CREATE TABLE workers (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    name            text        NOT NULL,
    -- Result of the worker's latest engine discovery, as []engine.Info.
    engines         jsonb       NOT NULL DEFAULT '[]'::jsonb,
    connected_at    timestamptz NOT NULL DEFAULT now(),
    last_seen_at    timestamptz NOT NULL DEFAULT now(),
    -- NULL while connected; set when the connection ends.
    disconnected_at timestamptz
);

-- +goose Down
DROP TABLE workers;
