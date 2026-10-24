package openrouter

import (
	"strings"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

const (
	calibratedCostModel     = "anthropic/claude-haiku-5.5"
	empiricalCostVersion    = "haiku-5.5-cost-v1"
	conservativeCostVersion = "conservative-cost-v1"
)

// costEstimate is a reservation policy, not a guaranteed upper bound or a bill.
// Coefficients come from recorded text requests and one 27-page native PDF
// (October 2026). The input margin is empirical; output reserves the full cap.
type costEstimate struct {
	Version              string
	TextBytes            int
	PDFTextBytes         int
	PDFPages             int
	EstimatedInputTokens int
	ReservedInputTokens  int
	MaxOutputTokens      int
	ReservedUSD          float64
}

func (c *Client) estimateCost(req providers.Request, body chatRequest) costEstimate {
	e := costEstimate{Version: empiricalCostVersion, MaxOutputTokens: body.MaxTokens}
	baseModel, _, _ := strings.Cut(body.Model, ":")
	empirical := baseModel == calibratedCostModel && len(providers.RequestImages(req)) == 0
	for _, pdf := range req.PDFs {
		e.PDFPages += pdf.Pages
		e.PDFTextBytes += max(0, pdf.TextBytes)
		if pdf.TextBytes <= 0 {
			empirical = false
		}
	}
	cacheWrite := false
	for _, m := range body.Messages {
		switch content := m.Content.(type) {
		case string:
			e.TextBytes += len(content)
		case []part:
			for _, p := range content {
				e.TextBytes += len(p.Text)
				cacheWrite = cacheWrite || p.CacheControl != nil
			}
		}
	}
	if body.ResponseFormat != nil {
		e.TextBytes += len(body.ResponseFormat.JSONSchema.Schema)
	}
	if empirical {
		e.EstimatedInputTokens = (e.TextBytes+e.PDFTextBytes+2)/3 + 1536*e.PDFPages + 1024
		e.ReservedInputTokens = (e.EstimatedInputTokens*5 + 3) / 4
	} else {
		e.Version = conservativeCostVersion
		e.EstimatedInputTokens = c.inputBound(req)
		e.ReservedInputTokens = e.EstimatedInputTokens
	}
	cacheMultiplier := 1.0
	if cacheWrite {
		cacheMultiplier = 1.25
	}
	e.ReservedUSD = float64(e.ReservedInputTokens)*maxPromptPrice*cacheMultiplier/1e6 + float64(e.MaxOutputTokens)*maxCompletionPrice/1e6
	return e
}

func (e costEstimate) attributes() []any {
	return []any{
		"cost_estimate_version", e.Version,
		"text_bytes", e.TextBytes,
		"pdf_text_bytes", e.PDFTextBytes,
		"pdf_pages", e.PDFPages,
		"estimated_input_tokens", e.EstimatedInputTokens,
		"reserved_input_tokens", e.ReservedInputTokens,
		"max_output_tokens", e.MaxOutputTokens,
		"reserved_cost_usd", e.ReservedUSD,
	}
}
