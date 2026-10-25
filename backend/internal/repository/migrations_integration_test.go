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
				// Real accounts carry a platform ID and must not be taken as agreeing.
				if _, err = pool.Exec(ctx, "INSERT INTO users (first_name, max_id, tg_id) VALUES ('Real MAX', 777, NULL), ('Real Telegram', NULL, 888)"); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := repository.RunMigrations(ctx, url); err != nil {
					t.Fatal(err)
				}
				var version, applied int
				if err := pool.QueryRow(ctx, "SELECT MAX(version_id), COUNT(*) FILTER (WHERE version_id IN (14, 15, 16, 17, 18, 19, 20) AND is_applied) FROM goose_db_version").Scan(&version, &applied); err != nil || version != 20 || applied != 7 {
					t.Fatalf("migration version=%d applications=%d error=%v", version, applied, err)
				}
				var nonemptyWarnings int
				if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM sets WHERE warnings IS NULL OR cardinality(warnings) != 0").Scan(&nonemptyWarnings); err != nil || nonemptyWarnings != 0 {
					t.Fatalf("existing sets have nonempty warnings: %d, error=%v", nonemptyWarnings, err)
				}
				var illustrated int
				if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM cards WHERE question_svg IS NOT NULL OR explanation_svg IS NOT NULL").Scan(&illustrated); err != nil || illustrated != 0 {
					t.Fatalf("old cards gained illustrations: %d %v", illustrated, err)
				}
				var reports int
				if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM set_processing_reports").Scan(&reports); err != nil || reports != 0 {
					t.Fatalf("reports=%d error=%v", reports, err)
				}
				var demo, demoAgreed, platform, platformAgreed int
				if err := pool.QueryRow(ctx, `SELECT
					COUNT(*) FILTER (WHERE max_id IS NULL AND tg_id IS NULL),
					COUNT(*) FILTER (WHERE max_id IS NULL AND tg_id IS NULL AND data_consent_at IS NOT NULL),
					COUNT(*) FILTER (WHERE max_id IS NOT NULL OR tg_id IS NOT NULL),
					COUNT(*) FILTER (WHERE (max_id IS NOT NULL OR tg_id IS NOT NULL) AND data_consent_at IS NOT NULL)
					FROM users`).Scan(&demo, &demoAgreed, &platform, &platformAgreed); err != nil || demoAgreed != demo || platformAgreed != 0 {
					t.Fatalf("consent: demo %d/%d agreed, platform %d/%d agreed, error=%v", demoAgreed, demo, platformAgreed, platform, err)
				}
				// The demo sets' author is a real MAX account, so it stays out too.
				if fromMain && (demo != 31 || platform != 3) {
					t.Fatalf("consent fixture: demo=%d platform=%d", demo, platform)
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
