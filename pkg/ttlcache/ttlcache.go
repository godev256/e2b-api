// Package ttlcache is a small in-process cache that loads missing values
// through singleflight, so a burst of requests for one cold key results in a
// single call to the loader.
package ttlcache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// evictFraction is the share of entries dropped when a full cache has nothing
// expired to remove. Dropping a slice instead of everything avoids a stampede
// of reloads right after the cache fills up.
const evictFraction = 10

type entry[V any] struct {
	value     V
	expiresAt time.Time
}

// Cache is safe for concurrent use.
type Cache[K comparable, V any] struct {
	ttl         time.Duration
	maxEntries  int
	loadTimeout time.Duration
	now         func() time.Time

	mu      sync.RWMutex
	entries map[K]entry[V]
	group   singleflight.Group
}

// New creates a cache whose entries live for ttl and holds at most maxEntries.
// A loader never runs longer than loadTimeout.
func New[K comparable, V any](ttl time.Duration, maxEntries int, loadTimeout time.Duration) *Cache[K, V] {
	return &Cache[K, V]{
		ttl:         ttl,
		maxEntries:  maxEntries,
		loadTimeout: loadTimeout,
		now:         time.Now,
		entries:     make(map[K]entry[V]),
	}
}

// GetOrLoad returns the cached value for key, or calls load once for all
// concurrent callers and caches a successful result. Errors are not cached.
//
// The loader is shared, so it runs detached from any single caller's
// cancellation (one impatient request must not fail the load for everyone
// waiting on it) but under its own timeout. Each caller still stops waiting
// as soon as its own context is done.
func (c *Cache[K, V]) GetOrLoad(ctx context.Context, key K, load func(context.Context) (V, error)) (V, error) {
	if v, ok := c.get(key); ok {
		return v, nil
	}

	// %#v includes field names and quotes strings, so distinct struct keys
	// cannot produce the same flight key.
	flight := c.group.DoChan(fmt.Sprintf("%#v", key), func() (any, error) {
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.loadTimeout)
		defer cancel()

		v, err := load(loadCtx)
		if err != nil {
			return nil, err
		}
		c.set(key, v)
		return v, nil
	})

	select {
	case <-ctx.Done():
		var zero V
		return zero, ctx.Err()
	case res := <-flight:
		if res.Err != nil {
			var zero V
			return zero, res.Err
		}
		return res.Val.(V), nil
	}
}

func (c *Cache[K, V]) get(key K) (V, bool) {
	c.mu.RLock()
	e, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || !c.now().Before(e.expiresAt) {
		var zero V
		return zero, false
	}
	return e.value, true
}

func (c *Cache[K, V]) set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxEntries {
		c.evictLocked()
	}
	c.entries[key] = entry[V]{value: value, expiresAt: c.now().Add(c.ttl)}
}

func (c *Cache[K, V]) evictLocked() {
	now := c.now()
	for k, e := range c.entries {
		if !now.Before(e.expiresAt) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) < c.maxEntries {
		return
	}
	// Nothing expired: drop an arbitrary slice (map order is random).
	drop := max(c.maxEntries/evictFraction, 1)
	for k := range c.entries {
		if drop == 0 {
			break
		}
		delete(c.entries, k)
		drop--
	}
}
