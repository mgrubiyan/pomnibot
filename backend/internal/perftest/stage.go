package perftest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	transport "github.com/mgrubiyan/pomnibot/backend/internal/transport/http"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// StageResult includes latency, correctness and database diagnostics for one load level.
type StageResult struct {
	Name                string                     `json:"name"`
	Users               int                        `json:"users"`
	Rate                int                        `json:"scheduled_rps"`
	Passed              bool                       `json:"passed"`
	StartupPassed       bool                       `json:"startup_passed"`
	SteadyPassed        bool                       `json:"steady_passed"`
	Startup             measurements               `json:"startup"`
	Steady              measurements               `json:"steady"`
	Journeys            int64                      `json:"completed_journeys"`
	CompletedOperations int64                      `json:"completed_scheduled_operations"`
	Missed              int64                      `json:"missed_deadlines"`
	Verification        string                     `json:"verification"`
	Postgres            string                     `json:"postgres"`
	Pool                map[string]any             `json:"pool"`
	Settings            json.RawMessage            `json:"postgres_settings"`
	Samples             []dbSample                 `json:"samples"`
	Queries             json.RawMessage            `json:"queries"`
	Plans               map[string]json.RawMessage `json:"explain_analyze_buffers"`
}

func runStage(ctx context.Context, cfg Options, name string, n, rate int, d time.Duration) (StageResult, error) {
	result := StageResult{Name: name, Users: n, Rate: rate}
	setup, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(setup, "postgres:18-alpine", postgres.WithDatabase("pomnibot_perf"), postgres.WithUsername("perf"), postgres.WithPassword("disposable"), postgres.BasicWaitStrategies(), testcontainers.WithCmd("postgres", "-c", "shared_preload_libraries=pg_stat_statements", "-c", "track_io_timing=on"))
	if err != nil {
		return result, fmt.Errorf("disposable PostgreSQL required (no fallback): %w", err)
	}
	defer func() { _ = container.Terminate(context.Background()) }()
	dbURL, err := container.ConnectionString(setup, "sslmode=disable")
	if err != nil {
		return result, err
	}
	if err = repository.RunMigrations(setup, dbURL); err != nil {
		return result, err
	}
	// Same pgxpool.New defaults as internal/app. Never read DATABASE_URL or .env.
	pool, err := pgxpool.New(setup, dbURL)
	if err != nil {
		return result, err
	}
	defer pool.Close()
	observerCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return result, err
	}
	observerCfg.MaxConns = 1
	observer, err := pgxpool.NewWithConfig(setup, observerCfg)
	if err != nil {
		return result, err
	}
	defer observer.Close()
	if _, err = observer.Exec(setup, "CREATE EXTENSION pg_stat_statements"); err != nil {
		return result, err
	}
	if err = seed(setup, pool); err != nil {
		return result, err
	}
	pc := pool.Config()
	result.Pool = map[string]any{"max_conns": pc.MaxConns, "min_conns": pc.MinConns, "max_conn_lifetime": pc.MaxConnLifetime.String(), "max_conn_idle_time": pc.MaxConnIdleTime.String(), "health_check_period": pc.HealthCheckPeriod.String()}
	if err = observer.QueryRow(setup, "SELECT version()").Scan(&result.Postgres); err != nil {
		return result, err
	}
	if err = observer.QueryRow(setup, `SELECT json_agg(s) FROM (SELECT name,setting,unit FROM pg_settings WHERE name IN ('max_connections','shared_buffers','work_mem','effective_cache_size','fsync','synchronous_commit','shared_preload_libraries','track_io_timing')) s`).Scan(&result.Settings); err != nil {
		return result, err
	}
	q := db.New(pool)
	users := usecase.NewUserService(q)
	store := repository.NewManualStore(pool)
	handler := transport.NewAPIHandler(usecase.NewSetService(q, users, store), usecase.NewCardService(q, users, usecase.WithTransactions(store), usecase.WithManualCards(store)), usecase.NewHomescreenService(q, users), users)
	router, err := transport.NewRouter(handler, staticFS, maxToken, users, nil)
	if err != nil {
		return result, err
	}
	server := httptest.NewServer(router)
	defer server.Close()
	httpTransport := &http.Transport{MaxIdleConns: n * 6, MaxIdleConnsPerHost: n * 6, MaxConnsPerHost: n * 6}
	defer httpTransport.CloseIdleConnections()
	w := &workload{server: server, client: &http.Client{Transport: httpTransport, Timeout: cfg.Timeout}, owner: identity(-1)}
	if err = w.entry(ctx, nil, w.owner); err != nil {
		return result, err
	}
	for i := 0; i < n; i++ {
		w.actors = append(w.actors, identity(i))
	}
	startup := newRecorder()
	start := time.Now()
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for _, a := range w.actors {
		wg.Go(func() {
			err := w.entry(ctx, startup, a)
			if err == nil {
				err = w.burst(ctx, startup, a)
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	result.Startup = startup.summary(time.Since(start))
	result.StartupPassed = result.Startup.passes()
	for e := range errs {
		if e != nil {
			result.StartupPassed = false
		}
	}
	// Existing study progress on the other two sets, retaining a truly new shared-set join.
	for _, a := range w.actors {
		if a.id == uuid.Nil {
			continue
		}
		if _, err = pool.Exec(ctx, `INSERT INTO user_sets(user_id,set_id) SELECT $1,id FROM sets WHERE id<>$2::uuid ON CONFLICT DO NOTHING`, a.id, setID(1)); err != nil {
			return result, err
		}
		if _, err = pool.Exec(ctx, `INSERT INTO user_fact_progress(user_id,fact_id,step,stability,difficulty,next_review_at) SELECT $1,id,1,2.3065,5.0,'2020-01-01' FROM facts WHERE set_id<>$2::uuid`, a.id, setID(1)); err != nil {
			return result, err
		}
	}
	warmEnd := time.Now().Add(cfg.Warmup)
	for time.Now().Before(warmEnd) {
		if err = w.burst(ctx, nil, w.actors[0]); err != nil {
			return result, fmt.Errorf("warm-up: %w", err)
		}
	}
	if _, err = observer.Exec(ctx, "SELECT pg_stat_statements_reset()"); err != nil {
		return result, err
	}
	first, err := sample(ctx, pool, observer)
	if err != nil {
		return result, err
	}
	result.Samples = append(result.Samples, first)
	stop := make(chan struct{})
	done := make(chan struct{})
	var monitorErr error
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				v, e := sample(ctx, pool, observer)
				if e != nil {
					monitorErr = e
					return
				}
				result.Samples = append(result.Samples, v)
			}
		}
	}()
	steady := newRecorder()
	start = time.Now()
	if rate == 0 {
		result.Journeys = w.study(ctx, steady, d, cfg.Think)
	} else {
		result.CompletedOperations, result.Missed = w.rate(ctx, steady, d, rate, cfg.Deadline)
	}
	// A final measured refresh also covers short smoke stages.
	_ = w.leaderboard(ctx, steady)
	result.Steady = steady.summary(time.Since(start))
	close(stop)
	<-done
	if monitorErr != nil {
		return result, monitorErr
	}
	last, err := sample(ctx, pool, observer)
	if err != nil {
		return result, err
	}
	result.Samples = append(result.Samples, last)
	result.SteadyPassed = result.Steady.passes() && result.Missed == 0
	if rate == 0 && (result.Journeys != int64(n) || !allActorsAnswered(w.actors)) {
		result.SteadyPassed = false
	}
	if err = observer.QueryRow(ctx, `SELECT COALESCE(json_agg(s),'[]'::json) FROM (SELECT query,calls,total_exec_time,mean_exec_time,max_exec_time,rows,shared_blks_hit,shared_blks_read,shared_blk_read_time,shared_blk_write_time FROM pg_stat_statements WHERE dbid=(SELECT oid FROM pg_database WHERE datname=current_database()) ORDER BY total_exec_time DESC LIMIT 20) s`).Scan(&result.Queries); err != nil {
		return result, err
	}
	if err = verify(ctx, pool, w); err != nil {
		result.Verification = err.Error()
	} else {
		result.Verification = "passed"
	}
	result.Passed = result.SteadyPassed && result.Verification == "passed"
	if rate == 0 {
		result.Passed = result.Passed && result.StartupPassed
	}
	result.Plans, err = explain(ctx, observer, w.actors[0].id)
	return result, err
}

