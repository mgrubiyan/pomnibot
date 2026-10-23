-- +goose Up
-- Migration 00009: Unify identifiers to UUIDv7 and drop redundant created_at columns.
-- Preserves historical timestamps embedded in UUIDv7 via uuidv7(created_at - clock_timestamp()).

-- 1. Create temporary mapping tables for all tables whose IDs are changing or receiving UUIDv7s
CREATE TEMP TABLE _user_id_map AS
SELECT
    id AS old_id,
    CASE
        WHEN id = 0 THEN '00000000-0000-0000-0000-000000000000'::uuid
        ELSE uuidv7(created_at - clock_timestamp())
    END AS new_id
FROM users;

CREATE TEMP TABLE _set_id_map AS
SELECT
    id AS old_id,
    uuidv7(created_at - clock_timestamp()) AS new_id
FROM sets;

CREATE TEMP TABLE _topic_id_map AS
SELECT
    id AS old_id,
    uuidv7(created_at - clock_timestamp()) AS new_id
FROM topics;

CREATE TEMP TABLE _fact_id_map AS
SELECT
    id AS old_id,
    uuidv7(created_at - clock_timestamp()) AS new_id
FROM facts;

CREATE TEMP TABLE _card_id_map AS
SELECT
    id AS old_id,
    uuidv7(created_at - clock_timestamp()) AS new_id
FROM cards;

CREATE TEMP TABLE _answer_result_id_map AS
SELECT
    id AS old_id,
    uuidv7(created_at - clock_timestamp()) AS new_id
FROM answer_results;

-- 2. Drop all foreign key constraints referencing the old IDs
ALTER TABLE sets DROP CONSTRAINT sets_author_id_fkey;
ALTER TABLE user_sets DROP CONSTRAINT user_sets_user_id_fkey;
ALTER TABLE user_sets DROP CONSTRAINT user_sets_set_id_fkey;
ALTER TABLE user_fact_progress DROP CONSTRAINT user_fact_progress_user_id_fkey;
ALTER TABLE user_fact_progress DROP CONSTRAINT user_fact_progress_fact_id_fkey;
ALTER TABLE facts DROP CONSTRAINT facts_set_id_fkey;
ALTER TABLE facts DROP CONSTRAINT facts_topic_id_fkey;
ALTER TABLE cards DROP CONSTRAINT cards_fact_id_fkey;
ALTER TABLE card_options DROP CONSTRAINT card_options_card_id_fkey;
ALTER TABLE card_table_columns DROP CONSTRAINT card_table_columns_card_id_fkey;
ALTER TABLE card_table_items DROP CONSTRAINT card_table_items_card_id_fkey;
ALTER TABLE answer_results DROP CONSTRAINT answer_results_user_id_fkey;
ALTER TABLE answer_results DROP CONSTRAINT answer_results_card_id_fkey;

-- 3. Drop primary key constraints on tables that need type conversions or column swaps
ALTER TABLE user_sets DROP CONSTRAINT user_sets_pkey;
ALTER TABLE user_fact_progress DROP CONSTRAINT user_fact_progress_pkey;

-- 4. Update USERS table
ALTER TABLE users
    DROP CONSTRAINT users_pkey,
    ADD COLUMN new_id UUID;

UPDATE users u
SET new_id = m.new_id
FROM _user_id_map m
WHERE u.id = m.old_id;

UPDATE users SET new_id = uuidv7() WHERE new_id IS NULL;

-- Remove users_internal_id_seq ownership and sequence if exists
ALTER TABLE users ALTER COLUMN id DROP DEFAULT;
DROP SEQUENCE IF EXISTS users_internal_id_seq CASCADE;

ALTER TABLE users
    DROP COLUMN id,
    DROP COLUMN created_at;

ALTER TABLE users
    RENAME COLUMN new_id TO id;

ALTER TABLE users
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN id SET DEFAULT uuidv7(),
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

