-- +goose Up

-- An agent's first reply to a top-level @-mention becomes the root of the
-- topic (thread) the rest of its turn lives in. The root is written before
-- the agent has said anything, so an agent message may be empty for a
-- moment; people and the system still have to say something.
ALTER TABLE messages DROP CONSTRAINT messages_body_check;
ALTER TABLE messages ADD CONSTRAINT messages_body_check
    CHECK (sender_kind = 'agent' OR btrim(body) <> '');

-- The turn a hub-written message belongs to, so a topic can be read one
-- turn at a time. NULL for messages people write.
ALTER TABLE messages ADD COLUMN turn_id uuid REFERENCES turns (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE messages DROP COLUMN turn_id;
ALTER TABLE messages DROP CONSTRAINT messages_body_check;
ALTER TABLE messages ADD CONSTRAINT messages_body_check CHECK (btrim(body) <> '');
