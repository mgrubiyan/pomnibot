-- +goose Up
CREATE TABLE set_images (
    id UUID PRIMARY KEY,
    set_id UUID NOT NULL REFERENCES sets(id) ON DELETE CASCADE,
    data BYTEA NOT NULL CHECK (octet_length(data) BETWEEN 1 AND 10000000),
    alt TEXT NOT NULL,
    width INTEGER NOT NULL CHECK (width > 0),
    height INTEGER NOT NULL CHECK (height > 0),
    source_name TEXT NOT NULL,
    source_page INTEGER NOT NULL CHECK (source_page > 0),
    source_file INTEGER NOT NULL CHECK (source_file > 0),
    region DOUBLE PRECISION[] NOT NULL CHECK (array_length(region, 1) = 4 AND region[1] >= 0 AND region[2] >= 0 AND region[3] <= 1 AND region[4] <= 1 AND region[1] < region[3] AND region[2] < region[4]),
    hash TEXT NOT NULL,
    UNIQUE (set_id, hash)
);
CREATE TABLE card_images (
    card_id UUID NOT NULL REFERENCES cards(id) ON DELETE CASCADE,
    asset_id UUID NOT NULL REFERENCES set_images(id) ON DELETE CASCADE,
    side TEXT NOT NULL CHECK (side IN ('question', 'explanation')),
    position INTEGER NOT NULL CHECK (position >= 0),
    PRIMARY KEY (card_id, side, position),
    UNIQUE (card_id, side, asset_id)
);
CREATE INDEX card_images_asset_id_idx ON card_images(asset_id);

-- +goose Down
DROP TABLE card_images;
DROP TABLE set_images;
