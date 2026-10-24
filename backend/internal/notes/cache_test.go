package notes

import (
	"context"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/processingcache"
)

type readingCounter struct {
	calls  int
	failed bool
}

func (r *readingCounter) Extract(context.Context, []ingest.File) (ingest.Result, error) {
	r.calls++
	return ingest.Result{Text: "Прочитанный текст", Pages: []ingest.PageInfo{{ReadFailed: r.failed, Issues: []string{"Обрезанный источник; условие не восстановлено."}}}}, nil
}
func TestReadingReuseSurvivesGeneratorChangeButIsolatesReaderContentAndUser(t *testing.T) {
	reader := &readingCounter{}
	m := New(reader, &fakeGenerator{}, &fakeSaver{}, 1).WithCache(processingcache.New(1<<20), "reader-model/prompt/schema")
	ctx := processingcache.WithScope(context.Background(), "u1")
	files := file("source.pdf")
	_, _ = m.extract(ctx, files)
	m.generator = &fakeGenerator{} // a new writer must not repeat successful reading
	_, _ = m.extract(ctx, files)
	if reader.calls != 1 {
		t.Fatal("generator change repeated reading")
	}
	_, _ = m.extract(processingcache.WithScope(context.Background(), "u2"), files)
	m.cacheIdentity = "different-reader"
	_, _ = m.extract(ctx, files)
	files[0].Data = []byte("changed content")
	_, _ = m.extract(ctx, files)
	if reader.calls != 4 {
		t.Fatalf("stale or cross-user reuse: calls=%d", reader.calls)
	}
	reader.failed = true
	files[0].Data = []byte("new failed reading")
	_, _ = m.extract(ctx, files)
	_, _ = m.extract(ctx, files)
	if reader.calls != 6 {
		t.Fatal("failed reading reused as success")
	}
}
