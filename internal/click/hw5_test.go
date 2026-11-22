package click

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type hw5Store struct {
	mu       sync.Mutex
	batches  [][]Click
	block    chan struct{}
	failErr  error
	appended chan struct{} // receives one value per successful Append
}

func newHW5Store() *hw5Store { return &hw5Store{appended: make(chan struct{}, 1000)} }

func (s *hw5Store) Append(ctx context.Context, batch []Click) error {
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if s.failErr != nil {
		return s.failErr
	}
	s.mu.Lock()
	s.batches = append(s.batches, batch)
	s.mu.Unlock()
	s.appended <- struct{}{}
	return nil
}

func (s *hw5Store) Range(context.Context, string, Window) ([]Click, error) { return nil, nil }

func (s *hw5Store) total() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, b := range s.batches {
		n += len(b)
	}
	return n
}

func (s *hw5Store) sizes() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []int
	for _, b := range s.batches {
		out = append(out, len(b))
	}
	return out
}

type hw5Metrics struct {
	dropped atomic.Int64
	mu      sync.Mutex
	depths  []int
}

func (m *hw5Metrics) ObserveClickDropped() { m.dropped.Add(1) }
func (m *hw5Metrics) SetQueueDepth(n int) {
	m.mu.Lock()
	m.depths = append(m.depths, n)
	m.mu.Unlock()
}

type hw5Sink struct {
	mu     sync.Mutex
	calls  int
	clicks int
}

func (s *hw5Sink) Add(batch []Click) {
	s.mu.Lock()
	s.calls++
	s.clicks += len(batch)
	s.mu.Unlock()
}

func hw5Click(code string) Click {
	return Click{Code: code, At: time.Date(2030, 1, 2, 10, 0, 0, 0, time.UTC)}
}

func hw5Shutdown(t *testing.T, p *Pipeline) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

var _ Ingester = (*Pipeline)(nil)

func TestPipelineFlushesFullBatches(t *testing.T) {
	st := newHW5Store()
	p := NewPipeline(st, WithWorkers(1), WithBatchSize(3), WithFlushInterval(time.Hour))
	p.Start()
	for i := 0; i < 6; i++ {
		if !p.Submit(hw5Click("a")) {
			t.Fatal("Submit refused an event with an empty queue")
		}
	}
	hw5Shutdown(t, p)
	if got := st.sizes(); len(got) != 2 || got[0] != 3 || got[1] != 3 {
		t.Errorf("batch sizes = %v, want [3 3]", got)
	}
}

func TestPipelineFlushesOnInterval(t *testing.T) {
	st := newHW5Store()
	p := NewPipeline(st, WithWorkers(1), WithBatchSize(100), WithFlushInterval(20*time.Millisecond))
	p.Start()
	p.Submit(hw5Click("a"))
	p.Submit(hw5Click("b"))
	select {
	case <-st.appended:
	case <-time.After(3 * time.Second):
		t.Fatal("a half-empty batch was never flushed by the interval")
	}
	if st.total() != 2 {
		t.Errorf("flushed %d events, want 2", st.total())
	}
	hw5Shutdown(t, p)
}

func TestPipelineShutdownDrainsTheQueue(t *testing.T) {
	st := newHW5Store()
	p := NewPipeline(st, WithWorkers(3), WithBatchSize(1000), WithFlushInterval(time.Hour))
	p.Start()
	for i := 0; i < 10; i++ {
		p.Submit(hw5Click("a"))
	}
	hw5Shutdown(t, p)
	if st.total() != 10 {
		t.Errorf("stored %d events, want all 10", st.total())
	}
}

