// Package grader checks a student's typed answer to a card. The answer need
// not match word for word: a local check forgives form, word order and
// typos, and a language model judges answers in other words ("массив
// должен быть упорядочен" for "на отсортированном массиве").
//
//	g := &grader.Grader{Model: llm}
//	v := g.Check(ctx, grader.Input{Question: q, Expected: a, Quote: quote, Given: typed})
package grader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/prompts"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// DefaultTimeout bounds the wait for the model: the student is looking at
// the card. On the free plan the model serves one request at a time, so a
// check may queue behind card generation.
const DefaultTimeout = 8 * time.Second

// Grader checks answers. A nil Grader, or one without a Model, uses the
// local check alone.
type Grader struct {
	Model   providers.Provider
	Timeout time.Duration // 0 means DefaultTimeout
}

// Input is a card and the student's answer to it.
type Input struct {
	Question string
	Expected string // the card's answer
	Quote    string // the notes the card is built on, for the model's context
	Given    string
}

// Method tells how a verdict was reached.
type Method string

// Methods of reaching a verdict.
const (
	MethodLocal    Method = "local"    // the local check decided
	MethodModel    Method = "model"    // the model judged
	MethodFallback Method = "fallback" // the model could not answer in time: local verdict
)

// Verdict is the result of a check. Reason, from the model, explains it to
// the student.
type Verdict struct {
	Correct bool
	Reason  string
	Method  Method
}

// Check grades the answer. It never fails: when the model cannot answer,
// the local verdict stands.
func (g *Grader) Check(ctx context.Context, in Input) Verdict {
	switch local(in.Question, in.Expected, in.Given) {
	case localMatch:
		return Verdict{Correct: true, Method: MethodLocal}
	case localMismatch:
		return Verdict{Correct: false, Method: MethodLocal}
	}
	if g == nil || g.Model == nil {
		return Verdict{Correct: false, Method: MethodLocal}
	}

	timeout := g.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	resp, err := g.Model.Complete(ctx, providers.Request{
		System: prompts.GraderSystem,
		User:   userPrompt(in, leftOut(in.Question, in.Expected, in.Given)),
		Schema: schema,
	})
	if err != nil {
		slog.Warn("grader: model failed, local verdict stands", "err", err)
		return Verdict{Correct: false, Method: MethodFallback}
	}
	v, err := parseVerdict(resp.Content)
	if err != nil {
		slog.Warn("grader: unusable model answer, local verdict stands", "err", err, "answer", string(resp.Content))
		return Verdict{Correct: false, Method: MethodFallback}
	}
	return v
}

// schema is the model's answer, passed to structured output.
var schema = json.RawMessage(`{"type":"object","properties":{` +
	`"named":{"type":"string","description":"Что по сути назвал студент"},` +
	`"correct":{"type":"boolean","description":"Засчитать ли ответ студента"},` +
	`"reason":{"type":"string","description":"Пояснение студенту на «ты», до 12 слов"}},` +
	`"required":["named","correct","reason"],"additionalProperties":false}`)

func userPrompt(in Input, leftOut []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, prompts.GraderUser, in.Question, in.Expected, in.Quote, in.Given)
	if len(leftOut) > 0 {
		fmt.Fprintf(&b, prompts.GraderMissingWords, strings.Join(leftOut, " "))
	}
	return b.String()
}

// parseVerdict reads the model's JSON, possibly wrapped in markdown.
func parseVerdict(content []byte) (Verdict, error) {
	start, end := bytes.IndexByte(content, '{'), bytes.LastIndexByte(content, '}')
	if start < 0 || end < start {
		return Verdict{}, fmt.Errorf("no JSON object")
	}
	var out struct {
		Correct *bool  `json:"correct"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(content[start:end+1], &out); err != nil {
		return Verdict{}, err
	}
	if out.Correct == nil {
		return Verdict{}, fmt.Errorf("no verdict")
	}
	return Verdict{Correct: *out.Correct, Reason: out.Reason, Method: MethodModel}, nil
}
