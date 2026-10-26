-- name: EnsureUser :one
INSERT INTO users (id, first_name, last_name, username, is_bot, last_active_at)
VALUES ($1, '', NULL, NULL, FALSE, CURRENT_TIMESTAMP)
ON CONFLICT (id) DO UPDATE
SET last_active_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;


-- name: GetUsersWithDueFacts :many
SELECT DISTINCT u.id
FROM users u
JOIN user_sets us ON us.user_id = u.id
JOIN facts f ON f.set_id = us.set_id
LEFT JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = u.id
WHERE u.is_bot = FALSE
  AND u.id <> '00000000-0000-0000-0000-000000000000'::uuid
  AND (
    ufp.next_review_at IS NULL
    OR (ufp.next_review_at AT TIME ZONE sqlc.arg('tz')::text)::date <= (sqlc.arg('now')::timestamptz AT TIME ZONE sqlc.arg('tz')::text)::date
  );

-- name: UpsertMAXUser :one
INSERT INTO users (max_id, first_name, last_name, username, is_bot)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (max_id) DO UPDATE SET
    first_name = CASE WHEN EXCLUDED.first_name <> '' THEN EXCLUDED.first_name ELSE users.first_name END,
    last_name = COALESCE(EXCLUDED.last_name, users.last_name),
    username = COALESCE(EXCLUDED.username, users.username),
    is_bot = EXCLUDED.is_bot,
    last_active_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: UpsertTelegramUser :one
INSERT INTO users (tg_id, username, is_bot, tg_bot_started, profile_complete, profile_step)
VALUES ($1, NULLIF(sqlc.arg(username)::text, ''), $2, $3, FALSE, 'last_name')
ON CONFLICT (tg_id) DO UPDATE SET
    username = COALESCE(EXCLUDED.username, users.username),
    is_bot = EXCLUDED.is_bot,
    tg_bot_started = users.tg_bot_started OR EXCLUDED.tg_bot_started,
    last_active_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: SaveTelegramProfile :one
UPDATE users SET first_name = sqlc.arg(first_name), last_name = sqlc.arg(last_name),
    profile_complete = TRUE, profile_step = '', pending_last_name = ''
WHERE id = sqlc.arg(id) AND tg_id IS NOT NULL
RETURNING *;

-- name: TelegramProfileInput :one
UPDATE users SET
    first_name = CASE WHEN NOT sqlc.arg(restart)::boolean AND profile_step = 'first_name' AND sqlc.arg(input)::text <> '' THEN sqlc.arg(input) ELSE first_name END,
    last_name = CASE WHEN NOT sqlc.arg(restart)::boolean AND profile_step = 'first_name' AND sqlc.arg(input)::text <> '' THEN pending_last_name ELSE last_name END,
    profile_complete = profile_complete OR (NOT sqlc.arg(restart)::boolean AND profile_step = 'first_name' AND sqlc.arg(input)::text <> ''),
    pending_last_name = CASE WHEN sqlc.arg(restart)::boolean OR (profile_step = 'first_name' AND sqlc.arg(input)::text <> '') THEN '' WHEN profile_step = 'last_name' AND sqlc.arg(input)::text <> '' THEN sqlc.arg(input) ELSE pending_last_name END,
    profile_step = CASE WHEN sqlc.arg(restart)::boolean THEN 'last_name' WHEN sqlc.arg(input)::text = '' THEN profile_step WHEN profile_step = 'last_name' THEN 'first_name' ELSE '' END
WHERE id = sqlc.arg(id) AND tg_id IS NOT NULL
RETURNING *;

-- name: SetDataConsent :one
-- Giving consent again keeps the time it was first given, and any answer
-- keeps the time of the first one. Withdrawing it moves the photo to a new
-- address: authors may still hold the old one.
WITH moved AS (
    UPDATE avatars SET id = gen_random_uuid()
    WHERE user_id = sqlc.arg(id) AND NOT sqlc.arg(given)::boolean
    RETURNING id
)
UPDATE users SET
    data_consent_at = CASE
        WHEN sqlc.arg(given)::boolean THEN COALESCE(data_consent_at, CURRENT_TIMESTAMP)
    END,
    data_consent_answered_at = COALESCE(data_consent_answered_at, CURRENT_TIMESTAMP),
    avatar_id = COALESCE((SELECT id FROM moved), avatar_id)
WHERE users.id = sqlc.arg(id)
RETURNING *;

-- name: SaveAvatar :one
-- Replaces the user's photo with a new one under a new ID.
WITH added AS (
    INSERT INTO avatars (id, user_id, image)
    VALUES (gen_random_uuid(), sqlc.arg(id), sqlc.arg(image))
    ON CONFLICT (user_id) DO UPDATE SET id = EXCLUDED.id, image = EXCLUDED.image
    RETURNING id
)
UPDATE users SET avatar_id = (SELECT id FROM added)
WHERE users.id = sqlc.arg(id)
RETURNING *;

-- name: DeleteAvatar :one
WITH removed AS (
    DELETE FROM avatars WHERE user_id = sqlc.arg(id)
)
UPDATE users SET avatar_id = NULL
WHERE users.id = sqlc.arg(id)
RETURNING *;

-- name: SaveImportedAvatar :one
-- Replaces the user's photo with the one from their messenger profile.
WITH added AS (
    INSERT INTO avatars (id, user_id, image)
    VALUES (gen_random_uuid(), sqlc.arg(id), sqlc.arg(image))
    ON CONFLICT (user_id) DO UPDATE SET id = EXCLUDED.id, image = EXCLUDED.image
    RETURNING id
)
UPDATE users SET avatar_id = (SELECT id FROM added), avatar_source = sqlc.arg(source)
WHERE users.id = sqlc.arg(id)
RETURNING *;

-- name: ForgetImportedAvatar :exec
-- The messenger profile has no photo any more; one the user uploaded stays.
WITH removed AS (
    DELETE FROM avatars a USING users u
    WHERE a.user_id = sqlc.arg(id) AND u.id = a.user_id AND u.avatar_source IS NOT NULL
)
UPDATE users SET avatar_id = NULL, avatar_source = NULL
WHERE users.id = sqlc.arg(id) AND avatar_source IS NOT NULL;

-- name: GetAvatarImage :one
SELECT image FROM avatars WHERE id = $1;
