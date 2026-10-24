-- +goose Up
-- Private provenance: never included in shared card-set or feed queries.
CREATE TABLE set_processing_reports (
    set_id UUID PRIMARY KEY REFERENCES sets(id) ON DELETE CASCADE,
    version TEXT NOT NULL,
    report JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE set_processing_reports;
