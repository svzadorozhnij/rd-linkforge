package click

import (
	"context"
	"runtime"
	"sync"
	"time"
)

type Pipeline struct {
	store   Store
	metrics Metrics
	sink    BatchSink

	queueSize     int
	workers       int
	batchSize     int
	flushInterval time.Duration

	queue chan Click

	mu      sync.RWMutex
	started bool
	closed  bool

	wg   sync.WaitGroup
	done chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
}

var _ Ingester = (*Pipeline)(nil)

func NewPipeline(store Store, opts ...PipelineOption) *Pipeline {
	p := &Pipeline{
		store:         store,
		metrics:       noopMetrics{},
		queueSize:     10000,
		workers:       runtime.GOMAXPROCS(0),
		batchSize:     500,
		flushInterval: 1 * time.Second,
		done:          make(chan struct{}),
	}
	for i := range opts {
		opts[i](p)
	}
	// The queue is created after the options, so WithQueueSize takes effect.
	p.queue = make(chan Click, p.queueSize)
	p.ctx, p.cancel = context.WithCancel(context.Background())
	return p
}

type PipelineOption func(*Pipeline)

// Values below 1 (or a non-positive interval, or a nil Metrics) are ignored: the default stays.

func WithQueueSize(n int) PipelineOption {
	return func(pipeline *Pipeline) {
		if n >= 1 {
			pipeline.queueSize = n
		}
	}
}

func WithWorkers(n int) PipelineOption {
	return func(pipeline *Pipeline) {
		if n >= 1 {
			pipeline.workers = n
		}
	}
}

func WithBatchSize(n int) PipelineOption {
	return func(pipeline *Pipeline) {
		if n >= 1 {
			pipeline.batchSize = n
		}
	}
}

func WithFlushInterval(d time.Duration) PipelineOption {
	return func(pipeline *Pipeline) {
		if d > 0 {
			pipeline.flushInterval = d
		}
	}
}

func WithMetrics(m Metrics) PipelineOption {
	return func(pipeline *Pipeline) {
		if m != nil {
			pipeline.metrics = m
		}
	}
}

func WithSink(s BatchSink) PipelineOption {
	return func(pipeline *Pipeline) {
		pipeline.sink = s
	}
}

// Start launches the workers. A second Start, or a Start after Shutdown, does nothing.
func (p *Pipeline) Start() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.started || p.closed {
		return
	}
	p.started = true
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	go func() {
		p.wg.Wait()
		close(p.done)
	}()
}

// Submit enqueues a click without ever blocking. It returns false, and counts
// the click as dropped, when the queue is full or the pipeline is shut down.
func (p *Pipeline) Submit(c Click) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		p.metrics.ObserveClickDropped()
		return false
	}
	select {
	case p.queue <- c:
		p.metrics.SetQueueDepth(len(p.queue))
		return true
	default:
		p.metrics.ObserveClickDropped()
		return false
	}
}

// Shutdown stops accepting clicks, closes the queue and waits until every
// worker has drained it and flushed its last batch. If ctx ends first, it
// cancels the workers' context and returns ctx.Err(). It may be called again.
func (p *Pipeline) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	started := p.started
	if !p.closed {
		p.closed = true
		close(p.queue)
	}
	p.mu.Unlock()

	if !started {
		p.cancel()
		return nil
	}

	select {
	case <-p.done:
		p.cancel()
		return nil
	case <-ctx.Done():
		p.cancel()
		return ctx.Err()
	}
}

// worker collects clicks into its own batch and flushes it when it is full,
// on every tick of the flush interval, and once more when the queue is closed.
func (p *Pipeline) worker() {
	defer p.wg.Done()
	ticker := time.NewTicker(p.flushInterval)
	defer ticker.Stop()

	batch := make([]Click, 0, p.batchSize)
	for {
		select {
		case c, ok := <-p.queue:
			if !ok {
				p.flush(batch)
				return
			}
			batch = append(batch, c)
			if len(batch) >= p.batchSize {
				p.flush(batch)
				batch = make([]Click, 0, p.batchSize)
			}
		case <-ticker.C:
			if len(batch) > 0 {
				p.flush(batch)
				batch = make([]Click, 0, p.batchSize)
			}
		}
	}
}

// flush stores a batch and hands it to the sink. A failed batch is dropped:
// every click in it is counted as dropped and the sink never sees it.
// The batch is never touched again after this call.
func (p *Pipeline) flush(batch []Click) {
	if len(batch) == 0 {
		return
	}
	if err := p.store.Append(p.ctx, batch); err != nil {
		for range batch {
			p.metrics.ObserveClickDropped()
		}
		return
	}
	if p.sink != nil {
		p.sink.Add(batch)
	}
}

type noopMetrics struct{}

func (noopMetrics) ObserveClickDropped() {}
func (noopMetrics) SetQueueDepth(int)    {}

type Metrics interface {
	ObserveClickDropped()
	SetQueueDepth(n int)
}

type BatchSink interface {
	Add(batch []Click)
}
