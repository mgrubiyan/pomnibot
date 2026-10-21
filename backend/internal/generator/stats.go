package generator

import "time"

func addStats(a *Stats, b Stats) {
	a.CacheHits += b.CacheHits
	a.CostUSD += b.CostUSD
	if a.StageTimes == nil {
		a.StageTimes = map[string]time.Duration{}
	}
	for stage, duration := range b.StageTimes {
		a.StageTimes[stage] += duration
	}
	a.ReviewRejected += b.ReviewRejected
	a.ReviewFailures += b.ReviewFailures
	a.CoverageRetries += b.CoverageRetries
	a.Chunks += b.Chunks
	a.ChunksFiltered += b.ChunksFiltered
	a.ChunksSkipped += b.ChunksSkipped
	a.ChunksFailed += b.ChunksFailed
	a.ChunksInvalid += b.ChunksInvalid
	a.ChunksRefused += b.ChunksRefused
	a.ModelCalls += b.ModelCalls
	a.InvalidResponses += b.InvalidResponses
	a.PromptTokens += b.PromptTokens
	a.CompletionTokens += b.CompletionTokens
	a.FactsFromModel += b.FactsFromModel
	a.Facts += b.Facts
	a.CardsFromModel += b.CardsFromModel
	a.DroppedInvalid += b.DroppedInvalid
	a.DroppedQuote += b.DroppedQuote
	a.DroppedPersonal += b.DroppedPersonal
	a.DroppedUnsupported += b.DroppedUnsupported
	a.DroppedTable += b.DroppedTable
	a.DroppedExample += b.DroppedExample
	a.DroppedDuplicate += b.DroppedDuplicate
	a.DroppedByLimit += b.DroppedByLimit
	a.DroppedVariants += b.DroppedVariants
	a.DowngradedToFlip += b.DowngradedToFlip
	a.DroppedDistractors += b.DroppedDistractors
	a.InputToFlip += b.InputToFlip
	a.ExplanatoryChoiceToFlip += b.ExplanatoryChoiceToFlip
	a.Cards += b.Cards
	a.TrimmedExplanations += b.TrimmedExplanations
	if a.CallsByModel == nil {
		a.CallsByModel = map[string]int{}
	}
	for m, n := range b.CallsByModel {
		a.CallsByModel[m] += n
	}
	if a.FilteredBy == nil {
		a.FilteredBy = map[string]int{}
	}
	for m, n := range b.FilteredBy {
		a.FilteredBy[m] += n
	}
	a.ChunkTimeMax = max(a.ChunkTimeMax, b.ChunkTimeMax)
}
