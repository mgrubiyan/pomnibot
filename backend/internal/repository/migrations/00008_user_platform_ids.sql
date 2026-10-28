-- +goose Up
CREATE SEQUENCE users_internal_id_seq OWNED BY users.id;
SELECT setval('users_internal_id_seq', GREATEST(COALESCE((SELECT MAX(id) FROM users), 0), 100001));
ALTER TABLE users
    ALTER COLUMN id SET DEFAULT nextval('users_internal_id_seq'),
    ADD COLUMN max_id BIGINT UNIQUE;

-- Existing users.id values are MAX IDs. Keep them and all learning-data links.
-- IDs 0, 1..30 and 100001 belong to the shipped demonstrations.
UPDATE users SET max_id = id WHERE id > 30 AND id <> 100001;

-- +goose Down
ALTER TABLE users
    ALTER COLUMN id DROP DEFAULT,
    DROP COLUMN max_id;
DROP SEQUENCE users_internal_id_seq;
