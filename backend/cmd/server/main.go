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
		slog.Error("PORT environment variable not set")
		os.Exit(1)
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
	gigachatCfg, err := gigachat.ConfigFromEnv()
	if err != nil {
		slog.Error("failed to get gigachat config", "error", err)
		os.Exit(1)
	}
	gigachatClient, err = gigachat.New(gigachatCfg)
	if err != nil {
		slog.Error("failed to create gigachat client", "error", err)
		os.Exit(1)
	}

	answerGrader := &grader.Grader{}
	answerGrader.Model = gigachatClient

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL is not set")
		os.Exit(1)
	}

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

	botToken := os.Getenv("BOT_TOKEN")
	if botToken == "" {
		slog.Error("BOT_TOKEN is not set")
		os.Exit(1)
	}

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

	if userService == nil || setService == nil {
		slog.Error("cannot start MAX bot without database and usecase services")
		os.Exit(1)
	}

	domain := os.Getenv("DOMAIN")
	if domain == "" {
		slog.Error("cannot start MAX bot without domain")
		os.Exit(1)
	}
	appURL := "https://" + domain

	apiURL := os.Getenv("MAX_API_URL")
	if apiURL == "" {
		slog.Error("cannot start MAX bot without api url")
		os.Exit(1)
	}

	botClient, err := bot.NewClient(botToken, apiURL)
	if err != nil {
		slog.Error("failed to create bot client", "error", err)
		os.Exit(1)
	}

	// Yandex OCR turns photos upright and checks GigaChat's reading, or
	// reads them alone without GigaChat.
	yandexCfg, err := yandex.ConfigFromEnv()
	if err != nil {
		slog.Error("failed to read yandex OCR config", "error", err)
		os.Exit(1)
	}

	yandexClient, err := yandex.New(yandexCfg)
	if err != nil {
		slog.Error("failed to initialize yandex OCR client", "error", err)
		os.Exit(1)
	}
	slog.Info("yandex OCR client initialized")

	// GigaChat reads photos and scans, Yandex OCR turns them upright
	// and checks GigaChat's reading.
	ocr := &ingest.VisionOCR{Model: gigachatClient, Checker: yandexClient}
	slog.Info("photos and scans are read by GigaChat")

	extractor, err := ingest.NewExtractor(ocr, ingest.Options{})
	if err != nil {
		slog.Error("extractor initialized with PDF disabled", "error", err)
		os.Exit(1)
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
		os.Exit(1)
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
