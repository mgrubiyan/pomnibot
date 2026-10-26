package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

const imageDiscoveryPrompt = `Inspect the original PDFs for useful educational figures: photographs, diagrams, graphs, including figures within scanned pages. Return each figure's 1-based file index, page within that file and a neutral description. Exclude logos, decoration and recurring headers/footers. Do not transcribe the whole document. Return {"figures":[] } when none exist. At most 80 figures.`
const imageLocalizationPrompt = `Locate each requested educational figure on the attached rendered page. Return its candidate index and normalized bounds [left,top,right,bottom], all between 0 and 1. Preserve the entire figure, vector elements, labels and caption needed for understanding. Exclude unrelated text. Use coordinates of the attached image, including its displayed rotation. Omit a candidate if no useful readable figure exists. Never invent one.`
const imageGenerationRules = `The images array is a catalog in exactly the order of the attached crop images. Reference only catalog assetId values in questionImageIds and explanationImageIds, preserving display order. Use a figure when necessary to solve a concrete question or helpful to explain it; empty arrays are valid. Never put a figure, caption or alternative text revealing the answer on the question side: use it in the explanation instead. Keep textual correct answers and explanations. Never ask a question requiring a missing image. When the catalog is empty, all questions must be independently answerable from their text. Do not generate SVG for a side with PDF images.`

const maxPDFFigures = 80

type figureCandidate struct {
	File        int    `json:"file"`
	Page        int    `json:"page"`
	Description string `json:"description"`
}

type figureRegion struct {
	Index  int       `json:"index"`
	Bounds []float64 `json:"bounds"`
}

func (r *figureRegion) UnmarshalJSON(data []byte) error {
	if err := requiredFields(data, "index", "bounds"); err != nil {
		return err
	}
	var wire struct {
		Index  int               `json:"index"`
		Bounds []json.RawMessage `json:"bounds"`
	}
	if err := strictJSON(data, &wire); err != nil {
		return err
	}
	if len(wire.Bounds) != 4 {
		return fmt.Errorf("figure bounds must have four coordinates")
	}
	r.Index = wire.Index
	r.Bounds = make([]float64, 4)
	for i, v := range wire.Bounds {
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("null figure coordinate")
		}
		if err := json.Unmarshal(v, &r.Bounds[i]); err != nil {
			return err
		}
	}
	return nil
}

func (g *Generator) preparePDFImages(ctx context.Context, doc Document, out *Result) error {
	if len(doc.PDFs) == 0 {
		return nil
	}
	began := time.Now()
	defer func() { out.Stats.ImageDuration = time.Since(began) }()
	images, err := g.extractPDFImages(ctx, doc, out)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		out.Stats.ImageErrors++
		out.Coverage.Issues = append(out.Coverage.Issues, "PDF figures unavailable; generated text-only cards: "+err.Error())
		return nil
	}
	out.Images = images
	out.Stats.ImagesExtracted = len(images)
	return nil
}

