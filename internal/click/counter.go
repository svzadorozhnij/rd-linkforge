package click

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/skskuzan/rd-linkforge/pkg/topk"
)

// hourCounts maps a code to the start of each UTC hour and the clicks in it.
type hourCounts map[string]map[time.Time]int64

// Counters keeps click counts per link and per UTC hour. A single owner
// goroutine holds the data; every other goroutine sends it work over a channel,
// so there is no mutex around the counts.
type Counters struct {
	requests  chan func(hourCounts) // run by the owner goroutine
	done      chan struct{}         // closed by Close; stops the owner goroutine
	closeOnce sync.Once
}

var _ Aggregator = (*Counters)(nil)
var _ BatchSink = (*Counters)(nil)

var (
	ErrInvalidGranularity = errors.New("click: invalid granularity")
	ErrInvalidN           = errors.New("click: n must be positive")
	ErrClosed             = errors.New("click: counters are closed")
)

// NewCounters starts the owner goroutine.
func NewCounters() *Counters {
	c := &Counters{
		requests: make(chan func(hourCounts)),
		done:     make(chan struct{}),
	}
	go func() {
		counts := make(hourCounts) // only this goroutine ever touches it
		for {
			select {
			case req := <-c.requests:
				req(counts)
			case <-c.done:
				return
			}
		}
	}()
	return c
}

// Add counts the clicks of a batch. It returns when the owner goroutine has taken the batch.
// A click with an empty Code is ignored. After Close it does nothing and never blocks.
func (c *Counters) Add(batch []Click) {
	select {
	case c.requests <- func(counts hourCounts) { addClicks(counts, batch) }:
	case <-c.done:
	}
}

func (c *Counters) Series(ctx context.Context, code string, w Window, g Granularity) ([]Bucket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g != GranularityHour && g != GranularityDay {
		return nil, ErrInvalidGranularity
	}
	return query(ctx, c, func(counts hourCounts) []Bucket { return series(counts, code, w, g) })
}

func (c *Counters) Top(ctx context.Context, w Window, n int) ([]Total, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if n < 1 {
		return nil, ErrInvalidN
	}
	return query(ctx, c, func(counts hourCounts) []Total { return top(counts, w, n) })
}

// Close stops the owner goroutine. It may be called more than once, and at the same time as the other methods.
func (c *Counters) Close() {
	c.closeOnce.Do(func() { close(c.done) })
}

// query has the owner goroutine compute a result and waits for it, giving up
// when ctx ends or the counters close.
func query[T any](ctx context.Context, c *Counters, compute func(hourCounts) T) (T, error) {
	var zero T
	select {
	case <-c.done:
		return zero, ErrClosed
	default:
	}
	reply := make(chan T, 1) // room for one value: the owner never waits for a caller that left
	select {
	case c.requests <- func(counts hourCounts) { reply <- compute(counts) }:
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-c.done:
		return zero, ErrClosed
	}
	select {
	case res := <-reply:
		return res, nil
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-c.done:
		return zero, ErrClosed
	}
}

// --- run only by the owner goroutine ---

func addClicks(counts hourCounts, batch []Click) {
	for _, cl := range batch {
		if cl.Code == "" {
			continue
		}
		if counts[cl.Code] == nil {
			counts[cl.Code] = make(map[time.Time]int64)
		}
		counts[cl.Code][cl.At.UTC().Truncate(time.Hour)]++
	}
}

// inWindow selects an hour bucket by its start: From <= start < To.
func inWindow(start time.Time, w Window) bool {
	return !start.Before(w.From) && start.Before(w.To)
}

func series(counts hourCounts, code string, w Window, g Granularity) []Bucket {
	sums := make(map[time.Time]int64)
	for start, n := range counts[code] {
		if !inWindow(start, w) {
			continue
		}
		if g == GranularityDay {
			start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
		}
		sums[start] += n
	}
	out := make([]Bucket, 0, len(sums))
	for start, n := range sums {
		out = append(out, Bucket{Start: start, Clicks: n})
	}
	slices.SortFunc(out, func(a, b Bucket) int { return a.Start.Compare(b.Start) })
	return out
}

func top(counts hourCounts, w Window, n int) []Total {
	best, _ := topk.New(n) // Top has already checked n >= 1
	for code, hours := range counts {
		var sum int64
		for start, clicks := range hours {
			if inWindow(start, w) {
				sum += clicks
			}
		}
		if sum > 0 {
			best.Add(code, sum)
		}
	}
	var out []Total
	for _, it := range best.Result() {
		out = append(out, Total{Code: it.Key, Clicks: it.Count})
	}
	return out
}
