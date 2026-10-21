// Package processingcache provides bounded, process-local private artifact reuse.
package processingcache

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

type scopeKey struct{}

// WithScope assigns the private namespace for one user.
func WithScope(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, scopeKey{}, user)
}

// Scope returns the user namespace, empty outside private processing.
func Scope(ctx context.Context) string { s, _ := ctx.Value(scopeKey{}).(string); return s }

type entry struct {
	key     string
	data    []byte
	expires time.Time
}

// Cache is an in-memory LRU with a byte limit and one-hour expiry.
type Cache struct {
	mu          sync.Mutex
	limit, size int
	entries     map[string]*list.Element
	order       *list.List
}

// New allocates an empty cache with the given byte limit.
func New(bytes int) *Cache {
	return &Cache{limit: max(bytes, 0), entries: map[string]*list.Element{}, order: list.New()}
}

// Get returns an independent copy of an unexpired artifact.
func (c *Cache) Get(key string) ([]byte, bool) {
	if key == "" {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	v := e.Value.(entry)
	if time.Now().After(v.expires) {
		c.remove(e)
		return nil, false
	}
	c.order.MoveToFront(e)
	return slices.Clone(v.data), true
}

// Put stores a copy, evicting old artifacts within the byte limit.
func (c *Cache) Put(key string, data []byte) {
	if key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if old, ok := c.entries[key]; ok {
		c.remove(old)
	}
	if len(data) > c.limit {
		return
	}
	for c.size+len(data) > c.limit {
		c.remove(c.order.Back())
	}
	c.entries[key] = c.order.PushFront(entry{key: key, data: slices.Clone(data), expires: time.Now().Add(time.Hour)})
	c.size += len(data)
}
func (c *Cache) remove(e *list.Element) {
	v := e.Value.(entry)
	delete(c.entries, v.key)
	c.size -= len(v.data)
	c.order.Remove(e)
}

// Key hashes structured identity components.
func Key(parts ...any) string {
	b, err := json.Marshal(parts)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

type cached struct {
	p        providers.Provider
	c        *Cache
	identity string
}

// Wrap caches model responses within a user; identity includes processing version and all configured model identities in a
// fallback chain. Request bytes include prompt, schema, temperature and images.
// No scope means no reuse (e.g. interactive student grading).
func Wrap(p providers.Provider, c *Cache, identity string) providers.Provider {
	return cached{p, c, identity}
}
func (p cached) Complete(ctx context.Context, r providers.Request) (providers.Response, error) {
	if err := ctx.Err(); err != nil {
		return providers.Response{}, err
	}
	scope := Scope(ctx)
	if scope == "" || r.NoCache {
		return p.p.Complete(ctx, r)
	}
	key := Key(scope, p.identity, r)
	if data, ok := p.c.Get(key); ok {
		var out providers.Response
		if json.Unmarshal(data, &out) == nil {
			// Cached tokens/cost were billed on the original call, never again.
			out.Usage = providers.Usage{}
			out.Cached = true
			slog.Debug("processing: model cache hit")
			return out, nil
		}
	}
	out, err := p.p.Complete(ctx, r)
	if err == nil && !out.Truncated && ctx.Err() == nil && reusable(out.Content, r.Schema) {
		data, _ := json.Marshal(out)
		p.c.Put(key, data)
	}
	return out, err
}
