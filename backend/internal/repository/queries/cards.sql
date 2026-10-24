-- name: CreateCard :one
INSERT INTO cards (id, fact_id, kind, question, answer_text, explanation, source_quote, source_ref, step, question_svg, explanation_svg)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: CreateCardOption :one
INSERT INTO card_options (card_id, position, text, is_correct)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateCardTableColumn :one
INSERT INTO card_table_columns (card_id, position, name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCardTableItem :one
INSERT INTO card_table_items (card_id, item_text, column_name, position)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetCardByID :one
SELECT
    c.id,
    f.set_id,
    c.fact_id,
    f.name AS fact_name,
    c.kind,
    c.question,
    c.answer_text,
    c.explanation,
    c.question_svg,
    c.explanation_svg,
    c.source_quote,
    c.source_ref,
    c.updated_at
FROM cards c
JOIN facts f ON c.fact_id = f.id
WHERE c.id = $1
  AND EXISTS (
      SELECT 1 FROM sets s
      WHERE s.id = f.set_id
        AND (s.author_id = @user_id OR EXISTS (SELECT 1 FROM user_sets us WHERE us.set_id = s.id AND us.user_id = @user_id))
  );

-- name: GetCardByIDForAuthor :one
SELECT
    c.id,
    f.set_id,
    c.fact_id,
    f.name AS fact_name,
    c.kind,
    c.question,
    c.answer_text,
    c.explanation,
    c.question_svg,
    c.explanation_svg,
    c.source_quote,
    c.source_ref,
    c.updated_at
FROM cards c
JOIN facts f ON c.fact_id = f.id
JOIN sets s ON f.set_id = s.id
WHERE c.id = $1 AND s.author_id = $2;

-- name: GetCardsBySetID :many
SELECT
    c.id,
    f.set_id,
    c.fact_id,
    f.name AS fact_name,
    c.kind,
    c.question,
    c.answer_text,
    c.explanation,
    c.question_svg,
    c.explanation_svg,
    c.source_quote,
    c.source_ref,
    c.updated_at,
    c.step
FROM cards c
JOIN facts f ON c.fact_id = f.id
WHERE f.set_id = $1
  AND EXISTS (
      SELECT 1 FROM sets s
      WHERE s.id = f.set_id
        AND (s.author_id = @user_id OR EXISTS (SELECT 1 FROM user_sets us WHERE us.set_id = s.id AND us.user_id = @user_id))
  )
ORDER BY f.id ASC, c.step ASC, c.id ASC;

-- name: GetCardOptions :many
SELECT *
FROM card_options
WHERE card_id = $1
ORDER BY position ASC;

-- name: GetCardTableColumns :many
SELECT *
FROM card_table_columns
WHERE card_id = $1
ORDER BY position ASC;

-- name: GetCardTableItems :many
SELECT *
FROM card_table_items
WHERE card_id = $1
ORDER BY position, id;

-- name: UpdateCard :one
UPDATE cards
SET
    kind = COALESCE($2, kind),
    question = COALESCE($3, question),
    answer_text = COALESCE($4, answer_text),
    explanation = COALESCE($5, explanation),
    source_quote = COALESCE($6, source_quote),
    source_ref = COALESCE($7, source_ref),
    question_svg = NULL,
    explanation_svg = NULL,
    updated_at = CURRENT_TIMESTAMP
FROM facts f
JOIN sets s ON f.set_id = s.id
WHERE cards.id = $1
  AND cards.fact_id = f.id
  AND s.author_id = $8
RETURNING cards.id, cards.fact_id, cards.kind, cards.question, cards.answer_text, cards.explanation, cards.question_svg, cards.explanation_svg, cards.source_quote, cards.source_ref, cards.updated_at;

-- name: DeleteCard :one
WITH removed AS (
    DELETE FROM cards c
    USING facts f, sets s
    WHERE c.id = $1 AND c.fact_id = f.id AND f.set_id = s.id AND s.author_id = $2
    RETURNING c.fact_id
), cleaned AS (
    DELETE FROM facts f USING removed r
    WHERE f.id = r.fact_id
      AND NOT EXISTS (SELECT 1 FROM cards c WHERE c.fact_id = f.id AND c.id <> $1)
)
SELECT COUNT(*) FROM removed;

-- name: DeleteCardOptions :exec
DELETE FROM card_options WHERE card_id = $1;

-- name: DeleteCardTableColumns :exec
DELETE FROM card_table_columns WHERE card_id = $1;

-- name: DeleteCardTableItems :exec
DELETE FROM card_table_items WHERE card_id = $1;

-- name: GetFeedCardsForUser :many
WITH user_due_facts AS (
    SELECT
        f.id AS fact_id,
        f.set_id,
        f.name AS fact_name,
        COALESCE(ufp.step, 0) AS step,
        COALESCE(ufp.next_review_at, '1970-01-01'::timestamptz) AS due_time
    FROM user_sets us
    JOIN facts f ON us.set_id = f.set_id
    LEFT JOIN user_fact_progress ufp ON ufp.fact_id = f.id AND ufp.user_id = us.user_id
    WHERE us.user_id = $1
      AND (ufp.next_review_at IS NULL OR ufp.next_review_at <= NOW())
),
ranked_cards AS (
    SELECT
        c.id,
        udf.set_id,
        c.fact_id,
        udf.fact_name,
        c.kind,
        c.question,
        c.answer_text,
        c.explanation,
        c.question_svg,
        c.explanation_svg,
        c.source_quote,
        c.source_ref,
        c.updated_at,
        udf.due_time,
        udf.step,
        ROW_NUMBER() OVER (
            PARTITION BY c.fact_id
            ORDER BY c.step ASC, c.id ASC
        ) - 1 AS card_idx,
        COUNT(*) OVER (PARTITION BY c.fact_id) AS total_cards
    FROM cards c
    JOIN user_due_facts udf ON c.fact_id = udf.fact_id
    -- A reader who complained about a card will not see it or any subsequent
    -- harder cards of that fact until the author fixes the card or keeps it.
    -- The author sees all cards as normal.
    WHERE NOT EXISTS (
        SELECT 1 FROM card_issues i
        JOIN cards reported ON reported.id = i.card_id
        JOIN sets s ON s.id = udf.set_id
        WHERE reported.fact_id = c.fact_id
          AND c.id >= reported.id
          AND i.user_id = $1
          AND s.author_id <> $1
          AND i.resolved_at IS NULL
          AND i.created_at > reported.updated_at
    )
)
SELECT
    id,
    set_id,
    fact_id,
    fact_name,
    kind,
    question,
    answer_text,
    explanation,
    question_svg,
    explanation_svg,
    source_quote,
    source_ref,
    updated_at
FROM ranked_cards
WHERE card_idx = LEAST(step, total_cards - 1)
ORDER BY due_time ASC, fact_id ASC;

-- name: CountCardsByFactID :one
SELECT COUNT(*)::INT FROM cards WHERE fact_id = $1;

-- name: GetCardsByFactID :many
SELECT c.id, c.fact_id, c.step
FROM cards c
WHERE c.fact_id = $1
ORDER BY c.step ASC, c.id ASC;

-- name: UpdateCardStep :execrows
UPDATE cards
SET step = $2, updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND fact_id = $3;

-- name: GetMaxCardStepInFact :one
SELECT COALESCE(MAX(step), -1)::INT AS max_step
FROM cards
WHERE fact_id = $1;

-- name: GetCardOptionsBatch :many
SELECT * FROM card_options
WHERE card_id = ANY(sqlc.arg(card_ids)::UUID[])
ORDER BY card_id, position;

-- name: GetCardTableColumnsBatch :many
SELECT * FROM card_table_columns
WHERE card_id = ANY(sqlc.arg(card_ids)::UUID[])
ORDER BY card_id, position;

-- name: GetCardTableItemsBatch :many
SELECT * FROM card_table_items
WHERE card_id = ANY(sqlc.arg(card_ids)::UUID[])
ORDER BY card_id, position, id;
