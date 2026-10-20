# Capacity measurement — 21 October 2026

The optimized API passes the five-minute **50-user** study scenario with correct persisted results and zero HTTP errors. The repeated 50-user startup in the shorter sweep fails the p95 threshold (559 ms), so 50-user entry is **borderline, not consistently demonstrated**. The 100-user scenario fails latency thresholds. The highest concurrency with a passing complete five-minute run is **50 users**; the highest passing level in the separate concurrency sweep is **25 users**. Sustainable tested scheduled load rises from **100 to 400 offered RPS** (398.5 achieved RPS after optimization). These scheduled operations do not represent 400 simultaneous students.

For the demo, the API/database path is substantially improved but should not receive an unconditional readiness sign-off for a simultaneous 50-user start. Repeat on demo-equivalent hardware; consider staggered entry. AI grading, document processing, browser rendering, production logging, TLS, and production networking remain unmeasured. No deployment or merge was performed.

## Environment and reproduction

Base application commit: `1f23904ca0c0a551ce94dd2bf2b5de2136296f8f`; baseline uses its unmodified application code, optimized runs include this PR’s SQL and batching changes. Both use the same runner workload and default pool settings. Full commands and methodology: [README](README.md).

Hardware: AMD Ryzen 7 5825U with Radeon Graphics, 16 logical CPUs, 15.02 GiB RAM, linux/amd64. Go: `go1.27.2-X:nodwarf5,simd`. PostgreSQL: **17.11**, `postgres:17-alpine` on the same Docker host. Application pool: 16 maximum, 0 minimum connections, 1 h maximum lifetime, 30 min idle timeout, 1 min health checks. PostgreSQL has 100 maximum connections, 128 MiB shared buffers, 4 MiB work memory, fsync and synchronous commit enabled. No pool tuning or indexes were added.

```sh
task perf -- -out ../docs/perf/baseline.json
task perf -- -only rate -out ../docs/perf/baseline-rate.json
# Apply the SQL and batching changes, regenerate, then repeat:
task perf -- -out ../docs/perf/optimized.json
```

The first baseline run incorrectly coupled the rate sweep’s stop condition to startup latency. The separate baseline-rate artifact completes the sustainable-RPS sweep with startup reported independently, as the final runner does. It also checks scheduler lateness when each goroutine starts, in addition to dispatch. Durations, request mix, timeouts, fixtures, think time, and pool defaults are unchanged. The stopped sweeps intentionally omit higher untested loads.

## Five-minute study results

Every operation must meet p95 ≤500 ms and p99 ≤1,000 ms. Overall percentiles below combine operations; the per-operation table is decisive. Setup and warm-up are excluded.

| Version | Users | Startup | Steady | HTTP RPS | Overall p50/p95/p99 ms | Errors | Journeys | Stored data |
|---|---:|---|---|---:|---|---:|---:|---|
| Baseline | 50 | Fail | Fail | 21.3 | 566/1125/1205 | 0 | 50 | passed |
| Baseline | 100 | Fail | Fail | 39.8 | 959/2225/2303 | 0 | 100 | passed |
| Optimized | 50 | Pass | Pass | 22.4 | 21/139/410 | 0 | 50 | passed |
| Optimized | 100 | Fail | Fail | 44.7 | 27/460/879 | 0 | 100 | passed |

Per-operation steady p95/p99 (ms); machine-readable artifacts also retain per-operation p50, RPS, counts, and errors.

| Operation | Baseline 50 | Optimized 50 | Baseline 100 | Optimized 100 |
|---|---:|---:|---:|---:|
| cards | 1228/1253 | 399/419 | 2379/2593 | 869/885 |
| check | 289/294 | 19/27 | 46/60 | 56/199 |
| feed | 988/1029 | 117/325 | 1856/1912 | 376/671 |
| home | 961/980 | 401/420 | 1802/1818 | 871/885 |
| leaderboard | 249/313 | 62/88 | 1033/1160 | 91/113 |
| results | 210/347 | 121/242 | 637/668 | 366/639 |
| set | 887/989 | 178/375 | 1635/1792 | 425/743 |

Startup at 50 users:

