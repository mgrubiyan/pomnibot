package processingcache

import (
	"context"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type counting struct{ calls int }

func (c *counting) Complete(context.Context, providers.Request) (providers.Response, error) {
	c.calls++
	return providers.Response{Content: []byte(`{"ok":true}`), Model: "test"}, nil
}
func TestCacheIsolatesUsersAndInvalidatesRequestAndModel(t *testing.T) {
	c := New(4096)
	p := &counting{}
	cached := Wrap(p, c, "pipeline/schema-v1/model-a")
	a := WithScope(context.Background(), "user-a")
	b := WithScope(context.Background(), "user-b")
	req := providers.Request{System: "prompt-v1", User: "private"}
	for range 2 {
		if _, err := cached.Complete(a, req); err != nil {
			t.Fatal(err)
		}
	}
	if p.calls != 1 {
		t.Fatal("no reuse")
	}
	_, _ = cached.Complete(b, req)
	req.System = "prompt-v2"
	_, _ = cached.Complete(a, req)
	req.Schema = []byte(`{"type":"object"}`)
	_, _ = cached.Complete(a, req)
	_, _ = Wrap(p, c, "pipeline/schema-v1/model-b").Complete(a, req)
	_, _ = cached.Complete(context.Background(), req)
	_, _ = cached.Complete(context.Background(), req)
	if p.calls != 7 {
		t.Fatalf("isolation/invalidation failed: %d", p.calls)
	}
}
func TestBoundedCacheCopiesDataAndDoesNotCacheCancelledCalls(t *testing.T) {
	c := New(10)
	c.Put("a", []byte("123456"))
	c.Put("b", []byte("789012"))
	if _, ok := c.Get("a"); ok {
		t.Fatal("memory bound not enforced")
	}
	data, _ := c.Get("b")
	data[0] = 'x'
	again, _ := c.Get("b")
	if string(again) != "789012" {
		t.Fatal("cache mutated by caller")
	}
	p := &counting{}
	ctx, cancel := context.WithCancel(WithScope(context.Background(), "a"))
	cancel()
	_, err := Wrap(p, c, "m").Complete(ctx, providers.Request{})
	if err == nil || p.calls != 0 {
		t.Fatal("cancelled call executed")
	}
}

type answerProvider struct {
	calls   int
	content []byte
}

func (p *answerProvider) Complete(context.Context, providers.Request) (providers.Response, error) {
	p.calls++
	return providers.Response{Content: p.content}, nil
}
func TestCacheDoesNotFreezeMalformedOrUncertainReadings(t *testing.T) {
	for _, body := range []string{`{"issues":[]}`, `{"text":"reading","issues":["ambiguous symbol"]}`} {
		p := &answerProvider{content: []byte(body)}
		cached := Wrap(p, New(4096), "reader-v1")
		req := providers.Request{Schema: []byte(`{"type":"object","properties":{"text":{"type":"string"},"issues":{"type":"array","items":{"type":"string"}}},"required":["text","issues"]}`)}
		for range 2 {
			if _, err := cached.Complete(WithScope(context.Background(), "a"), req); err != nil {
				t.Fatal(err)
			}
		}
		if p.calls != 2 {
			t.Fatal("malformed/uncertain reading frozen in cache")
		}
	}
}

func TestDeferredSemanticValidationDoesNotCacheUnvalidatedSuccess(t *testing.T) {
	p := &counting{}
	cached := Wrap(p, New(4096), "model")
	ctx := WithScope(context.Background(), "user")
	for range 2 {
		_, _ = cached.Complete(ctx, providers.Request{NoCache: true})
	}
	if p.calls != 2 {
		t.Fatal("raw response cached before semantic validation")
	}
}
