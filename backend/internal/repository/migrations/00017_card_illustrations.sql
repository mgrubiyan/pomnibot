-- +goose Up
ALTER TABLE cards
    ADD COLUMN question_svg TEXT,
    ADD COLUMN explanation_svg TEXT;

-- +goose Down
ALTER TABLE cards
    DROP COLUMN question_svg,
    DROP COLUMN explanation_svg;
