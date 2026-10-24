-- +goose Up
ALTER TABLE sets ADD COLUMN warnings TEXT[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE sets DROP COLUMN IF EXISTS warnings;
