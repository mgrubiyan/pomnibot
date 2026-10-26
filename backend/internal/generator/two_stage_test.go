package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func selectedReply(t *testing.T, facts []Fact, figures string, svg bool) []byte {
	t.Helper()
	data, err := marshalModelFixture(facts...)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err = json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	var selected any
	if err = json.Unmarshal([]byte(figures), &selected); err != nil {
		t.Fatal(err)
	}
	root["figures"] = selected
	for _, f := range root["facts"].([]any) {
		fact := f.(map[string]any)
		fact["recall"].(map[string]any)["needsSvg"] = svg
		for _, c := range fact["extraCards"].([]any) {
			c.(map[string]any)["needsSvg"] = svg
		}
	}
	data, _ = json.Marshal(root)
	return data
}

func renderedPage(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, image.Rect(0, 0, w/2, h), &image.Uniform{C: color.RGBA{R: 255, A: 255}}, image.Point{}, draw.Src)
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestTwoStagePDFBatchesSelectedPagesAndPublishesOrderedBindings(t *testing.T) {
	for _, pages := range []int{4, 42} {
		t.Run(fmt.Sprint(pages), func(t *testing.T) {
			f := learningFact("Compare the two diagrams")
			b := f.Cards[0].Base()
			b.QuestionImageIDs = []string{"second", "first"}
			b.ExplanationImageIDs = []string{"first"}
			f.Cards[0] = withCardBase(f.Cards[0], b)
			calls, renders := 0, 0
			p := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
				calls++
				if !req.NoRetry || !req.NoFallback {
					t.Fatal("transport retries/fallback enabled")
				}
				if calls == 1 {
					if len(req.PDFs) != 1 || len(req.Images) != 0 {
						t.Fatal("first request must contain original PDF only")
					}
					return providers.Response{Content: selectedReply(t, []Fact{f}, `[{"id":"first","file":1,"page":2,"description":"left diagram with labels"},{"id":"second","file":1,"page":4,"description":"single photo on right"}]`, false)}, nil
				}
				if calls > 2 || len(req.PDFs) != 0 || req.Source != "" || len(req.Images) != 2 {
					t.Fatalf("unexpected second request: calls=%d images=%d PDFs=%d", calls, len(req.Images), len(req.PDFs))
				}
				for _, img := range req.Images {
					cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
					if err != nil || max(cfg.Width, cfg.Height) > 1536 {
						t.Fatal("unbounded preview", cfg, err)
					}
				}
				return providers.Response{Content: []byte(`{"regions":[{"id":"second","bounds":[0.5,0,1,1]},{"id":"first","bounds":[0,0,0.5,1]}],"illustrations":[]}`)}, nil
			})
			r, err := NewGenerator(p, Options{RenderPage: func(_ context.Context, _ []byte, page int) ([]byte, error) {
				renders++
				if page != 1 && page != 3 {
					t.Fatal("rendered unselected page", page)
				}
				return renderedPage(t, 2000, 1000), nil
			}}).Generate(t.Context(), Document{PDFs: []providers.PDF{{Data: []byte("pdf"), Pages: pages}}})
			if err != nil || calls != 2 || renders != 2 || r.Stats.ImagesFound != 2 || r.Stats.ImagesUsed != 2 {
				t.Fatalf("calls=%d renders=%d stats=%+v error=%v", calls, renders, r.Stats, err)
			}
			for _, c := range r.Facts[0].Cards {
				b := c.Base()
				if len(b.QuestionImageIDs) != 2 || b.QuestionImageIDs[1] != b.ExplanationImageIDs[0] || b.QuestionImageIDs[0] == "second" {
					t.Fatal("lost permanent ordered bindings", b)
				}
			}
			if r.Images[0].Width != 1000 {
				t.Fatal("crop used preview instead of local original")
			}
		})
	}
}

