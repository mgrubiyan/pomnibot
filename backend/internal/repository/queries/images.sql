-- name: CreateSetImage :exec
INSERT INTO set_images (id, set_id, data, alt, width, height, source_name, source_page, source_file, region, hash)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11);

-- name: GetSetImages :many
SELECT id, set_id, alt, width, height, source_name, source_page FROM set_images
WHERE set_id = $1 ORDER BY source_file, source_page, id;

-- name: GetImageForUser :one
SELECT i.data FROM set_images i JOIN sets s ON s.id = i.set_id
WHERE i.id = $1 AND (s.author_id = @user_id OR EXISTS (SELECT 1 FROM user_sets us WHERE us.set_id = s.id AND us.user_id = @user_id));

-- name: GetCardImages :many
SELECT i.id, i.set_id, i.alt, i.width, i.height, i.source_name, i.source_page, ci.side, ci.position
FROM card_images ci JOIN set_images i ON i.id = ci.asset_id
WHERE ci.card_id = $1 ORDER BY ci.side, ci.position;

-- name: DeleteCardImages :exec
DELETE FROM card_images WHERE card_id = $1 AND side = $2;

-- name: AttachCardImage :execrows
INSERT INTO card_images (card_id, asset_id, side, position)
SELECT c.id, i.id, @side, @position FROM cards c JOIN facts f ON f.id = c.fact_id
JOIN set_images i ON i.set_id = f.set_id
WHERE c.id = @card_id AND i.id = @asset_id;

-- name: GetCardImagesBatch :many
SELECT ci.card_id, i.id, i.set_id, i.alt, i.width, i.height, i.source_name, i.source_page, ci.side, ci.position
FROM card_images ci JOIN set_images i ON i.id = ci.asset_id
WHERE ci.card_id = ANY(sqlc.arg(card_ids)::UUID[])
ORDER BY ci.card_id, ci.side, ci.position;
