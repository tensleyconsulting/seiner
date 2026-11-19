// Package cache is a small generic TTL cache for list results, so repeat
// expands within the TTL skip the provider call. It has no background
// goroutine; entries expire lazily on read. Flight adds single-flight
// fetching on top, for lookups many callers share (enabled APIs, regions).
package cache

import (
	"strings"
	"sync"
	"time"
)

// Cache is a mutex-protected map of key to value, each entry expiring ttl
// after it was Set. The zero value is not usable; use New.
type Cache[V any] struct {
	ttl time.Duration
	now func() time.Time

	mu sync.Mutex
	m  map[string]entry[V]
}

type entry[V any] struct {
	val V
	exp time.Time
}

// New returns a Cache whose entries live for ttl after Set.
func New[V any](ttl time.Duration) *Cache[V] {
	return &Cache[V]{ttl: ttl, now: time.Now, m: map[string]entry[V]{}}
}

// Get returns the value for key, or false if it is absent or expired. An
// expired entry is deleted.
func (c *Cache[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		var zero V
		return zero, false
	}
	if c.now().After(e.exp) {
		delete(c.m, key)
		var zero V
		return zero, false
	}
	return e.val, true
}

// Set stores v under key, expiring after the cache's TTL.
func (c *Cache[V]) Set(key string, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = entry[V]{val: v, exp: c.now().Add(c.ttl)}
}

// DeletePrefix removes every entry whose key starts with prefix.
func (c *Cache[V]) DeletePrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.m {
		if strings.HasPrefix(k, prefix) {
			delete(c.m, k)
		}
	}
}