-- 5. Update SETS table
ALTER TABLE sets
    DROP CONSTRAINT sets_pkey,
    ADD COLUMN new_id UUID,
    ADD COLUMN new_author_id UUID;

UPDATE sets s
SET new_id = m.new_id,
    new_author_id = u.new_id
FROM _set_id_map m, _user_id_map u
WHERE s.id = m.old_id AND s.author_id = u.old_id;

UPDATE sets SET new_id = uuidv7() WHERE new_id IS NULL;

ALTER TABLE sets
    DROP COLUMN id,
    DROP COLUMN author_id,
    DROP COLUMN created_at;

ALTER TABLE sets
    RENAME COLUMN new_id TO id;
ALTER TABLE sets
    RENAME COLUMN new_author_id TO author_id;

ALTER TABLE sets
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN id SET DEFAULT uuidv7(),
    ALTER COLUMN author_id SET NOT NULL,
    ADD CONSTRAINT sets_pkey PRIMARY KEY (id);

-- 6. Update USER_SETS table
ALTER TABLE user_sets
    ADD COLUMN new_user_id UUID,
    ADD COLUMN new_set_id UUID;

UPDATE user_sets us
SET new_user_id = u.new_id,
    new_set_id = s.new_id
FROM _user_id_map u, _set_id_map s
WHERE us.user_id = u.old_id AND us.set_id = s.old_id;

ALTER TABLE user_sets
    DROP COLUMN user_id,
    DROP COLUMN set_id;

ALTER TABLE user_sets
    RENAME COLUMN new_user_id TO user_id;
ALTER TABLE user_sets
    RENAME COLUMN new_set_id TO set_id;

ALTER TABLE user_sets
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN set_id SET NOT NULL,
    ADD CONSTRAINT user_sets_pkey PRIMARY KEY (user_id, set_id);

-- 7. Update TOPICS table
ALTER TABLE topics
    DROP CONSTRAINT topics_pkey,
    ADD COLUMN new_id UUID;

UPDATE topics t
SET new_id = m.new_id
FROM _topic_id_map m
WHERE t.id = m.old_id;

UPDATE topics SET new_id = uuidv7() WHERE new_id IS NULL;

ALTER TABLE topics
    DROP COLUMN id,
    DROP COLUMN created_at;

ALTER TABLE topics
    RENAME COLUMN new_id TO id;

ALTER TABLE topics
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN id SET DEFAULT uuidv7(),
    ADD CONSTRAINT topics_pkey PRIMARY KEY (id);

-- 8. Update FACTS table
ALTER TABLE facts
    DROP CONSTRAINT facts_pkey,
    ADD COLUMN new_id UUID,
    ADD COLUMN new_set_id UUID,
    ADD COLUMN new_topic_id UUID;

UPDATE facts f
SET new_id = m.new_id,
    new_set_id = s.new_id,
    new_topic_id = t.new_id
FROM _fact_id_map m
JOIN _set_id_map s ON true
LEFT JOIN _topic_id_map t ON true
WHERE f.id = m.old_id
  AND f.set_id = s.old_id
  AND (f.topic_id IS NULL OR f.topic_id = t.old_id);

UPDATE facts SET new_id = uuidv7() WHERE new_id IS NULL;

ALTER TABLE facts
    DROP COLUMN id,
    DROP COLUMN set_id,
    DROP COLUMN topic_id,
    DROP COLUMN created_at;

ALTER TABLE facts
    RENAME COLUMN new_id TO id;
ALTER TABLE facts
    RENAME COLUMN new_set_id TO set_id;
ALTER TABLE facts
    RENAME COLUMN new_topic_id TO topic_id;

ALTER TABLE facts
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN id SET DEFAULT uuidv7(),
    ALTER COLUMN set_id SET NOT NULL,
    ADD CONSTRAINT facts_pkey PRIMARY KEY (id);

-- 9. Update USER_FACT_PROGRESS table
ALTER TABLE user_fact_progress
    ADD COLUMN new_user_id UUID,
    ADD COLUMN new_fact_id UUID;

