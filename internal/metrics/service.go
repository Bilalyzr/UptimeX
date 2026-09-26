package metrics

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"uptimex/internal/models"
	"uptimex/internal/storage"
)

// Limits protect dashboard queries from unbounded raw scans (PRD §23:
// percentiles must come from a bounded, documented population).
const (
	MaxSamplesPerEndpoint = 20_000
	MaxWindow             = 30 * 24 * time.Hour
	DefaultWindow         = time.Hour
	TrendPoints           = 48
)

// Service assembles metric responses from the repository.
type Service struct {
	Repo storage.Repository
}

// EndpointCounts is the endpoint health census.
type EndpointCounts struct {
	Total    int `json:"total"`
	Healthy  int `json:"healthy"`
	Failing  int `json:"failing"`
	Down     int `json:"down"`
	Disabled int `json:"disabled"`
}

// EndpointMetrics is the analytics payload for one endpoint.
type EndpointMetrics struct {
	EndpointID         int64        `json:"endpoint_id"`
	Window             string       `json:"window"`
	From               time.Time    `json:"from"`
	To                 time.Time    `json:"to"`
	TotalChecks        int64        `json:"total_checks"`
	SuccessfulChecks   int64        `json:"successful_checks"`
	FailedChecks       int64        `json:"failed_checks"`
	UptimePct          float64      `json:"uptime_pct"`
	FailureRatePct     float64      `json:"failure_rate_pct"`
	Latency            LatencyStats `json:"latency"`
	StatusDistribution Distribution `json:"status_distribution"`
	Trend              []TrendPoint `json:"latency_trend"`
}

// OverviewMetrics is the global analytics payload.
type OverviewMetrics struct {
	Window             string         `json:"window"`
	From               time.Time      `json:"from"`
	To                 time.Time      `json:"to"`
	Endpoints          EndpointCounts `json:"endpoints"`
	TotalChecks        int64          `json:"total_checks"`
	SuccessfulChecks   int64          `json:"successful_checks"`
	FailedChecks       int64          `json:"failed_checks"`
	UptimePct          float64        `json:"uptime_pct"`
	FailureRatePct     float64        `json:"failure_rate_pct"`
	Latency            LatencyStats   `json:"latency"`
	StatusDistribution Distribution   `json:"status_distribution"`
	Trend              []TrendPoint   `json:"latency_trend"`
	OpenIncidents      int64          `json:"open_incidents"`
}

// EndpointSummary is one row of the endpoints table: configuration, live
// state and windowed reliability metrics together.
type EndpointSummary struct {
	models.Endpoint
	State               models.EndpointState `json:"state"`
	ConsecutiveFailures int                  `json:"consecutive_failures"`
	LastSuccessAt       *time.Time           `json:"last_success_at"`
	LastFailureAt       *time.Time           `json:"last_failure_at"`
	LastCheckedAt       *time.Time           `json:"last_checked_at"`
	// Windowed metrics (nullable when the endpoint has no checks yet).
	Window         *string  `json:"window"`
	UptimePct      *float64 `json:"uptime_pct"`
	P99Ms          *float64 `json:"p99_ms"`
	AvgMs          *float64 `json:"avg_ms"`
	TotalChecks    *int64   `json:"total_checks"`
	OpenIncidentID *int64   `json:"open_incident_id"`
}

// ParseWindow validates a window string like "1h", "24h", "7d", "30m".
func ParseWindow(s string, def time.Duration) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	unit := s[len(s)-1]
	num, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || num <= 0 {
		return 0, fmt.Errorf("invalid window %q (use e.g. 30m, 1h, 24h, 7d)", s)
	}
	var d time.Duration
	switch unit {
	case 'm':
		d = time.Duration(num) * time.Minute
	case 'h':
		d = time.Duration(num) * time.Hour
	case 'd':
		d = time.Duration(num) * 24 * time.Hour
	default:
		return 0, fmt.Errorf("invalid window unit in %q (m/h/d)", s)
	}
	if d > MaxWindow {
		return 0, fmt.Errorf("window %q exceeds maximum %s", s, MaxWindow)
	}
	return d, nil
}

// WindowString renders a duration as the canonical window label.
func WindowString(d time.Duration) string {
	switch {
	case d%24*time.Hour == 0 && d >= 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return d.String()
	}
}