func TestPipelineDropsWhenTheQueueIsFull(t *testing.T) {
	st := newHW5Store()
	st.block = make(chan struct{})
	m := &hw5Metrics{}
	p := NewPipeline(st, WithWorkers(1), WithQueueSize(2), WithBatchSize(1), WithMetrics(m), WithFlushInterval(time.Hour))
	p.Start()
	accepted, rejected := 0, 0
	submitted := make(chan struct{})
	go func() {
		defer close(submitted)
		for i := 0; i < 20; i++ {
			if p.Submit(hw5Click("a")) {
				accepted++
			} else {
				rejected++
			}
		}
	}()
	select {
	case <-submitted:
	case <-time.After(3 * time.Second):
		t.Fatal("Submit blocks on a full queue; it must drop the event and return false")
	}
	if rejected == 0 || accepted < 2 {
		t.Fatalf("accepted %d, rejected %d: a full queue must reject, and the queue holds at least 2", accepted, rejected)
	}
	if got := int(m.dropped.Load()); got != rejected {
		t.Errorf("dropped metric = %d, want %d", got, rejected)
	}
	close(st.block)
	hw5Shutdown(t, p)
	if st.total() != accepted {
		t.Errorf("stored %d events, accepted %d", st.total(), accepted)
	}
}

func TestPipelineRefusesAfterShutdown(t *testing.T) {
	m := &hw5Metrics{}
	p := NewPipeline(newHW5Store(), WithWorkers(1), WithMetrics(m))
	p.Start()
	hw5Shutdown(t, p)
	if p.Submit(hw5Click("a")) {
		t.Error("Submit accepted an event after Shutdown")
	}
	if m.dropped.Load() != 1 {
		t.Errorf("dropped metric = %d, want 1", m.dropped.Load())
	}
	hw5Shutdown(t, p) // a second call is fine
}

func TestPipelineSubmitAndShutdownRace(t *testing.T) {
	st := newHW5Store()
	p := NewPipeline(st, WithWorkers(4), WithQueueSize(64), WithBatchSize(10), WithFlushInterval(5*time.Millisecond))
	p.Start()
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 2000; i++ {
				if p.Submit(hw5Click("a")) {
					accepted.Add(1)
				}
			}
		}()
	}
	time.Sleep(5 * time.Millisecond)
	hw5Shutdown(t, p)
	wg.Wait()
	if int64(st.total()) != accepted.Load() {
		t.Errorf("stored %d events, but %d were accepted", st.total(), accepted.Load())
	}
}

func TestPipelineStoreErrorDropsTheBatch(t *testing.T) {
	st := newHW5Store()
	st.failErr = errors.New("disk on fire")
	m := &hw5Metrics{}
	sink := &hw5Sink{}
	p := NewPipeline(st, WithWorkers(1), WithBatchSize(2), WithMetrics(m), WithSink(sink), WithFlushInterval(time.Hour))
	p.Start()
	for i := 0; i < 4; i++ {
		p.Submit(hw5Click("a"))
	}
	hw5Shutdown(t, p)
	if m.dropped.Load() != 4 {
		t.Errorf("dropped metric = %d, want 4 (one per event of the failed batches)", m.dropped.Load())
	}
	if sink.calls != 0 {
		t.Errorf("the sink got %d batches although the store failed", sink.calls)
	}
}

func TestPipelineSinkGetsStoredBatches(t *testing.T) {
	st := newHW5Store()
	sink := &hw5Sink{}
	p := NewPipeline(st, WithWorkers(2), WithBatchSize(5), WithSink(sink), WithFlushInterval(time.Hour))
	p.Start()
	for i := 0; i < 23; i++ {
		p.Submit(hw5Click("a"))
	}
	hw5Shutdown(t, p)
	if sink.clicks != 23 || st.total() != 23 {
		t.Errorf("sink got %d clicks, store %d, want 23 each", sink.clicks, st.total())
	}
}

