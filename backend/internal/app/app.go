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
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/aistudio"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/gigachat"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	httptransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/http"
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
		if err := startBot(ctx, cfg, llm, users, sets); err != nil {
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

// models are the model clients the app shares.
type models struct {
	text   providers.Provider // writes cards and grades answers
	vision providers.Provider // reads photos of notes
	slots  int                // documents turned into cards at once
}

// aiStudioSlots is how many requests AI Studio serves at once without a
// subscription.
const aiStudioSlots = 2

// newModels returns Qwen backed by GigaChat for everything, or the other way
// round with LLM_PRIMARY=gigachat; a photo neither of them reads goes to the
// OCR. One chain serves cards, grading and photos, so a model that is down is
// skipped by all of them at once. Without AI Studio, GigaChat does it all.
func newModels(cfg config.Config) (models, error) {
	giga, err := gigachat.New(cfg.GigaChat)
	if err != nil {
		return models{}, fmt.Errorf("initialize GigaChat: %w", err)
	}
	// Two documents, or a photo and the cards of another, would reach
	// GigaChat at once; its plan answers the second with 429.
	gigaOne := providers.Limit(giga, 1)

	if cfg.AIStudio == nil {
		slog.Warn("AI Studio is off: GigaChat does it all, no backup when it is down")
		// GigaChat alone still gets a timeout on photos, so that a hang
		// leaves the OCR time.
		photos := providers.NewFallback(cfg.LLM.Cooldown,
			providers.Backend{Name: "gigachat", Provider: gigaOne, Timeout: cfg.LLM.PrimaryTimeout})
		return models{text: gigaOne, vision: photos, slots: 1}, nil
	}
	studio, err := aistudio.New(*cfg.AIStudio)
	if err != nil {
		return models{}, fmt.Errorf("initialize AI Studio: %w", err)
	}

	first := providers.Backend{Name: "aistudio", Provider: studio}
	second := providers.Backend{Name: "gigachat", Provider: gigaOne}
	slots := aiStudioSlots
	if cfg.LLM.Primary == config.PrimaryGigaChat {
		first, second, slots = second, first, 1
	}
	first.Timeout = cfg.LLM.PrimaryTimeout
	chain := providers.NewFallback(cfg.LLM.Cooldown, first, second)

	slog.Info("models", "primary", cfg.LLM.Primary, "aistudio_model", cfg.AIStudio.Model,
		"primary_timeout", cfg.LLM.PrimaryTimeout, "cooldown", cfg.LLM.Cooldown, "documents_at_once", slots)
	return models{text: chain, vision: chain, slots: slots}, nil
}

func startBot(ctx context.Context, cfg config.Config, llm models, users usecase.UserService, sets usecase.SetService) error {
	client, err := bot.NewClient(cfg.BotToken, cfg.MaxAPIURL, cfg.MaxInsecureSkipVerify)
	if err != nil {
		return fmt.Errorf("initialize MAX client: %w", err)
	}
	ocr, err := yandex.New(cfg.OCR)
	if err != nil {
		return fmt.Errorf("initialize OCR: %w", err)
	}
	extractor, err := ingest.NewExtractor(&ingest.VisionOCR{Model: llm.vision, Checker: ocr}, ingest.Options{})
	if err != nil {
		return fmt.Errorf("initialize extractor: %w", err)
	}
	cardGenerator := bot.NewGeneratorAdapter(generator.NewGenerator(llm.text, generator.Options{}))
	runner, err := bot.NewBot(client, cfg.AppURL, users, sets, extractor, cardGenerator, cfg.Notifications)
	if err != nil {
		return fmt.Errorf("initialize MAX bot: %w", err)
	}
	runner.SetGenerationSlots(llm.slots)
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
