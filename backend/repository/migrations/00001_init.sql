-- +goose Up
-- +goose StatementBegin
CREATE TYPE card_kind AS ENUM (
    'choice',
    'flip',
    'input',
    'boolean',
    'table'
);

CREATE TYPE card_issue_reason AS ENUM (
    'answer',
    'wording',
    'not-in-notes',
    'other'
);

CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    username TEXT,
    first_name TEXT,
    last_name TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE card_sets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    share_code VARCHAR(6) NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_card_sets (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    set_id UUID NOT NULL REFERENCES card_sets(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, set_id)
);

CREATE TABLE cards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    set_id UUID NOT NULL REFERENCES card_sets(id) ON DELETE CASCADE,
    kind card_kind NOT NULL,
    question TEXT NOT NULL,
    answer_choice_index INT,
    answer_text TEXT,
    answer_boolean BOOLEAN,
    explanation TEXT NOT NULL,
    source_quote TEXT NOT NULL,
    source_ref TEXT,
    topic TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE card_options (
    id BIGSERIAL PRIMARY KEY,
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    option_index INT NOT NULL,
    text TEXT NOT NULL,
    CONSTRAINT uq_card_options_card_index UNIQUE (card_id, option_index)
);

CREATE TABLE card_table_columns (
    id BIGSERIAL PRIMARY KEY,
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    column_index INT NOT NULL,
    title TEXT NOT NULL,
    CONSTRAINT uq_card_table_columns_card_index UNIQUE (card_id, column_index)
);

CREATE TABLE card_table_items (
    id BIGSERIAL PRIMARY KEY,
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    column_id BIGINT NOT NULL REFERENCES card_table_columns(id) ON DELETE CASCADE,
    text TEXT NOT NULL
);

CREATE TABLE card_issues (
    id BIGSERIAL PRIMARY KEY,
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason card_issue_reason NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE card_reviews (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    correct BOOLEAN NOT NULL,
    answered_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_card_progress (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    next_review_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_reviewed_at TIMESTAMPTZ,
    repetitions INT NOT NULL DEFAULT 0,
    interval_days INT NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, card_id)
);

CREATE INDEX idx_card_sets_creator ON card_sets(creator_id);
CREATE INDEX idx_card_sets_share_code ON card_sets(share_code);
CREATE INDEX idx_user_card_sets_user ON user_card_sets(user_id);
CREATE INDEX idx_cards_set_id ON cards(set_id);
CREATE INDEX idx_cards_topic ON cards(topic);
CREATE INDEX idx_card_options_card_id ON card_options(card_id);
CREATE INDEX idx_card_table_columns_card_id ON card_table_columns(card_id);
CREATE INDEX idx_card_table_items_card_id ON card_table_items(card_id);
CREATE INDEX idx_card_table_items_column_id ON card_table_items(column_id);
CREATE INDEX idx_card_issues_card_id ON card_issues(card_id);
CREATE INDEX idx_card_reviews_user_card ON card_reviews(user_id, card_id, answered_at DESC);
CREATE INDEX idx_user_card_progress_next_review ON user_card_progress(user_id, next_review_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_card_progress;
DROP TABLE IF EXISTS card_reviews;
DROP TABLE IF EXISTS card_issues;
DROP TABLE IF EXISTS card_table_items;
DROP TABLE IF EXISTS card_table_columns;
DROP TABLE IF EXISTS card_options;
DROP TABLE IF EXISTS cards;
DROP TABLE IF EXISTS user_card_sets;
DROP TABLE IF EXISTS card_sets;
DROP TABLE IF EXISTS users;
DROP TYPE IF EXISTS card_issue_reason;
DROP TYPE IF EXISTS card_kind;
-- +goose StatementEnd
