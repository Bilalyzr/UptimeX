// Package worker implements a bounded worker pool. Concurrency is capped at
// construction: the scheduler submits jobs into a channel, N workers consume
// them and push results into a shared channel. Unbounded goroutine creation
// is deliberately avoided (PRD §14).
package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"uptimex/internal/checker"
	"uptimex/internal/models"
)

// ErrShuttingDown is returned by Submit after Stop has begun.
var ErrShuttingDown = errors.New("worker pool is shutting down")

// Stats exposes pool observability (PRD §27: worker utilization, queue depth).
type Stats struct {
	Workers    int   `json:"workers"`
	Active     int64 `json:"active_workers"`
	QueueDepth int   `json:"queue_depth"`
	Processed  int64 `json:"processed_total"`
	InFlight   int64 `json:"in_flight"`
}

// Pool is a fixed-size worker pool executing endpoint checks.
type Pool struct {
	checker   checker.Checker
	workers   int
	jobs      chan models.Endpoint
	results   chan models.CheckResult
	processed atomic.Int64
	submitted atomic.Int64
	active    atomic.Int64
	wg        sync.WaitGroup
	stopOnce  sync.Once
	stopped   atomic.Bool
}

// NewPool builds a pool with the given worker count and job queue capacity.
func NewPool(c checker.Checker, workers, queueSize int) *Pool {
	if workers < 1 {
		workers = 1
	}
	if queueSize < 1 {
		queueSize = 1
	}
	return &Pool{
		checker: c,
		workers: workers,
		jobs:    make(chan models.Endpoint, queueSize),
		results: make(chan models.CheckResult, queueSize),
	}
}

// Start launches the workers. It is called once.
func (p *Pool) Start(logger *slog.Logger) {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.work(logger)
	}
}

func (p *Pool) work(logger *slog.Logger) {
	defer p.wg.Done()
	for ep := range p.jobs {
		p.active.Add(1)
		res := p.safeCheck(context.Background(), ep, logger)
		p.active.Add(-1)
		p.processed.Add(1)
		p.results <- res
	}
}

// safeCheck guarantees a probe panic becomes a structured failed result
// instead of killing a worker.
func (p *Pool) safeCheck(ctx context.Context, ep models.Endpoint, logger *slog.Logger) (res models.CheckResult) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("probe_panic_recovered", "endpoint_id", ep.ID, "panic", r)
			res = models.CheckResult{
				EndpointID:   ep.ID,
				CheckedAt:    nowUTC(),
				Success:      false,
				ErrorType:    "internal",
				ErrorMessage: "probe panicked; see monitor logs",
			}
		}
	}()
	return p.checker.Check(ctx, ep)
}

// Submit enqueues an endpoint check. It blocks while the queue is full,
// honoring context cancellation; it returns ErrShuttingDown after Stop.
func (p *Pool) Submit(ctx context.Context, ep models.Endpoint) error {
	if p.stopped.Load() {
		return ErrShuttingDown
	}
	select {
	case p.jobs <- ep:
		p.submitted.Add(1)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Results is the result stream consumed by the collector.
func (p *Pool) Results() <-chan models.CheckResult { return p.results }

// Stop drains gracefully: no new jobs are accepted, queued jobs are executed,
// workers exit, and the results channel is closed after the last result.
func (p *Pool) Stop() {
	p.stopOnce.Do(func() {
		p.stopped.Store(true)
		close(p.jobs)
		p.wg.Wait()
		close(p.results)
	})
}

// Stats reports live pool counters.
func (p *Pool) Stats() Stats {
	return Stats{
		Workers:    p.workers,
		Active:     p.active.Load(),
		QueueDepth: len(p.jobs),
		Processed:  p.processed.Load(),
		InFlight:   p.submitted.Load() - p.processed.Load(),
	}
}
