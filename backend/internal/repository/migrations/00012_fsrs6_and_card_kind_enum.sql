-- +goose Up
-- Migration 00012: card_kind enum and FSRS user_fact_progress schema.

CREATE TYPE card_kind AS ENUM ('flip', 'boolean', 'choice', 'table', 'input');

ALTER TABLE cards DROP CONSTRAINT IF EXISTS cards_kind_fkey;
ALTER TABLE cards ALTER COLUMN kind TYPE card_kind USING kind::card_kind;
DROP TABLE IF EXISTS card_kinds;

ALTER TABLE user_fact_progress
    ADD COLUMN step INT NOT NULL DEFAULT 0,
    ADD COLUMN stability DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    ADD COLUMN difficulty DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    ADD COLUMN reps INT NOT NULL DEFAULT 0,
    ADD COLUMN lapses INT NOT NULL DEFAULT 0,
    ADD COLUMN state SMALLINT NOT NULL DEFAULT 0;

-- Migrate existing legacy rows: step is mapped from elo
UPDATE user_fact_progress ufp
SET
    step = LEAST(
        GREATEST(1, COALESCE((SELECT COUNT(*)::INT FROM cards c WHERE c.fact_id = ufp.fact_id), 1)),
        CASE
            WHEN ufp.elo < 800 THEN 0
            WHEN ufp.elo < 1000 THEN 1
            WHEN ufp.elo < 1200 THEN 2
            WHEN ufp.elo < 1400 THEN 3
            ELSE 4
        END
    );

ALTER TABLE user_fact_progress
    DROP COLUMN elo,
    DROP COLUMN ceiling_found,
    DROP COLUMN consecutive_correct,
    DROP COLUMN consecutive_incorrect,
    DROP COLUMN interval_days,
    DROP COLUMN ease_factor;

-- +goose Down
ALTER TABLE user_fact_progress
    ADD COLUMN elo INT NOT NULL DEFAULT 700,
    ADD COLUMN ceiling_found BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN consecutive_correct INT NOT NULL DEFAULT 0,
    ADD COLUMN consecutive_incorrect INT NOT NULL DEFAULT 0,
    ADD COLUMN interval_days INT NOT NULL DEFAULT 0,
    ADD COLUMN ease_factor NUMERIC(4,2) NOT NULL DEFAULT 2.50;

ALTER TABLE user_fact_progress
    DROP COLUMN step,
    DROP COLUMN stability,
    DROP COLUMN difficulty,
    DROP COLUMN reps,
    DROP COLUMN lapses,
    DROP COLUMN state;

CREATE TABLE IF NOT EXISTS card_kinds (
    kind VARCHAR(20) PRIMARY KEY,
    difficulty_level SMALLINT UNIQUE NOT NULL
);

INSERT INTO card_kinds (kind, difficulty_level) VALUES
    ('flip', 0),
    ('boolean', 1),
    ('choice', 2),
    ('table', 3),
    ('input', 4)
ON CONFLICT (kind) DO NOTHING;

ALTER TABLE cards ALTER COLUMN kind TYPE VARCHAR(20) USING kind::text;
ALTER TABLE cards ADD CONSTRAINT cards_kind_fkey FOREIGN KEY (kind) REFERENCES card_kinds(kind);
DROP TYPE IF EXISTS card_kind;