UPDATE user_fact_progress ufp
SET new_user_id = u.new_id,
    new_fact_id = f.new_id
FROM _user_id_map u, _fact_id_map f
WHERE ufp.user_id = u.old_id AND ufp.fact_id = f.old_id;

ALTER TABLE user_fact_progress
    DROP COLUMN user_id,
    DROP COLUMN fact_id;

ALTER TABLE user_fact_progress
    RENAME COLUMN new_user_id TO user_id;
ALTER TABLE user_fact_progress
    RENAME COLUMN new_fact_id TO fact_id;

ALTER TABLE user_fact_progress
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN fact_id SET NOT NULL,
    ADD CONSTRAINT user_fact_progress_pkey PRIMARY KEY (user_id, fact_id);

-- 10. Update CARDS table
ALTER TABLE cards
    DROP CONSTRAINT cards_pkey,
    ADD COLUMN new_id UUID,
    ADD COLUMN new_fact_id UUID;

UPDATE cards c
SET new_id = m.new_id,
    new_fact_id = f.new_id
FROM _card_id_map m, _fact_id_map f
WHERE c.id = m.old_id AND c.fact_id = f.old_id;

UPDATE cards SET new_id = uuidv7() WHERE new_id IS NULL;

ALTER TABLE cards
    DROP COLUMN id,
    DROP COLUMN fact_id,
    DROP COLUMN created_at;

ALTER TABLE cards
    RENAME COLUMN new_id TO id;
ALTER TABLE cards
    RENAME COLUMN new_fact_id TO fact_id;

ALTER TABLE cards
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN id SET DEFAULT uuidv7(),
    ALTER COLUMN fact_id SET NOT NULL,
    ADD CONSTRAINT cards_pkey PRIMARY KEY (id);

-- 11. Update CARD_OPTIONS, CARD_TABLE_COLUMNS, CARD_TABLE_ITEMS tables
ALTER TABLE card_options
    ALTER COLUMN id SET DEFAULT uuidv7(),
    ADD COLUMN new_card_id UUID;

UPDATE card_options co
SET new_card_id = c.new_id
FROM _card_id_map c
WHERE co.card_id = c.old_id;

ALTER TABLE card_options
    DROP COLUMN card_id;

ALTER TABLE card_options
    RENAME COLUMN new_card_id TO card_id;

ALTER TABLE card_options
    ALTER COLUMN card_id SET NOT NULL,
    DROP CONSTRAINT IF EXISTS card_options_card_id_position_key,
    ADD CONSTRAINT card_options_card_id_position_key UNIQUE (card_id, position);

ALTER TABLE card_table_columns
    ALTER COLUMN id SET DEFAULT uuidv7(),
    ADD COLUMN new_card_id UUID;

UPDATE card_table_columns ctc
SET new_card_id = c.new_id
FROM _card_id_map c
WHERE ctc.card_id = c.old_id;

ALTER TABLE card_table_columns
    DROP COLUMN card_id;

ALTER TABLE card_table_columns
    RENAME COLUMN new_card_id TO card_id;

ALTER TABLE card_table_columns
    ALTER COLUMN card_id SET NOT NULL,
    DROP CONSTRAINT IF EXISTS card_table_columns_card_id_position_key,
    ADD CONSTRAINT card_table_columns_card_id_position_key UNIQUE (card_id, position);

ALTER TABLE card_table_items
    ALTER COLUMN id SET DEFAULT uuidv7(),
    ADD COLUMN new_card_id UUID;

UPDATE card_table_items cti
SET new_card_id = c.new_id
FROM _card_id_map c
WHERE cti.card_id = c.old_id;

ALTER TABLE card_table_items
    DROP COLUMN card_id;

ALTER TABLE card_table_items
    RENAME COLUMN new_card_id TO card_id;

ALTER TABLE card_table_items
    ALTER COLUMN card_id SET NOT NULL;