func TestTwoStageSkipsUnrequestedIllustrations(t *testing.T) {
	calls := 0
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		return providers.Response{Content: selectedReply(t, []Fact{learningFact("Text")}, `[]`, false)}, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: "source"})
	if err != nil || calls != 1 || len(r.Facts) != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestTwoStageMissingQuestionDropsFactButKeepsTextAndExplanation(t *testing.T) {
	for _, failure := range []string{"provider", "coordinates", "omitted"} {
		t.Run(failure, func(t *testing.T) {
			visual := learningFact("Visual")
			b := visual.Cards[0].Base()
			b.QuestionImageIDs = []string{"a"}
			visual.Cards[0] = withCardBase(visual.Cards[0], b)
			text := learningFact("Text")
			b = text.Cards[0].Base()
			b.ExplanationImageIDs = []string{"a"}
			text.Cards[0] = withCardBase(text.Cards[0], b)
			calls := 0
			p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
				calls++
				if calls == 1 {
					return providers.Response{Content: selectedReply(t, []Fact{visual, text}, `[{"id":"a","file":1,"page":1,"description":"photo"}]`, false)}, nil
				}
				if failure == "provider" {
					return providers.Response{}, errors.New("unavailable")
				}
				content := `{"regions":[],"illustrations":[]}`
				if failure == "coordinates" {
					content = `{"regions":[{"id":"a","bounds":[0,0,2,1]}],"illustrations":[]}`
				}
				return providers.Response{Content: []byte(content)}, nil
			})
			r, err := NewGenerator(p, Options{RenderPage: func(context.Context, []byte, int) ([]byte, error) { return renderedPage(t, 100, 80), nil }}).Generate(t.Context(), Document{PDFs: []providers.PDF{{Data: []byte("pdf"), Pages: 1}}})
			if err != nil || len(r.Facts) != 1 || r.Facts[0].Name != "Text" || len(r.Facts[0].Cards[0].Base().ExplanationImageIDs) != 0 || len(r.Coverage.Issues) == 0 || calls != 2 {
				t.Fatalf("result=%+v err=%v", r, err)
			}
		})
	}
}

func TestSelectedPageLimitsPrioritizeQuestionsAndCardOrder(t *testing.T) {
	f := learningFact("Text")
	// The explanation appears first in the response catalog, but questions win.
	b := f.Cards[0].Base()
	b.ExplanationImageIDs = []string{"explain"}
	f.Cards[0] = withCardBase(f.Cards[0], b)
	for i := 1; i <= 14; i++ {
		b = cardBase(fmt.Sprintf("Diagram %d?", i))
		b.QuestionImageIDs = []string{fmt.Sprintf("q%d", i)}
		f.Cards = append(f.Cards, BooleanCard{CardBase: b, Answer: true})
	}
	figures := []map[string]any{{"id": "explain", "file": 1, "page": 42, "description": "explanation photo"}}
	for i := 1; i <= 14; i++ {
		figures = append(figures, map[string]any{"id": fmt.Sprintf("q%d", i), "file": 1, "page": (i + 2) / 3, "description": "specific diagram"})
	}
	rawFigures, _ := json.Marshal(figures)
	calls, renders := 0, 0
	p := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{Content: selectedReply(t, []Fact{f}, string(rawFigures), false)}, nil
		}
		var payload struct {
			Figures []figureCandidate `json:"figures"`
			Pages   []previewPage     `json:"pages"`
			Cards   []any             `json:"cards"`
		}
		if err := json.Unmarshal([]byte(req.User), &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Figures) != 12 || len(payload.Pages) != 4 || len(payload.Cards) != 0 {
			t.Fatalf("wrong limits/SVG subset: %s", req.User)
		}
		regions := []map[string]any{}
		for i, c := range payload.Figures {
			if c.ID != fmt.Sprintf("q%d", i+1) {
				t.Fatal("lost question priority/order", c.ID)
			}
			regions = append(regions, map[string]any{"id": c.ID, "bounds": []float64{0, 0, .5, 1}})
		}
		data, _ := json.Marshal(map[string]any{"regions": regions, "illustrations": []any{}})
		return providers.Response{Content: data}, nil
	})
	r, err := NewGenerator(p, Options{RenderPage: func(_ context.Context, _ []byte, page int) ([]byte, error) {
		renders++
		if page > 3 {
			t.Fatal("rendered discarded page")
		}
		return renderedPage(t, 100, 80), nil
	}}).Generate(t.Context(), Document{PDFs: []providers.PDF{{Data: []byte("pdf"), Pages: 42}}})
	if err != nil || calls != 2 || renders != 4 || CardCount(r.Facts) != 14 || r.Stats.ImagesFound != 15 || r.Stats.ImagesExtracted != 1 || r.Stats.ImagesUsed != 1 {
		t.Fatalf("calls=%d renders=%d stats=%+v err=%v", calls, renders, r.Stats, err)
	}
}

