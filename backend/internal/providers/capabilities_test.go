package providers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type capabilityFunc func(context.Context, Request) (Response, error)

func (f capabilityFunc) Complete(c context.Context, r Request) (Response, error) { return f(c, r) }
func TestFallbackRetainsImageAndEmulatesSchemaForCompatibleBackend(t *testing.T) {
	calls := 0
	p := NewFallback(0,
		Backend{Name: "text-only", Capabilities: &Capabilities{Images: false}, Provider: capabilityFunc(func(context.Context, Request) (Response, error) {
			t.Fatal("image sent to text-only model")
			return Response{}, nil
		})},
		Backend{Name: "vision", Capabilities: &Capabilities{Images: true, Structured: false}, Provider: capabilityFunc(func(_ context.Context, r Request) (Response, error) {
			calls++
			if r.Image == nil || string(r.Image.Data) != "original" || r.User != "source" || r.Schema != nil || len(r.System) == 0 {
				t.Fatal("fallback lost semantics")
			}
			return Response{Content: []byte(`{}`)}, nil
		})})
	_, err := p.Complete(context.Background(), Request{User: "source", Image: &Image{Data: []byte("original")}, Schema: json.RawMessage(`{"type":"object"}`)})
	if err != nil || calls != 1 {
		t.Fatalf("%v, calls=%d", err, calls)
	}
}
func TestFallbackDoesNotTruncateAnOversizedTask(t *testing.T) {
	p := NewFallback(0, Backend{Name: "small", Capabilities: &Capabilities{MaxInputBytes: 2}, Provider: capabilityFunc(func(context.Context, Request) (Response, error) {
		t.Fatal("oversized source was sent")
		return Response{}, nil
	})})
	_, err := p.Complete(context.Background(), Request{User: "full source"})
	if !errors.Is(err, ErrCapability) {
		t.Fatalf("%v", err)
	}
}
