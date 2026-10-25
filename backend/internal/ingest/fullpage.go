package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"log/slog"
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/prompts"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// A separate field prevents an interpretation of a diagram from becoming a
// verbatim source quote merely because the model wrote it while reading.
var pageSchema = json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"observations":{"type":"string"},"issues":{"type":"array","items":{"type":"string"}},"blocks":{"type":"array","items":{"type":"object","properties":{"kind":{"type":"string","enum":["heading","text","list","code","formula","table","diagram","image"]},"text":{"type":"string"},"observations":{"type":"string"},"region":{"type":"array","items":{"type":"number"},"minItems":4,"maxItems":4},"cells":{"type":"array","items":{"type":"array","items":{"type":"string"}}},"issues":{"type":"array","items":{"type":"string"}}},"required":["kind","text","observations","region","cells","issues"],"additionalProperties":false}}},"required":["text","observations","issues","blocks"],"additionalProperties":false}`)

func (v *VisionOCR) readFullPage(ctx context.Context, data []byte, mime, model, reference string) (OCRPage, error) {
	if err := ctx.Err(); err != nil {
		return OCRPage{}, err
	}
	check := OCRPage{Text: reference, FromTextLayer: reference != ""}
	checked := countChars(reference) >= DefaultMinPageChars
	if !checked && v.Checker != nil {
		got, err := v.Checker.Recognize(ctx, OCRRequest{Data: data, MimeType: mime, Model: model, Pages: 1})
		if ctx.Err() != nil {
			return OCRPage{}, ctx.Err()
		}
		if err == nil && len(got) > 0 {
			check, checked = got[0], true
		}
	}
	img, err := decodeImage(data)
	if err != nil {
		return OCRPage{}, fmt.Errorf("ingest: decode full page image: %w", err)
	}
	orientation := exifOrientation(data)
	if check.Rotate != 0 {
		orientation = map[int]int{90: 6, 180: 3, 270: 8}[check.Rotate]
	}
	// Preserve color in diagrams unless rotation requires re-encoding.
	if orientation == 3 || orientation == 6 || orientation == 8 {
		var b bytes.Buffer
		if err := jpeg.Encode(&b, orient(toGray(img), orientation), &jpeg.Options{Quality: 90}); err != nil {
			return OCRPage{}, err
		}
		data, mime = b.Bytes(), "image/jpeg"
	}
	user := prompts.VisionUser
	if reference != "" {
		user += prompts.FullPageReference + reference
	}
	resp, err := v.completeBounded(ctx, providers.Request{System: prompts.FullPageSystem, User: user, Schema: pageSchema, Image: &providers.Image{Data: data, MimeType: mime}})
	if ctx.Err() != nil {
		return OCRPage{}, ctx.Err()
	}
	var reading struct {
		Text         *string          `json:"text"`
		Observations string           `json:"observations"`
		Issues       []string         `json:"issues"`
		Blocks       []material.Block `json:"blocks"`
	}
	if err == nil && (resp.Truncated || json.Unmarshal(resp.Content, &reading) != nil || reading.Text == nil) {
		err = errors.New("ingest: incomplete or invalid full page transcription")
	}
	if err != nil {
		if checked || strings.TrimSpace(reference) != "" {
			slog.Warn("ingest: full page reading failed, keeping reference text")
			check.ReadFailed = true
			check.Issues = append(check.Issues, "Полное изображение страницы не прочитано; сохранён запасной текст.")
			return check, nil
		}
		return OCRPage{}, err
	}
	// An admitted completion from general knowledge is interpretation, even if
	// the replacement happens to be correct. Keep the locally observed source.
	if admittedReconstruction(reading.Issues) {
		reading.Observations += "\nМодель предложила реконструкцию отсутствующих частей; она отклонена как недословное чтение."
		reading.Issues = append(reading.Issues, "Реконструкция по внешнему синтаксису отклонена; неполнота источника сохранена.")
		raw := reference
		if raw == "" {
			raw = check.Text
		}
		reading.Text = &raw
		reading.Blocks = nil
	}
	text := strings.TrimSpace(*reading.Text)
	page := OCRPage{Image: data, ImageMime: mime, Text: text, Observations: reading.Observations, Issues: reading.Issues, Rotate: check.Rotate}
	if checked {
		_, confirmed, judged := keepConfirmed(text, checkWords(check.Text))
		if confirmed < judged {
			page.Issues = append(page.Issues, "Распознавание расходится с текстовым слоем/OCR; необходима сверка с изображением.")
		}
	}
	// Agreement is not confidence. Preserve disagreements instead of silently
	// deleting image-only content, mathematical conditions or handwritten lines.
	for _, b := range reading.Blocks {
		if !material.ValidRegion(b.Region) {
			page.Issues = append(page.Issues, "Некорректная область блока")
			continue
		}
		if b.Text != "" && !strings.Contains(text, b.Text) {
			page.Issues = append(page.Issues, "Блок не совпадает с транскрипцией")
			continue
		}
		page.Blocks = append(page.Blocks, b)
	}
	if len(page.Blocks) > 0 {
		page.Blocks = append(page.Blocks, uncoveredTextBlocks(text, page.Blocks)...)
	}
	return page, nil
}

func admittedReconstruction(issues []string) bool {
	for _, issue := range issues {
		s := strings.ToLower(issue)
		if (strings.Contains(s, "восстанов") || strings.Contains(s, "дополн")) && (strings.Contains(s, "стандартн") || strings.Contains(s, "по смыслу") || strings.Contains(s, "по контексту")) {
			return true
		}
	}
	return false
}

// uncoveredTextBlocks preserves missing contiguous spans without copying the
// whole page for an omitted page number or joining separated fragments.
func uncoveredTextBlocks(text string, blocks []material.Block) []material.Block {
	covered := make([]bool, len(text))
	for _, b := range blocks {
		if b.Text == "" {
			continue
		}
		from := 0
		for from < len(text) {
			i := strings.Index(text[from:], b.Text)
			if i < 0 {
				break
			}
			i += from
			end := i + len(b.Text)
			fresh := false
			for _, v := range covered[i:end] {
				fresh = fresh || !v
			}
			if fresh {
				for j := i; j < end; j++ {
					covered[j] = true
				}
				break
			}
			from = end
		}
	}
	var out []material.Block
	for i := 0; i < len(text); {
		if covered[i] {
			i++
			continue
		}
		start := i
		for i < len(text) && !covered[i] {
			i++
		}
		if source := strings.TrimSpace(text[start:i]); source != "" {
			out = append(out, material.Block{Kind: "text", Text: source})
		}
	}
	return out
}
