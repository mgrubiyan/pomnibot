-- +goose Up
ALTER TABLE answers ADD COLUMN attempt_id UUID;
CREATE UNIQUE INDEX idx_answers_attempt ON answers(user_id, card_id, attempt_id);

-- +goose Down
DROP INDEX idx_answers_attempt;
ALTER TABLE answers DROP COLUMN attempt_id;
