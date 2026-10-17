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
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/yandexgpt"
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

// yandexGPTSlots is how many requests YandexGPT serves at once without a
// subscription.
const yandexGPTSlots = 2

// newModels returns YandexGPT backed by GigaChat for text, or the other way
// round with LLM_PRIMARY=gigachat, and Qwen backed by GigaChat for photos;
// when both fail on a photo, the OCR reads it. Without the Yandex models,
// GigaChat does it all.
func newModels(cfg config.Config) (models, error) {
	giga, err := gigachat.New(cfg.GigaChat)
	if err != nil {
		return models{}, fmt.Errorf("initialize GigaChat: %w", err)
	}
	// Photos of two documents, or text and a photo that fell back, would
	// reach GigaChat at once; its plan answers the second with 429.
	gigaOne := providers.Limit(giga, 1)
	m := models{text: gigaOne, slots: 1}

	if cfg.YandexGPT == nil {
		slog.Warn("YandexGPT is off: GigaChat writes the cards, no backup when it is down")
	} else {
		yc, err := yandexgpt.New(*cfg.YandexGPT)
		if err != nil {
			return models{}, fmt.Errorf("initialize YandexGPT: %w", err)
		}
		first, second := providers.Backend{Name: "yandexgpt", Provider: yc}, providers.Backend{Name: "gigachat", Provider: gigaOne}
		if cfg.LLM.Primary == config.PrimaryGigaChat {
			first, second = second, first
		} else {
			m.slots = yandexGPTSlots
		}
		first.Timeout = cfg.LLM.PrimaryTimeout
		m.text = providers.NewFallback(cfg.LLM.Cooldown, first, second)
	}

	// GigaChat alone still gets a timeout, so that a hang leaves the OCR time.
	gigaPhotos := providers.Backend{Name: "gigachat", Provider: gigaOne, Timeout: cfg.LLM.PrimaryTimeout}
	if cfg.Vision == nil {
		slog.Warn("Qwen is off: GigaChat reads the photos")
		m.vision = providers.NewFallback(cfg.LLM.Cooldown, gigaPhotos)
	} else {
		qwen, err := aistudio.New(*cfg.Vision)
		if err != nil {
			return models{}, fmt.Errorf("initialize Qwen: %w", err)
		}
		gigaPhotos.Timeout = 0
		m.vision = providers.NewFallback(cfg.LLM.Cooldown,
			providers.Backend{Name: "qwen", Provider: qwen, Timeout: cfg.LLM.PrimaryTimeout},
			gigaPhotos,
		)
	}

	slog.Info("models", "text_primary", cfg.LLM.Primary, "yandexgpt", cfg.YandexGPT != nil, "qwen", cfg.Vision != nil,
		"primary_timeout", cfg.LLM.PrimaryTimeout, "cooldown", cfg.LLM.Cooldown, "documents_at_once", m.slots)
	return m, nil
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
