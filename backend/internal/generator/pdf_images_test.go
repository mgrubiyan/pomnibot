package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestCropPDFImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 80))
	img.Set(20, 20, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	for _, region := range []material.Region{{.1, .2, .6, .7}, {0, 0, 1, 1}} {
		data, w, h, err := cropPDFImage(b.Bytes(), region)
		if err != nil || w != int(region[2]*100)-int(region[0]*100) || h != int(region[3]*80)-int(region[1]*80) || len(data) == 0 {
			t.Fatalf("crop %v: %d %d %v", region, w, h, err)
		}
	}
	for _, r := range []material.Region{{-.1, 0, 1, 1}, {0, 0, 2, 1}, {.5, 0, .5, 1}} {
		if _, _, _, err := cropPDFImage(b.Bytes(), r); err == nil {
			t.Fatalf("accepted %v", r)
		}
	}
}

func TestPDFImagePipelineReusesPageAndPreservesRecallOrder(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 80))
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	renders := 0
	p := providerFunc(func(_ context.Context, r providers.Request) (providers.Response, error) {
		var content []byte
		switch r.System {
		case imageDiscoveryPrompt:
			content = []byte(`{"figures":[{"file":1,"page":2,"description":"graph"},{"file":1,"page":2,"description":"same graph"}]}`)
		case imageLocalizationPrompt:
			if len(r.Images) != 1 {
				t.Fatalf("localization images %d", len(r.Images))
			}
			content = []byte(`{"regions":[{"index":0,"bounds":[0,0,0.5,1]},{"index":1,"bounds":[0,0,0.5,1]}]}`)
		default:
			if len(r.PDFs) > 0 {
				var input struct {
					Images []material.ImageAsset `json:"images"`
				}
				_ = json.Unmarshal([]byte(r.User), &input)
				if len(input.Images) != 1 || len(r.Images) != 1 {
					t.Fatalf("catalog/images: %s %d", r.User, len(r.Images))
				}
				f := learningFact("graph")
				base := f.Cards[0].Base()
				base.QuestionImageIDs = []string{input.Images[0].ID}
				f.Cards[0] = withCardBase(f.Cards[0], base)
				content, _ = marshalModelFixture(f)
			} else {
				content = []byte(`{"illustrations":[]}`)
			}
		}
		return providers.Response{Content: content}, nil
	})
	g := NewGenerator(p, Options{RenderPage: func(context.Context, []byte, int) ([]byte, error) { renders++; return b.Bytes(), nil }})
	r, err := g.Generate(context.Background(), Document{PDFs: []providers.PDF{{Data: []byte("pdf"), Name: "source.pdf", Pages: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if renders != 1 || len(r.Images) != 1 || r.Images[0].SourcePage != 2 || r.Stats.ImagesFound != 2 || r.Stats.ImagesUsed != 1 {
		t.Fatalf("renders=%d result=%+v", renders, r)
	}
	for _, c := range r.Facts[0].Cards {
		if len(c.Base().QuestionImageIDs) != 1 {
			t.Fatal("recall image lost")
		}
	}
}

func TestPDFImagesFallbackAndCancellation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		g := NewGenerator(providerFunc(func(_ context.Context, r providers.Request) (providers.Response, error) {
			if r.System == imageDiscoveryPrompt {
				if cancel {
					stop()
				}
				return providers.Response{}, errors.New("unavailable")
			}
			if len(r.PDFs) > 0 && !bytes.Contains([]byte(r.User), []byte("Never ask")) {
				t.Fatal("fallback instruction missing")
			}
			return stageFixture(learningFact("text")).Complete(ctx, r)
		}), Options{})
		r, err := g.Generate(ctx, Document{PDFs: []providers.PDF{{Data: []byte("pdf"), Pages: 1}}})
		if cancel {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation swallowed: %v", err)
			}
		} else if err != nil || len(r.Images) != 0 || r.Stats.ImageErrors == 0 {
			t.Fatalf("fallback: %+v %v", r, err)
		}
	}
}

func TestPDFImageReferencesRejectWholeCard(t *testing.T) {
	for _, ids := range [][]string{{"missing"}, {"a", "a"}} {
		f := learningFact("figure")
		base := f.Cards[0].Base()
		base.QuestionImageIDs = ids
		f.Cards[0] = withCardBase(f.Cards[0], base)
		if err := validateImageBindings([]Fact{f}, []material.ImageAsset{{ID: "a"}}); err == nil {
			t.Fatal("accepted invalid reference", ids)
		}
	}
}
func TestCloneFactsDoesNotShareImageOrder(t *testing.T) {
	f := learningFact("compare")
	base := f.Cards[0].Base()
	base.QuestionImageIDs = []string{"a", "b"}
	base.ExplanationImageIDs = []string{"b"}
	f.Cards[0] = withCardBase(f.Cards[0], base)
	cloned := cloneFacts([]Fact{f})
	cloned[0].Cards[0].Base().QuestionImageIDs[0] = "changed"
	cloned[0].Cards[0].Base().ExplanationImageIDs[0] = "changed"
	if f.Cards[0].Base().QuestionImageIDs[0] != "a" || f.Cards[0].Base().ExplanationImageIDs[0] != "b" {
		t.Fatal("callback mutated published attachments")
	}
}
func TestPDFImageSignatureDistinguishesVisualQuestions(t *testing.T) {
	a := FlipCard{CardBase: cardBase("What is shown?"), Answer: "A"}
	b := a
	a.QuestionImageIDs = []string{"a"}
	b.QuestionImageIDs = []string{"b"}
	if cardSignature(a) == cardSignature(b) {
		t.Fatal("different figures deduplicated")
	}
}

func TestPDFLocalizationRejectsMissingAndNullCoordinates(t *testing.T) {
	for _, raw := range []string{`{"bounds":[0,0,1,1]}`, `{"index":null,"bounds":[0,0,1,1]}`, `{"index":0,"bounds":[null,0,1,1]}`, `{"index":0,"bounds":[0,0,1,1,1]}`} {
		var region figureRegion
		if err := json.Unmarshal([]byte(raw), &region); err == nil {
			t.Fatalf("accepted malformed region: %s", raw)
		}
	}
}

func TestUnknownExtraImageRejectsCardWithoutRemovingItsAttachment(t *testing.T) {
	f := learningFact("text")
	f.Cards = append(f.Cards, BooleanCard{CardBase: CardBase{Question: "Is the graph increasing?", Source: "Graph explanation", QuestionImageIDs: []string{"missing"}}, Answer: true})
	result, err := NewGenerator(stageFixture(f), Options{}).Generate(t.Context(), Document{Text: "source"})
	if err != nil || CardCount(result.Facts) != 2 || result.Stats.DroppedInvalid != 1 {
		t.Fatalf("invalid image card wasn't discarded: %+v %v", result, err)
	}
}