func TestPipelineShutdownTimeout(t *testing.T) {
	st := newHW5Store()
	st.block = make(chan struct{}) // never released
	p := NewPipeline(st, WithWorkers(1), WithBatchSize(1), WithFlushInterval(time.Hour))
	p.Start()
	p.Submit(hw5Click("a"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := p.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown err = %v, want context.DeadlineExceeded", err)
	}
	// The workers' context was cancelled, so the stuck Append returns and a second Shutdown completes.
	hw5Shutdown(t, p)
}

func TestPipelineReportsQueueDepth(t *testing.T) {
	m := &hw5Metrics{}
	p := NewPipeline(newHW5Store(), WithQueueSize(5), WithMetrics(m)) // never started: nothing reads the queue
	for i := 0; i < 3; i++ {
		p.Submit(hw5Click("a"))
	}
	if len(m.depths) != 3 || m.depths[0] != 1 || m.depths[1] != 2 || m.depths[2] != 3 {
		t.Errorf("queue depths = %v, want [1 2 3]", m.depths)
	}
	hw5Shutdown(t, p)
}

func TestPipelineIgnoresInvalidOptions(t *testing.T) {
	p := NewPipeline(newHW5Store(), WithQueueSize(0), WithWorkers(-1), WithBatchSize(0), WithFlushInterval(0), WithMetrics(nil))
	for i := 0; i < 100; i++ {
		if !p.Submit(hw5Click("a")) {
			t.Fatal("an invalid WithQueueSize must keep the default queue size")
		}
	}
}

func hw5At(day, hour, minute int) time.Time {
	return time.Date(2030, 1, day, hour, minute, 0, 0, time.UTC)
}

func hw5Counters(t *testing.T) *Counters {
	t.Helper()
	c := NewCounters()
	t.Cleanup(c.Close)
	return c
}

var hw5Whole = Window{From: hw5At(1, 0, 0), To: hw5At(31, 0, 0)}

func TestCountersSeries(t *testing.T) {
	c := hw5Counters(t)
	c.Add([]Click{
		{Code: "a", At: hw5At(2, 10, 15)}, {Code: "a", At: hw5At(2, 10, 45)}, {Code: "a", At: hw5At(2, 11, 5)},
		{Code: "a", At: hw5At(3, 1, 0)}, {Code: "b", At: hw5At(2, 10, 0)}, {Code: "", At: hw5At(2, 10, 0)},
	})
	ctx := context.Background()

	hours, err := c.Series(ctx, "a", hw5Whole, GranularityHour)
	want := []Bucket{{hw5At(2, 10, 0), 2}, {hw5At(2, 11, 0), 1}, {hw5At(3, 1, 0), 1}}
	if err != nil || len(hours) != 3 || hours[0] != want[0] || hours[1] != want[1] || hours[2] != want[2] {
		t.Errorf("hourly = %v, %v; want %v", hours, err, want)
	}

	days, err := c.Series(ctx, "a", hw5Whole, GranularityDay)
	if err != nil || len(days) != 2 || days[0] != (Bucket{hw5At(2, 0, 0), 3}) || days[1] != (Bucket{hw5At(3, 0, 0), 1}) {
		t.Errorf("daily = %v, %v", days, err)
	}

	if none, err := c.Series(ctx, "unknown", hw5Whole, GranularityHour); err != nil || len(none) != 0 {
		t.Errorf("unknown code = %v, %v", none, err)
	}
	if _, err := c.Series(ctx, "a", hw5Whole, Granularity("week")); !errors.Is(err, ErrInvalidGranularity) {
		t.Errorf("bad granularity err = %v", err)
	}
}

func TestCountersWindowSelectsHoursByTheirStart(t *testing.T) {
	c := hw5Counters(t)
	c.Add([]Click{{Code: "a", At: hw5At(2, 10, 15)}, {Code: "a", At: hw5At(2, 11, 5)}})
	ctx := context.Background()

	got, _ := c.Series(ctx, "a", Window{From: hw5At(2, 10, 30), To: hw5At(2, 12, 0)}, GranularityHour)
	if len(got) != 1 || got[0].Start != hw5At(2, 11, 0) {
		t.Errorf("the 10:00 bucket starts before From and must be left out: %v", got)
	}
	got, _ = c.Series(ctx, "a", Window{From: hw5At(2, 10, 0), To: hw5At(2, 11, 0)}, GranularityHour)
	if len(got) != 1 || got[0].Start != hw5At(2, 10, 0) {
		t.Errorf("To is exclusive, so the 11:00 bucket must be left out: %v", got)
	}
}

func TestCountersTop(t *testing.T) {
	c := hw5Counters(t)
	add := func(code string, n int, at time.Time) {
		batch := make([]Click, n)
		for i := range batch {
			batch[i] = Click{Code: code, At: at}
		}
		c.Add(batch)
	}
	add("a", 5, hw5At(2, 10, 0))
	add("b", 3, hw5At(2, 10, 0))
	add("c", 3, hw5At(2, 11, 0))
	add("d", 1, hw5At(2, 12, 0))
	add("old", 9, hw5At(1, 9, 0))
	ctx := context.Background()
	w := Window{From: hw5At(2, 0, 0), To: hw5At(3, 0, 0)}

	got, err := c.Top(ctx, w, 3)
	want := []Total{{"a", 5}, {"b", 3}, {"c", 3}}
	if err != nil || len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("Top(3) = %v, %v; want %v (ties go to the smaller code)", got, err, want)
	}
	if got, _ := c.Top(ctx, w, 2); len(got) != 2 || got[1].Code != "b" {
		t.Errorf("Top(2) = %v", got)
	}
	if got, _ := c.Top(ctx, w, 10); len(got) != 4 {
		t.Errorf("Top(10) = %v, want the 4 codes seen in the window", got)
	}
	if got, _ := c.Top(ctx, Window{From: hw5At(20, 0, 0), To: hw5At(21, 0, 0)}, 3); len(got) != 0 {
		t.Errorf("an empty window gave %v", got)
	}
	for _, n := range []int{0, -1} {
		if _, err := c.Top(ctx, w, n); !errors.Is(err, ErrInvalidN) {
			t.Errorf("Top(n=%d) err = %v, want ErrInvalidN", n, err)
		}
	}
}

