-- name: CreateCardIssue :execrows
-- Inserts nothing when the card is gone or the user can't see its set.
INSERT INTO card_issues (card_id, set_id, user_id, reason, kind, question, answer_text, explanation, source_quote)
SELECT c.id, f.set_id, @user_id, @reason, c.kind, c.question, c.answer_text, c.explanation, c.source_quote
FROM cards c
JOIN facts f ON c.fact_id = f.id
WHERE c.id = @card_id
  AND EXISTS (
      SELECT 1 FROM sets s
      WHERE s.id = f.set_id
        AND (s.author_id = @user_id OR EXISTS (SELECT 1 FROM user_sets us WHERE us.set_id = s.id AND us.user_id = @user_id))
  );

-- name: GetOpenCardIssuesBySet :many
-- Open complaints of the author's set: made after the card's last edit and
-- not dismissed. Complaints about deleted cards are not listed.
SELECT
    i.card_id,
    i.reason,
    COUNT(*)::int AS count,
    MAX(i.created_at)::timestamptz AS last_reported_at
FROM card_issues i
JOIN cards c ON c.id = i.card_id
JOIN facts f ON f.id = c.fact_id
JOIN sets s ON s.id = f.set_id
WHERE f.set_id = @set_id
  AND s.author_id = @user_id
  AND i.resolved_at IS NULL
  AND i.created_at > c.updated_at
GROUP BY i.card_id, i.reason
ORDER BY last_reported_at DESC, i.card_id, i.reason;

-- name: ResolveCardIssues :one
-- Dismisses the open complaints of a card; false when the user is not the
-- author of its set or the card is gone.
WITH card AS (
    SELECT c.id
    FROM cards c
    JOIN facts f ON f.id = c.fact_id
    JOIN sets s ON s.id = f.set_id
    WHERE c.id = @card_id AND s.author_id = @user_id
), resolved AS (
    UPDATE card_issues
    SET resolved_at = CURRENT_TIMESTAMP
    WHERE card_id IN (SELECT id FROM card) AND resolved_at IS NULL
)
SELECT EXISTS (SELECT 1 FROM card) AS found;
