package usecase

import (
	"context"

	"github.com/mgrubiyan/pomnibot/backend/internal/grader"
)

// AnswerGrader checks typed answers without tying the use case to an integration.
type AnswerGrader interface {
	Check(context.Context, grader.Input) grader.Verdict
}

// LocalAnswerGrader is the offline implementation used unless a model grader is injected.
type LocalAnswerGrader struct{}

// Check evaluates the answer using only the local matching rules.
func (LocalAnswerGrader) Check(ctx context.Context, in grader.Input) grader.Verdict {
	return (&grader.Grader{}).Check(ctx, in)
}
