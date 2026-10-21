package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/processingcache"
)

func TestPrivateReadingSnapshotSurvivesGeneratorChangeButNotReaderOrContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reading.json")
	r := ingest.Result{Text: "raw reading", Pages: []ingest.PageInfo{{Issues: []string{"source cropped"}}}}
	if err := saveReading(path, "user/content/reader/prompt/schema", r); err != nil {
		t.Fatal(err)
	}
	got, ok := loadReading(path, "user/content/reader/prompt/schema")
	if !ok || got.Text != r.Text {
		t.Fatal("successful reading not reused")
	}
	if _, ok = loadReading(path, "different-reader"); ok {
		t.Fatal("stale reading reused")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private reading is readable by others")
	}
	r.Pages[0].ReadFailed = true
	if err = saveReading(path, "failed", r); err != nil {
		t.Fatal(err)
	}
	if _, ok = loadReading(path, "failed"); ok {
		t.Fatal("failed reading cached")
	}
}

func TestWriterModelEnvironmentDoesNotInvalidateAnotherReader(t *testing.T) {
	t.Setenv("YC_LLM_MODEL", "reader-a")
	t.Setenv("OPENROUTER_MODEL", "writer-a")
	first := processingcache.Key(readerEnvironment("qwen"))
	t.Setenv("OPENROUTER_MODEL", "writer-b")
	if processingcache.Key(readerEnvironment("qwen")) != first {
		t.Fatal("writer-only model change invalidated successful reader")
	}
	t.Setenv("YC_LLM_MODEL", "reader-b")
	if processingcache.Key(readerEnvironment("qwen")) == first {
		t.Fatal("reader model change reused stale reading")
	}
}