-- 12. Update ANSWER_RESULTS table
ALTER TABLE answer_results
    DROP CONSTRAINT answer_results_pkey,
    ADD COLUMN new_id UUID,
    ADD COLUMN new_user_id UUID,
    ADD COLUMN new_card_id UUID;

UPDATE answer_results ar
SET new_id = m.new_id,
    new_user_id = u.new_id,
    new_card_id = c.new_id
FROM _answer_result_id_map m, _user_id_map u, _card_id_map c
WHERE ar.id = m.old_id AND ar.user_id = u.old_id AND ar.card_id = c.old_id;

UPDATE answer_results SET new_id = uuidv7() WHERE new_id IS NULL;

ALTER TABLE answer_results
    DROP COLUMN id,
    DROP COLUMN user_id,
    DROP COLUMN card_id,
    DROP COLUMN created_at;

ALTER TABLE answer_results
    RENAME COLUMN new_id TO id;
ALTER TABLE answer_results
    RENAME COLUMN new_user_id TO user_id;
ALTER TABLE answer_results
    RENAME COLUMN new_card_id TO card_id;

ALTER TABLE answer_results
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN id SET DEFAULT uuidv7(),
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN card_id SET NOT NULL,
    ADD CONSTRAINT answer_results_pkey PRIMARY KEY (id);

-- 13. Recreate all foreign key constraints
ALTER TABLE sets
    ADD CONSTRAINT sets_author_id_fkey FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE user_sets
    ADD CONSTRAINT user_sets_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    ADD CONSTRAINT user_sets_set_id_fkey FOREIGN KEY (set_id) REFERENCES sets(id) ON DELETE CASCADE;

ALTER TABLE facts
    ADD CONSTRAINT facts_set_id_fkey FOREIGN KEY (set_id) REFERENCES sets(id) ON DELETE CASCADE,
    ADD CONSTRAINT facts_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES topics(id) ON DELETE SET NULL;

ALTER TABLE user_fact_progress
    ADD CONSTRAINT user_fact_progress_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    ADD CONSTRAINT user_fact_progress_fact_id_fkey FOREIGN KEY (fact_id) REFERENCES facts(id) ON DELETE CASCADE;

ALTER TABLE cards
    ADD CONSTRAINT cards_fact_id_fkey FOREIGN KEY (fact_id) REFERENCES facts(id) ON DELETE CASCADE;

ALTER TABLE card_options
    ADD CONSTRAINT card_options_card_id_fkey FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE;

ALTER TABLE card_table_columns
    ADD CONSTRAINT card_table_columns_card_id_fkey FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE;

ALTER TABLE card_table_items
    ADD CONSTRAINT card_table_items_card_id_fkey FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE;

ALTER TABLE answer_results
    ADD CONSTRAINT answer_results_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    ADD CONSTRAINT answer_results_card_id_fkey FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE;

-- 14. Recreate Indexes
DROP INDEX IF EXISTS idx_sets_author_id;
CREATE INDEX idx_sets_author_id ON sets(author_id);

DROP INDEX IF EXISTS idx_facts_set_id;
CREATE INDEX idx_facts_set_id ON facts(set_id);

DROP INDEX IF EXISTS idx_facts_topic_id;
CREATE INDEX idx_facts_topic_id ON facts(topic_id);

DROP INDEX IF EXISTS idx_cards_fact_id;
CREATE INDEX idx_cards_fact_id ON cards(fact_id);

DROP INDEX IF EXISTS idx_card_options_card_id;
CREATE INDEX idx_card_options_card_id ON card_options(card_id);

DROP INDEX IF EXISTS idx_card_table_columns_card_id;
CREATE INDEX idx_card_table_columns_card_id ON card_table_columns(card_id);

DROP INDEX IF EXISTS idx_card_table_items_card_id;
CREATE INDEX idx_card_table_items_card_id ON card_table_items(card_id);

DROP INDEX IF EXISTS idx_user_fact_progress_due;
CREATE INDEX idx_user_fact_progress_due ON user_fact_progress(user_id, next_review_at);

