package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestProcessingReportPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := context.Background()
	q := db.New(pool)
	users := usecase.NewUserService(q)
	for _, id := range []int64{11, 22} {
		if err := users.EnsureUser(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	generated := generator.Result{Artifact: &generator.Artifact{Version: generator.PipelineVersion, Files: []ingest.File{{Name: "private.txt", Data: []byte("private source")}}}}
	set, _, err := repository.SaveGeneratedSet(ctx, q, 11, "private", repository.GeneratedSet{Report: generated})
	if err != nil {
		t.Fatal(err)
	}
	data, err := q.GetProcessingReport(ctx, db.GetProcessingReportParams{SetID: set.ID, AuthorID: 11})
	if err != nil {
		t.Fatal(err)
	}
	var restored generator.Result
	if err = json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if string(restored.Artifact.Files[0].Data) != "private source" {
		t.Fatal("original lost")
	}
	if _, err = q.JoinSet(ctx, db.JoinSetParams{SetID: set.ID, UserID: 22}); err != nil {
		t.Fatal(err)
	}
	if _, err = q.GetProcessingReport(ctx, db.GetProcessingReportParams{SetID: set.ID, AuthorID: 22}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("member accessed private original: %v", err)
	}
	if _, err = pool.Exec(ctx, "DELETE FROM sets WHERE id=$1", set.ID); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM set_processing_reports WHERE set_id=$1", set.ID).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("private report survived deletion: %d %v", remaining, err)
	}
}
