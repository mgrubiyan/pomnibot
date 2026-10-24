-- +goose Up
-- Migration 00010: Rename answer_results to answers and drop redundant answered_at column.
-- UUIDv7 ID embeds the timestamp of the answer.

ALTER TABLE answer_results RENAME TO answers;

ALTER TABLE answers DROP COLUMN answered_at;

ALTER TABLE answers RENAME CONSTRAINT answer_results_pkey TO answers_pkey;
ALTER TABLE answers RENAME CONSTRAINT answer_results_user_id_fkey TO answers_user_id_fkey;
ALTER TABLE answers RENAME CONSTRAINT answer_results_card_id_fkey TO answers_card_id_fkey;

DROP INDEX IF EXISTS idx_answer_results_user;
CREATE INDEX idx_answers_user ON answers(user_id, id);

-- +goose Down
DROP INDEX IF EXISTS idx_answers_user;

ALTER TABLE answers ADD COLUMN answered_at TIMESTAMPTZ;
UPDATE answers SET answered_at = COALESCE(uuid_extract_timestamp(id), CURRENT_TIMESTAMP);
ALTER TABLE answers ALTER COLUMN answered_at SET NOT NULL;

ALTER TABLE answers RENAME CONSTRAINT answers_pkey TO answer_results_pkey;
ALTER TABLE answers RENAME CONSTRAINT answers_user_id_fkey TO answer_results_user_id_fkey;
ALTER TABLE answers RENAME CONSTRAINT answers_card_id_fkey TO answer_results_card_id_fkey;

ALTER TABLE answers RENAME TO answer_results;

CREATE INDEX idx_answer_results_user ON answer_results(user_id, answered_at);