DROP INDEX IF EXISTS idx_answer_results_user;
CREATE INDEX idx_answer_results_user ON answer_results(user_id, answered_at);

-- Drop temp tables
DROP TABLE _user_id_map;
DROP TABLE _set_id_map;
DROP TABLE _topic_id_map;
DROP TABLE _fact_id_map;
DROP TABLE _card_id_map;
DROP TABLE _answer_result_id_map;


-- +goose Down
-- Full DDL rollback to Migration 00008 schema.
-- Recreates created_at using uuid_extract_timestamp(id), reverts users and answer_results user_id to BIGINT,
-- facts.id and foreign keys to TEXT, and restores sequences and constraints.

-- 1. Create temporary rollback mapping for users
CREATE SEQUENCE IF NOT EXISTS users_internal_id_seq;

CREATE TEMP TABLE _user_rollback_map AS
SELECT
    id AS uuid_id,
    CASE
        WHEN id = '00000000-0000-0000-0000-000000000000'::uuid THEN 0::bigint
        WHEN max_id IS NOT NULL THEN max_id
        ELSE nextval('users_internal_id_seq')
    END AS bigint_id
FROM users;

SELECT setval('users_internal_id_seq', GREATEST(COALESCE((SELECT MAX(bigint_id) FROM _user_rollback_map), 0), 100001));

-- 2. Drop all foreign keys and composite primary keys
ALTER TABLE sets DROP CONSTRAINT IF EXISTS sets_author_id_fkey;
ALTER TABLE user_sets DROP CONSTRAINT IF EXISTS user_sets_user_id_fkey;
ALTER TABLE user_sets DROP CONSTRAINT IF EXISTS user_sets_set_id_fkey;
ALTER TABLE user_fact_progress DROP CONSTRAINT IF EXISTS user_fact_progress_user_id_fkey;
ALTER TABLE user_fact_progress DROP CONSTRAINT IF EXISTS user_fact_progress_fact_id_fkey;
ALTER TABLE facts DROP CONSTRAINT IF EXISTS facts_set_id_fkey;
ALTER TABLE facts DROP CONSTRAINT IF EXISTS facts_topic_id_fkey;
ALTER TABLE cards DROP CONSTRAINT IF EXISTS cards_fact_id_fkey;
ALTER TABLE card_options DROP CONSTRAINT IF EXISTS card_options_card_id_fkey;
ALTER TABLE card_table_columns DROP CONSTRAINT IF EXISTS card_table_columns_card_id_fkey;
ALTER TABLE card_table_items DROP CONSTRAINT IF EXISTS card_table_items_card_id_fkey;
ALTER TABLE answer_results DROP CONSTRAINT IF EXISTS answer_results_user_id_fkey;
ALTER TABLE answer_results DROP CONSTRAINT IF EXISTS answer_results_card_id_fkey;

ALTER TABLE user_sets DROP CONSTRAINT IF EXISTS user_sets_pkey;
ALTER TABLE user_fact_progress DROP CONSTRAINT IF EXISTS user_fact_progress_pkey;

-- 3. Rollback USERS table
ALTER TABLE users
    DROP CONSTRAINT users_pkey,
    ADD COLUMN old_id BIGINT,
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

UPDATE users u
SET old_id = m.bigint_id,
    created_at = COALESCE(uuid_extract_timestamp(u.id), CURRENT_TIMESTAMP)
FROM _user_rollback_map m
WHERE u.id = m.uuid_id;

ALTER TABLE users
    DROP COLUMN id;

ALTER TABLE users
    RENAME COLUMN old_id TO id;

ALTER TABLE users
    ALTER COLUMN id SET NOT NULL,
    ALTER COLUMN id SET DEFAULT nextval('users_internal_id_seq'),
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

ALTER SEQUENCE users_internal_id_seq OWNED BY users.id;

