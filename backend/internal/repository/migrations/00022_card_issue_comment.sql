-- +goose Up
-- What the reader wrote about the card in their own words; NULL when they
-- only picked a reason. «Другое» always comes with one.
ALTER TABLE card_issues ADD COLUMN comment TEXT;

-- +goose Down
ALTER TABLE card_issues DROP COLUMN comment;
