//go:build integration

package repository_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/pressly/goose/v3"
)

// Pull requests add migrations side by side, and the one with a higher
// version may reach prod first. The lower ones must still apply then,
// instead of goose refusing to start.
func TestRunMigrationsAppliesMissingPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	_, url := postgresTestDatabase(t)
	conn, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err = goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if _, err = goose.EnsureDBVersionContext(ctx, conn); err != nil {
		t.Fatal(err)
	}
	// The newest migration is applied, the ones below it are not.
	if _, err = conn.ExecContext(ctx, "INSERT INTO goose_db_version (version_id, is_applied) VALUES (11, true)"); err != nil {
		t.Fatal(err)
	}

	if err = repository.RunMigrations(ctx, url); err != nil {
		t.Fatal(err)
	}
	var applied int
	if err = conn.QueryRowContext(ctx, "SELECT COUNT(DISTINCT version_id) FROM goose_db_version WHERE version_id BETWEEN 1 AND 8 AND is_applied").Scan(&applied); err != nil || applied != 8 {
		t.Fatalf("applied below the newest: %d, %v", applied, err)
	}
}