| Operation | Baseline p95/p99 ms | Optimized five-minute run | Optimized short repeat |
|---|---:|---:|---:|
| cards | 1183/1189 | 449/453 | 558/563 |
| feed | 688/754 | 333/339 | 431/438 |
| home | 894/898 | 449/455 | 557/557 |
| join | 121/150 | 123/129 | 124/153 |
| profile | 56/61 | 53/54 | 52/54 |
| save_profile | 47/60 | 48/50 | 51/53 |
| set | 931/973 | 400/403 | 503/521 |

## Capacity sweeps

Concurrency sweep: baseline passes 10 and fails 25; optimized passes 10 and 25, then fails 50 on startup while its steady phase passes. Each thirty-second study stage has zero HTTP errors and verifies stored data. Stages 100/200/400 are skipped after the first failure; the separate five-minute 100-user headroom run is still recorded.

Scheduled-rate stages use 100 authenticated students and assess steady state independently of their slow startup. Worst per-operation percentiles determine the result. The final teacher refresh and draining in-flight requests are included in achieved RPS.

| Version | Offered RPS | Achieved RPS | Worst operation p95/p99 ms | HTTP errors | Missed schedules | Result |
|---|---:|---:|---:|---:|---:|---|
| Baseline | 25 | 24.8 | 162/168 | 0 | 0 | Pass |
| Baseline | 50 | 49.6 | 150/159 | 0 | 0 | Pass |
| Baseline | 100 | 99.2 | 167/331 | 0 | 0 | Pass |
| Baseline | 200 | 197.9 | 3545/4188 | 0 | 0 | Fail |
| Optimized | 25 | 25.0 | 53/59 | 0 | 0 | Pass |
| Optimized | 50 | 49.9 | 53/63 | 0 | 0 | Pass |
| Optimized | 100 | 99.7 | 129/233 | 0 | 0 | Pass |
| Optimized | 200 | 199.4 | 68/307 | 0 | 0 | Pass |
| Optimized | 400 | 398.5 | 448/622 | 0 | 0 | Pass |
| Optimized | 800 | 560.8 | 1506/1712 | 0 | 6892 | Fail |

## Why these changes

At baseline 50 users, `GetUserSetRating` consumed 199.9 s accumulated database execution time (222.1 ms/call); the next largest query consumed 9.9 s. `EXPLAIN (ANALYZE, BUFFERS)` showed hashing roughly 158,000 progress rows and expanding participants across all 50 facts. Ranking now aggregates existing ELO deltas per user first, then applies the default 700 for missing facts. Authors, outsiders, ties, low scores, and empty sets retain their semantics.

The baseline also issued 153,000 separate option/table detail queries during the five-minute 50-user run. Card lists now fetch each detail type once for the authorized card IDs and reuse the existing card mappers through a request-local cache. A 20-card regression first failed with 30 queries and now passes with three batch queries; the PostgreSQL round-trip test preserves all five card kinds and ordered table data. No cross-request cache or authorization change was introduced.

At optimized 50 users, ranking averages 51.2 ms/call and its diagnostic plan takes 63.4 ms versus 160.2 ms before. Aggregate pool waiting falls from 3,044.4 s to 131.3 s across concurrent requests. The remaining ranking cost and pool contention explain the limited concurrency margin; the short repeated startup and 100-user results are retained rather than extrapolated away. Both changes were applied as one measured optimization iteration and the full workload was rerun.

## Validation and artifacts

`task lint`, `task test`, opt-in lint (`golangci-lint run --build-tags perf ./...`), and race checks (`go test -race -tags perf ./cmd/perf ./internal/usecase`) pass. `task gen` regenerated contracts/SQL; SQL outputs were finalized and checked with sqlc **v1.31.1**, matching repository headers. API contracts and migrations are unchanged.

Every measured stage, including failing latency/overload stages, passes persisted-data verification: distinct platform identities, all memberships, exact successful answer counts, per-fact progress, historical answers, and independent leaderboard scores/order. Failed stages fail on latency or missed schedules, not silently dropped data.

Artifacts: [baseline](baseline.json), [baseline scheduled-rate sweep](baseline-rate.json), [optimized](optimized.json). They include pool wait/utilization samples, PostgreSQL activity/lock/transaction snapshots, effective settings, query timings, and diagnostic plans. One-second sampling can miss brief peaks; transaction counters can lag. The host was shared, so timing variability remains a limit. These observations qualify capacity only for this hardware, fixture distribution, and workload.
