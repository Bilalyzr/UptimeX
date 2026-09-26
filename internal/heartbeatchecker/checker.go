// Package heartbeatchecker implements the checker-of-checkers (PRD §17).
// It runs OUTSIDE the monitor process and periodically calls the monitor's
// /health endpoint. If heartbeats go missing beyond a tolerance threshold,
// it raises a MONITOR_DOWN alert through its own notification path —
// independent of the monitor's alerting, because a dead monitor cannot
// alert about itself.
package heartbeatchecker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"uptimex/internal/models"
)

// Config configures the heartbeat checker.
type Config struct {
	MonitorURL      string        // monitor /health endpoint
	Interval        time.Duration // heartbeat poll interval
	MissesThreshold int           // consecutive misses before alerting
	Timeout         time.Duration // per-request timeout
	// MaxHeartbeatAge bounds how old the reported heartbeat_at timestamp may
	// be; a serving-but-frozen frontend (or a hard-proxied cache) must still
	// count as a missed heartbeat.
	MaxHeartbeatAge time.Duration
	// OnMonitorDown and OnMonitorRecovered deliver out-of-band alerts.
	OnMonitorDown      func(ctx context.Context, reason string, misses int)
	OnMonitorRecovered func(ctx context.Context, downDuration time.Duration)
	Logger             *slog.Logger
}

// Checker polls the monitor heartbeat.
type Checker struct {
	cfg    Config
	client *http.Client

	misses      int
	alerted     bool
	downSince   time.Time
	lastSuccess time.Time
}

// New builds a checker.
func New(cfg Config) *Checker {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Second
	}
	if cfg.MissesThreshold < 1 {
		cfg.MissesThreshold = 3
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 3 * time.Second
	}
	if cfg.MaxHeartbeatAge <= 0 {
		cfg.MaxHeartbeatAge = 3 * cfg.Interval
	}
	return &Checker{cfg: cfg, client: &http.Client{Timeout: cfg.Timeout}}
}

// Run polls until ctx is cancelled.
func (c *Checker) Run(ctx context.Context) {
	c.cfg.Logger.Info("heartbeat_checker_started",
		"monitor_url", c.cfg.MonitorURL,
		"interval", c.cfg.Interval.String(),
		"misses_threshold", c.cfg.MissesThreshold)
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.cfg.Logger.Info("heartbeat_checker_stopped")
			return
		case <-ticker.C:
			c.poll(ctx)
		}
	}
}

func (c *Checker) poll(ctx context.Context) {
	err := c.Probe(ctx)
	if err == nil {
		if c.alerted {
			down := time.Since(c.downSince)
			c.alerted = false
			c.misses = 0
			c.cfg.Logger.Info("monitor_heartbeat_restored", "down_duration", down.String())
			if c.cfg.OnMonitorRecovered != nil {
				c.cfg.OnMonitorRecovered(ctx, down)
			}
		}
		return
	}

	c.misses++
	c.cfg.Logger.Warn("heartbeat_missed",
		"misses", c.misses,
		"threshold", c.cfg.MissesThreshold,
		"reason", err.Error())

	if c.misses >= c.cfg.MissesThreshold && !c.alerted {
		c.alerted = true
		c.downSince = time.Now().UTC()
		c.cfg.Logger.Error("monitor_down_detected",
			"monitor_url", c.cfg.MonitorURL,
			"misses", c.misses,
			"reason", err.Error())
		if c.cfg.OnMonitorDown != nil {
			c.cfg.OnMonitorDown(ctx, err.Error(), c.misses)
		}
	}
}

// Probe performs one heartbeat check. It fails on transport errors, non-2xx
// responses, wrong payload shape, and stale heartbeat timestamps.
func (c *Checker) Probe(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.MonitorURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("heartbeat request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("heartbeat endpoint returned %s", resp.Status)
	}

	var body struct {
		Status      string    `json:"status"`
		Service     string    `json:"service"`
		HeartbeatAt time.Time `json:"heartbeat_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("heartbeat payload unreadable: %w", err)
	}
	if body.Status != "healthy" {
		return fmt.Errorf("monitor reports status %q", body.Status)
	}
	if time.Since(body.HeartbeatAt) > c.cfg.MaxHeartbeatAge {
		return fmt.Errorf("heartbeat timestamp stale: %s old (max %s)",
			time.Since(body.HeartbeatAt).Round(time.Second), c.cfg.MaxHeartbeatAge)
	}
	c.lastSuccess = time.Now().UTC()
	return nil
}

// BuildDownAlert constructs the out-of-band MONITOR_DOWN alert payload.
func BuildDownAlert(monitorURL, reason string, misses int, monitorName string) models.Alert {
	return models.Alert{
		Type:    models.AlertMonitorDown,
		Message: fmt.Sprintf("monitor %s appears DOWN: %d consecutive heartbeat misses (last error: %s)", monitorURL, misses, reason),
	}
}

// BuildRecoveredAlert constructs the monitor-recovery alert payload.
func BuildRecoveredAlert(monitorURL string, down time.Duration) models.Alert {
	return models.Alert{
		Type:    models.AlertRecovered,
		Message: fmt.Sprintf("monitor %s heartbeat restored after %s", monitorURL, down.Round(time.Second)),
	}
}

// Misses reports the current consecutive miss count (observability).
func (c *Checker) Misses() int { return c.misses }

// Alerted reports whether a MONITOR_DOWN alert is currently outstanding.
func (c *Checker) Alerted() bool { return c.alerted }