// Overview assembles global metrics for the window.
func (s *Service) Overview(ctx context.Context, window time.Duration) (*OverviewMetrics, error) {
	to := time.Now().UTC()
	from := to.Add(-window)

	out := &OverviewMetrics{
		Window: WindowString(window),
		From:   from,
		To:     to,
	}

	// Endpoint census from configuration + live state.
	endpoints, err := s.Repo.ListEndpoints(ctx, false)
	if err != nil {
		return nil, err
	}
	statuses, err := s.Repo.ListStatuses(ctx)
	if err != nil {
		return nil, err
	}
	stateByEndpoint := map[int64]models.EndpointStatus{}
	for _, st := range statuses {
		stateByEndpoint[st.EndpointID] = st
	}
	out.Endpoints.Total = len(endpoints)
	for _, e := range endpoints {
		if !e.Enabled {
			out.Endpoints.Disabled++
			continue
		}
		switch stateByEndpoint[e.ID].State {
		case models.StateDown:
			out.Endpoints.Down++
		case models.StateFailing:
			out.Endpoints.Failing++
		default:
			out.Endpoints.Healthy++
		}
	}

	total, success, err := s.Repo.CountOutcomes(ctx, nil, from)
	if err != nil {
		return nil, err
	}
	out.TotalChecks, out.SuccessfulChecks, out.FailedChecks = total, success, total-success
	out.UptimePct = Pct(success, total)
	out.FailureRatePct = 100 - out.UptimePct
	if out.FailureRatePct < 0 {
		out.FailureRatePct = 0
	}

	counts, err := s.Repo.StatusCounts(ctx, nil, from)
	if err != nil {
		return nil, err
	}
	out.StatusDistribution = BuildDistribution(counts)

	samplesByEp, err := s.Repo.SelectAllSamples(ctx, from, MaxSamplesPerEndpoint)
	if err != nil {
		return nil, err
	}
	var all []storage.LatencySample
	for _, samps := range samplesByEp {
		all = append(all, samps...)
	}
	values := make([]int64, 0, len(all))
	for _, smp := range all {
		values = append(values, smp.ResponseTimeMs)
	}
	out.Latency = SummarizeLatency(values)
	sortSamplesAsc(all)
	out.Trend = BuildTrend(all, from, to, TrendPoints)

	incidents, err := s.Repo.ListIncidents(ctx, models.IncidentOpen, 1000)
	if err != nil {
		return nil, err
	}
	out.OpenIncidents = int64(len(incidents))
	return out, nil
}

// EndpointMetricsFor assembles per-endpoint analytics.
func (s *Service) EndpointMetricsFor(ctx context.Context, endpointID int64, window time.Duration) (*EndpointMetrics, error) {
	to := time.Now().UTC()
	from := to.Add(-window)

	out := &EndpointMetrics{
		EndpointID: endpointID,
		Window:     WindowString(window),
		From:       from,
		To:         to,
	}
	id := endpointID
	total, success, err := s.Repo.CountOutcomes(ctx, &id, from)
	if err != nil {
		return nil, err
	}
	out.TotalChecks, out.SuccessfulChecks, out.FailedChecks = total, success, total-success
	out.UptimePct = Pct(success, total)
	out.FailureRatePct = 100 - out.UptimePct
	if out.FailureRatePct < 0 {
		out.FailureRatePct = 0
	}

	counts, err := s.Repo.StatusCounts(ctx, &id, from)
	if err != nil {
		return nil, err
	}
	out.StatusDistribution = BuildDistribution(counts)

	samples, err := s.Repo.SelectEndpointSamples(ctx, endpointID, from, MaxSamplesPerEndpoint)
	if err != nil {
		return nil, err
	}
	values := make([]int64, 0, len(samples))
	for _, smp := range samples {
		values = append(values, smp.ResponseTimeMs)
	}
	out.Latency = SummarizeLatency(values)
	sortSamplesAsc(samples)
	out.Trend = BuildTrend(samples, from, to, TrendPoints)
	return out, nil
}