func TestSelectedUniquePagesAndPreviewByteLimits(t *testing.T) {
	for _, byteLimit := range []bool{false, true} {
		t.Run(fmt.Sprint(byteLimit), func(t *testing.T) {
			f := learningFact("Text")
			figures := []map[string]any{}
			for i := 1; i <= 8; i++ {
				id := fmt.Sprint(i)
				b := cardBase("Question " + id)
				b.QuestionImageIDs = []string{id}
				f.Cards = append(f.Cards, BooleanCard{CardBase: b, Answer: true})
				figures = append(figures, map[string]any{"id": id, "file": 1, "page": i, "description": "diagram"})
			}
			data, _ := json.Marshal(figures)
			page := renderedPage(t, 100, 80)
			preview, err := pdfPreview(page, 1536)
			if err != nil {
				t.Fatal(err)
			}
			opts := Options{RenderPage: func(context.Context, []byte, int) ([]byte, error) { return page, nil }}
			wantPages := 6
			if byteLimit {
				opts.PreviewMaxBytes = len(preview) * 2
				wantPages = 2
			}
			calls := 0
			p := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
				calls++
				if calls == 1 {
					return providers.Response{Content: selectedReply(t, []Fact{f}, string(data), false)}, nil
				}
				if len(req.Images) != wantPages {
					t.Fatalf("preview count %d want %d", len(req.Images), wantPages)
				}
				total := 0
				for _, img := range req.Images {
					total += len(img.Data)
				}
				if byteLimit && total > opts.PreviewMaxBytes {
					t.Fatal("preview byte cap escaped")
				}
				return providers.Response{Content: []byte(`{"regions":[],"illustrations":[]}`)}, nil
			})
			r, err := NewGenerator(p, opts).Generate(t.Context(), Document{PDFs: []providers.PDF{{Data: []byte("pdf"), Pages: 42}}})
			if err != nil || calls != 2 || CardCount(r.Facts) != 2 || len(r.Coverage.Issues) == 0 {
				t.Fatalf("stats=%+v err=%v", r.Stats, err)
			}
		})
	}
}

func TestTwoStageMultipleFilesReusePageAndDeduplicateCrops(t *testing.T) {
	f := learningFact("Compare")
	b := f.Cards[0].Base()
	b.QuestionImageIDs = []string{"a", "same", "b"}
	f.Cards[0] = withCardBase(f.Cards[0], b)
	calls, renders := 0, 0
	p := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			if req.PDFs[1].StartPage != 3 {
				t.Fatal("wrong global PDF page offset")
			}
			return providers.Response{Content: selectedReply(t, []Fact{f}, `[{"id":"a","file":2,"page":1,"description":"left"},{"id":"same","file":2,"page":1,"description":"same photo"},{"id":"b","file":1,"page":2,"description":"right"}]`, false)}, nil
		}
		if len(req.Images) != 2 {
			t.Fatal("page not reused")
		}
		return providers.Response{Content: []byte(`{"regions":[{"id":"a","bounds":[0,0,0.5,1]},{"id":"same","bounds":[0,0,0.5,1]},{"id":"b","bounds":[0.5,0,1,1]}],"illustrations":[]}`)}, nil
	})
	r, err := NewGenerator(p, Options{RenderPage: func(_ context.Context, file []byte, page int) ([]byte, error) {
		renders++
		if string(file) == "one" && page != 1 || string(file) == "two" && page != 0 {
			t.Fatal("wrong file/page")
		}
		return renderedPage(t, 100, 80), nil
	}}).Generate(t.Context(), Document{PDFs: []providers.PDF{{Data: []byte("one"), Name: "one.pdf", Pages: 2}, {Data: []byte("two"), Name: "two.pdf", Pages: 1}}})
	if err != nil || calls != 2 || renders != 2 || len(r.Images) != 2 || r.Images[0].SourceName != "two.pdf" || r.Images[0].SourcePage != 1 || r.Images[1].SourceName != "one.pdf" || r.Images[1].SourcePage != 2 {
		t.Fatalf("stats=%+v error=%v", r.Stats, err)
	}
	for _, card := range r.Facts[0].Cards {
		if ids := card.Base().QuestionImageIDs; len(ids) != 2 || ids[0] != r.Images[0].ID || ids[1] != r.Images[1].ID {
			t.Fatal("dedup lost binding order", ids)
		}
	}
}

