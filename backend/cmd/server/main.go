// Package main starts the Pomnibot server.
package main

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mgrubiyan/pomnibot/backend/internal/app"
	"github.com/mgrubiyan/pomnibot/backend/internal/config"
)

//go:embed all:dist
var assets embed.FS

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	static, err := fs.Sub(assets, "dist")
	if err != nil {
		return err
	}
	slog.Info("starting application", "environment", cfg.Environment)
	return app.Run(ctx, cfg, static)
}
