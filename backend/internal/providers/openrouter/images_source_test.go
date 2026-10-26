package openrouter

import (
	"context"
	"encoding/json"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"strings"
	"testing"
)

func TestSourcePDFAndCropImagesAreBothSent(t *testing.T) {
	c, err := New(Config{APIKey: "test", Model: DefaultModel, BaseURL: "https://example.invalid", MaxTokens: 1000})
	if err != nil {
		t.Fatal(err)
	}
	body, err := c.requestBody(context.Background(), providers.Request{Source: "material", User: "catalog order", PDFs: []providers.PDF{{Data: []byte("pdf"), Name: "book.pdf", Pages: 1}}, Images: []providers.Image{{Data: []byte("crop"), MimeType: "image/png"}}})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(body)
	for _, want := range []string{"file_data", "image_url", "catalog order", "material"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("lost %s: %s", want, data)
		}
	}
}
