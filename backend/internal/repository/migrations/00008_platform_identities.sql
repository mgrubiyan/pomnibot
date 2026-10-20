-- +goose Up
CREATE SEQUENCE users_internal_id_seq;
SELECT setval('users_internal_id_seq', GREATEST(COALESCE((SELECT MAX(id) FROM users), 0), 100001));
ALTER TABLE users ALTER COLUMN id SET DEFAULT nextval('users_internal_id_seq');
ALTER SEQUENCE users_internal_id_seq OWNED BY users.id;

CREATE TABLE user_identities (
    user_id BIGINT NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    platform TEXT NOT NULL CHECK (platform IN ('max', 'telegram')),
    external_id BIGINT NOT NULL CHECK (external_id > 0),
    profile_complete BOOLEAN NOT NULL DEFAULT FALSE,
    profile_step TEXT NOT NULL DEFAULT 'last_name' CHECK (profile_step IN ('', 'last_name', 'first_name')),
    pending_last_name TEXT NOT NULL DEFAULT '',
    bot_started BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (platform, external_id)
);
-- IDs 1..30, 0 and 100001 belong to the shipped demonstrations.
INSERT INTO user_identities (user_id, platform, external_id, profile_complete, profile_step, bot_started)
SELECT id, 'max', id, TRUE, '', TRUE FROM users WHERE id > 30 AND id <> 100001;

-- Lock by external identity before creating either row, including simultaneous
-- bot and Mini App logins. The unique constraints are an additional safeguard.
-- +goose StatementBegin
CREATE FUNCTION resolve_identity(p_platform TEXT, p_external_id BIGINT, p_first TEXT,
    p_last TEXT, p_username TEXT, p_bot BOOLEAN, p_started BOOLEAN) RETURNS BIGINT
LANGUAGE plpgsql AS $$
DECLARE internal_id BIGINT;
BEGIN
    IF p_platform NOT IN ('max', 'telegram') OR p_external_id <= 0 THEN
        RAISE EXCEPTION 'invalid identity';
    END IF;
    PERFORM pg_advisory_xact_lock(hashtextextended(p_platform || ':' || p_external_id::text, 0));
    SELECT user_id INTO internal_id FROM user_identities
        WHERE platform = p_platform AND external_id = p_external_id FOR UPDATE;
    IF internal_id IS NULL THEN
        INSERT INTO users (first_name, last_name, username, is_bot)
            VALUES (CASE WHEN p_platform = 'max' THEN p_first ELSE '' END,
                CASE WHEN p_platform = 'max' THEN NULLIF(p_last, '') ELSE NULL END,
                NULLIF(p_username, ''), p_bot) RETURNING id INTO internal_id;
        INSERT INTO user_identities (user_id, platform, external_id, profile_complete, profile_step, bot_started)
            VALUES (internal_id, p_platform, p_external_id, p_platform = 'max',
                CASE WHEN p_platform = 'max' THEN '' ELSE 'last_name' END, p_started);
    ELSE
        UPDATE users SET
            first_name = CASE WHEN p_platform = 'max' AND p_first <> '' THEN p_first ELSE first_name END,
            last_name = CASE WHEN p_platform = 'max' THEN COALESCE(NULLIF(p_last, ''), last_name) ELSE last_name END,
            username = COALESCE(NULLIF(p_username, ''), username), is_bot = p_bot,
            last_active_at = CURRENT_TIMESTAMP WHERE id = internal_id;
        UPDATE user_identities SET bot_started = bot_started OR p_started WHERE user_id = internal_id;
    END IF;
    RETURN internal_id;
END;
$$;
-- +goose StatementEnd

-- Both the form and bot lock the identity first. A completed form clears the
-- pending bot input, so a stale first-name message cannot undo the saved profile.
-- +goose StatementBegin
CREATE FUNCTION save_telegram_profile(p_user_id BIGINT, p_first TEXT, p_last TEXT) RETURNS VOID
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM user_identities WHERE user_id = p_user_id AND platform = 'telegram' FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'Telegram identity required'; END IF;
    UPDATE users SET first_name = p_first, last_name = p_last WHERE id = p_user_id;
    UPDATE user_identities SET profile_complete = TRUE, profile_step = '', pending_last_name = '' WHERE user_id = p_user_id;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION telegram_profile_input(p_user_id BIGINT, p_text TEXT, p_restart BOOLEAN) RETURNS VOID
LANGUAGE plpgsql AS $$
DECLARE identity_row user_identities;
BEGIN
    SELECT * INTO STRICT identity_row FROM user_identities WHERE user_id = p_user_id AND platform = 'telegram' FOR UPDATE;
    IF p_restart THEN
        UPDATE user_identities SET profile_step = 'last_name', pending_last_name = '' WHERE user_id = p_user_id;
    ELSIF p_text <> '' AND identity_row.profile_step = 'last_name' THEN
        UPDATE user_identities SET profile_step = 'first_name', pending_last_name = p_text WHERE user_id = p_user_id;
    ELSIF p_text <> '' AND identity_row.profile_step = 'first_name' THEN
        PERFORM save_telegram_profile(p_user_id, p_text, identity_row.pending_last_name);
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION telegram_profile_input(BIGINT, TEXT, BOOLEAN);
DROP FUNCTION save_telegram_profile(BIGINT, TEXT, TEXT);
DROP FUNCTION resolve_identity(TEXT, BIGINT, TEXT, TEXT, TEXT, BOOLEAN, BOOLEAN);
DROP TABLE user_identities;
ALTER TABLE users ALTER COLUMN id DROP DEFAULT;
DROP SEQUENCE users_internal_id_seq;
