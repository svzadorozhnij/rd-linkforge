package cache_test

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/skskuzan/rd-linkforge/pkg/cache"
)

func newLRU[K comparable, V any](t *testing.T, capacity int) *cache.LRU[K, V] {
	t.Helper()
	c, err := cache.NewLRU[K, V](capacity)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func checkKeys[K comparable, V any](t *testing.T, c *cache.LRU[K, V], want ...K) {
	t.Helper()
	if got := c.Keys(); !slices.Equal(got, want) {
		t.Errorf("Keys() = %v, want %v", got, want)
	}
	if got := c.Len(); got != len(want) {
		t.Errorf("Len() = %d, want %d", got, len(want))
	}
}

func TestNewLRUInvalidCapacity(t *testing.T) {
	for _, capacity := range []int{0, -1, -100} {
		c, err := cache.NewLRU[string, int](capacity)
		if c != nil || !errors.Is(err, cache.ErrInvalidCapacity) {
			t.Errorf("NewLRU(%d) = %v, %v; want nil, ErrInvalidCapacity", capacity, c, err)
		}
	}
}

func TestLRUGetAndEviction(t *testing.T) {
	c := newLRU[string, int](t, 2)
	checkKeys(t, c)
	if value, ok := c.Get("missing"); value != 0 || ok {
		t.Fatalf("Get(missing) = %d, %v; want 0, false", value, ok)
	}
	c.Delete("missing")
	c.Put("a", 1)
	c.Put("b", 2)
	checkKeys(t, c, "b", "a")

	if value, ok := c.Get("a"); value != 1 || !ok {
		t.Fatalf("Get(a) = %d, %v; want 1, true", value, ok)
	}
	checkKeys(t, c, "a", "b")
	// A miss must not affect which entry gets evicted next.
	c.Get("missing")
	c.Put("c", 3)
	checkKeys(t, c, "c", "a")
	if value, ok := c.Get("b"); value != 0 || ok {
		t.Fatalf("Get(evicted b) = %d, %v; want 0, false", value, ok)
	}
}

func TestLRUPutReplacesAndPromotes(t *testing.T) {
	c := newLRU[string, int](t, 2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("a", 10)
	checkKeys(t, c, "a", "b")
	c.Put("c", 3)
	checkKeys(t, c, "c", "a")
	if value, ok := c.Get("a"); value != 10 || !ok {
		t.Errorf("Get(updated a) = %d, %v; want 10, true", value, ok)
	}
	if _, ok := c.Get("b"); ok {
		t.Error("b should have been evicted")
	}
}

func TestLRUDeleteAndReinsert(t *testing.T) {
	c := newLRU[string, int](t, 3)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)
	c.Delete("b")
	checkKeys(t, c, "c", "a")
	if _, ok := c.Get("b"); ok {
		t.Error("deleted b is still present")
	}
	c.Delete("missing")
	checkKeys(t, c, "c", "a")
	c.Put("b", 20)
	checkKeys(t, c, "b", "c", "a")
	c.Delete("b")
	c.Delete("a")
	c.Delete("c")
	c.Delete("c")
	checkKeys(t, c)
	c.Put("d", 4)
	checkKeys(t, c, "d")
}

func TestLRUCapacityOneAndZeroValues(t *testing.T) {
	c := newLRU[int, *string](t, 1)
	c.Put(0, nil)
	if value, ok := c.Get(0); value != nil || !ok {
		t.Fatalf("Get(stored nil) = %v, %v; want nil, true", value, ok)
	}
	value := "replacement"
	c.Put(0, &value)
	if got, ok := c.Get(0); got != &value || !ok {
		t.Fatalf("Get(updated 0) = %v, %v; want stored pointer, true", got, ok)
	}
	c.Put(1, nil)
	checkKeys(t, c, 1)
	if got, ok := c.Get(0); got != nil || ok {
		t.Errorf("Get(evicted 0) = %v, %v; want nil, false", got, ok)
	}
}

func TestLRUKeysSnapshotDoesNotChangeOrder(t *testing.T) {
	c := newLRU[string, int](t, 2)
	c.Put("a", 1)
	c.Put("b", 2)
	keys := c.Keys()
	checkKeys(t, c, "b", "a")
	keys[0] = "changed"
	checkKeys(t, c, "b", "a")
	c.Put("c", 3)
	checkKeys(t, c, "c", "b")
	if !slices.Equal(keys, []string{"changed", "a"}) {
		t.Errorf("cache mutation changed an earlier Keys snapshot: %v", keys)
	}
}

func TestLRUConcurrentUse(t *testing.T) {
	const capacity = 16
	c := newLRU[int, int](t, capacity)
	var workers sync.WaitGroup
	for worker := range 8 {
		workers.Go(func() {
			for i := range 100 {
				key := worker*100 + i
				c.Put(key, i)
				c.Get(key)
				c.Keys()
				if size := c.Len(); size > capacity {
					t.Errorf("Len() = %d exceeds capacity %d", size, capacity)
				}
				c.Delete(key)
			}
		})
	}
	workers.Wait()
	checkKeys(t, c)
	// Ensure concurrent changes have left the cache usable.
	c.Put(1, 10)
	if value, ok := c.Get(1); value != 10 || !ok {
		t.Errorf("Get(1) = %d, %v; want 10, true", value, ok)
	}
}

func BenchmarkLRUGetHit(b *testing.B) {
	c, err := cache.NewLRU[string, int](1024)
	if err != nil {
		b.Fatal(err)
	}
	c.Put("hot", 42)
	b.ReportAllocs()
	for b.Loop() {
		if value, ok := c.Get("hot"); value != 42 || !ok {
			b.Fatalf("Get(hot) = %d, %v; want 42, true", value, ok)
		}
	}
}