-- 4. Rollback SETS table
ALTER TABLE sets
    ADD COLUMN old_author_id BIGINT,
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

UPDATE sets s
SET old_author_id = m.bigint_id,
    created_at = COALESCE(uuid_extract_timestamp(s.id), CURRENT_TIMESTAMP)
FROM _user_rollback_map m
WHERE s.author_id = m.uuid_id;

ALTER TABLE sets
    DROP COLUMN author_id;

ALTER TABLE sets
    RENAME COLUMN old_author_id TO author_id;

ALTER TABLE sets
    ALTER COLUMN author_id SET NOT NULL,
    ALTER COLUMN id SET DEFAULT gen_random_uuid();

-- 5. Rollback USER_SETS table
ALTER TABLE user_sets
    ADD COLUMN old_user_id BIGINT;

UPDATE user_sets us
SET old_user_id = m.bigint_id
FROM _user_rollback_map m
WHERE us.user_id = m.uuid_id;

ALTER TABLE user_sets
    DROP COLUMN user_id;

ALTER TABLE user_sets
    RENAME COLUMN old_user_id TO user_id;

ALTER TABLE user_sets
    ALTER COLUMN user_id SET NOT NULL,
    ADD CONSTRAINT user_sets_pkey PRIMARY KEY (user_id, set_id);

-- 6. Rollback TOPICS table
ALTER TABLE topics
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

UPDATE topics
SET created_at = COALESCE(uuid_extract_timestamp(id), CURRENT_TIMESTAMP);

ALTER TABLE topics
    ALTER COLUMN id SET DEFAULT gen_random_uuid();

-- 7. Rollback FACTS table
ALTER TABLE facts
    DROP CONSTRAINT facts_pkey,
    ADD COLUMN old_id TEXT,
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

UPDATE facts
SET old_id = id::text,
    created_at = COALESCE(uuid_extract_timestamp(id), CURRENT_TIMESTAMP);

ALTER TABLE facts
    DROP COLUMN id;

ALTER TABLE facts
    RENAME COLUMN old_id TO id;

ALTER TABLE facts
    ALTER COLUMN id SET NOT NULL,
    ADD CONSTRAINT facts_pkey PRIMARY KEY (id);

-- 8. Rollback CARDS table
ALTER TABLE cards
    ADD COLUMN old_fact_id TEXT,
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

UPDATE cards
SET old_fact_id = fact_id::text,
    created_at = COALESCE(uuid_extract_timestamp(id), CURRENT_TIMESTAMP);

ALTER TABLE cards
    DROP COLUMN fact_id;

ALTER TABLE cards
    RENAME COLUMN old_fact_id TO fact_id;

ALTER TABLE cards
    ALTER COLUMN fact_id SET NOT NULL,
    ALTER COLUMN id SET DEFAULT gen_random_uuid();

-- 9. Rollback CARD_OPTIONS, CARD_TABLE_COLUMNS, CARD_TABLE_ITEMS defaults
ALTER TABLE card_options ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE card_table_columns ALTER COLUMN id SET DEFAULT gen_random_uuid();
ALTER TABLE card_table_items ALTER COLUMN id SET DEFAULT gen_random_uuid();

-- 10. Rollback USER_FACT_PROGRESS table
ALTER TABLE user_fact_progress
    ADD COLUMN old_user_id BIGINT,
    ADD COLUMN old_fact_id TEXT;

UPDATE user_fact_progress ufp
SET old_user_id = m.bigint_id,
    old_fact_id = ufp.fact_id::text
FROM _user_rollback_map m
WHERE ufp.user_id = m.uuid_id;

ALTER TABLE user_fact_progress
    DROP COLUMN user_id,
    DROP COLUMN fact_id;

ALTER TABLE user_fact_progress
    RENAME COLUMN old_user_id TO user_id;
ALTER TABLE user_fact_progress
    RENAME COLUMN old_fact_id TO fact_id;

ALTER TABLE user_fact_progress
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN fact_id SET NOT NULL,
    ADD CONSTRAINT user_fact_progress_pkey PRIMARY KEY (user_id, fact_id);

