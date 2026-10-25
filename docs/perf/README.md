# Local API and PostgreSQL capacity

For isolated retention forecast CPU benchmarks and before/after results, see
[retention forecast sampling](retention.md).

Run from the repository root with Docker available:

```sh
task perf                         # full run, writes docs/perf/results.json
task perf -- -out ../docs/perf/baseline.json
task perf -- -only rate -out ../docs/perf/baseline-rate.json
task perf -- -smoke -stage 8s -think 100ms -warmup 1s -out ../docs/perf/smoke.json
go -C backend test -tags perf ./cmd/perf
(cd backend && golangci-lint run --build-tags perf ./...)
```

The `perf` build tag keeps the runner out of ordinary unit tests and production binaries. PostgreSQL unavailability is an error. The runner creates and removes its own PostgreSQL 17 container for **each stage**, uses the actual embedded Goose migrations, and never reads `DATABASE_URL` or connects to a pre-existing application database. Run the commands without other tests or builds competing for CPU. Expect approximately fifteen minutes plus database setup for a full run. The `-only study|concurrency|rate` flag repeats a single group. Durations, answer delay, HTTP timeout, warm-up, and scheduler deadline are command flags; changing them changes the workload.

## Workload and interpretation

Fixtures replace the shipped demonstrations inside the disposable container: three shared sets, 50 facts per set, **five cards per fact** (750 cards covering flip, boolean, choice, table, and input), 1,000 background participants, 150,150 existing progress rows including the teacher, and 100,000 historical answers. Fixture IDs and historical timestamps are deterministic. Each active student has existing progress on the other two sets; joining the target set is new. Half the students use MAX and half use Telegram, with overlapping external IDs across platforms to catch identity collisions. Signatures are independently generated; the real auth middleware resolves internal users. Telegram students complete their profile through HTTP.

The simultaneous-start measurement includes profile, Telegram profile completion, shared-set join, and home/feed/set/cards bursts. Home and set-screen requests reproduce the frontend's concurrent calls (`Home.tsx`, `SetScreen.tsx`). The steady measurement follows an unmeasured warm-up and uses real loopback TCP HTTP connections, production router, local answer grader, services, SQL, and unmodified production pgx pool defaults. The study loop waits three seconds between answers, checks typed answers through `/cards/{id}/check`, submits results in batches of five, reloads home/feed/set/cards after each batch, and refreshes the teacher's leaderboard every fifteen seconds and at the end. All five card kinds appear in submitted results. UI-graded answers are correct in this workload. A completed journey means the student finished the study loop and flushed all results.

The full run measures five minutes each at 50 and 100 users, then thirty-second study stages at 10, 25, 50, 100, 200, and 400 users, stopping the concurrency sweep at its first failure. Scheduled-rate stages independently offer 25, 50, 100, 200, 400, and 800 HTTP operations per second with 100 authenticated students. The deterministic request mix is 10% home, 20% feed, 10% cards, 10% set, 20% check, 20% five-answer submission, and 10% teacher leaderboard. This deliberately includes more teacher refreshes than the study loop. A bounded in-flight limit of three times the student count prevents unbounded queues. Requests that cannot start within 100 ms of their scheduled time, or cannot acquire an in-flight slot, count as failures. In-flight work drains before measurements end; achieved RPS uses actual elapsed time, including drain. Scheduled operation counts are distinct from completed study journeys.

Each operation must have p95 ≤500 ms and p99 ≤1,000 ms with zero unexpected failures. Startup and steady state are assessed separately; a study/concurrency stage passes only when both pass, all journeys finish, and persisted data verifies. Rate stages assess steady-state thresholds, missed schedules, and persisted data independently; their startup metrics remain reported separately. This applies the same latency thresholds to the capacity sweeps. RPS is completed HTTP attempts per actual second; errors are included in attempt counts and shown separately. Percentiles use nearest rank, including failed request durations. Setup, warm-up, verification, and diagnostic `EXPLAIN` runs are excluded from latency and RPS measurements.

The JSON artifacts retain per-operation counts/RPS/p50/p95/p99/errors and failure examples; PostgreSQL/app versions and host CPU/memory; effective pool and PostgreSQL settings; one-second samples of pool utilization, acquire/wait counters, PostgreSQL activity/lock waits/transaction counters; the twenty queries with greatest accumulated execution time from `pg_stat_statements`; and the actual ranking SQL's `EXPLAIN (ANALYZE, BUFFERS)` plans. Pool wait and transaction fields are cumulative counters: subtract the first steady sample from the last. Query timings are reset after warm-up and collected before verification. One observer connection collects telemetry separately from the application pool. Sampling can miss brief locks or utilization peaks. PostgreSQL transaction statistics can lag activity briefly.

Verification checks the signed platform-to-internal identity mapping, distinct users, membership of all three sets, exact per-user submitted answer counts, per-fact ELO and consecutive-correct updates, unchanged historical answer counts, and every leaderboard participant/rank/percentile/order against independently calculated scores. Failed submissions can leave partial production writes; verification exposes those as a stage failure. The suite does not claim batch atomicity.

The runner discards slog output, so production log I/O is unmeasured. These results exclude external AI latency and quotas, uploads/document generation, browser rendering, TLS/Traefik, production networking, and other server workloads. They describe this machine and this workload; repeat on demo-equivalent hardware before claiming its capacity. No unlimited user count is inferred.
