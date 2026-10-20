-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied
DROP TABLE IF EXISTS card_issues;
DROP TYPE IF EXISTS card_issue_reason;

-- +goose Down
-- SQL in section 'Down' is executed when this migration is rolled back
CREATE TYPE card_issue_reason AS ENUM (
    'answer',
    'wording',
    'not-in-notes',
    'other'
);

CREATE TABLE card_issues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason card_issue_reason NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_card_issues_card_id ON card_issues(card_id);
