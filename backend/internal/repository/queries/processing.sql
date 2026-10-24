-- name: SaveProcessingReport :execrows
INSERT INTO set_processing_reports (set_id, version, report)
SELECT s.id, $3, $4 FROM sets s WHERE s.id = $1 AND s.author_id = $2;

-- name: GetProcessingReport :one
SELECT r.report FROM set_processing_reports r
JOIN sets s ON s.id = r.set_id
WHERE r.set_id = $1 AND s.author_id = $2;
