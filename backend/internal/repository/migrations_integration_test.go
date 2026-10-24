package repository_test

import (
	"io/fs"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/pressly/goose/v3"
)

func TestProcessingReportsMigrationPostgres(t *testing.T) {
	for _, fromMain := range []bool{false, true} {
		name := "empty database"
		if fromMain {
			name = "main version 13"
		}
		t.Run(name, func(t *testing.T) {
			pool, url := postgresTestDatabase(t)
			ctx := t.Context()
			var before int
			if fromMain {
				migrations, err := fs.Sub(repository.MigrationsFS, "migrations")
				if err != nil {
					t.Fatal(err)
				}
				sqlDB := stdlib.OpenDBFromPool(pool)
				t.Cleanup(func() { _ = sqlDB.Close() })
				provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = provider.UpTo(ctx, 13); err != nil {
					t.Fatal(err)
				}
				version, err := provider.GetDBVersion(ctx)
				if err != nil || version != 13 {
					t.Fatalf("main version: %d, %v", version, err)
				}
				if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM cards").Scan(&before); err != nil || before == 0 {
					t.Fatalf("missing main demo data: %d, %v", before, err)
				}
			}
			for range 2 {
				if err := repository.RunMigrations(ctx, url); err != nil {
					t.Fatal(err)
				}
				var version, applied int
				if err := pool.QueryRow(ctx, "SELECT MAX(version_id), COUNT(*) FILTER (WHERE version_id IN (14, 15) AND is_applied) FROM goose_db_version").Scan(&version, &applied); err != nil || version != 15 || applied != 2 {
					t.Fatalf("migration version=%d applications=%d error=%v", version, applied, err)
				}
				var nonemptyWarnings int
				if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM sets WHERE warnings IS NULL OR cardinality(warnings) != 0").Scan(&nonemptyWarnings); err != nil || nonemptyWarnings != 0 {
					t.Fatalf("existing sets have nonempty warnings: %d, error=%v", nonemptyWarnings, err)
				}
				var reports int
				if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM set_processing_reports").Scan(&reports); err != nil || reports != 0 {
					t.Fatalf("reports=%d error=%v", reports, err)
				}
				if fromMain {
					var after int
					if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM cards").Scan(&after); err != nil || after != before {
						t.Fatalf("main cards changed: before=%d after=%d error=%v", before, after, err)
					}
				}
			}
		})
	}
}
