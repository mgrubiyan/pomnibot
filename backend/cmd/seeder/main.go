// Package main provides a CLI tool to seed the database with mock data from res.json.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	dbURL := flag.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL connection string")
	filePath := flag.String("file", "res.json", "Path to res.json file")
	flag.Parse()

	if *dbURL == "" {
		slog.Error("DATABASE_URL is required (pass -db flag or set DATABASE_URL environment variable)")
		os.Exit(1)
	}

	dataBytes, err := os.ReadFile(*filePath)
	if err != nil {
		slog.Error("failed to read mock data file", "path", *filePath, "error", err)
		os.Exit(1)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, *dbURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer func() {
		_ = conn.Close(ctx)
	}()

	queries := db.New(conn)
	if err := repository.SeedFromData(ctx, queries, dataBytes); err != nil {
		slog.Error("seeding failed", "error", err)
		os.Exit(1)
	}

	slog.Info("seeding completed successfully")
}
