-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied

ALTER TABLE users DROP COLUMN IF EXISTS name;
ALTER TABLE users ADD COLUMN first_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN last_name TEXT;
ALTER TABLE users ADD COLUMN username TEXT;
ALTER TABLE users ADD COLUMN is_bot BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
-- SQL in section 'Down' is executed when this migration is rollback

ALTER TABLE users DROP COLUMN IF EXISTS is_bot;
ALTER TABLE users DROP COLUMN IF EXISTS username;
ALTER TABLE users DROP COLUMN IF EXISTS last_name;
ALTER TABLE users DROP COLUMN IF EXISTS first_name;
ALTER TABLE users ADD COLUMN name TEXT NOT NULL DEFAULT '';
