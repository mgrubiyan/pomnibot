-- +goose Up
-- What the reader wrote about the card in their own words; NULL when they
-- only picked a reason. The API asks for one with «Другое», but complaints
-- made before this column have none.
ALTER TABLE card_issues ADD COLUMN comment TEXT;

-- +goose Down
ALTER TABLE card_issues DROP COLUMN comment;
