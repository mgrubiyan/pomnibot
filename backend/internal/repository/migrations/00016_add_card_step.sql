-- +goose Up
ALTER TABLE cards ADD COLUMN step INT NOT NULL DEFAULT 0;

-- Backfill step for existing cards according to their creation order (id ASC)
WITH ranked AS (
    SELECT id, ROW_NUMBER() OVER (PARTITION BY fact_id ORDER BY id ASC) - 1 AS new_step
    FROM cards
)
UPDATE cards c
SET step = r.new_step
FROM ranked r
WHERE c.id = r.id;

-- +goose Down
ALTER TABLE cards DROP COLUMN step;
