package main

import (
	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"os"
	"path/filepath"
	"testing"
)

func TestExportPDFImages(t *testing.T) {
	dir := t.TempDir()
	id := uuid.New().String()
	if err := exportImages(dir, []material.ImageAsset{{ID: id, Data: []byte("png"), SourceName: "source.pdf", SourcePage: 2}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".png"))
	if err != nil || string(data) != "png" {
		t.Fatal("missing crop", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "catalog.json")); err != nil {
		t.Fatal(err)
	}
	if err := exportImages(dir, []material.ImageAsset{{ID: "../outside"}}); err == nil {
		t.Fatal("invalid asset filename accepted")
	}
}
