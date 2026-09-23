// Package main provides a lightweight HTTP server serving embedded SPA assets.
package main

import (
	"context"
	"embed"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/bot"
)

//go:embed all:dist
var embeddedDist embed.FS

func getFileSystem() (fs.FS, error) {
	if staticDir := os.Getenv("STATIC_DIR"); staticDir != "" {
		slog.Info("serving static files from local directory", "dir", staticDir)
		return os.DirFS(staticDir), nil
	}

	distFS, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return nil, err
	}
	slog.Info("serving static files from embedded filesystem")
	return distFS, nil
}

type spaHandler struct {
	fileSystem fs.FS
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean up request path
	cleanPath := path.Clean(r.URL.Path)
	if cleanPath == "/" {
		cleanPath = "index.html"
	} else {
		cleanPath = strings.TrimPrefix(cleanPath, "/")
	}

	// Attempt to open the requested file
	file, err := h.fileSystem.Open(cleanPath)
	if err != nil {
		// If file not found, serve index.html (SPA Fallback)
		h.serveIndex(w, r)
		return
	}
	defer func() { _ = file.Close() }()

	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		h.serveIndex(w, r)
		return
	}

	// Cache headers: immutable for versioned static assets, no-cache for index.html
	if strings.HasPrefix(cleanPath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else if cleanPath == "index.html" {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}

	http.FileServer(http.FS(h.fileSystem)).ServeHTTP(w, r)
}

func (h *spaHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	indexFile, err := h.fileSystem.Open("index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusNotFound)
		return
	}
	defer func() { _ = indexFile.Close() }()

	stat, err := indexFile.Stat()
	if err != nil {
		http.Error(w, "failed to stat index.html", http.StatusInternalServerError)
		return
	}

	seeker, ok := indexFile.(io.ReadSeeker)
	if !ok {
		content, err := io.ReadAll(indexFile)
		if err != nil {
			http.Error(w, "failed to read index.html", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
		return
	}

	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	http.ServeContent(w, r, "index.html", stat.ModTime(), seeker)
}

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *responseRecorder) WriteHeader(statusCode int) {
	rec.statusCode = statusCode
	rec.ResponseWriter.WriteHeader(statusCode)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)

		if r.URL.Path == "/health" {
			return
		}

		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.statusCode,
			"duration", time.Since(start).String(),
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		)
	})
}

type apiService struct{}

func (s *apiService) GetHealth(_ context.Context) (*contracts.HealthResponse, error) {
	return &contracts.HealthResponse{
		Status: "ok",
	}, nil
}

func setupRouter(staticFS fs.FS) (http.Handler, error) {
	mux := http.NewServeMux()

	apiServer, err := contracts.NewServer(&apiService{})
	if err != nil {
		return nil, err
	}
	mux.Handle("/health", apiServer)

	// SPA & static files handler
	mux.Handle("/", &spaHandler{fileSystem: staticFS})

	return loggingMiddleware(mux), nil
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

	router, err := setupRouter(staticFS)
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
