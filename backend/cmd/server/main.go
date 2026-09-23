// Package main provides a lightweight HTTP server serving embedded SPA assets.
package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/bot"
	httptransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/http"
)

//go:embed all:dist
var embeddedDist embed.FS

func getFileSystem() (fs.FS, error) {
	distFS, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return nil, err
	}
	slog.Info("serving static files from embedded filesystem")
	return distFS, nil
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	staticFS, err := getFileSystem()
	if err != nil {
		slog.Error("failed to initialize static filesystem", "error", err)
		os.Exit(1)
	}

	router, err := httptransport.NewRouter(httptransport.NewAPIHandler(), staticFS)
	if err != nil {
		slog.Error("failed to initialize router", "error", err)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Initialize MAX Bot if BOT_TOKEN is provided
	if botToken := os.Getenv("BOT_TOKEN"); botToken != "" {
		appURL := os.Getenv("APP_URL")
		if appURL == "" {
			if domain := os.Getenv("DOMAIN"); domain != "" {
				appURL = "https://" + domain
			} else {
				appURL = "https://pomnibot.steins.ru"
			}
		}

		apiURL := os.Getenv("MAX_API_URL")
		botClient, err := bot.NewClient(botToken, apiURL)
		if err != nil {
			slog.Error("failed to create bot client", "error", err)
		} else {
			maxBot := bot.NewBot(botClient, appURL)
			if err := maxBot.Start(ctx); err != nil {
				slog.Error("failed to start MAX bot", "error", err)
			}
		}
	} else {
		slog.Warn("BOT_TOKEN not provided, running in static SPA mode only")
	}

	go func() {
		slog.Info("starting server", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down server gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("failed to shutdown server gracefully", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped successfully")
}
