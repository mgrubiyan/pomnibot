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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mgrubiyan/pomnibot/backend/internal/bot"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/grader"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest/yandex"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/gigachat"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	httptransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/http"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	staticFS, err := getFileSystem()
	if err != nil {
		slog.Error("failed to initialize static filesystem", "error", err)
		os.Exit(1)
	}

	var (
		userService       usecase.UserService
		setService        usecase.SetService
		cardService       usecase.CardService
		homescreenService usecase.HomescreenService
	)

	// GigaChat writes the cards, reads photos and scans, and checks typed
	// answers by meaning.
	var gigachatClient *gigachat.Client
	if gigachatCfg, err := gigachat.ConfigFromEnv(); err == nil {
		if gigachatClient, err = gigachat.New(gigachatCfg); err != nil {
			slog.Warn("failed to initialize gigachat client", "error", err)
		}
	} else {
		slog.Warn("gigachat configuration not found in environment", "error", err)
	}
	answerGrader := &grader.Grader{}
	if gigachatClient != nil {
		answerGrader.Model = gigachatClient
	}

	if dbURL := os.Getenv("DATABASE_URL"); dbURL != "" {
		slog.Info("connecting to database and executing migrations...")
		if err := repository.RunMigrations(ctx, dbURL); err != nil {
			slog.Error("failed to run database migrations", "error", err)
			os.Exit(1)
		}

		pool, err := pgxpool.New(ctx, dbURL)
		if err != nil {
			slog.Error("failed to initialize database connection pool", "error", err)
			os.Exit(1)
		}
		defer pool.Close()

		if err := pool.Ping(ctx); err != nil {
			slog.Error("failed to ping database", "error", err)
			os.Exit(1)
		}

		queries := db.New(pool)

		userService = usecase.NewUserService(queries)
		setService = usecase.NewSetService(queries, userService)
		cardService = usecase.NewCardService(queries, userService, usecase.WithGrader(answerGrader))
		homescreenService = usecase.NewHomescreenService(queries, userService)
		slog.Info("persistence layer and usecase services wired successfully")
	} else {
		slog.Warn("DATABASE_URL is not set; running in static SPA mode with unimplemented handlers")
	}

	botToken := os.Getenv("BOT_TOKEN")

	apiHandler := httptransport.NewAPIHandler(setService, cardService, homescreenService)
	router, err := httptransport.NewRouter(apiHandler, staticFS, botToken, userService)
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

	// Initialize MAX Bot if BOT_TOKEN is provided
	if botToken := os.Getenv("BOT_TOKEN"); botToken != "" {
		if userService == nil || setService == nil {
			slog.Error("cannot start MAX bot without database and usecase services")
			os.Exit(1)
		}

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
			os.Exit(1)
		}

		// Yandex OCR turns photos upright and checks GigaChat's reading, or
		// reads them alone without GigaChat.
		var ocr ingest.OCR
		if yandexCfg := yandex.ConfigFromEnv(); yandexCfg.APIKey != "" {
			yandexClient, err := yandex.New(yandexCfg)
			if err != nil {
				slog.Warn("failed to initialize yandex OCR client", "error", err)
			} else {
				ocr = yandexClient
				slog.Info("yandex OCR client initialized")
			}
		}
		if gigachatClient != nil {
			if ocr == nil {
				slog.Warn("photos are read by GigaChat unchecked and not turned upright: set YC_API_KEY for Yandex OCR")
			}
			ocr = &ingest.VisionOCR{Model: gigachatClient, Checker: ocr}
			slog.Info("photos and scans are read by GigaChat")
		}

		extractor, err := ingest.NewExtractor(ocr, ingest.Options{})
		if err != nil {
			slog.Warn("extractor initialized with PDF disabled", "error", err)
			extractor, _ = ingest.NewExtractor(ocr, ingest.Options{DisablePDF: true})
		}

		var cardGen bot.CardGenerator
		if gigachatClient != nil {
			gen := generator.NewGenerator(gigachatClient, generator.Options{})
			cardGen = bot.NewGeneratorAdapter(gen)
			slog.Info("gigachat card generator initialized")
		}

		maxBot, err := bot.NewBot(botClient, appURL, userService, setService, extractor, cardGen)
		if err != nil {
			slog.Error("failed to create MAX bot", "error", err)
			os.Exit(1)
		}

		if err := maxBot.Start(ctx); err != nil {
			slog.Error("failed to start MAX bot", "error", err)
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