// EndpointSummaries builds the dashboard endpoint table with live state and
// windowed reliability metrics in one pass.
func (s *Service) EndpointSummaries(ctx context.Context, window time.Duration) ([]EndpointSummary, error) {
	endpoints, err := s.Repo.ListEndpoints(ctx, false)
	if err != nil {
		return nil, err
	}
	statuses, err := s.Repo.ListStatuses(ctx)
	if err != nil {
		return nil, err
	}
	stateByEndpoint := map[int64]models.EndpointStatus{}
	for _, st := range statuses {
		stateByEndpoint[st.EndpointID] = st
	}

	from := time.Now().UTC().Add(-window)
	samplesByEp, err := s.Repo.SelectAllSamples(ctx, from, MaxSamplesPerEndpoint)
	if err != nil {
		return nil, err
	}
	outcomes := map[int64][2]int64{} // endpoint -> {total, success}
	for _, samps := range samplesByEp {
		var ok int64
		for _, smp := range samps {
			if smp.Success {
				ok++
			}
		}
		outcomes[samps[0].EndpointID] = [2]int64{int64(len(samps)), ok}
	}

	openIncidents, err := s.Repo.ListIncidents(ctx, models.IncidentOpen, 1000)
	if err != nil {
		return nil, err
	}
	openByEndpoint := map[int64]int64{}
	for _, inc := range openIncidents {
		openByEndpoint[inc.EndpointID] = inc.ID
	}

	winLabel := WindowString(window)
	out := make([]EndpointSummary, 0, len(endpoints))
	for _, e := range endpoints {
		row := EndpointSummary{Endpoint: e}
		if st, ok := stateByEndpoint[e.ID]; ok {
			row.State = st.State
			row.ConsecutiveFailures = st.ConsecutiveFailures
			row.LastSuccessAt = st.LastSuccessAt
			row.LastFailureAt = st.LastFailureAt
			row.LastCheckedAt = st.LastCheckedAt
		} else {
			row.State = models.StateHealthy
		}
		if id, ok := openByEndpoint[e.ID]; ok {
			row.OpenIncidentID = &id
		}

		if o, ok := outcomes[e.ID]; ok && o[0] > 0 {
			uptime := Pct(o[1], o[0])
			var values []int64
			for _, smp := range samplesByEp[e.ID] {
				values = append(values, smp.ResponseTimeMs)
			}
			lat := SummarizeLatency(values)
			total := o[0]
			row.Window = &winLabel
			row.UptimePct = &uptime
			row.P99Ms = &lat.P99Ms
			row.AvgMs = &lat.AvgMs
			row.TotalChecks = &total
		}
		out = append(out, row)
	}
	return out, nil
}

func sortSamplesAsc(s []storage.LatencySample) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// OverviewInOrg assembles the tenant-scoped overview (SaaS dashboard).
func (s *Service) OverviewInOrg(ctx context.Context, orgID int64, window time.Duration) (*OverviewMetrics, error) {
	to := time.Now().UTC()
	from := to.Add(-window)

	out := &OverviewMetrics{Window: WindowString(window), From: from, To: to}

	endpoints, err := s.Repo.ListEndpoints(ctx, false)
	if err != nil {
		return nil, err
	}
	orgEndpoints := make([]models.Endpoint, 0, len(endpoints))
	for _, e := range endpoints {
		if e.OrgID != nil && *e.OrgID == orgID {
			orgEndpoints = append(orgEndpoints, e)
		}
	}
	statuses, err := s.Repo.ListStatuses(ctx)
	if err != nil {
		return nil, err
	}
	stateByEndpoint := map[int64]models.EndpointStatus{}
	for _, st := range statuses {
		stateByEndpoint[st.EndpointID] = st
	}
	out.Endpoints.Total = len(orgEndpoints)
	for _, e := range orgEndpoints {
		if !e.Enabled {
			out.Endpoints.Disabled++
			continue
		}
		switch stateByEndpoint[e.ID].State {
		case models.StateDown:
			out.Endpoints.Down++
		case models.StateFailing:
			out.Endpoints.Failing++
		default:
			out.Endpoints.Healthy++
		}
	}

	total, success, err := s.Repo.CountOutcomesInOrg(ctx, orgID, from)
	if err != nil {
		return nil, err
	}
	out.TotalChecks, out.SuccessfulChecks, out.FailedChecks = total, success, total-success
	out.UptimePct = Pct(success, total)
	out.FailureRatePct = 100 - out.UptimePct
	if out.FailureRatePct < 0 {
		out.FailureRatePct = 0
	}

	counts, err := s.Repo.StatusCountsInOrg(ctx, orgID, from)
	if err != nil {
		return nil, err
	}
	out.StatusDistribution = BuildDistribution(counts)

	samplesByEp, err := s.Repo.SelectSamplesInOrg(ctx, orgID, from, MaxSamplesPerEndpoint)
	if err != nil {
		return nil, err
	}
	var all []storage.LatencySample
	for _, samps := range samplesByEp {
		all = append(all, samps...)
	}
	values := make([]int64, 0, len(all))
	for _, smp := range all {
		values = append(values, smp.ResponseTimeMs)
	}
	out.Latency = SummarizeLatency(values)
	sortSamplesAsc(all)
	out.Trend = BuildTrend(all, from, to, TrendPoints)

	incidents, err := s.Repo.ListIncidentsInOrg(ctx, orgID, models.IncidentOpen, 1000)
	if err != nil {
		return nil, err
	}
	out.OpenIncidents = int64(len(incidents))
	return out, nil
}

