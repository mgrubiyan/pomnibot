//go:build perf

package main

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type operation struct {
	Count   int       `json:"count"`
	Errors  int       `json:"errors"`
	RPS     float64   `json:"rps"`
	P50     float64   `json:"p50_ms"`
	P95     float64   `json:"p95_ms"`
	P99     float64   `json:"p99_ms"`
	Samples []float64 `json:"-"`
}
type measurements struct {
	Seconds    float64              `json:"seconds"`
	Overall    operation            `json:"overall"`
	Operations map[string]operation `json:"operations"`
	Failures   []string             `json:"failure_examples,omitempty"`
}
type recorder struct {
	sync.Mutex
	ops      map[string]operation
	failures []string
}

func newRecorder() *recorder { return &recorder{ops: map[string]operation{}} }
func (r *recorder) add(name string, d time.Duration, err error) {
	r.Lock()
	defer r.Unlock()
	o := r.ops[name]
	o.Count++
	o.Samples = append(o.Samples, float64(d)/float64(time.Millisecond))
	if err != nil {
		o.Errors++
		if len(r.failures) < 10 {
			r.failures = append(r.failures, name+": "+err.Error())
		}
	}
	r.ops[name] = o
}
func summarize(o operation, seconds float64) operation {
	sort.Float64s(o.Samples)
	pct := func(p float64) float64 {
		if len(o.Samples) == 0 {
			return 0
		}
		return o.Samples[int(math.Ceil(float64(len(o.Samples))*p))-1]
	}
	o.P50 = pct(.5)
	o.P95 = pct(.95)
	o.P99 = pct(.99)
	o.RPS = float64(o.Count) / seconds
	return o
}
func (r *recorder) summary(d time.Duration) measurements {
	r.Lock()
	defer r.Unlock()
	m := measurements{Seconds: d.Seconds(), Operations: map[string]operation{}, Failures: r.failures}
	for name, o := range r.ops {
		m.Operations[name] = summarize(o, m.Seconds)
		m.Overall.Count += o.Count
		m.Overall.Errors += o.Errors
		m.Overall.Samples = append(m.Overall.Samples, o.Samples...)
	}
	m.Overall = summarize(m.Overall, m.Seconds)
	return m
}
func (m measurements) passes() bool {
	if m.Overall.Count == 0 {
		return false
	}
	for _, o := range m.Operations {
		if o.Errors > 0 || o.P95 > 500 || o.P99 > 1000 {
			return false
		}
	}
	return true
}

type dbSample struct {
	At            time.Time       `json:"at"`
	Connections   int32           `json:"connections"`
	Acquired      int32           `json:"acquired"`
	EmptyAcquires int64           `json:"empty_acquires"`
	AcquireMS     float64         `json:"acquire_ms"`
	EmptyWaitMS   float64         `json:"empty_wait_ms"`
	Canceled      int64           `json:"canceled_acquires"`
	Active        int             `json:"pg_active"`
	LockWaits     int             `json:"pg_lock_waits"`
	Stats         json.RawMessage `json:"pg_transactions"`
}

func sample(ctx context.Context, pool, observer *pgxpool.Pool) (dbSample, error) {
	s := pool.Stat()
	v := dbSample{At: time.Now().UTC(), Connections: s.TotalConns(), Acquired: s.AcquiredConns(), EmptyAcquires: s.EmptyAcquireCount(), AcquireMS: float64(s.AcquireDuration()) / float64(time.Millisecond), EmptyWaitMS: float64(s.EmptyAcquireWaitTime()) / float64(time.Millisecond), Canceled: s.CanceledAcquireCount()}
	err := observer.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='active' AND pid<>pg_backend_pid()),(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'),(SELECT row_to_json(d) FROM (SELECT xact_commit,xact_rollback,blks_read,blks_hit,tup_returned,tup_fetched,tup_inserted,tup_updated,deadlocks,temp_bytes FROM pg_stat_database WHERE datname=current_database()) d)`).Scan(&v.Active, &v.LockWaits, &v.Stats)
	return v, err
}
