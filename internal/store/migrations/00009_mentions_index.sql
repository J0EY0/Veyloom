-- +goose Up

-- The inbox lists every message that mentions a user, across rooms. The
-- containment query needs an index on the mentions array.
CREATE INDEX messages_mentions ON messages USING gin (mentions jsonb_path_ops);

-- +goose Down
DROP INDEX messages_mentions;
