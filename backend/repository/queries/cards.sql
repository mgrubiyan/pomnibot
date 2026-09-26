-- name: CreateCard :one
INSERT INTO cards (
    set_id,
    kind,
    question,
    answer_choice_index,
    answer_text,
    answer_boolean,
    explanation,
    source_quote,
    source_ref,
    topic
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: CreateCardOption :exec
INSERT INTO card_options (card_id, option_index, text)
VALUES ($1, $2, $3);

-- name: CreateCardTableColumn :one
INSERT INTO card_table_columns (card_id, column_index, title)
VALUES ($1, $2, $3)
RETURNING id, card_id, column_index, title;

-- name: CreateCardTableItem :exec
INSERT INTO card_table_items (card_id, column_id, text)
VALUES ($1, $2, $3);

-- name: GetCardByID :one
SELECT *
FROM cards
WHERE id = $1;

-- name: ListCardOptionsByCardID :many
SELECT id, card_id, option_index, text
FROM card_options
WHERE card_id = $1
ORDER BY option_index ASC;

-- name: ListCardTableColumnsByCardID :many
SELECT id, card_id, column_index, title
FROM card_table_columns
WHERE card_id = $1
ORDER BY column_index ASC;

-- name: ListCardTableItemsByCardID :many
SELECT 
    i.id,
    i.card_id,
    i.column_id,
    i.text,
    c.title AS column_title
FROM card_table_items i
JOIN card_table_columns c ON c.id = i.column_id
WHERE i.card_id = $1
ORDER BY i.id ASC;

-- name: ListCardsBySetID :many
SELECT *
FROM cards
WHERE set_id = $1
ORDER BY created_at ASC;

-- name: UpdateCardByID :one
UPDATE cards
SET question = COALESCE($2, question),
    answer_choice_index = COALESCE($3, answer_choice_index),
    answer_text = COALESCE($4, answer_text),
    answer_boolean = COALESCE($5, answer_boolean),
    explanation = COALESCE($6, explanation),
    source_quote = COALESCE($7, source_quote),
    source_ref = COALESCE($8, source_ref),
    topic = COALESCE($9, topic),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteCardOptionsByCardID :exec
DELETE FROM card_options
WHERE card_id = $1;

-- name: DeleteCardTableColumnsByCardID :exec
DELETE FROM card_table_columns
WHERE card_id = $1;

-- name: DeleteCardByID :exec
DELETE FROM cards
WHERE id = $1;
