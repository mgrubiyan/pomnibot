package generator

import (
	"context"
	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type fakeProvider struct {
	answer func(int, string, providers.Request) (string, error)
}

func (p *fakeProvider) Complete(_ context.Context, r providers.Request) (providers.Response, error) {
	s, err := p.answer(1, r.User, r)
	return providers.Response{Content: []byte(s), Model: "fake-model"}, err
}

type providerFunc func(context.Context, providers.Request) (providers.Response, error)

func (p providerFunc) Complete(ctx context.Context, r providers.Request) (providers.Response, error) {
	return p(ctx, r)
}
func sourceFixture() material.Document {
	return material.Document{Blocks: []material.Block{{ID: "h", Kind: "heading", Text: "Удаление"}, {ID: "s", Kind: "text", Text: "При удалении записи индекс не перестраивается, если занято не более 900 байт."}}}
}
