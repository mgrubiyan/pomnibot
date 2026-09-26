-- name: CreateSet :one
INSERT INTO card_sets (creator_id, title, share_code)
VALUES ($1, $2, $3)
RETURNING id, creator_id, title, share_code, created_at, updated_at;

-- name: GetSetByID :one
SELECT 
    s.id,
    s.creator_id,
    s.title,
    s.share_code,
    s.created_at,
    s.updated_at,
    u.first_name AS author_name,
    COUNT(DISTINCT c.id)::int AS cards_total,
    COUNT(DISTINCT CASE WHEN p.next_review_at <= now() THEN c.id END)::int AS cards_due
FROM card_sets s
JOIN users u ON u.id = s.creator_id
LEFT JOIN cards c ON c.set_id = s.id
LEFT JOIN user_card_progress p ON p.card_id = c.id AND p.user_id = $2
WHERE s.id = $1
GROUP BY s.id, s.creator_id, s.title, s.share_code, s.created_at, s.updated_at, u.first_name;

-- name: GetSetByShareCode :one
SELECT id, creator_id, title, share_code, created_at, updated_at
FROM card_sets
WHERE share_code = $1;

-- name: DeleteSetByID :exec
DELETE FROM card_sets
WHERE id = $1 AND creator_id = $2;

-- name: JoinSet :exec
INSERT INTO user_card_sets (user_id, set_id)
VALUES ($1, $2)
ON CONFLICT (user_id, set_id) DO NOTHING;

-- name: LeaveSet :exec
DELETE FROM user_card_sets
WHERE user_id = $1 AND set_id = $2;

-- name: ListSetsByUserID :many
SELECT 
    s.id,
    s.creator_id,
    s.title,
    s.share_code,
    CASE WHEN s.creator_id != $1 THEN u.first_name ELSE '' END AS author_name,
    COUNT(DISTINCT c.id)::int AS cards_total,
    COUNT(DISTINCT CASE WHEN p.next_review_at <= now() THEN c.id END)::int AS cards_due
FROM card_sets s
JOIN users u ON u.id = s.creator_id
LEFT JOIN user_card_sets ucs ON ucs.set_id = s.id AND ucs.user_id = $1
LEFT JOIN cards c ON c.set_id = s.id
LEFT JOIN user_card_progress p ON p.card_id = c.id AND p.user_id = $1
WHERE s.creator_id = $1 OR ucs.user_id = $1
GROUP BY s.id, s.creator_id, s.title, s.share_code, s.created_at, u.first_name
ORDER BY s.created_at DESC;
