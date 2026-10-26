package generator

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
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
	for _, raw := range []string{`{"bounds":[0,0,1,1]}`, `{"id":null,"bounds":[0,0,1,1]}`, `{"id":"a","bounds":[null,0,1,1]}`, `{"id":"a","bounds":[0,0,1,1,1]}`} {
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
