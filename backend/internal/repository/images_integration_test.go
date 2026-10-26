//go:build integration

package repository_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository"
	"github.com/mgrubiyan/pomnibot/backend/internal/repository/db"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestPDFImagesRoundTripPermissionsAndRollbackPostgres(t *testing.T) {
	pool := manualTestPool(t)
	ctx := t.Context()
	q := db.New(pool)
	store := repository.NewManualStore(pool)
	users := usecase.NewUserService(q)
	sets := usecase.NewSetService(q, users, store)
	cards := usecase.NewCardService(q, users, usecase.WithTransactions(store), usecase.WithManualCards(store))
	images := usecase.NewImageService(q)
	owner, reader, stranger := uuid.New(), uuid.New(), uuid.New()
	a, b, unused := uuid.New(), uuid.New(), uuid.New()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 20, 10))); err != nil {
		t.Fatal(err)
	}
	asset := func(id uuid.UUID, page int) material.ImageAsset {
		return material.ImageAsset{ID: id.String(), Data: buf.Bytes(), Alt: "Figure", Width: 20, Height: 10, SourceName: "notes.pdf", SourcePage: page, SourceFile: 1, Region: material.Region{0, 0, 1, 1}, Hash: id.String()}
	}
	result := generator.Result{Images: []material.ImageAsset{asset(a, 2), asset(b, 3), asset(unused, 4)}, Facts: []generator.Fact{{Name: "Comparison", Pages: []int{2, 3}, Cards: []generator.Card{generator.FlipCard{CardBase: generator.CardBase{Question: "Compare these figures", Source: "Explanation", QuestionImageIDs: []string{b.String(), a.String()}, ExplanationImageIDs: []string{a.String()}}, Answer: "Text answer"}}}}}
	set, err := sets.SaveGeneratedSet(ctx, owner, "PDF", result, nil)
	if err != nil {
		t.Fatal(err)
	}
	all, err := sets.GetCardsBySetID(ctx, owner, set.ID)
	if err != nil || len(all) != 1 {
		t.Fatalf("cards %+v %v", all, err)
	}
	c := all[0]
	if len(c.QuestionImages) != 2 || c.QuestionImages[0].AssetId != b || len(c.ExplanationImages) != 1 {
		t.Fatalf("lost order: %+v", c)
	}
	if _, err = sets.JoinSetByShareCode(ctx, reader, set.ShareCode.Value); err != nil {
		t.Fatal(err)
	}
	for _, user := range []uuid.UUID{owner, reader} {
		catalog, err := images.GetSetImages(ctx, user, set.ID)
		if err != nil || len(catalog) != 3 {
			t.Fatalf("catalog: %+v %v", catalog, err)
		}
		data, err := images.GetCardImage(ctx, user, unused)
		if err != nil || !bytes.Equal(data, buf.Bytes()) {
			t.Fatalf("blob: %v", err)
		}
	}
	if _, err = images.GetSetImages(ctx, stranger, set.ID); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatal("foreign catalog", err)
	}
	if _, err = images.GetCardImage(ctx, stranger, a); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatal("foreign image", err)
	}
	feed, err := usecase.NewHomescreenService(q, users).GetFeedQuestions(ctx, reader, usecase.FeedOptions{})
	if err != nil || len(feed) != 1 || len(feed[0].QuestionImages) != 2 {
		t.Fatalf("feed %+v %v", feed, err)
	}
	data, _ := json.Marshal(&feed[0])
	if bytes.Contains(data, []byte("explanationImages")) || bytes.Contains(data, []byte("data")) {
		t.Fatal("leaked image answers/binary", string(data))
	}
	reveal, err := cards.RevealCard(ctx, reader, c.ID)
	if err != nil || len(reveal.ExplanationImages) != 1 {
		t.Fatalf("reveal %+v %v", reveal, err)
	}
	updated, err := cards.UpdateCard(ctx, owner, c.ID, &contracts.UpdateCardRequest{Question: contracts.NewOptString("Updated")})
	if err != nil || len(updated.QuestionImages) != 2 || len(updated.ExplanationImages) != 1 {
		t.Fatalf("text edit lost images %+v %v", updated, err)
	}
	updated, err = cards.UpdateCard(ctx, owner, c.ID, &contracts.UpdateCardRequest{QuestionImageIds: []uuid.UUID{a, b}, ExplanationImageIds: []uuid.UUID{}})
	if err != nil || len(updated.ExplanationImages) != 0 || updated.QuestionImages[0].AssetId != a {
		t.Fatalf("reorder/clear %+v %v", updated, err)
	}
	emptyJSON, _ := json.Marshal(updated)
	if !bytes.Contains(emptyJSON, []byte(`"explanationImages":[]`)) {
		t.Fatalf("missing empty array: %s", emptyJSON)
	}
	if _, err = cards.UpdateCard(ctx, reader, c.ID, &contracts.UpdateCardRequest{QuestionImageIds: []uuid.UUID{a}}); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatal("member edit", err)
	}
	other, err := sets.CreateSet(ctx, owner, &contracts.CreateSetRequest{Title: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cards.CreateCard(ctx, owner, other.ID, &contracts.CreateCardRequest{Kind: contracts.CardKindFlip, Question: "Foreign", Answer: contracts.NewStringCardAnswer("A"), QuestionImageIds: []uuid.UUID{a}}); !errors.Is(err, usecase.ErrValidation) {
		t.Fatal("assigned foreign asset", err)
	}
	otherCards, err := sets.GetCardsBySetID(ctx, owner, other.ID)
	if err != nil || len(otherCards) != 0 {
		t.Fatal("invalid create wasn't atomic", err)
	}
	if _, err = cards.CreateCardInFact(ctx, owner, c.FactId, &contracts.CreateCardRequest{Kind: contracts.CardKindFlip, Question: "New", Answer: contracts.NewStringCardAnswer("A"), ExplanationImageIds: []uuid.UUID{b}}); err != nil {
		t.Fatal(err)
	}
	if _, err = cards.UpdateCard(ctx, owner, c.ID, &contracts.UpdateCardRequest{Question: contracts.NewOptString("Must roll back"), QuestionImageIds: []uuid.UUID{uuid.New()}}); !errors.Is(err, usecase.ErrValidation) {
		t.Fatal("invalid update", err)
	}
	all, err = sets.GetCardsBySetID(ctx, owner, set.ID)
	if err != nil || all[0].Question != "Updated" || all[0].QuestionImages[0].AssetId != a {
		t.Fatal("invalid update changed card", err)
	}
	var before, after int
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM sets").Scan(&before)
	invalid := result
	invalid.Images = []material.ImageAsset{asset(uuid.New(), 1)}
	invalid.Images[0].Data = nil
	if _, err = sets.SaveGeneratedSet(ctx, owner, "Invalid image set", invalid, nil); err == nil {
		t.Fatal("invalid generated asset saved")
	}
	_ = pool.QueryRow(ctx, "SELECT COUNT(*) FROM sets").Scan(&after)
	if before != after {
		t.Fatalf("generated save wasn't atomic: %d -> %d", before, after)
	}
	if err = sets.DeleteSet(ctx, owner, set.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM set_images WHERE id=ANY($1::uuid[])", []uuid.UUID{a, b, unused}).Scan(&count); err != nil || count != 0 {
		t.Fatal("images survived set deletion", err)
	}
	if err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM card_images WHERE card_id=$1", c.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("links survived set deletion", err)
	}
}