func TestTwoStagePartialCoordinatesAndSVGSubset(t *testing.T) {
	text := learningFact("Text")
	text.Cards = append(text.Cards, BooleanCard{CardBase: CardBase{Question: "Missing visual?", Source: "S", QuestionImageIDs: []string{"bad"}}, Answer: true})
	visual := learningFact("Visual")
	b := visual.Cards[0].Base()
	b.QuestionImageIDs = []string{"good"}
	visual.Cards[0] = withCardBase(visual.Cards[0], b)
	calls := 0
	p := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			data := selectedReply(t, []Fact{text, visual}, `[{"id":"good","file":1,"page":1,"description":"left photo"},{"id":"bad","file":1,"page":1,"description":"right diagram"}]`, false)
			var root map[string]any
			_ = json.Unmarshal(data, &root)
			root["facts"].([]any)[1].(map[string]any)["recall"].(map[string]any)["needsSvg"] = true
			data, _ = json.Marshal(root)
			return providers.Response{Content: data}, nil
		}
		var payload map[string]json.RawMessage
		_ = json.Unmarshal([]byte(req.User), &payload)
		var cards []struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(payload["cards"], &cards)
		if len(cards) != 1 {
			t.Fatal("sent SVG-unrequested cards", req.User)
		}
		data, _ := json.Marshal(map[string]any{"regions": []any{map[string]any{"id": "bad", "bounds": []any{nil, 0, 1, 1}}, map[string]any{"id": "good", "bounds": []float64{0, 0, .5, 1}}}, "illustrations": []any{map[string]any{"cardId": cards[0].ID, "questionSvg": testSVG, "explanationSvg": testSVG}}})
		return providers.Response{Content: data}, nil
	})
	r, err := NewGenerator(p, Options{RenderPage: func(context.Context, []byte, int) ([]byte, error) { return renderedPage(t, 100, 80), nil }}).Generate(t.Context(), Document{PDFs: []providers.PDF{{Data: []byte("pdf"), Pages: 1}}})
	if err != nil || calls != 2 || CardCount(r.Facts) != 4 || r.Stats.ImagesUsed != 1 {
		t.Fatalf("stats=%+v err=%v", r.Stats, err)
	}
	for _, card := range r.Facts[1].Cards {
		b := card.Base()
		if len(b.QuestionImageIDs) != 1 || b.QuestionSvg != "" || b.ExplanationSvg != testSVG {
			t.Fatal("PDF/SVG priority or pair broken", b)
		}
	}
}

func TestTwoStageFailsWhenAllRequiredFiguresMissing(t *testing.T) {
	f := learningFact("Visual")
	b := f.Cards[0].Base()
	b.QuestionImageIDs = []string{"a"}
	f.Cards[0] = withCardBase(f.Cards[0], b)
	calls := 0
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{Content: selectedReply(t, []Fact{f}, `[{"id":"a","file":1,"page":1,"description":"photo"}]`, false)}, nil
		}
		return providers.Response{}, errors.New("unavailable")
	})
	r, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published unusable fact") }, RenderPage: func(context.Context, []byte, int) ([]byte, error) { return renderedPage(t, 100, 80), nil }}).Generate(t.Context(), Document{PDFs: []providers.PDF{{Data: []byte("pdf"), Pages: 1}}})
	if err == nil || len(r.Facts) != 0 || calls != 2 {
		t.Fatalf("stats=%+v err=%v", r.Stats, err)
	}
}

func TestTwoStagePDFTimeoutAndCancellation(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel", "render failure", "render cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			text, visual := learningFact("Text"), learningFact("Visual")
			b := visual.Cards[0].Base()
			b.QuestionImageIDs = []string{"a"}
			visual.Cards[0] = withCardBase(visual.Cards[0], b)
			calls := 0
			p := providerFunc(func(callCtx context.Context, _ providers.Request) (providers.Response, error) {
				calls++
				if calls == 1 {
					return providers.Response{Content: selectedReply(t, []Fact{text, visual}, `[{"id":"a","file":1,"page":1,"description":"photo"}]`, false)}, nil
				}
				if mode == "cancel" {
					cancel()
				} else {
					<-callCtx.Done()
				}
				return providers.Response{}, callCtx.Err()
			})
			result, err := NewGenerator(p, Options{CallTimeout: 5 * time.Millisecond, RenderPage: func(context.Context, []byte, int) ([]byte, error) {
				if mode == "render failure" {
					return nil, errors.New("render failed")
				}
				if mode == "render cancel" {
					cancel()
					return nil, ctx.Err()
				}
				return renderedPage(t, 100, 80), nil
			}, OnBatch: func(Batch) {
				if mode == "cancel" || mode == "render cancel" {
					t.Fatal("published canceled job")
				}
			}}).Generate(ctx, Document{PDFs: []providers.PDF{{Data: []byte("pdf"), Pages: 1}}})
			if mode == "cancel" || mode == "render cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("lost cancellation", err)
				}
				return
			}
			if err != nil || len(result.Facts) != 1 || result.Facts[0].Name != "Text" {
				t.Fatalf("stats=%+v err=%v", result.Stats, err)
			}
			expected := 2
			if mode == "render failure" {
				expected = 1
			}
			if calls != expected {
				t.Fatal("unexpected transport calls", calls)
			}
		})
	}
}

