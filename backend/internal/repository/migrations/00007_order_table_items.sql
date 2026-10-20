-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
-- The feed shows the terms of a table card in this order, and the generator
-- mixes them so that it does not give the columns away; without a position
-- Postgres may return them in any order, grouped by column too.
ALTER TABLE card_table_items ADD COLUMN position INT NOT NULL DEFAULT 0;

-- +goose Down
-- SQL in section 'Down' is executed when this migration is rolled back
ALTER TABLE card_table_items DROP COLUMN position;
