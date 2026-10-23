-- name: CreateCardIssue :one
-- Saves the complaint with a copy of the card; no row when the card is gone
-- or the user can't see its set. notify_author is true when no other reader
-- has an open complaint about the card: the author hears about it once, until
-- they edit the card or keep it. The main query does not see the new row.
WITH card AS (
    SELECT c.id, c.kind, c.question, c.answer_text, c.explanation, c.source_quote, c.updated_at,
        s.id AS set_id, s.title, s.author_id
    FROM cards c
    JOIN facts f ON c.fact_id = f.id
    JOIN sets s ON s.id = f.set_id
    WHERE c.id = @card_id
      AND (s.author_id = @user_id OR EXISTS (SELECT 1 FROM user_sets us WHERE us.set_id = s.id AND us.user_id = @user_id))
), saved AS (
    INSERT INTO card_issues (card_id, set_id, user_id, reason, kind, question, answer_text, explanation, source_quote)
    SELECT id, set_id, @user_id, @reason, kind, question, answer_text, explanation, source_quote
    FROM card
    RETURNING card_id
)
SELECT
    card.author_id,
    card.title AS set_title,
    (card.author_id <> @user_id::bigint AND NOT EXISTS (
        SELECT 1 FROM card_issues i
        WHERE i.card_id = card.id
          AND i.user_id <> card.author_id
          AND i.resolved_at IS NULL
          AND i.created_at > card.updated_at
    ))::boolean AS notify_author
FROM card
JOIN saved ON saved.card_id = card.id;

-- name: GetSetIssues :many
-- Open complaints of the author's set: made after the card's last edit and
-- not dismissed. Complaints about deleted cards are not listed.
SELECT
    i.card_id,
    i.reason,
    -- People, not complaints: a reader may complain again in a later session.
    COUNT(DISTINCT i.user_id)::int AS count,
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

-- name: CloseCardIssues :one
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

-- name: ListCardIssues :many
-- Every complaint, for admins, newest first. The status: deleted when the
-- card is gone; fixed when the card was edited after the complaint and before
-- any «Всё верно» (which also closes complaints the edit answered); kept when
-- the author left the card as it is; open otherwise.
SELECT
    i.id,
    i.reason,
    i.kind,
    i.question,
    i.answer_text,
    i.explanation,
    i.source_quote,
    i.created_at,
    s.title AS set_title,
    u.id AS reporter_id,
    u.first_name AS reporter_first_name,
    u.last_name AS reporter_last_name,
    u.username AS reporter_username,
    (CASE
        WHEN c.id IS NULL THEN 'deleted'
        WHEN c.updated_at > i.created_at AND (i.resolved_at IS NULL OR i.resolved_at > c.updated_at) THEN 'fixed'
        WHEN i.resolved_at IS NOT NULL THEN 'kept'
        ELSE 'open'
    END)::text AS status
FROM card_issues i
JOIN users u ON u.id = i.user_id
LEFT JOIN sets s ON s.id = i.set_id
LEFT JOIN cards c ON c.id = i.card_id
ORDER BY i.created_at DESC, i.id
LIMIT 1000;
