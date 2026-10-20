// Package repository provides database migration runners, seeders, and repository helpers.
package repository

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib" // registers pgx driver with database/sql for goose migrations
	"github.com/pressly/goose/v3"
)

// MigrationsFS embeds all sql migration files.
//
//go:embed migrations/*.sql
var MigrationsFS embed.FS

// RunMigrations applies all pending embedded Goose migrations.
func RunMigrations(ctx context.Context, dbURL string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("open migration db connection: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping migration db: %w", err)
	}

	goose.SetBaseFS(MigrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	slog.InfoContext(ctx, "running database migrations via goose...")
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("apply goose migrations: %w", err)
	}

	slog.InfoContext(ctx, "database migrations applied successfully")
	return nil
}
