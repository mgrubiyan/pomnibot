-- name: CreateCardIssue :one
INSERT INTO card_issues (card_id, user_id, reason)
VALUES ($1, $2, $3)
RETURNING *;
