//go:build integration

package repository_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func postgresTestDatabase(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18-alpine",
		postgres.WithDatabase("pomnibot_test"),
		postgres.WithUsername("pomnibot"),
		postgres.WithPassword("pomnibot"),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatalf("start test PostgreSQL (Docker must be running): %v", err)
	}
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, url
}

func manualTestPool(t *testing.T, beforeMigration ...func(*pgxpool.Pool)) *pgxpool.Pool {
	t.Helper()
	pool, _ := postgresTestDatabase(t)
	ctx := t.Context()
	entries, err := repository.MigrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		// Schema only: the demo sets and their fixes would fill the tables.
		if strings.Contains(entry.Name(), "seed") || strings.Contains(entry.Name(), "demo") {
			continue
		}
		if strings.HasPrefix(entry.Name(), "00008") {
			for _, before := range beforeMigration {
				before(pool)
			}
		}
		data, err := repository.MigrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, strings.Split(string(data), "-- +goose Down")[0]); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}