// Exercise the boundary between temporary model IDs and permanent PostgreSQL
// attachments; manual asset fixtures alone cannot catch publishing draft IDs.
func TestTwoStageGeneratedPDFBindingsPostgres(t *testing.T) {
	pool := manualTestPool(t)
	q := db.New(pool)
	users := usecase.NewUserService(q)
	sets := usecase.NewSetService(q, users, repository.NewManualStore(pool))
	images := usecase.NewImageService(q)
	owner, member, stranger := uuid.New(), uuid.New(), uuid.New()
	calls := 0
	var page bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	if err := png.Encode(&page, img); err != nil {
		t.Fatal(err)
	}
	p := imageGenerationProvider(func(_ context.Context, req providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			return providers.Response{Content: []byte(`{"issues":[],"figures":[{"id":"left","file":1,"page":1,"description":"left diagram"},{"id":"right","file":1,"page":1,"description":"right photo"}],"facts":[{"name":"Visual comparison","pages":[1],"recall":{"question":"Compare the figures","answer":"Text answer","source":"Explanation","sourceQuote":"","questionImageIds":["right","left"],"explanationImageIds":["left"],"needsSvg":false},"extraCards":[]}]}`)}, nil
		}
		if calls != 2 || len(req.PDFs) != 0 || len(req.Images) != 1 {
			t.Fatal("unexpected illustration transport")
		}
		return providers.Response{Content: []byte(`{"regions":[{"id":"left","bounds":[0,0,0.5,1]},{"id":"right","bounds":[0.5,0,1,1]}],"illustrations":[]}`)}, nil
	})
	result, err := generator.NewGenerator(p, generator.Options{RenderPage: func(context.Context, []byte, int) ([]byte, error) { return page.Bytes(), nil }}).Generate(t.Context(), generator.Document{PDFs: []providers.PDF{{Data: []byte("stub PDF"), Name: "source.pdf", Pages: 1}}})
	if err != nil || calls != 2 || result.Stats.ImagesUsed != 2 || len(result.Images) != 2 {
		t.Fatalf("generation stats=%+v err=%v", result.Stats, err)
	}
	set, err := sets.SaveGeneratedSet(t.Context(), owner, "Generated PDF", result, nil)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := sets.GetCardsBySetID(t.Context(), owner, set.ID)
	if err != nil || len(stored) != 2 {
		t.Fatalf("cards=%d err=%v", len(stored), err)
	}
	left, right := uuid.MustParse(result.Images[0].ID), uuid.MustParse(result.Images[1].ID)
	for _, card := range stored {
		if len(card.QuestionImages) != 2 || card.QuestionImages[0].AssetId != right || card.QuestionImages[1].AssetId != left || len(card.ExplanationImages) != 1 || card.ExplanationImages[0].AssetId != left {
			t.Fatalf("lost generated attachment order/pair: %+v", card)
		}
	}
	if _, err = sets.JoinSetByShareCode(t.Context(), member, set.ShareCode.Value); err != nil {
		t.Fatal(err)
	}
	for _, user := range []uuid.UUID{owner, member} {
		for _, asset := range result.Images {
			data, err := images.GetCardImage(t.Context(), user, uuid.MustParse(asset.ID))
			if err != nil || !bytes.Equal(data, asset.Data) {
				t.Fatalf("image access user=%s err=%v", user, err)
			}
		}
	}
	if _, err = images.GetCardImage(t.Context(), stranger, left); !errors.Is(err, usecase.ErrNotFound) {
		t.Fatalf("stranger received generated image: %v", err)
	}
}

type imageGenerationProvider func(context.Context, providers.Request) (providers.Response, error)

func (p imageGenerationProvider) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	return p(ctx, req)
}
