package http

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// SPAHandler serves static files from the provided filesystem,
// falling back to index.html for client-side routing.
type SPAHandler struct {
	fileSystem fs.FS
	fileServer http.Handler
}

// NewSPAHandler constructs a new SPAHandler with the given filesystem.
func NewSPAHandler(fileSystem fs.FS) *SPAHandler {
	return &SPAHandler{
		fileSystem: fileSystem,
		fileServer: http.FileServer(http.FS(fileSystem)),
	}
}

func (h *SPAHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		// If file not found, serve index.html for React Router
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

	h.fileServer.ServeHTTP(w, r)
}

func (h *SPAHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
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
