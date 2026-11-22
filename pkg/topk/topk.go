// Package topk keeps the best N items of a stream in a bounded heap.
package topk

import (
	"cmp"
	"container/heap"
	"errors"
	"slices"
)

// ErrInvalidN is returned when the requested heap size is not positive.
var ErrInvalidN = errors.New("topk: n must be positive")

// Item is a stream observation. Independent copies are safe for concurrent use.
type Item struct {
	Key   string
	Count int64
}

// TopK retains the highest counts, breaking ties by ascending key.
// It is not safe for concurrent use. Construct it with New.
type TopK struct {
	n     int
	items itemHeap
}

// New returns ErrInvalidN when n is zero or negative.
func New(n int) (*TopK, error) {
	if n <= 0 {
		return nil, ErrInvalidN
	}
	return &TopK{n: n, items: make(itemHeap, 0, n)}, nil
}

// Add offers an observation. Repeated keys are kept as separate items.
func (t *TopK) Add(key string, count int64) {
	item := Item{Key: key, Count: count}
	if len(t.items) < t.n {
		heap.Push(&t.items, item)
	} else if compare(item, t.items[0]) < 0 {
		heap.Pop(&t.items)
		heap.Push(&t.items, item)
	}
}

// Result returns an independent snapshot, best first, without changing the heap.
func (t *TopK) Result() []Item {
	result := slices.Clone(t.items)
	slices.SortFunc(result, compare)
	return result
}

// compare uses comparisons rather than subtraction to avoid int64 overflow.
func compare(a, b Item) int {
	if rank := cmp.Compare(b.Count, a.Count); rank != 0 {
		return rank
	}
	return cmp.Compare(a.Key, b.Key)
}

type itemHeap []Item

var _ heap.Interface = (*itemHeap)(nil)

func (h itemHeap) Len() int           { return len(h) }
func (h itemHeap) Less(i, j int) bool { return compare(h[i], h[j]) > 0 }
func (h itemHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *itemHeap) Push(value any)    { *h = append(*h, value.(Item)) }
func (h *itemHeap) Pop() any {
	last := len(*h) - 1
	item := (*h)[last]
	(*h)[last] = Item{}
	*h = (*h)[:last]
	return item
}
