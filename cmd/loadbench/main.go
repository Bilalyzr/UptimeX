// Command loadbench measures the monitoring engine's capacity and
// bottleneck behavior (PRD §24 performance requirements): cycle completion
// time, throughput, memory, worker utilization, queue depth and database
// insert latency for a configurable number of endpoints.
//
// Typical usage:
//
//	go run ./cmd/loadbench -endpoints 50 -rounds 3
//	go run ./cmd/loadbench -endpoints 1000 -workers 50 -rounds 3
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"sync/atomic"
	"time"

	"uptimex/internal/alert"
	"uptimex/internal/checker"
	"uptimex/internal/logging"
	"uptimex/internal/models"
	"uptimex/internal/monitor"
	"uptimex/internal/storage"
)

type roundResult struct {
	endpoints     int
	workers       int
	cycleMs       float64
	throughput    float64 // checks/sec
	maxActive     int64
	maxQueueDepth int
	heapMB        float64
	goroutines    int
}

func main() {
	var (
		endpoints = flag.Int("endpoints", 50, "number of endpoints to monitor")
		workers   = flag.Int("workers", 20, "worker pool size")
		rounds    = flag.Int("rounds", 3, "measured monitoring rounds")
		slowFrac  = flag.Float64("slow-frac", 0.05, "fraction of endpoints with 400ms responses")
		driver    = flag.String("db", "sqlite", "sqlite | postgres")
		dsn       = flag.String("dsn", "", "postgres DSN (when -db postgres)")
	)
	flag.Parse()

	logger := logging.New("production")
	ctx := context.Background()

	// Target fleet: 4 local servers; a fraction responds slowly to keep the
	// pool honestly busy and prove slow endpoints don't block the fleet.
	var targets []*httptest.Server
	for i := 0; i < 4; i++ {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("slow") == "1" {
				time.Sleep(400 * time.Millisecond)
			}
			w.WriteHeader(200)
		}))
		defer srv.Close()
		targets = append(targets, srv)
	}

	for _, f := range []string{"./loadbench.db", "./loadbench.db-wal", "./loadbench.db-shm"} {
		_ = os.Remove(f)
	}
	store, err := storage.Open(ctx, storage.Options{
		Driver: *driver, PostgresDSN: *dsn, SQLitePath: "./loadbench.db",
		MaxOpenConns: 20, MaxIdleConns: 10,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "open store:", err)
		os.Exit(1)
	}
	defer store.Close()

	engine := monitor.New(store, checker.NewHTTPChecker(checker.NewGuard(true)),
		alert.NewManager(nil, 0, logger),
		monitor.Config{WorkerCount: *workers, JobQueueSize: 1024, SchedulerTick: 100 * time.Millisecond},
		logger)
	if err := engine.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "engine start:", err)
		os.Exit(1)
	}

	// Register endpoints.
	slowEvery := int(1 / *slowFrac)
	if slowEvery < 1 {
		slowEvery = 0 // no slow endpoints
	}
	for i := 0; i < *endpoints; i++ {
		url := fmt.Sprintf("%s/e/%d", targets[i%len(targets)].URL, i)
		if slowEvery > 0 && i%slowEvery == 0 {
			url += "?slow=1"
		}
		e := models.Endpoint{
			Name: fmt.Sprintf("bench-%04d", i), URL: url, Method: "GET",
			IntervalSeconds: 5, TimeoutMs: 3000, FailureThreshold: 3,
			ExpectedStatusMin: 200, ExpectedStatusMax: 299, Enabled: true,
		}
		if err := store.CreateEndpoint(ctx, &e); err != nil {
			fmt.Fprintln(os.Stderr, "create endpoint:", err)
			os.Exit(1)
		}
	}
	if err := engine.ReloadEndpoints(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "reload:", err)
		os.Exit(1)
	}

	results := []roundResult{}
	for round := 1; round <= *rounds; round++ {
		// Warm-up round 1 is still measured but the DB may be cold; keep all
		// rounds reported for honesty.
		res := runRound(ctx, store, engine, *endpoints, *workers)
		results = append(results, res)
		fmt.Printf("round %d: cycle=%0.0fms throughput=%0.0f checks/s maxActive=%d maxQueue=%d heap=%0.1fMB goroutines=%d\n",
			round, res.cycleMs, res.throughput, res.maxActive, res.maxQueueDepth, res.heapMB, res.goroutines)
	}
	engine.Stop()

	// Database insert latency sample (200 sequential writes).
	p50, p95, p99 := dbInsertLatency(ctx, store, 200)
	printSummary(results, [3]float64{p50, p95, p99}, *endpoints, *workers)
	_ = os.Remove("./loadbench.db")
	_ = os.Remove("./loadbench.db-wal")
	_ = os.Remove("./loadbench.db-shm")
}

