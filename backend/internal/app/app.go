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
	"github.com/mgrubiyan/pomnibot/backend/internal/config"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/grader"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest/yandex"
	"github.com/mgrubiyan/pomnibot/backend/internal/notes"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/aistudio"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/gigachat"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	httptransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/http"
	maxtransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/max"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// Run starts the application and shuts it down when ctx is cancelled.
func Run(ctx context.Context, cfg config.Config, assets fs.FS) error {
	cardOptions := []usecase.CardServiceOption{}
	var llm models
	if cfg.Environment == config.Prod {
		var err error
		llm, err = newModels(cfg)
		if err != nil {
			return err
		}
		cardOptions = append(cardOptions, usecase.WithGrader(&grader.Grader{Model: llm.text}))
	}

	if err := repository.RunMigrations(ctx, cfg.DatabaseURL); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	if err := repository.PingDatabase(ctx, pool.Ping); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	queries := db.New(pool)
	store := repository.NewManualStore(pool)
	users := usecase.NewUserService(queries)
	sets := usecase.NewSetService(queries, users, store)
	issues, err := newIssueNotifier(cfg, users)
	if err != nil {
		return err
	}
	// Before the pool closes: the messages look up the author's accounts.
	defer issues.Wait()
	cardOptions = append(cardOptions, usecase.WithManualCards(store), usecase.WithIssueNotifier(issues))
	cards := usecase.NewCardService(queries, users, cardOptions...)
	home := usecase.NewHomescreenService(queries, users)
	router, err := httptransport.NewRouter(httptransport.NewAPIHandler(sets, cards, home, users), assets, cfg.MaxBotToken, cfg.TelegramBotToken, users)
	if err != nil {
		return fmt.Errorf("initialize router: %w", err)
	}

	// Stop background integrations on both startup failure and normal shutdown.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var bots []func()
	defer func() {
		cancel()
		for _, wait := range bots {
			wait()
		}
	}()
	if cfg.Environment == config.Prod {
		// One Maker for every bot: they share its queue, so together they
		// stay within what the model serves at once.
		ocr, err := yandex.New(cfg.OCR)
		if err != nil {
			return fmt.Errorf("initialize OCR: %w", err)
		}
		fullPages := cfg.LLM.Primary == config.PrimaryOpenRouter
		extractor, err := ingest.NewExtractor(&ingest.VisionOCR{Model: llm.vision, Checker: ocr, FullPage: fullPages}, ingest.Options{ReadAllPDFPages: fullPages})
		if err != nil {
			return fmt.Errorf("initialize extractor: %w", err)
		}
		cardGenerator := generator.NewGenerator(llm.text, generator.Options{})
		maker := notes.New(extractor, notes.FromGenerator(cardGenerator), sets, llm.slots)
		maxBot, err := startMAXBot(ctx, cfg, maker, users)
		if err != nil {
			return err
		}
		bots = append(bots, maxBot.Wait, startTelegramBot(ctx, cfg, maker, users))
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

// models are the model clients the app shares.
type models struct {
	text   providers.Provider // writes cards and grades answers
	vision providers.Provider // reads photos of notes
	slots  int                // documents turned into cards at once
}

// aiStudioSlots is how many documents are turned into cards at once on AI
// Studio, the quota for requests at once without a subscription.
const aiStudioSlots = 2

// aiStudioRequests is how many requests we send AI Studio at once. Measured
// on 2026-10-20: five pass, the sixth gets 429. A class checking answers at
// once queues behind them instead of retrying 429s, and the documents, at
// most aiStudioSlots requests, leave the rest of the slots to grading.
const aiStudioRequests = 5

// newModels shares a single fallback chain across cards, grading and photos.
// OpenRouter is backed by GigaChat, then AI Studio; without OpenRouter the
// legacy order remains available. Each backend retains its own request limit.
func newModels(cfg config.Config) (models, error) {
	giga, err := gigachat.New(cfg.GigaChat)
	if err != nil {
		return models{}, fmt.Errorf("initialize GigaChat: %w", err)
	}
	// Two documents, or a photo and the cards of another, would reach
	// GigaChat at once; its plan answers the second with 429.
	gigaOne := providers.Limit(giga, 1)

	if cfg.AIStudio == nil && cfg.OpenRouter == nil {
		slog.Warn("AI Studio is off: GigaChat does it all, no backup when it is down")
		// GigaChat alone still gets a timeout on photos, so that a hang
		// leaves the OCR time.
		photos := providers.NewFallback(cfg.LLM.Cooldown,
			providers.Backend{Name: "gigachat", Provider: gigaOne, Timeout: cfg.LLM.PrimaryTimeout})
		return models{text: gigaOne, vision: photos, slots: 1}, nil
	}
	backends := []providers.Backend{{Name: config.PrimaryGigaChat, Provider: gigaOne}}
	if cfg.AIStudio != nil {
		studio, err := aistudio.New(*cfg.AIStudio)
		if err != nil {
			return models{}, fmt.Errorf("initialize AI Studio: %w", err)
		}
		backends = append(backends, providers.Backend{Name: config.PrimaryAIStudio, Provider: providers.Limit(studio, aiStudioRequests)})
	}
	if cfg.OpenRouter != nil {
		router, err := openrouter.New(*cfg.OpenRouter)
		if err != nil {
			return models{}, fmt.Errorf("initialize OpenRouter: %w", err)
		}
		backends = append(backends, providers.Backend{Name: config.PrimaryOpenRouter, Provider: providers.Limit(router, aiStudioRequests)})
	}
	// Move the selected primary to the front, preserving the backup order.
	for i := range backends {
		if backends[i].Name == cfg.LLM.Primary {
			primary := backends[i]
			copy(backends[1:i+1], backends[:i])
			backends[0] = primary
			break
		}
	}
	for i := range len(backends) - 1 {
		backends[i].Timeout = cfg.LLM.PrimaryTimeout
	}
	chain := providers.NewFallback(cfg.LLM.Cooldown, backends...)
	slots := aiStudioSlots
	if cfg.LLM.Primary == config.PrimaryGigaChat {
		slots = 1
	}
	slog.Info("models", "primary", cfg.LLM.Primary,
		"primary_timeout", cfg.LLM.PrimaryTimeout, "cooldown", cfg.LLM.Cooldown, "documents_at_once", slots)
	return models{text: chain, vision: chain, slots: slots}, nil
}

func startMAXBot(ctx context.Context, cfg config.Config, maker *notes.Maker, users usecase.UserService) (*maxtransport.Bot, error) {
	client, err := maxtransport.NewClient(cfg.MaxBotToken, cfg.MaxAPIURL, cfg.MaxInsecureSkipVerify)
	if err != nil {
		return nil, fmt.Errorf("initialize MAX client: %w", err)
	}
	runner, err := maxtransport.NewBot(client, cfg.AppURL, users, maker, cfg.Notifications)
	if err != nil {
		return nil, fmt.Errorf("initialize MAX bot: %w", err)
	}
	if err := runner.Start(ctx); err != nil {
		return nil, fmt.Errorf("start MAX bot: %w", err)
	}
	return runner, nil
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
