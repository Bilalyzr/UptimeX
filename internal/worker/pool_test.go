package worker

import (
	"context"
	"sync"
	"testing"
	"time"

	"uptimex/internal/checker"
	"uptimex/internal/logging"
	"uptimex/internal/models"
)

// fakeChecker records concurrency and simulates latency per endpoint.
type fakeChecker struct {
	mu          sync.Mutex
	inFlight    int
	maxInFlight int
	delay       time.Duration
	checkedIDs  sync.Map
	panicsOn    map[int64]bool
}

func (f *fakeChecker) Check(ctx context.Context, ep models.Endpoint) models.CheckResult {
	f.mu.Lock()
	f.inFlight++
	if f.inFlight > f.maxInFlight {
		f.maxInFlight = f.inFlight
	}
	f.mu.Unlock()

	time.Sleep(f.delay)
	f.checkedIDs.Store(ep.ID, true)

	f.mu.Lock()
	f.inFlight--
	f.mu.Unlock()

	if f.panicsOn != nil && f.panicsOn[ep.ID] {
		panic("simulated probe bug")
	}
	sc := 200
	return models.CheckResult{EndpointID: ep.ID, StatusCode: &sc, Success: true, ResponseTimeMs: 5}
}

func ep(id int64) models.Endpoint { return models.Endpoint{ID: id, Name: "e", URL: "http://x"} }

func TestPoolProcessesAllJobs(t *testing.T) {
	c := &fakeChecker{}
	p := NewPool(c, 8, 128)
	p.Start(logging.NewForTest())

	go func() {
		for i := int64(1); i <= 100; i++ {
			if err := p.Submit(context.Background(), ep(i)); err != nil {
				t.Errorf("submit: %v", err)
			}
		}
	}()

	got := 0
	deadline := time.After(10 * time.Second)
	for got < 100 {
		select {
		case <-p.Results():
			got++
		case <-deadline:
			t.Fatalf("timeout: only %d/100 results", got)
		}
	}
	p.Stop()

	if _, ok := <-p.Results(); ok {
		t.Fatal("results channel must be closed after Stop")
	}
	if p.Stats().Processed != 100 {
		t.Fatalf("processed = %d, want 100", p.Stats().Processed)
	}
}

func TestPoolBoundsConcurrency(t *testing.T) {
	c := &fakeChecker{delay: 30 * time.Millisecond}
	const workers = 4
	p := NewPool(c, workers, 256)
	p.Start(logging.NewForTest())

	for i := int64(1); i <= 40; i++ {
		if err := p.Submit(context.Background(), ep(i)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 40; i++ {
		<-p.Results()
	}
	p.Stop()

	if c.maxInFlight > workers {
		t.Fatalf("concurrency exceeded pool size: %d > %d", c.maxInFlight, workers)
	}
	if c.maxInFlight < 2 {
		t.Fatalf("expected parallel execution, max in-flight = %d", c.maxInFlight)
	}
}

func TestPoolSurvivesProbePanic(t *testing.T) {
	c := &fakeChecker{panicsOn: map[int64]bool{7: true}}
	p := NewPool(c, 2, 16)
	p.Start(logging.NewForTest())

	_ = p.Submit(context.Background(), ep(7))
	res := <-p.Results()
	if res.Success || res.ErrorType != "internal" {
		t.Fatalf("panic must become structured failure: %+v", res)
	}
	p.Stop()
}

func TestPoolSlowEndpointDoesNotBlockOthers(t *testing.T) {
	// One slow probe must not delay results of fast probes (core PRD goal).
	slow := &slowChecker{slowID: 1, slowDelay: 700 * time.Millisecond}
	p := NewPool(slow, 4, 16)
	p.Start(logging.NewForTest())
	defer p.Stop()

	start := time.Now()
	_ = p.Submit(context.Background(), ep(1)) // slow
	_ = p.Submit(context.Background(), ep(2)) // fast

	// First result back must be the fast endpoint, well before slow finishes.
	res := <-p.Results()
	if res.EndpointID != 2 {
		t.Fatalf("first result = endpoint %d, want fast endpoint 2", res.EndpointID)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("fast endpoint waited %s for the slow one", elapsed)
	}
	<-p.Results() // drain slow
}

type slowChecker struct {
	slowID    int64
	slowDelay time.Duration
}

func (s *slowChecker) Check(ctx context.Context, ep models.Endpoint) models.CheckResult {
	if ep.ID == s.slowID {
		time.Sleep(s.slowDelay)
		return models.CheckResult{EndpointID: ep.ID, Success: false, ErrorType: "timeout"}
	}
	return models.CheckResult{EndpointID: ep.ID, Success: true, StatusCode: intPtr(200)}
}

func intPtr(n int) *int { return &n }

// Compile-time guard: the pool works with the real checker too.
var _ checker.Checker = (*slowChecker)(nil)
