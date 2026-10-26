package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	xdraw "golang.org/x/image/draw"
)

// Smaller crops cannot reliably show educational detail at the local PDF render
// resolution. In particular, image-key thumbnails are not usable replacements
// for the full figures on an adjacent page.
const minPDFFigureSide = 48

const imageGenerationRules = `Select PDF figures for concrete cards while reading the original PDFs. Return figures: [{id, file, page, description}], where id is a unique temporary string, file is the 1-based PDF file index and page is 1-based within that file. Each description must identify one specific photo, diagram or graph, its position on a composite page and the labels needed for this card. Do not describe a whole collage when a card needs one component. Select the full-sized figure rather than a tiny image-key/legend thumbnail; use the original figure on the adjacent page when available. Reuse an id across cards when the same figure is useful. questionImageIds and explanationImageIds reference these temporary ids in display order. Select only figures used by cards; prefer at most 12 figures on 6 unique pages. Use original PDF figures for concrete visual tasks, not decoration. Never put a figure, caption or description revealing the answer on the question side; use the explanation side instead. Keep textual answers and explanations. With no suitable PDF figure, every question must be answerable from text. Set needsSvg=true only for cards that would benefit from an additional instructional SVG; no SVG is needed on a side with PDF figures. Do not return SVG in this first response. Return figures=[] when no PDF figures are selected.`

type figureCandidate struct {
	ID          string `json:"id"`
	File        int    `json:"file"`
	Page        int    `json:"page"`
	Description string `json:"description"`
}

func (f *figureCandidate) UnmarshalJSON(data []byte) error {
	if err := requiredFields(data, "id", "file", "page", "description"); err != nil {
		return err
	}
	type wire figureCandidate
	var v wire
	if err := strictJSON(data, &v); err != nil {
		return err
	}
	*f = figureCandidate(v)
	return nil
}

type figureRegion struct {
	ID     string    `json:"id"`
	Bounds []float64 `json:"bounds"`
}

