package usecase

import (
	"fmt"
	"testing"
	"time"

	fsrs "github.com/open-spaced-repetition/go-fsrs"
)

// Distinct due times reproduce the full-set scans previously triggered by each
// individual review. Keep setup outside the measured region.
func BenchmarkBuildRetentionStaggered(b *testing.B) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	for _, size := range []int{100, 1000, 3000, 10000} {
		b.Run(fmt.Sprintf("facts_%d", size), func(b *testing.B) {
			facts := staggeredRetentionFacts(size, now)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := buildRetention(b.Context(), facts, now)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(result.Points)), "points/op")
			}
		})
	}
}

func staggeredRetentionFacts(size int, now time.Time) []retentionFact {
	facts := make([]retentionFact, size)
	for i := range facts {
		facts[i] = retentionFact{available: true, card: fsrs.Card{
			State: fsrs.Review, Stability: 2, Difficulty: 5, Reps: 2,
			LastReview: now.Add(-48 * time.Hour),
			Due:        now.Add(time.Duration(i+1) * (12 * time.Hour / time.Duration(size+1))),
		}}
	}
	return facts
}
