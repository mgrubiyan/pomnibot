# Retention forecast sampling

`BenchmarkBuildRetentionStaggered` isolates forecast CPU work from SQL and JSON
encoding. Every fact is learned and available, with a distinct due time spread
over the next twelve hours. Setup is outside the measured region. The benchmark
reports time, allocations, and output points for 100, 1,000, 3,000, and 10,000 facts.

Run from the repository root:

```sh
go -C backend test ./internal/usecase -run '^$' \
  -bench '^BenchmarkBuildRetentionStaggered$' -benchmem -benchtime=1x -count=3
```

For longer measurements, replace `-benchtime=1x` with `-benchtime=1s`. Keep other
builds and tests idle while comparing results. There are no machine-dependent
timing thresholds in unit tests.

Local results on an AMD Ryzen 7 5825U, Linux amd64, Go 1.27.2-X:nodwarf5;
medians of three runs with `-benchtime=1x -count=3`. The baseline is commit
`066d247`, using the same fixture and benchmark with the old function signature.

| Facts | Before (ms/op) | After (ms/op) | Speedup | Points before / after |
| ---: | ---: | ---: | ---: | ---: |
| 100 | 12.095 | 1.479 | 8.2x | 426 / 35 |
| 1,000 | 1,092.179 | 13.514 | 80.8x | 4,026 / 35 |
| 3,000 | 9,798.065 | 42.266 | 231.8x | 12,026 / 35 |
| 10,000 | not measured | 143.680 | — | not measured / 35 |

The forecast now samples a fixed twelve-hour grid plus at most two additional
review times per interval. Each sampled event can have a before/after pair, so
the thirteen-day response has at most 158 points regardless of the fact count.
Once an interval's event budget is exhausted, each card advances independently
through all skipped reviews at its exact FSRS due times. Full-set work is
`O(facts * maximum_display_points + reviews)`, with a constant display budget.
Dense clusters have fewer visible jumps; probabilities at emitted samples and
the final forecast still include every scheduled review.

Unit tests enforce the point limit for clustered and horizon-wide due times,
check the fixed grid, compare every emitted probability against an independent
per-card replay, and cover source immutability, unavailable facts, new cards,
simultaneous/overdue reviews, and a review exactly at the horizon. Cancellation
is tested after the database query, during CPU work, and for an expired deadline.
