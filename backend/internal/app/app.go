// Package app wires the API, persistence and production integrations.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mgrubiyan/pomnibot/backend/internal/bot"
	"github.com/mgrubiyan/pomnibot/backend/internal/config"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/grader"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest/yandex"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/gigachat"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/yandexgpt"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	httptransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/http"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// Run starts the application and shuts it down when ctx is cancelled.
func Run(ctx context.Context, cfg config.Config, assets fs.FS) error {
	cardOptions := []usecase.CardServiceOption{}
	var model providers.Provider
	if cfg.Environment == config.Prod {
		var err error
		model, err = newModel(cfg)
		if err != nil {
			return err
		}
		cardOptions = append(cardOptions, usecase.WithGrader(&grader.Grader{Model: model}))
	}

	if err := repository.RunMigrations(ctx, cfg.DatabaseURL); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	queries := db.New(pool)
	store := repository.NewManualStore(pool)
	users := usecase.NewUserService(queries)
	sets := usecase.NewSetService(queries, users, store)
	cardOptions = append(cardOptions, usecase.WithManualCards(store))
	cards := usecase.NewCardService(queries, users, cardOptions...)
	home := usecase.NewHomescreenService(queries, users)
	router, err := httptransport.NewRouter(httptransport.NewAPIHandler(sets, cards, home), assets, cfg.BotToken, users)
	if err != nil {
		return fmt.Errorf("initialize router: %w", err)
	}

	// Stop background integrations on both startup failure and normal shutdown.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if cfg.Environment == config.Prod {
		if err := startBot(ctx, cfg, model, users, sets); err != nil {
			return err
		}
	}

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return serve(ctx, server)
}

// newModel returns GigaChat, backed by YandexGPT when it is configured: the
// generator, the grader and the photo reader share it, so they all stop
// waiting on a GigaChat that is down at once.
func newModel(cfg config.Config) (providers.Provider, error) {
	primary, err := gigachat.New(cfg.GigaChat)
	if err != nil {
		return nil, fmt.Errorf("initialize GigaChat: %w", err)
	}
	if cfg.YandexGPT == nil {
		slog.Warn("no backup model: set YC_LLM_MODEL to use YandexGPT when GigaChat is down")
		return primary, nil
	}
	backup, err := yandexgpt.New(*cfg.YandexGPT)
	if err != nil {
		return nil, fmt.Errorf("initialize YandexGPT: %w", err)
	}
	slog.Info("backup model enabled", "model", cfg.YandexGPT.Model,
		"primary_timeout", cfg.LLM.PrimaryTimeout, "cooldown", cfg.LLM.Cooldown)
	return providers.NewFallback(cfg.LLM.Cooldown,
		providers.Backend{Name: "gigachat", Provider: primary, Timeout: cfg.LLM.PrimaryTimeout},
		providers.Backend{Name: "yandexgpt", Provider: backup},
	), nil
}

func startBot(ctx context.Context, cfg config.Config, model providers.Provider, users usecase.UserService, sets usecase.SetService) error {
	client, err := bot.NewClient(cfg.BotToken, cfg.MaxAPIURL, cfg.MaxInsecureSkipVerify)
	if err != nil {
		return fmt.Errorf("initialize MAX client: %w", err)
	}
	ocr, err := yandex.New(cfg.OCR)
	if err != nil {
		return fmt.Errorf("initialize OCR: %w", err)
	}
	extractor, err := ingest.NewExtractor(&ingest.VisionOCR{Model: model, Checker: ocr}, ingest.Options{})
	if err != nil {
		return fmt.Errorf("initialize extractor: %w", err)
	}
	cardGenerator := bot.NewGeneratorAdapter(generator.NewGenerator(model, generator.Options{}))
	runner, err := bot.NewBot(client, cfg.AppURL, users, sets, extractor, cardGenerator, cfg.Notifications)
	if err != nil {
		return fmt.Errorf("initialize MAX bot: %w", err)
	}
	if err := runner.Start(ctx); err != nil {
		return fmt.Errorf("start MAX bot: %w", err)
	}
	return nil
}

func serve(ctx context.Context, server *http.Server) error {
	done := make(chan error, 1)
	go func() {
		slog.Info("starting server", "addr", server.Addr)
		done <- server.ListenAndServe()
	}()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}
