-- name: CreateCardIssue :one
INSERT INTO card_issues (card_id, user_id, reason)
VALUES ($1, $2, $3)
RETURNING id, card_id, user_id, reason, created_at;

-- name: ListCardIssuesByCardID :many
SELECT id, card_id, user_id, reason, created_at
FROM card_issues
WHERE card_id = $1
ORDER BY created_at DESC;
