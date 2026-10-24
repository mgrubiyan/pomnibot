-- +goose Up
-- Migration 00011: Drop topics table and facts.topic_id column

ALTER TABLE facts DROP CONSTRAINT IF EXISTS facts_topic_id_fkey;

DROP INDEX IF EXISTS idx_facts_topic_id;

ALTER TABLE facts DROP COLUMN IF EXISTS topic_id;

DROP TABLE IF EXISTS topics;

-- +goose Down
CREATE TABLE IF NOT EXISTS topics (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    name TEXT UNIQUE NOT NULL
);

ALTER TABLE facts ADD COLUMN IF NOT EXISTS topic_id UUID;

ALTER TABLE facts
    ADD CONSTRAINT facts_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES topics(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_facts_topic_id ON facts(topic_id);
