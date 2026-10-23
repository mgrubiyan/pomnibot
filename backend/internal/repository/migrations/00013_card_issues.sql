-- +goose Up
-- Complaints keep a copy of the card as the reader saw it: the card may be
-- edited or deleted later, and the complaint must still say what was wrong.
CREATE TYPE card_issue_reason AS ENUM ('answer', 'wording', 'not-in-notes', 'other');

CREATE TABLE card_issues (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    card_id UUID REFERENCES cards(id) ON DELETE SET NULL,
    set_id UUID REFERENCES sets(id) ON DELETE SET NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason card_issue_reason NOT NULL,
    kind card_kind NOT NULL,
    question TEXT NOT NULL,
    answer_text TEXT,
    explanation TEXT NOT NULL,
    source_quote TEXT NOT NULL,
    -- Set when the author keeps the card as it is; an edit of the card
    -- answers the complaints made before it without touching them.
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_card_issues_card_id ON card_issues(card_id);

-- +goose Down
DROP TABLE IF EXISTS card_issues;
DROP TYPE IF EXISTS card_issue_reason;
