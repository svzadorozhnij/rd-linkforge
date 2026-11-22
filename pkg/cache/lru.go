package cache

import (
	"container/list"
	"errors"
	"sync"
)

// ErrInvalidCapacity is returned when a cache capacity is not positive.
var ErrInvalidCapacity = errors.New("cache: capacity must be positive")

// LRU is a bounded, concurrency-safe cache that evicts the least recently used
// entry. Construct it with NewLRU. An LRU must not be copied after first use.
type LRU[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	items    map[K]*list.Element
	order    list.List // Front is the most recently used entry.
}

type entry[K comparable, V any] struct {
	key   K
	value V
}

var _ Cache[string, int] = (*LRU[string, int])(nil)

// NewLRU returns ErrInvalidCapacity when capacity is zero or negative.
func NewLRU[K comparable, V any](capacity int) (*LRU[K, V], error) {
	if capacity <= 0 {
		return nil, ErrInvalidCapacity
	}
	return &LRU[K, V]{
		capacity: capacity,
		items:    make(map[K]*list.Element, capacity),
	}, nil
}

// Get returns the value for key and marks a hit as most recently used.
func (c *LRU[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		return elem.Value.(entry[K, V]).value, true
	}
	var zero V
	return zero, false
}

// Put inserts or replaces a value and marks it as most recently used.
func (c *LRU[K, V]) Put(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		elem.Value = entry[K, V]{key: key, value: value}
		c.order.MoveToFront(elem)
		return
	}
	if len(c.items) == c.capacity {
		oldest := c.order.Back()
		delete(c.items, oldest.Value.(entry[K, V]).key)
		c.order.Remove(oldest)
	}
	c.items[key] = c.order.PushFront(entry[K, V]{key: key, value: value})
}

// Delete removes key if it exists.
func (c *LRU[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		delete(c.items, key)
		c.order.Remove(elem)
	}
}

// Len returns the number of cached entries.
func (c *LRU[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Keys returns the keys from the most recently used to the least recently used.
// It does not change the order. The returned slice is independent of the cache.
func (c *LRU[K, V]) Keys() []K {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := make([]K, 0, len(c.items))
	for elem := c.order.Front(); elem != nil; elem = elem.Next() {
		keys = append(keys, elem.Value.(entry[K, V]).key)
	}
	return keys
}