func TestTwoStageEnforcesTransportCapEvenIfProviderTriesToRetry(t *testing.T) {
	ledger := providers.NewBudget(providers.BudgetLimits{USD: .1, Calls: 10})
	ctx := providers.WithBudget(t.Context(), ledger)
	// Account for an earlier paid reading attempt on the caller's ledger.
	finish, err := providers.ReserveCall(ctx, .02)
	if err != nil {
		t.Fatal(err)
	}
	finish(providers.Usage{CostUSD: .02}, true)
	attempts := 0
	p := providerFunc(func(callCtx context.Context, _ providers.Request) (providers.Response, error) {
		for i := 0; i < 3; i++ {
			finish, err := providers.ReserveCall(callCtx, .01)
			if err != nil {
				return providers.Response{Usage: providers.Usage{Attempts: 1, FailedAttempts: 1, CostUSD: .01}}, err
			}
			attempts++
			finish(providers.Usage{CostUSD: .01}, true)
			if attempts == 1 {
				return providers.Response{Content: selectedReply(t, []Fact{learningFact("Text")}, `[]`, true), Usage: providers.Usage{Attempts: 1, CostUSD: .01}}, nil
			}
		}
		t.Fatal("transport retries escaped stage cap")
		return providers.Response{}, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(ctx, Document{Text: "source"})
	if err != nil || attempts != 2 || r.Budget.Calls != 3 || r.Budget.CommittedUSD < .039999 || r.Budget.CommittedUSD > .040001 || CardCount(r.Facts) != 2 {
		t.Fatalf("attempts=%d stats=%+v budget=%+v error=%v", attempts, r.Stats, r.Budget, err)
	}
}

func TestUnknownDraftFiguresDoNotDiscardUnrelatedFacts(t *testing.T) {
	visual, text := learningFact("Visual"), learningFact("Text")
	b := visual.Cards[0].Base()
	b.QuestionImageIDs = []string{"unknown"}
	visual.Cards[0] = withCardBase(visual.Cards[0], b)
	b = text.Cards[0].Base()
	b.ExplanationImageIDs = []string{"unknown"}
	text.Cards[0] = withCardBase(text.Cards[0], b)
	calls := 0
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		return providers.Response{Content: selectedReply(t, []Fact{visual, text}, `[]`, false)}, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: "source"})
	if err != nil || len(r.Facts) != 1 || r.Facts[0].Name != "Text" || calls != 1 || len(r.Facts[0].Cards[0].Base().ExplanationImageIDs) != 0 || len(r.Coverage.Issues) == 0 {
		t.Fatalf("calls=%d stats=%+v err=%v", calls, r.Stats, err)
	}
}

func TestUnreadableThumbnailDoesNotPublishAsAnIllustration(t *testing.T) {
	f := learningFact("Visual")
	b := f.Cards[0].Base()
	b.QuestionImageIDs = []string{"good"}
	b.ExplanationImageIDs = []string{"tiny"}
	f.Cards[0] = withCardBase(f.Cards[0], b)
	f.Cards = append(f.Cards, BooleanCard{CardBase: CardBase{Question: "Identify the tiny figure", Source: "Explanation", QuestionImageIDs: []string{"tiny"}}, Answer: true})
	calls := 0
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{Content: selectedReply(t, []Fact{f}, `[{"id":"good","file":1,"page":1,"description":"full photograph"},{"id":"tiny","file":1,"page":1,"description":"legend thumbnail"}]`, false)}, nil
		}
		return providers.Response{Content: []byte(`{"regions":[{"id":"good","bounds":[0,0,0.5,0.5]},{"id":"tiny","bounds":[0.775,0.602,0.832,0.633]}],"illustrations":[]}`)}, nil
	})
	r, err := NewGenerator(p, Options{RenderPage: func(context.Context, []byte, int) ([]byte, error) { return renderedPage(t, 1650, 1275), nil }}).Generate(t.Context(), Document{PDFs: []providers.PDF{{Data: []byte("pdf"), Pages: 1}}})
	if err != nil || calls != 2 || r.Stats.ImagesFound != 2 || r.Stats.ImagesExtracted != 1 || r.Stats.ImagesUsed != 1 || CardCount(r.Facts) != 2 || len(r.Coverage.Issues) == 0 {
		t.Fatalf("stats=%+v err=%v", r.Stats, err)
	}
	for _, card := range r.Facts[0].Cards {
		if len(card.Base().QuestionImageIDs) != 1 || len(card.Base().ExplanationImageIDs) != 0 || card.Base().Source == "" {
			t.Fatal("lost usable card or published thumbnail", card.Base())
		}
	}
}
