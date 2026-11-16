package topk_test

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/skskuzan/rd-linkforge/pkg/topk"
)

func newTopK(t *testing.T, n int) *topk.TopK {
	t.Helper()
	k, err := topk.New(n)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestTopKInvalidN(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		k, err := topk.New(n)
		if k != nil || !errors.Is(err, topk.ErrInvalidN) {
			t.Errorf("New(%d) = %v, %v; want nil, ErrInvalidN", n, k, err)
		}
	}
}

func TestTopKExampleAndSnapshots(t *testing.T) {
	k := newTopK(t, 3)
	if got := k.Result(); len(got) != 0 {
		t.Fatalf("empty Result() = %v", got)
	}
	for i, count := range []int64{5, 1, 9, 3, 9, 7, 2} {
		k.Add(fmt.Sprintf("k%d", i), count)
	}
	want := []topk.Item{{Key: "k2", Count: 9}, {Key: "k4", Count: 9}, {Key: "k5", Count: 7}}
	for range 2 {
		if got := k.Result(); !slices.Equal(got, want) {
			t.Fatalf("Result() = %v, want %v", got, want)
		}
	}
	snapshot := k.Result()
	snapshot[0] = topk.Item{Key: "changed", Count: -1}
	if got := k.Result(); !slices.Equal(got, want) {
		t.Fatalf("caller mutated the heap through Result: %v", got)
	}
	k.Add("best", 10)
	want = []topk.Item{{Key: "best", Count: 10}, {Key: "k2", Count: 9}, {Key: "k4", Count: 9}}
	if got := k.Result(); !slices.Equal(got, want) {
		t.Fatalf("Add after Result() = %v, want %v", got, want)
	}
	if snapshot[0].Key != "changed" {
		t.Fatal("adding an item changed an earlier result snapshot")
	}
}

func TestTopKMatchesSortedStream(t *testing.T) {
	// Include duplicate keys, equal ranks, empty keys and integer extremes.
	stream := []topk.Item{
		{Key: "z", Count: 0}, {Key: "b", Count: 0}, {Key: "a", Count: 0},
		{Key: "a", Count: 0}, {Key: "", Count: -1},
		{Key: "min", Count: math.MinInt64}, {Key: "max", Count: math.MaxInt64},
	}
	for i := range 80 {
		stream = append(stream, topk.Item{Key: fmt.Sprintf("key%d", i%9), Count: int64(i*17%23 - 11)})
	}
	for _, n := range []int{1, 2, 3, 10, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			k := newTopK(t, n)
			for i, item := range stream {
				k.Add(item.Key, item.Count)
				want := slices.Clone(stream[:i+1])
				slices.SortFunc(want, func(a, b topk.Item) int {
					if a.Count != b.Count {
						return cmp.Compare(b.Count, a.Count)
					}
					return cmp.Compare(a.Key, b.Key)
				})
				want = want[:min(n, len(want))]
				if got := k.Result(); !slices.Equal(got, want) {
					t.Fatalf("after item %d: got %v, want %v", i, got, want)
				}
			}
		})
	}
}

func TestTopKDuplicateKeysAreNotMerged(t *testing.T) {
	k := newTopK(t, 3)
	k.Add("same", 5)
	k.Add("same", 5)
	k.Add("same", 4)
	k.Add("same", 4)
	want := []topk.Item{{Key: "same", Count: 5}, {Key: "same", Count: 5}, {Key: "same", Count: 4}}
	if got := k.Result(); !slices.Equal(got, want) {
		t.Fatalf("Result() = %v, want %v", got, want)
	}
}
