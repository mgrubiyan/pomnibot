package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/processingcache"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/aistudio"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
)

type readingSnapshot struct {
	Key    string        `json:"key"`
	Result ingest.Result `json:"result"`
}

func readingKey(paths []string, identity any) (string, error) {
	var files []ingest.File
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		files = append(files, ingest.File{Name: filepath.Base(path), Data: []byte(material.Digest(data))})
	}
	return processingcache.Key(os.Getuid(), files, identity, ingest.ProcessingIdentity()), nil
}
func loadReading(path, key string) (ingest.Result, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ingest.Result{}, false
	}
	var s readingSnapshot
	if json.Unmarshal(data, &s) != nil || s.Key != key || !ingest.ReusableReading(s.Result) {
		return ingest.Result{}, false
	}
	return s.Result, true
}
func saveReading(path, key string, r ingest.Result) error {
	if !ingest.ReusableReading(r) {
		return nil
	}
	data, err := json.Marshal(readingSnapshot{Key: key, Result: r})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".reading-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Only the actual reader settings enter a reading key. A different writer's
// environment must not repeat successful paid reading.
func readerEnvironment(name string) []any {
	var keys []string
	var defaults []any
	switch name {
	case "openrouter":
		keys = []string{"OPENROUTER_MODEL", "OPENROUTER_BASE_URL", "OPENROUTER_MAX_TOKENS"}
		defaults = []any{openrouter.DefaultModel, openrouter.DefaultBaseURL, openrouter.DefaultMaxTokens}
	case "qwen":
		keys = []string{"YC_LLM_MODEL", "YC_LLM_BASE_URL", "YC_LLM_MAX_TOKENS"}
		defaults = []any{aistudio.DefaultModel, aistudio.DefaultBaseURL, aistudio.DefaultMaxTokens}
	default:
		keys = []string{"GIGACHAT_MODEL", "GIGACHAT_FALLBACK_MODEL", "GIGACHAT_BASE_URL", "GIGACHAT_MAX_TOKENS"}
	}
	out := append([]any{name}, defaults...)
	for _, key := range keys {
		out = append(out, key, os.Getenv(key))
	}
	return out
}
