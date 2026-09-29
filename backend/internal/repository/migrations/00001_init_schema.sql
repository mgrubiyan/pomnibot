-- +goose Up
-- SQL in section 'Up' is executed when this migration is applied

CREATE TYPE card_issue_reason AS ENUM (
    'answer',
    'wording',
    'not-in-notes',
    'other'
);

CREATE TABLE users (
    id BIGINT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE sets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    author_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    share_code VARCHAR(6) UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE user_sets (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    set_id UUID NOT NULL REFERENCES sets(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, set_id)
);

CREATE TABLE topics (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE facts (
    id TEXT PRIMARY KEY,
    set_id UUID NOT NULL REFERENCES sets(id) ON DELETE CASCADE,
    topic_id UUID REFERENCES topics(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE card_kinds (
    kind VARCHAR(20) PRIMARY KEY,
    difficulty_level SMALLINT UNIQUE NOT NULL
);

INSERT INTO card_kinds (kind, difficulty_level) VALUES
    ('flip', 0),
    ('boolean', 1),
    ('choice', 2),
    ('table', 3),
    ('input', 4);

CREATE TABLE cards (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    fact_id TEXT NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
    kind VARCHAR(20) NOT NULL REFERENCES card_kinds(kind),
    question TEXT NOT NULL,
    answer_text TEXT,
    explanation TEXT NOT NULL,
    source_quote TEXT NOT NULL,
    source_ref TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE card_options (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    position INT NOT NULL,
    text TEXT NOT NULL,
    is_correct BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (card_id, position)
);

CREATE TABLE card_table_columns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    position INT NOT NULL,
    name TEXT NOT NULL,
    UNIQUE (card_id, position)
);

CREATE TABLE card_table_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    item_text TEXT NOT NULL,
    column_name TEXT NOT NULL
);

CREATE TABLE user_fact_progress (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    fact_id TEXT NOT NULL REFERENCES facts(id) ON DELETE CASCADE,
    elo INT NOT NULL DEFAULT 700,
    ceiling_found BOOLEAN NOT NULL DEFAULT FALSE,
    consecutive_correct INT NOT NULL DEFAULT 0,
    consecutive_incorrect INT NOT NULL DEFAULT 0,
    interval_days INT NOT NULL DEFAULT 0,
    ease_factor NUMERIC(4,2) NOT NULL DEFAULT 2.50,
    next_review_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_reviewed_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, fact_id)
);

CREATE TABLE answer_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    correct BOOLEAN NOT NULL,
    answered_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE card_issues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason card_issue_reason NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sets_share_code ON sets(share_code);
CREATE INDEX idx_sets_author_id ON sets(author_id);
CREATE INDEX idx_facts_set_id ON facts(set_id);
CREATE INDEX idx_facts_topic_id ON facts(topic_id);
CREATE INDEX idx_cards_fact_id ON cards(fact_id);
CREATE INDEX idx_cards_kind ON cards(kind);
CREATE INDEX idx_card_options_card_id ON card_options(card_id);
CREATE INDEX idx_card_table_columns_card_id ON card_table_columns(card_id);
CREATE INDEX idx_card_table_items_card_id ON card_table_items(card_id);
CREATE INDEX idx_user_fact_progress_due ON user_fact_progress(user_id, next_review_at);
CREATE INDEX idx_answer_results_user ON answer_results(user_id, answered_at);
CREATE INDEX idx_card_issues_card_id ON card_issues(card_id);

-- +goose Down
-- SQL in section 'Down' is executed when this migration is rolled back

DROP TABLE IF EXISTS card_issues;
DROP TABLE IF EXISTS answer_results;
DROP TABLE IF EXISTS user_fact_progress;
DROP TABLE IF EXISTS card_table_items;
DROP TABLE IF EXISTS card_table_columns;
DROP TABLE IF EXISTS card_options;
DROP TABLE IF EXISTS cards;
DROP TABLE IF EXISTS card_kinds;
DROP TABLE IF EXISTS facts;
DROP TABLE IF EXISTS topics;
DROP TABLE IF EXISTS user_sets;
DROP TABLE IF EXISTS sets;
DROP TABLE IF EXISTS users;
DROP TYPE IF EXISTS card_issue_reason;