func explain(ctx context.Context, pool *pgxpool.Pool, userID uuid.UUID) (map[string]json.RawMessage, error) {
	// Read the actual repository SQL so the diagnostic plan stays aligned with production.
	data, err := repository.QueriesFS.ReadFile("queries/sets.sql")
	if err != nil {
		return nil, err
	}
	plans := map[string]json.RawMessage{}
	for _, name := range []string{"GetSetLeaderboard", "GetUserSetRating"} {
		_, query, ok := strings.Cut(string(data), "-- name: "+name+" :")
		if !ok {
			return nil, fmt.Errorf("query %s missing", name)
		}
		_, query, _ = strings.Cut(query, "\n")
		query, _, _ = strings.Cut(query, "-- name:")
		query = strings.ReplaceAll(query, "sqlc.arg('set_id')", "$1")
		query = strings.ReplaceAll(query, "sqlc.arg('user_id')", "$2")
		args := []any{setID(1)}
		if name == "GetUserSetRating" {
			args = append(args, userID)
		}
		var plan json.RawMessage
		if err = pool.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&plan); err != nil {
			return nil, err
		}
		plans[name] = plan
	}
	return plans, nil
}

func allActorsAnswered(actors []*actor) bool {
	for _, a := range actors {
		if a.submitted.Load() == 0 {
			return false
		}
	}
	return len(actors) > 0
}
