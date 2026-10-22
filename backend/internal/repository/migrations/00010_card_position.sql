-- +goose Up
ALTER TABLE cards ADD COLUMN position INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE cards DROP COLUMN position;
