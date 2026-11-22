package cache

import (
	"sync"
	"testing"
)

func hw5Hammer(t *testing.T, c Cache[int, int], workers, ops int) {
	t.Helper()
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				k := (w*31 + i) % 97
				switch i % 4 {
				case 0, 1:
					c.Put(k, i)
				case 2:
					c.Get(k)
				default:
					c.Delete(k)
				}
				c.Len()
			}
		}(w)
	}
	wg.Wait()
}

func TestLRUIsSafeForConcurrentUse(t *testing.T) {
	c, err := NewLRU[int, int](50)
	if err != nil {
		t.Fatal(err)
	}
	hw5Hammer(t, c, 8, 3000)
	if c.Len() > 50 {
		t.Errorf("Len = %d, capacity is 50", c.Len())
	}
	if len(c.Keys()) != c.Len() {
		t.Errorf("Keys has %d entries but Len = %d", len(c.Keys()), c.Len())
	}
}

func TestLRUKeysWhileWriting(t *testing.T) {
	c, _ := NewLRU[int, int](20)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 5000; i++ {
			c.Put(i%40, i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 5000; i++ {
			if n := len(c.Keys()); n > 20 {
				t.Errorf("Keys returned %d entries, capacity is 20", n)
				return
			}
		}
	}()
	wg.Wait()
}
