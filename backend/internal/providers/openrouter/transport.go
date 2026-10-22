package openrouter

import (
	"io"
	"net/http"
	"sync"
	"sync/atomic"
)

type transportPool struct {
	transport *http.Transport
	retired   atomic.Bool
}

// recoveryTransport lets a retry start on a fresh pool even when other HTTP/2
// streams keep the old connection active. Existing requests may finish normally.
type recoveryTransport struct {
	mu       sync.RWMutex
	template *http.Transport
	current  *transportPool
}

func newRecoveryTransport(base *http.Transport) *recoveryTransport {
	return &recoveryTransport{template: base.Clone(), current: &transportPool{transport: base.Clone()}}
}

func (t *recoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.RLock()
	pool := t.current
	t.mu.RUnlock()
	resp, err := pool.transport.RoundTrip(req)
	if err != nil {
		if pool.retired.Load() {
			pool.transport.CloseIdleConnections()
		}
		return nil, err
	}
	resp.Body = &poolBody{ReadCloser: resp.Body, pool: pool}
	return resp, nil
}

func (t *recoveryTransport) CloseIdleConnections() {
	t.mu.Lock()
	old := t.current
	old.retired.Store(true)
	t.current = &transportPool{transport: t.template.Clone()}
	t.mu.Unlock()
	old.transport.CloseIdleConnections()
}

type poolBody struct {
	io.ReadCloser
	pool *transportPool
}

func (b *poolBody) Close() error {
	err := b.ReadCloser.Close()
	if b.pool.retired.Load() {
		b.pool.transport.CloseIdleConnections()
	}
	return err
}