func TestCountersHonourACancelledContext(t *testing.T) {
	c := hw5Counters(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Series(ctx, "a", hw5Whole, GranularityHour); !errors.Is(err, context.Canceled) {
		t.Errorf("Series err = %v", err)
	}
	if _, err := c.Top(ctx, hw5Whole, 3); !errors.Is(err, context.Canceled) {
		t.Errorf("Top err = %v", err)
	}
}

func TestCountersClose(t *testing.T) {
	c := NewCounters()
	c.Add([]Click{{Code: "a", At: hw5At(2, 10, 0)}})
	c.Close()
	c.Close() // twice is fine
	finished := make(chan struct{})
	go func() {
		c.Add([]Click{{Code: "a", At: hw5At(2, 10, 0)}})
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("Add hangs on closed counters")
	}
	ctx := context.Background()
	if _, err := c.Series(ctx, "a", hw5Whole, GranularityHour); !errors.Is(err, ErrClosed) {
		t.Errorf("Series err = %v, want ErrClosed", err)
	}
	if _, err := c.Top(ctx, hw5Whole, 3); !errors.Is(err, ErrClosed) {
		t.Errorf("Top err = %v, want ErrClosed", err)
	}
}

func TestCountersConcurrentUse(t *testing.T) {
	c := hw5Counters(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				c.Add([]Click{{Code: "a", At: hw5At(2, 10, 0)}, {Code: "b", At: hw5At(2, 11, 0)}})
				if i%50 == 0 {
					if _, err := c.Series(ctx, "a", hw5Whole, GranularityDay); err != nil {
						t.Error(err)
						return
					}
				}
			}
		}()
	}
	wg.Wait()
	got, err := c.Series(ctx, "a", hw5Whole, GranularityDay)
	if err != nil || len(got) != 1 || got[0].Clicks != 4000 {
		t.Errorf("Series = %v, %v; want one bucket with 4000 clicks", got, err)
	}
}

func TestPipelineFeedsCounters(t *testing.T) {
	counters := hw5Counters(t)
	st := newHW5Store()
	p := NewPipeline(st, WithWorkers(3), WithBatchSize(7), WithSink(counters), WithFlushInterval(time.Hour))
	p.Start()
	for i := 0; i < 30; i++ {
		code := "a"
		if i%3 == 0 {
			code = "b"
		}
		p.Submit(Click{Code: code, At: hw5At(2, 10, i)})
	}
	hw5Shutdown(t, p)
	got, err := counters.Top(context.Background(), hw5Whole, 5)
	if err != nil || len(got) != 2 || got[0] != (Total{"a", 20}) || got[1] != (Total{"b", 10}) {
		t.Errorf("Top = %v, %v; want a=20, b=10", got, err)
	}
}

var (
	_ Aggregator = (*Counters)(nil)
	_ BatchSink  = (*Counters)(nil)
)
