package metrics

import (
	"testing"
	"time"

	"uptimex/internal/storage"
)

func TestPercentileNearestRank(t *testing.T) {
	// 1..100: P50 = 50, P95 = 95, P99 = 99, P100 = 100.
	vals := make([]int64, 100)
	for i := range vals {
		vals[i] = int64(i + 1)
	}
	if p := Percentile(vals, 50); p != 50 {
		t.Errorf("P50 = %v, want 50", p)
	}
	if p := Percentile(vals, 95); p != 95 {
		t.Errorf("P95 = %v, want 95", p)
	}
	if p := Percentile(vals, 99); p != 99 {
		t.Errorf("P99 = %v, want 99", p)
	}
	if p := Percentile(vals, 100); p != 100 {
		t.Errorf("P100 = %v, want 100", p)
	}
	if p := Percentile(nil, 99); p != 0 {
		t.Errorf("empty population P99 = %v, want 0", p)
	}
	// Input must not be mutated (callers reuse slices).
	if vals[0] != 1 || vals[99] != 100 {
		t.Error("Percentile mutated its input")
	}
}

func TestPercentileSmallPopulation(t *testing.T) {
	// With 3 observations, nearest-rank P99 = max.
	vals := []int64{30, 10, 20}
	if p := Percentile(vals, 99); p != 30 {
		t.Errorf("P99 = %v, want 30", p)
	}
	if p := Percentile(vals, 50); p != 20 {
		t.Errorf("P50 = %v, want 20", p)
	}
}

func TestSummarizeLatency(t *testing.T) {
	vals := []int64{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
	st := SummarizeLatency(vals)
	if st.SampleCount != 10 || st.MinMs != 100 || st.MaxMs != 1000 {
		t.Fatalf("stats = %+v", st)
	}
	if st.AvgMs != 550 {
		t.Errorf("avg = %v, want 550", st.AvgMs)
	}
	if st.P50Ms != 500 {
		t.Errorf("p50 = %v, want 500", st.P50Ms)
	}
	if st.P99Ms != 1000 {
		t.Errorf("p99 = %v, want 1000", st.P99Ms)
	}
}

func TestBucketStatus(t *testing.T) {
	cases := []struct {
		code int64
		err  string
		want string
	}{
		{200, "", "200"},
		{201, "", "201"},
		{204, "", "2xx"},
		{301, "", "3xx"},
		{404, "", "4xx"},
		{500, "", "5xx"},
		{503, "", "5xx"},
		{0, "timeout", "timeout"},
		{0, "dns", "network_error"},
		{0, "connection", "network_error"},
		{0, "tls", "network_error"},
		{0, "blocked", "blocked"},
		{0, "", "network_error"},
	}
	for _, c := range cases {
		if got := BucketStatus(c.code, c.err); got != c.want {
			t.Errorf("BucketStatus(%d,%q) = %q, want %q", c.code, c.err, got, c.want)
		}
	}
}

func TestBuildDistributionAndTrend(t *testing.T) {
	counts := []storage.StatusCount{
		{StatusCode: 200, Count: 10},
		{StatusCode: 201, Count: 2},
		{StatusCode: 503, Count: 3},
		{StatusCode: 0, ErrorType: "timeout", Count: 1},
	}
	dist := BuildDistribution(counts)
	if dist["200"] != 10 || dist["201"] != 2 || dist["5xx"] != 3 || dist["timeout"] != 1 {
		t.Fatalf("dist = %v", dist)
	}

	from := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	samples := []storage.LatencySample{
		{CheckedAt: from.Add(1 * time.Minute), ResponseTimeMs: 100, Success: true},
		{CheckedAt: from.Add(2 * time.Minute), ResponseTimeMs: 300, Success: true},
		{CheckedAt: from.Add(30 * time.Minute), ResponseTimeMs: 900, Success: false},
	}
	trend := BuildTrend(samples, from, to, 2) // two 30m buckets
	if len(trend) != 2 {
		t.Fatalf("trend len = %d", len(trend))
	}
	if trend[0].Checks != 2 || trend[0].Failures != 0 || trend[0].AvgMs != 200 {
		t.Fatalf("bucket0 = %+v", trend[0])
	}
	if trend[1].Checks != 1 || trend[1].Failures != 1 || trend[1].AvgMs != 900 {
		t.Fatalf("bucket1 = %+v", trend[1])
	}
}

func TestParseWindow(t *testing.T) {
	good := map[string]int64{"30m": 30, "1h": 1, "24h": 24, "7d": 7 * 24}
	for in, hours := range good {
		d, err := ParseWindow(in, time.Hour)
		if err != nil {
			t.Errorf("ParseWindow(%q) err = %v", in, err)
			continue
		}
		if d != time.Duration(hours)*time.Hour && in != "30m" {
			t.Errorf("ParseWindow(%q) = %s", in, d)
		}
	}
	if _, err := ParseWindow("30m", 0); err != nil || true {
		// "30m" should parse to 30 minutes
		d, _ := ParseWindow("30m", time.Hour)
		if d != 30*time.Minute {
			t.Errorf("30m = %s", d)
		}
	}
	for _, bad := range []string{"x", "5", "0h", "-1h", "999d", "5s"} {
		if _, err := ParseWindow(bad, time.Hour); err == nil {
			t.Errorf("ParseWindow(%q) should fail", bad)
		}
	}
	if d, _ := ParseWindow("", time.Hour); d != time.Hour {
		t.Errorf("empty window must return default")
	}
}

func TestPct(t *testing.T) {
	if Pct(9921, 10000) != 99.21 {
		t.Errorf("Pct = %v", Pct(9921, 10000))
	}
	if Pct(5, 0) != 0 {
		t.Error("Pct with zero total must be 0")
	}
}