// EndpointSummariesInOrg is EndpointSummaries scoped to one tenant.
func (s *Service) EndpointSummariesInOrg(ctx context.Context, orgID int64, window time.Duration) ([]EndpointSummary, error) {
	endpoints, err := s.Repo.ListEndpoints(ctx, false)
	if err != nil {
		return nil, err
	}
	orgEndpoints := make([]models.Endpoint, 0, len(endpoints))
	for _, e := range endpoints {
		if e.OrgID != nil && *e.OrgID == orgID {
			orgEndpoints = append(orgEndpoints, e)
		}
	}
	statuses, err := s.Repo.ListStatuses(ctx)
	if err != nil {
		return nil, err
	}
	stateByEndpoint := map[int64]models.EndpointStatus{}
	for _, st := range statuses {
		stateByEndpoint[st.EndpointID] = st
	}

	from := time.Now().UTC().Add(-window)
	samplesByEp, err := s.Repo.SelectSamplesInOrg(ctx, orgID, from, MaxSamplesPerEndpoint)
	if err != nil {
		return nil, err
	}
	outcomes := map[int64][2]int64{} // endpoint -> {total, success}
	for _, samps := range samplesByEp {
		var ok int64
		for _, smp := range samps {
			if smp.Success {
				ok++
			}
		}
		outcomes[samps[0].EndpointID] = [2]int64{int64(len(samps)), ok}
	}

	openIncidents, err := s.Repo.ListIncidentsInOrg(ctx, orgID, models.IncidentOpen, 1000)
	if err != nil {
		return nil, err
	}
	openByEndpoint := map[int64]int64{}
	for _, inc := range openIncidents {
		openByEndpoint[inc.EndpointID] = inc.ID
	}

	winLabel := WindowString(window)
	out := make([]EndpointSummary, 0, len(orgEndpoints))
	for _, e := range orgEndpoints {
		row := EndpointSummary{Endpoint: e}
		if st, ok := stateByEndpoint[e.ID]; ok {
			row.State = st.State
			row.ConsecutiveFailures = st.ConsecutiveFailures
			row.LastSuccessAt = st.LastSuccessAt
			row.LastFailureAt = st.LastFailureAt
			row.LastCheckedAt = st.LastCheckedAt
		} else {
			row.State = models.StateHealthy
		}
		if id, ok := openByEndpoint[e.ID]; ok {
			row.OpenIncidentID = &id
		}
		if o, ok := outcomes[e.ID]; ok && o[0] > 0 {
			uptime := Pct(o[1], o[0])
			var values []int64
			for _, smp := range samplesByEp[e.ID] {
				values = append(values, smp.ResponseTimeMs)
			}
			lat := SummarizeLatency(values)
			total := o[0]
			row.Window = &winLabel
			row.UptimePct = &uptime
			row.P99Ms = &lat.P99Ms
			row.AvgMs = &lat.AvgMs
			row.TotalChecks = &total
		}
		out = append(out, row)
	}
	return out, nil
}