func (g *Generator) extractPDFImages(ctx context.Context, doc Document, out *Result) ([]material.ImageAsset, error) {
	s := map[string]any{"type": "string"}
	n := map[string]any{"type": "integer"}
	figures := schemaArray(schemaObject(map[string]any{"file": n, "page": n, "description": s}))
	figures["maxItems"] = maxPDFFigures
	req := providers.Request{System: imageDiscoveryPrompt, PDFs: doc.PDFs, Schema: schemaBytes(schemaObject(map[string]any{"figures": figures})), NoFallback: true, NoCache: true, ReasoningEffort: g.opts.ReasoningEffort, MaxTokens: g.opts.MaxTokens}
	var candidates struct {
		Figures []figureCandidate `json:"figures"`
	}
	if err := g.runStage(ctx, "image_discovery", req, out, func(r providers.Response, _ *StageStats) error {
		if r.Truncated {
			return fmt.Errorf("figure discovery truncated")
		}
		if err := requiredFields(r.Content, "figures"); err != nil {
			return err
		}
		return strictJSON(r.Content, &candidates)
	}); err != nil {
		return nil, err
	}
	out.Stats.ImagesFound = len(candidates.Figures)
	if len(candidates.Figures) > maxPDFFigures {
		return nil, fmt.Errorf("too many PDF figures")
	}
	type pageKey struct{ file, page int }
	groups := map[pageKey][]int{}
	keys := []pageKey{}
	for i, c := range candidates.Figures {
		if c.File < 1 || c.File > len(doc.PDFs) || c.Page < 1 || c.Page > doc.PDFs[c.File-1].Pages || strings.TrimSpace(c.Description) == "" {
			return nil, fmt.Errorf("invalid figure source")
		}
		k := pageKey{c.File, c.Page}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], i)
	}
	render := g.opts.RenderPage
	if render == nil {
		render = ingest.RenderPDFPage
	}
	assets := []material.ImageAsset{}
	hashes := map[string]bool{}
	for _, k := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := render(ctx, doc.PDFs[k.file-1].Data, k.page-1)
		if err != nil {
			return nil, err
		}
		type entry struct {
			Index       int    `json:"index"`
			Description string `json:"description"`
		}
		entries := []entry{}
		allowed := map[int]bool{}
		for _, i := range groups[k] {
			entries = append(entries, entry{i, candidates.Figures[i].Description})
			allowed[i] = true
		}
		bounds := schemaArray(map[string]any{"type": "number"})
		bounds["minItems"] = 4
		bounds["maxItems"] = 4
		req := providers.Request{System: imageLocalizationPrompt, User: string(schemaBytes(map[string]any{"figures": entries})), Images: []providers.Image{{Data: page, MimeType: "image/jpeg"}}, Schema: schemaBytes(schemaObject(map[string]any{"regions": schemaArray(schemaObject(map[string]any{"index": n, "bounds": bounds}))})), Backend: out.Stats.Stages["image_discovery"].Backend, NoFallback: true, NoCache: true, ReasoningEffort: g.opts.ReasoningEffort, MaxTokens: g.opts.MaxTokens}
		var localized struct {
			Regions []figureRegion `json:"regions"`
		}

		stage := fmt.Sprintf("image_localization_%d_%d", k.file, k.page)
		if err := g.runStage(ctx, stage, req, out, func(r providers.Response, _ *StageStats) error {
			if r.Truncated {
				return fmt.Errorf("figure localization truncated")
			}
			if err := requiredFields(r.Content, "regions"); err != nil {
				return err
			}
			return strictJSON(r.Content, &localized)
		}); err != nil {
			return nil, err
		}
		seen := map[int]bool{}
		for _, r := range localized.Regions {
			if !allowed[r.Index] || seen[r.Index] {
				return nil, fmt.Errorf("invalid localized figure index")
			}
			seen[r.Index] = true
			if len(r.Bounds) != 4 {
				return nil, fmt.Errorf("figure bounds must have four coordinates")
			}
			region := material.Region(r.Bounds)
			data, w, h, err := cropPDFImage(page, region)
			if err != nil {
				return nil, err
			}
			hash := material.Digest(data)
			if hashes[hash] {
				continue
			}
			hashes[hash] = true
			id, err := uuid.NewV7()
			if err != nil {
				return nil, err
			}
			assets = append(assets, material.ImageAsset{ID: id.String(), Alt: candidates.Figures[r.Index].Description, SourceFile: k.file, SourceName: doc.PDFs[k.file-1].Name, SourcePage: k.page, Region: region, Width: w, Height: h, Hash: hash, Data: data})
		}
	}
	return assets, nil
}

func cropPDFImage(data []byte, region material.Region) ([]byte, int, int, error) {
	if !material.ValidRegion(&region) {
		return nil, 0, 0, fmt.Errorf("invalid figure bounds")
	}
	for _, v := range region {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, 0, 0, fmt.Errorf("invalid figure coordinate")
		}
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	if int64(cfg.Width)*int64(cfg.Height) > ingest.MaxOCRPixels || len(data) > ingest.MaxOCRBytes {
		return nil, 0, 0, fmt.Errorf("rendered page exceeds image limits")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, 0, err
	}
	rect := image.Rect(int(region[0]*float64(cfg.Width)), int(region[1]*float64(cfg.Height)), int(region[2]*float64(cfg.Width)), int(region[3]*float64(cfg.Height)))
	if rect.Empty() {
		return nil, 0, 0, fmt.Errorf("empty figure crop")
	}
	crop := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(crop, crop.Bounds(), img, rect.Min, draw.Src)
	var b bytes.Buffer
	if err := png.Encode(&b, crop); err != nil {
		return nil, 0, 0, err
	}
	if b.Len() > ingest.MaxOCRBytes {
		return nil, 0, 0, fmt.Errorf("figure crop exceeds image limit")
	}
	return b.Bytes(), rect.Dx(), rect.Dy(), nil
}
func assetImages(assets []material.ImageAsset) []providers.Image {
	images := make([]providers.Image, 0, len(assets))
	for _, a := range assets {
		images = append(images, providers.Image{Data: a.Data, MimeType: "image/png"})
	}
	return images
}
func validateImageBindings(facts []Fact, assets []material.ImageAsset) error {
	known := map[string]bool{}
	for _, a := range assets {
		known[a.ID] = true
	}
	for _, f := range facts {
		for _, c := range f.Cards {
			b := c.Base()
			for _, ids := range [][]string{b.QuestionImageIDs, b.ExplanationImageIDs} {
				seen := map[string]bool{}
				for _, id := range ids {
					if !known[id] || seen[id] {
						return problem("validation", "images", "unknown or duplicate image reference")
					}
					seen[id] = true
				}
			}
		}
	}
	return nil
}
func usedImageCount(facts []Fact) int {
	ids := map[string]bool{}
	for _, f := range facts {
		for _, c := range f.Cards {
			b := c.Base()
			for _, side := range [][]string{b.QuestionImageIDs, b.ExplanationImageIDs} {
				for _, id := range side {
					ids[id] = true
				}
			}
		}
	}
	return len(ids)
}
