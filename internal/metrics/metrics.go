// Package metrics computes monitoring analytics: latency percentiles
// (nearest-rank), uptime, failure rates, status-code distributions and
// time-bucketed trends. Pure functions here are deterministic and unit
// tested; service.go assembles them from storage.
//
// Percentiles use the nearest-rank method over the most recent N raw
// observations in the requested window (population documented in every API
// response). For very large-scale deployments this would move to
// histogram-based aggregation; the seam is SummarizeLatency.
package metrics

import (
	"math"
	"sort"
	"time"

	"uptimex/internal/models"
	"uptimex/internal/storage"
)

// LatencyStats describes the latency distribution of a population.
type LatencyStats struct {
	P50Ms       float64 `json:"p50_ms"`
	P95Ms       float64 `json:"p95_ms"`
	P99Ms       float64 `json:"p99_ms"`
	AvgMs       float64 `json:"avg_ms"`
	MinMs       int64   `json:"min_ms"`
	MaxMs       int64   `json:"max_ms"`
	SampleCount int     `json:"sample_count"`
}

// TrendPoint is one time bucket of the latency trend.
type TrendPoint struct {
	Time     time.Time `json:"time"`
	AvgMs    float64   `json:"avg_ms"`
	P95Ms    float64   `json:"p95_ms"`
	P99Ms    float64   `json:"p99_ms"`
	Checks   int       `json:"checks"`
	Failures int       `json:"failures"`
}

// Distribution maps status buckets to counts, e.g. "200", "3xx", "timeout".
type Distribution map[string]int64

// Percentile returns the nearest-rank percentile p (0 < p <= 100) of values.
// An empty population returns 0. The input slice is not modified.
func Percentile(values []int64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if p <= 0 {
		p = 1
	}
	if p > 100 {
		p = 100
	}
	sorted := make([]int64, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Nearest-rank: the smallest value whose cumulative share is >= p%.
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return float64(sorted[rank-1])
}

// SummarizeLatency computes descriptive stats over raw response times.
func SummarizeLatency(values []int64) LatencyStats {
	if len(values) == 0 {
		return LatencyStats{}
	}
	var sum int64
	minV, maxV := values[0], values[0]
	for _, v := range values {
		sum += v
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	return LatencyStats{
		P50Ms:       Percentile(values, 50),
		P95Ms:       Percentile(values, 95),
		P99Ms:       Percentile(values, 99),
		AvgMs:       float64(sum) / float64(len(values)),
		MinMs:       minV,
		MaxMs:       maxV,
		SampleCount: len(values),
	}
}

// BucketStatus classifies one observation into a distribution bucket.
// Buckets follow the dashboard requirements: exact 200/201, families for the
// rest, timeout and network/config errors separated (PRD §24).
func BucketStatus(statusCode int64, errorType string) string {
	if statusCode == 0 {
		switch errorType {
		case models.ErrTypeTimeout:
			return "timeout"
		case models.ErrTypeBlocked:
			return "blocked"
		case models.ErrTypeInvalidURL:
			return "invalid_url"
		default:
			return "network_error"
		}
	}
	switch {
	case statusCode == 200:
		return "200"
	case statusCode == 201:
		return "201"
	case statusCode >= 200 && statusCode < 300:
		return "2xx"
	case statusCode >= 300 && statusCode < 400:
		return "3xx"
	case statusCode >= 400 && statusCode < 500:
		return "4xx"
	case statusCode >= 500 && statusCode < 600:
		return "5xx"
	default:
		return "other"
	}
}

// BuildDistribution folds raw status counts into bucket counts.
func BuildDistribution(counts []storage.StatusCount) Distribution {
	dist := Distribution{}
	for _, c := range counts {
		b := BucketStatus(c.StatusCode, c.ErrorType)
		dist[b] += c.Count
	}
	return dist
}

// BuildTrend buckets samples into evenly spaced points across
// [from, to] (bucketCount >= 1). Samples must be ascending by time.
func BuildTrend(samples []storage.LatencySample, from, to time.Time, bucketCount int) []TrendPoint {
	if bucketCount < 1 {
		bucketCount = 1
	}
	span := to.Sub(from)
	if span <= 0 {
		span = time.Second
	}
	width := span / time.Duration(bucketCount)

	buckets := make([]TrendPoint, bucketCount)
	for i := range buckets {
		buckets[i] = TrendPoint{Time: from.Add(time.Duration(i) * width)}
	}
	for _, s := range samples {
		idx := int(s.CheckedAt.Sub(from) / width)
		if idx < 0 {
			idx = 0
		}
		if idx >= bucketCount {
			idx = bucketCount - 1
		}
		buckets[idx].Checks++
		if !s.Success {
			buckets[idx].Failures++
		}
	}

	// Second pass: latency values per bucket.
	// samples are already ascending; collect per-bucket values.
	values := make([][]int64, bucketCount)
	for _, s := range samples {
		idx := int(s.CheckedAt.Sub(from) / width)
		if idx < 0 {
			idx = 0
		}
		if idx >= bucketCount {
			idx = bucketCount - 1
		}
		values[idx] = append(values[idx], s.ResponseTimeMs)
	}
	for i, vals := range values {
		if len(vals) == 0 {
			continue
		}
		st := SummarizeLatency(vals)
		buckets[i].AvgMs = st.AvgMs
		buckets[i].P95Ms = st.P95Ms
		buckets[i].P99Ms = st.P99Ms
	}
	return buckets
}

// Pct computes a percentage safely.
func Pct(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}