// runRound backdates every endpoint (one cycle becomes due at once) and
// measures until all endpoints have a new check recorded.
func runRound(ctx context.Context, store storage.Repository, engine *monitor.Engine, n, workers int) roundResult {
	// Preparation (not measured): backdate all statuses.
	statuses, err := store.ListStatuses(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "list statuses:", err)
		os.Exit(1)
	}
	old := time.Now().UTC().Add(-time.Hour)
	var expected int64
	for _, st := range statuses {
		st.LastCheckedAt = &old
		if err := store.UpsertStatus(ctx, &st); err != nil {
			fmt.Fprintln(os.Stderr, "upsert:", err)
			os.Exit(1)
		}
		expected++
	}

	// Aggregate check count before the round (single COUNT query keeps the
	// measurement loop from becoming its own bottleneck at high N).
	startTotal, _, _ := store.CountOutcomes(ctx, nil, time.Time{})

	var maxActive, maxQueue int64
	var stopSampling atomic.Bool
	go func() {
		for !stopSampling.Load() {
			st := engine.Stats()
			if st.Pool.Active > maxActive {
				maxActive = st.Pool.Active
			}
			if int64(st.Pool.QueueDepth) > maxQueue {
				maxQueue = int64(st.Pool.QueueDepth)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}()

	start := time.Now()
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		total, _, _ := store.CountOutcomes(ctx, nil, time.Time{})
		if total-startTotal >= expected {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	elapsed := time.Since(start)
	stopSampling.Store(true)

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return roundResult{
		endpoints:     n,
		workers:       workers,
		cycleMs:       float64(elapsed.Milliseconds()),
		throughput:    float64(expected) / elapsed.Seconds(),
		maxActive:     maxActive,
		maxQueueDepth: int(maxQueue),
		heapMB:        float64(ms.HeapAlloc) / 1024 / 1024,
		goroutines:    runtime.NumGoroutine(),
	}
}

// dbInsertLatency measures sequential health-check INSERT latency.
func dbInsertLatency(ctx context.Context, store storage.Repository, samples int) (p50, p95, p99 float64) {
	var endpointID int64 = 1
	eps, _ := store.ListEndpoints(ctx, false)
	if len(eps) > 0 {
		endpointID = eps[0].ID
	}
	var lat []float64
	for i := 0; i < samples; i++ {
		t0 := time.Now()
		_, err := store.InsertCheck(ctx, models.CheckResult{
			EndpointID: endpointID, CheckedAt: time.Now().UTC(),
			ResponseTimeMs: 5, Success: true,
		})
		if err != nil {
			continue
		}
		lat = append(lat, float64(time.Since(t0).Microseconds())/1000.0)
	}
	if len(lat) == 0 {
		return 0, 0, 0
	}
	sort.Float64s(lat)
	pct := func(p float64) float64 { return lat[int(float64(len(lat)-1)*p)] }
	return pct(0.50), pct(0.95), pct(0.99)
}

func printSummary(results []roundResult, dbLat [3]float64, n, workers int) {
	var best roundResult
	for i, r := range results {
		if i == 0 || r.cycleMs < best.cycleMs {
			best = r
		}
	}
	fmt.Println()
	fmt.Printf("## Summary — %d endpoints, %d workers\n\n", n, workers)
	fmt.Printf("| Metric | Value |\n|---|---|\n")
	fmt.Printf("| Best cycle completion | %.0f ms |\n", best.cycleMs)
	fmt.Printf("| Peak throughput | %.0f checks/s |\n", best.throughput)
	fmt.Printf("| Max active workers (of %d) | %d |\n", workers, best.maxActive)
	fmt.Printf("| Max queue depth | %d |\n", best.maxQueueDepth)
	fmt.Printf("| Heap after run | %.1f MB |\n", best.heapMB)
	fmt.Printf("| Goroutines | %d |\n", best.goroutines)
	fmt.Printf("| DB insert latency p50/p95/p99 | %.2f / %.2f / %.2f ms |\n", dbLat[0], dbLat[1], dbLat[2])
}