func (r *figureRegion) UnmarshalJSON(data []byte) error {
	if err := requiredFields(data, "id", "bounds"); err != nil {
		return err
	}
	var wire struct {
		ID     string            `json:"id"`
		Bounds []json.RawMessage `json:"bounds"`
	}
	if err := strictJSON(data, &wire); err != nil {
		return err
	}
	if strings.TrimSpace(wire.ID) == "" || len(wire.Bounds) != 4 {
		return fmt.Errorf("figure requires id and four coordinates")
	}
	r.ID = wire.ID
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

// The draft validates temporary references without publishing them as asset IDs.
func draftFigureCatalog(figures []figureCandidate) ([]material.ImageAsset, error) {
	catalog := make([]material.ImageAsset, 0, len(figures))
	seen := map[string]bool{}
	for _, f := range figures {
		if strings.TrimSpace(f.ID) == "" || seen[f.ID] {
			return nil, problem("validation", "figures", "empty or duplicate temporary figure id")
		}
		seen[f.ID] = true
		catalog = append(catalog, material.ImageAsset{ID: f.ID})
	}
	return catalog, nil
}

type pdfPageKey struct{ file, page int }
type previewPage struct {
	File       int `json:"file"`
	Page       int `json:"page"`
	ImageIndex int `json:"imageIndex"`
}
type figurePreparation struct {
	Figures   []figureCandidate
	Pages     []previewPage
	Images    []providers.Image
	Originals map[pdfPageKey][]byte
}

func (g *Generator) selectFigurePages(ctx context.Context, doc Document, r AIResponse, out *Result) figurePreparation {
	prepared := figurePreparation{Figures: []figureCandidate{}, Pages: []previewPage{}, Images: []providers.Image{}, Originals: map[pdfPageKey][]byte{}}
	candidates := map[string]figureCandidate{}
	for _, f := range r.Figures {
		candidates[f.ID] = f
	}
	selected := []figureCandidate{}
	seen := map[string]bool{}
	pages := map[pdfPageKey]bool{}
	// All question figures have priority over all explanation figures, then
	// preserve card and attachment order. A reused figure consumes one slot.
	for side := 0; side < 2; side++ {
		for _, fact := range r.Facts {
			for _, card := range fact.Cards {
				ids := card.Base().QuestionImageIDs
				if side == 1 {
					ids = card.Base().ExplanationImageIDs
				}
				for _, id := range ids {
					if seen[id] {
						continue
					}
					seen[id] = true
					f, ok := candidates[id]
					if !ok {
						out.imageIssue("unknown temporary figure reference")
						continue
					}
					if f.File < 1 || f.File > len(doc.PDFs) || f.Page < 1 || f.Page > doc.PDFs[f.File-1].Pages || strings.TrimSpace(f.Description) == "" {
						out.imageIssue("figure " + id + ": invalid PDF source or description")
						continue
					}
					key := pdfPageKey{f.File, f.Page}
					if len(selected) >= g.opts.MaxPDFFigures || !pages[key] && len(pages) >= g.opts.MaxPDFPages {
						out.imageIssue("figure " + id + ": figure/page limit")
						continue
					}
					pages[key] = true
					selected = append(selected, f)
				}
			}
		}
	}
	render := g.opts.RenderPage
	if render == nil {
		render = ingest.RenderPDFPage
	}
	attempted := map[pdfPageKey]bool{}
	total := 0
	for _, f := range selected {
		key := pdfPageKey{f.File, f.Page}
		if !attempted[key] {
			attempted[key] = true
			if ctx.Err() != nil {
				break
			}
			original, err := render(ctx, doc.PDFs[f.File-1].Data, f.Page-1)
			var preview []byte
			if err == nil {
				preview, err = pdfPreview(original, g.opts.PreviewMaxDimension)
			}
			switch {
			case err != nil:
				out.imageIssue(fmt.Sprintf("PDF file %d page %d: render/preview failed: %v", f.File, f.Page, err))
			case total+len(preview) > g.opts.PreviewMaxBytes:
				out.imageIssue(fmt.Sprintf("PDF file %d page %d: preview byte limit", f.File, f.Page))
			default:
				total += len(preview)
				prepared.Originals[key] = original
				prepared.Pages = append(prepared.Pages, previewPage{f.File, f.Page, len(prepared.Images) + 1})
				prepared.Images = append(prepared.Images, providers.Image{Data: preview, MimeType: "image/jpeg"})
			}
		}
		if prepared.Originals[key] != nil {
			prepared.Figures = append(prepared.Figures, f)
		}
	}
	return prepared
}

func pdfPreview(data []byte, maxDimension int) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > ingest.MaxOCRPixels || len(data) > ingest.MaxOCRBytes {
		return nil, fmt.Errorf("rendered page exceeds image limits")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if longest := max(cfg.Width, cfg.Height); longest > maxDimension {
		width := max(1, cfg.Width*maxDimension/longest)
		height := max(1, cfg.Height*maxDimension/longest)
		resized := image.NewRGBA(image.Rect(0, 0, width, height))
		xdraw.CatmullRom.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Src, nil)
		img = resized
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (out *Result) imageIssue(reason string) {
	out.Stats.ImageErrors++
	out.Coverage.Issues = append(out.Coverage.Issues, reason)
}

func extractSelectedRegions(raws []json.RawMessage, prepared figurePreparation, doc Document, out *Result) map[string]string {
	resolved := map[string]string{}
	candidates := map[string]figureCandidate{}
	seen := map[string]bool{}
	hashes := map[string]string{}
	for _, f := range prepared.Figures {
		candidates[f.ID] = f
	}
	for _, raw := range raws {
		var r figureRegion
		if err := json.Unmarshal(raw, &r); err != nil {
			out.imageIssue("invalid localized figure: " + err.Error())
			continue
		}
		f, ok := candidates[r.ID]
		if !ok || seen[r.ID] {
			out.imageIssue("unknown or duplicate localized figure id")
			continue
		}
		seen[r.ID] = true
		region := material.Region(r.Bounds)
		data, w, h, err := cropPDFImage(prepared.Originals[pdfPageKey{f.File, f.Page}], region)
		if err != nil {
			out.imageIssue("figure " + f.ID + ": " + err.Error())
			continue
		}
		if min(w, h) < minPDFFigureSide {
			out.imageIssue(fmt.Sprintf("figure %s: crop too small to read (%dx%d; minimum side %d)", f.ID, w, h, minPDFFigureSide))
			continue
		}
		hash := material.Digest(data)
		id := hashes[hash]
		if id == "" {
			assetID, err := uuid.NewV7()
			if err != nil {
				out.imageIssue("figure id allocation failed")
				continue
			}
			id = assetID.String()
			hashes[hash] = id
			out.Images = append(out.Images, material.ImageAsset{ID: id, Alt: f.Description, SourceFile: f.File, SourceName: doc.PDFs[f.File-1].Name, SourcePage: f.Page, Region: region, Width: w, Height: h, Hash: hash, Data: data})
		}
		resolved[f.ID] = id
	}
	for _, f := range prepared.Figures {
		if resolved[f.ID] == "" {
			out.imageIssue("figure " + f.ID + ": no usable localized image")
		}
	}
	out.Stats.ImagesExtracted = len(out.Images)
	return resolved
}

// Resolve draft references or publish permanent IDs. Required question images
// own their card; the mandatory recall owns its fact. Draft resolution preserves
// SVG requests, while publication clears this private planning flag.
func resolveFigureBindings(facts []Fact, resolved map[string]string, out *Result, publish bool) ([]Fact, error) {
	published := []Fact{}
	for i, f := range facts {
		cards := []Card{}
		paths := []string{}
		indices := []int{}
		dropFact := false
		for j, c := range f.Cards {
			base := c.Base()
			if publish {
				base.needsSVG = false
			}
			resolve := func(ids []string) ([]string, bool) {
				if len(ids) == 0 {
					return nil, false
				}
				result := make([]string, 0, len(ids))
				seen := map[string]bool{}
				missing := false
				for _, temp := range ids {
					id := resolved[temp]
					if id == "" {
						missing = true
						continue
					}
					if !seen[id] {
						result = append(result, id)
						seen[id] = true
					}
				}
				return result, missing
			}
			var questionMissing, explanationMissing bool
			base.QuestionImageIDs, questionMissing = resolve(base.QuestionImageIDs)
			base.ExplanationImageIDs, explanationMissing = resolve(base.ExplanationImageIDs)
			if questionMissing {
				out.imageIssue(fmt.Sprintf("facts[%d].%s: required question image unavailable; card excluded", i, f.cardPaths[j]))
				out.Stats.DroppedInvalid++
				if f.cardPaths[j] == "recall" {
					dropFact = true
				}
				continue
			}
			if explanationMissing {
				out.imageIssue(fmt.Sprintf("facts[%d].%s: explanation image unavailable; text retained", i, f.cardPaths[j]))
			}
			if len(base.QuestionImageIDs) > 0 {
				base.QuestionSvg = ""
			}
			if len(base.ExplanationImageIDs) > 0 {
				base.ExplanationSvg = ""
			}
			cards = append(cards, withCardBase(c, base))
			paths = append(paths, f.cardPaths[j])
			if len(f.cardIndices) > j {
				indices = append(indices, f.cardIndices[j])
			}
		}
		if dropFact {
			continue
		}
		f.Cards = cards
		f.cardPaths = paths
		f.cardIndices = indices
		if len(cards) > 0 {
			published = append(published, f)
		}
	}
	if len(published) == 0 {
		return nil, problem("validation", "facts", "no usable cards after illustration preparation")
	}
	return published, nil
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
