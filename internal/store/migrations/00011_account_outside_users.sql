-- +goose Up

-- The one account lives in the state dir (internal/account), not in
-- users, so what it writes cannot point at a users row. users stays for
-- people who arrive through IM later.
ALTER TABLE messages DROP CONSTRAINT messages_user_id_fkey;
ALTER TABLE approvals DROP CONSTRAINT approvals_decided_by_fkey;

-- +goose Down
ALTER TABLE messages ADD CONSTRAINT messages_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id);
ALTER TABLE approvals ADD CONSTRAINT approvals_decided_by_fkey FOREIGN KEY (decided_by) REFERENCES users (id);