-- 11. Rollback ANSWER_RESULTS table
ALTER TABLE answer_results
    ADD COLUMN old_user_id BIGINT,
    ADD COLUMN created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP;

UPDATE answer_results ar
SET old_user_id = m.bigint_id,
    created_at = COALESCE(uuid_extract_timestamp(ar.id), CURRENT_TIMESTAMP)
FROM _user_rollback_map m
WHERE ar.user_id = m.uuid_id;

ALTER TABLE answer_results
    DROP COLUMN user_id;

ALTER TABLE answer_results
    RENAME COLUMN old_user_id TO user_id;

ALTER TABLE answer_results
    ALTER COLUMN user_id SET NOT NULL,
    ALTER COLUMN id SET DEFAULT gen_random_uuid();

-- 12. Recreate foreign key constraints
ALTER TABLE sets
    ADD CONSTRAINT sets_author_id_fkey FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE user_sets
    ADD CONSTRAINT user_sets_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    ADD CONSTRAINT user_sets_set_id_fkey FOREIGN KEY (set_id) REFERENCES sets(id) ON DELETE CASCADE;

ALTER TABLE facts
    ADD CONSTRAINT facts_set_id_fkey FOREIGN KEY (set_id) REFERENCES sets(id) ON DELETE CASCADE,
    ADD CONSTRAINT facts_topic_id_fkey FOREIGN KEY (topic_id) REFERENCES topics(id) ON DELETE SET NULL;

ALTER TABLE cards
    ADD CONSTRAINT cards_fact_id_fkey FOREIGN KEY (fact_id) REFERENCES facts(id) ON DELETE CASCADE;

ALTER TABLE card_options
    ADD CONSTRAINT card_options_card_id_fkey FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE;

ALTER TABLE card_table_columns
    ADD CONSTRAINT card_table_columns_card_id_fkey FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE;

ALTER TABLE card_table_items
    ADD CONSTRAINT card_table_items_card_id_fkey FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE;

ALTER TABLE user_fact_progress
    ADD CONSTRAINT user_fact_progress_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    ADD CONSTRAINT user_fact_progress_fact_id_fkey FOREIGN KEY (fact_id) REFERENCES facts(id) ON DELETE CASCADE;

ALTER TABLE answer_results
    ADD CONSTRAINT answer_results_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    ADD CONSTRAINT answer_results_card_id_fkey FOREIGN KEY (card_id) REFERENCES cards(id) ON DELETE CASCADE;

-- 13. Recreate Indexes
DROP INDEX IF EXISTS idx_sets_author_id;
CREATE INDEX idx_sets_author_id ON sets(author_id);

DROP INDEX IF EXISTS idx_facts_set_id;
CREATE INDEX idx_facts_set_id ON facts(set_id);

DROP INDEX IF EXISTS idx_facts_topic_id;
CREATE INDEX idx_facts_topic_id ON facts(topic_id);

DROP INDEX IF EXISTS idx_cards_fact_id;
CREATE INDEX idx_cards_fact_id ON cards(fact_id);

DROP INDEX IF EXISTS idx_card_options_card_id;
CREATE INDEX idx_card_options_card_id ON card_options(card_id);

DROP INDEX IF EXISTS idx_card_table_columns_card_id;
CREATE INDEX idx_card_table_columns_card_id ON card_table_columns(card_id);

DROP INDEX IF EXISTS idx_card_table_items_card_id;
CREATE INDEX idx_card_table_items_card_id ON card_table_items(card_id);

DROP INDEX IF EXISTS idx_user_fact_progress_due;
CREATE INDEX idx_user_fact_progress_due ON user_fact_progress(user_id, next_review_at);

DROP INDEX IF EXISTS idx_answer_results_user;
CREATE INDEX idx_answer_results_user ON answer_results(user_id, answered_at);

DROP TABLE IF EXISTS _user_rollback_map;
