-- name: ResolveIdentity :one
SELECT resolve_identity(sqlc.arg(platform)::text, sqlc.arg(external_id)::bigint,
    sqlc.arg(first_name)::text, sqlc.arg(last_name)::text, sqlc.arg(username)::text,
    sqlc.arg(is_bot)::boolean, sqlc.arg(bot_started)::boolean)::bigint AS user_id;

-- name: GetAccount :one
SELECT u.*, i.platform, i.external_id, i.profile_complete, i.profile_step, i.pending_last_name, i.bot_started
FROM users u JOIN user_identities i ON i.user_id = u.id WHERE u.id = $1;

-- name: SaveTelegramProfile :exec
SELECT save_telegram_profile(sqlc.arg(user_id)::bigint, sqlc.arg(first_name)::text, sqlc.arg(last_name)::text);

-- name: TelegramProfileInput :exec
SELECT telegram_profile_input(sqlc.arg(user_id)::bigint, sqlc.arg(input)::text, sqlc.arg(restart)::boolean);

-- name: GetDeliveryIdentities :many
SELECT i.* FROM user_identities i
WHERE i.user_id = $1 AND (i.platform = 'max' OR (i.bot_started AND i.profile_complete));
